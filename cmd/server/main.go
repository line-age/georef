package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/line-age/georef/internal/api"
	"github.com/line-age/georef/internal/config"
	"github.com/line-age/georef/internal/proxy"
	"github.com/line-age/georef/internal/sheet"
	"github.com/line-age/georef/internal/storage"
)

const (
	logMsgInvalidConfig  = "invalid configuration"
	logMsgConnectDB      = "connect to database"
	logMsgServerStarting = "server starting"
	logMsgServerStopped  = "server stopped"
	logMsgServerShutdown = "server shutting down"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.LoadServer()
	if err != nil {
		slog.Error(logMsgInvalidConfig, "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error(logMsgConnectDB, "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	sheets := sheet.NewRepository(pool)
	store := storage.NewLocalStore(cfg.StorageRoot)
	imageProxy := proxy.New(proxy.Config{
		MaxIdleConns:        cfg.ProxyMaxIdleConns,
		MaxIdleConnsPerHost: cfg.ProxyMaxIdleConnsPerHost,
		IdleConnTimeout:     cfg.ProxyIdleConnTimeout,
		RequestTimeout:      cfg.ProxyRequestTimeout,
		MaxInfoBodySize:     cfg.ProxyMaxInfoBodySize,
	})
	apiHandler := api.New(sheets, store, imageProxy, api.Config{
		BaseURL:              cfg.BaseURL,
		ImageServerURL:       cfg.ImageServerURL,
		IIIFImageAPIVersion:  cfg.IIIFImageAPIVersion,
		IIIFImageAPILevel:    cfg.IIIFImageAPILevel,
		RetryAfterSeconds:    cfg.RetryAfterSeconds,
		MultipartMemoryLimit: cfg.MultipartMemoryLimit,
		MaxUploadSize:        cfg.MaxUploadSize,
	})

	server := &http.Server{
		Addr:    cfg.Addr,
		Handler: apiHandler.Routes(),
	}

	go func() {
		slog.Info(logMsgServerStarting, "addr", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error(logMsgServerStopped, "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	stop()

	slog.Info(logMsgServerShutdown, "timeout", cfg.ShutdownTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error(logMsgServerStopped, "err", err)
		os.Exit(1)
	}
}
