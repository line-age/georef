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

func selectStore(ctx context.Context, backend, root string, s3cfg storage.S3Config) (storage.Store, error) {
	if backend == "s3" {
		return storage.NewS3Store(ctx, s3cfg)
	}
	return storage.NewLocalStore(root), nil
}

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
	appStore, err := selectStore(ctx, cfg.StorageBackend, cfg.StorageRoot, storage.S3Config{
		Bucket:   cfg.S3Bucket,
		Region:   cfg.S3Region,
		Endpoint: cfg.S3Endpoint,
	})
	if err != nil {
		slog.Error("connect to storage", "err", err)
		os.Exit(1)
	}
	converter := convert.VipsConverter{
		TileWidth:  cfg.VipsTileWidth,
		TileHeight: cfg.VipsTileHeight,
	}
	conversionWorker := worker.New(sheets, appStore, converter, cfg.PollInterval, cfg.MaxConversionRetries)

	slog.Info(logMsgWorkerStarting, "poll_interval", cfg.PollInterval)
	conversionWorker.Run(ctx)
	slog.Info(logMsgWorkerStopped)
}
