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

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.LoadServer()
	if err != nil {
		slog.Error("invalid configuration", "err", err)
		os.Exit(1)
	}

	ctx := context.Background()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	sheets := sheet.NewDB(pool)
	store := storage.NewLocalStore(cfg.StorageRoot)
	h := api.New(sheets, store, cfg.BaseURL, cfg.MaxUploadSize)

	slog.Info("server starting", "addr", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, h.Routes()); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
