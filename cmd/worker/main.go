package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/line-age/georef/internal/config"
	"github.com/line-age/georef/internal/convert"
	"github.com/line-age/georef/internal/sheet"
	"github.com/line-age/georef/internal/storage"
	"github.com/line-age/georef/internal/worker"
)

const (
	logMsgInvalidConfig  = "invalid configuration"
	logMsgConnectDB      = "connect to database"
	logMsgWorkerStarting = "worker starting"
	logMsgWorkerStopped  = "worker stopped"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.LoadWorker()
	if err != nil {
		slog.Error(logMsgInvalidConfig, "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error(logMsgConnectDB, "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	sheets := sheet.NewRepository(pool)
	store := storage.NewLocalStore(cfg.StorageRoot)
	converter := convert.VipsConverter{
		TileWidth:  cfg.VipsTileWidth,
		TileHeight: cfg.VipsTileHeight,
	}
	conversionWorker := worker.New(sheets, store, converter, cfg.PollInterval)

	slog.Info(logMsgWorkerStarting, "poll_interval", cfg.PollInterval)
	conversionWorker.Run(ctx)
	slog.Info(logMsgWorkerStopped)
}
