package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/line-age/georef/internal/api"
	"github.com/line-age/georef/internal/config"
	"github.com/line-age/georef/internal/sheet"
	"github.com/line-age/georef/internal/storage"
)

const (
	logMsgInvalidConfig  = "invalid configuration"
	logMsgConnectDB      = "connect to database"
	logMsgServerStarting = "server starting"
	logMsgServerStopped  = "server stopped"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.LoadServer()
	if err != nil {
		slog.Error(logMsgInvalidConfig, "err", err)
		os.Exit(1)
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error(logMsgConnectDB, "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	sheets := sheet.NewRepository(pool)
	store := storage.NewLocalStore(cfg.StorageRoot)
	apiHandler := api.New(sheets, store, cfg.BaseURL, cfg.MaxUploadSize)

	slog.Info(logMsgServerStarting, "addr", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, apiHandler.Routes()); err != nil {
		slog.Error(logMsgServerStopped, "err", err)
		os.Exit(1)
	}
}
