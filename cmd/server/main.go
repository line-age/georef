package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	georef "github.com/line-age/georef"
	"github.com/line-age/georef/internal/api"
	"github.com/line-age/georef/internal/auth"
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

func selectStore(ctx context.Context, backend, root string, s3cfg storage.S3Config) (storage.Store, error) {
	if backend == "s3" {
		return storage.NewS3Store(ctx, s3cfg)
	}
	return storage.NewLocalStore(root), nil
}

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
	appStore, err := selectStore(ctx, cfg.StorageBackend, cfg.StorageRoot, storage.S3Config{
		Bucket:   cfg.S3Bucket,
		Region:   cfg.S3Region,
		Endpoint: cfg.S3Endpoint,
	})
	if err != nil {
		slog.Error("connect to storage", "err", err)
		os.Exit(1)
	}
	imageProxy := proxy.New(proxy.Config{
		MaxIdleConns:        cfg.ProxyMaxIdleConns,
		MaxIdleConnsPerHost: cfg.ProxyMaxIdleConnsPerHost,
		IdleConnTimeout:     cfg.ProxyIdleConnTimeout,
		RequestTimeout:      cfg.ProxyRequestTimeout,
		MaxInfoBodySize:     cfg.ProxyMaxInfoBodySize,
	})
	authenticator := auth.New(auth.Config{
		AuthRequired:       cfg.AuthRequired,
		GitHubClientID:     cfg.GitHubClientID,
		GitHubClientSecret: cfg.GitHubClientSecret,
		AllowedTeams:       cfg.AllowedTeams,
		SessionKey:         cfg.SessionKey,
		CallbackURL:        cfg.BaseURL + "/auth/callback",
		SecureCookies:      cfg.SecureCookies,
		SessionMaxAge:      cfg.SessionMaxAge,
		ReadLoginRequired:  cfg.ReadLoginRequired,
		ServiceCredential:  cfg.ServiceCredential,
	})
	apiHandler := api.New(sheets, appStore, imageProxy, authenticator, api.Config{
		BaseURL:              cfg.BaseURL,
		ImageServerURL:       cfg.ImageServerURL,
		IIIFImageAPIVersion:  cfg.IIIFImageAPIVersion,
		IIIFImageAPILevel:    cfg.IIIFImageAPILevel,
		RetryAfterSeconds:    cfg.RetryAfterSeconds,
		MultipartMemoryLimit: cfg.MultipartMemoryLimit,
		MaxUploadSize:        cfg.MaxUploadSize,
		IIIFStatusCacheTTL:   cfg.IIIFStatusCacheTTL,
		OpenAPISpec:          georef.OpenAPISpec,
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
