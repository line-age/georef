package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// ServerConfig holds all configuration for the HTTP server process.
// Loaded once at startup via LoadServer; never read from env after that.
type ServerConfig struct {
	DatabaseURL   string
	StorageRoot   string
	BaseURL       string
	Addr          string
	MaxUploadSize int64 // bytes; default 1 GB
}

// WorkerConfig holds all configuration for the conversion worker process.
// Loaded once at startup via LoadWorker; never read from env after that.
type WorkerConfig struct {
	DatabaseURL    string
	StorageRoot    string
	ImageServerURL string
	PollInterval   time.Duration
	MaxRetries     int
}

// LoadServer reads and validates the server configuration from the environment.
func LoadServer() (*ServerConfig, error) {
	maxUploadSize, err := envInt64("MAX_UPLOAD_SIZE", 1<<30)
	if err != nil {
		return nil, err
	}
	c := &ServerConfig{
		DatabaseURL:   mustEnv("DATABASE_URL"),
		StorageRoot:   mustEnv("STORAGE_ROOT"),
		BaseURL:       mustEnv("BASE_URL"),
		Addr:          envOr("ADDR", ":8080"),
		MaxUploadSize: maxUploadSize,
	}
	return c, validate(
		required("DATABASE_URL", c.DatabaseURL),
		required("STORAGE_ROOT", c.StorageRoot),
		required("BASE_URL", c.BaseURL),
	)
}

// LoadWorker reads and validates the worker configuration from the environment.
func LoadWorker() (*WorkerConfig, error) {
	pollInterval, err := envDuration("POLL_INTERVAL", 5*time.Second)
	if err != nil {
		return nil, err
	}
	maxRetries, err := envInt("MAX_RETRIES", 3)
	if err != nil {
		return nil, err
	}

	c := &WorkerConfig{
		DatabaseURL:    mustEnv("DATABASE_URL"),
		StorageRoot:    mustEnv("STORAGE_ROOT"),
		ImageServerURL: mustEnv("IMAGE_SERVER_URL"),
		PollInterval:   pollInterval,
		MaxRetries:     maxRetries,
	}
	return c, validate(
		required("DATABASE_URL", c.DatabaseURL),
		required("STORAGE_ROOT", c.StorageRoot),
		required("IMAGE_SERVER_URL", c.ImageServerURL),
	)
}

func mustEnv(key string) string {
	return os.Getenv(key)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("env %s: invalid duration %q: %w", key, v, err)
	}
	return d, nil
}

func envInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("env %s: invalid integer %q: %w", key, v, err)
	}
	return n, nil
}

func envInt64(key string, def int64) (int64, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("env %s: invalid integer %q: %w", key, v, err)
	}
	return n, nil
}

func required(key, val string) error {
	if val == "" {
		return fmt.Errorf("required environment variable %s is not set", key)
	}
	return nil
}

func validate(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
