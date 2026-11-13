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
	PollInterval   time.Duration
	VipsTileWidth  int
	VipsTileHeight int
}

const (
	errFmtInvalidDuration = "env %s: invalid duration %q: %w"
	errFmtInvalidInteger  = "env %s: invalid integer %q: %w"
	errFmtMissingEnvVar   = "required environment variable %s is not set"
)

// LoadServer reads and validates the server configuration from the environment.
func LoadServer() (*ServerConfig, error) {
	maxUploadSize, err := envInt64("MAX_UPLOAD_SIZE", 1<<30)
	if err != nil {
		return nil, err
	}
	cfg := &ServerConfig{
		DatabaseURL:   readEnv("DATABASE_URL"),
		StorageRoot:   readEnv("STORAGE_ROOT"),
		BaseURL:       readEnv("BASE_URL"),
		Addr:          envWithDefault("ADDR", ":8080"),
		MaxUploadSize: maxUploadSize,
	}
	return cfg, checkRequired(
		requireNonEmpty("DATABASE_URL", cfg.DatabaseURL),
		requireNonEmpty("STORAGE_ROOT", cfg.StorageRoot),
		requireNonEmpty("BASE_URL", cfg.BaseURL),
	)
}

// LoadWorker reads and validates the worker configuration from the environment.
func LoadWorker() (*WorkerConfig, error) {
	pollInterval, err := envDuration("POLL_INTERVAL", 5*time.Second)
	if err != nil {
		return nil, err
	}
	vipsTileWidth, err := envInt("VIPS_TILE_WIDTH", 256)
	if err != nil {
		return nil, err
	}
	vipsTileHeight, err := envInt("VIPS_TILE_HEIGHT", 256)
	if err != nil {
		return nil, err
	}

	cfg := &WorkerConfig{
		DatabaseURL:    readEnv("DATABASE_URL"),
		StorageRoot:    readEnv("STORAGE_ROOT"),
		PollInterval:   pollInterval,
		VipsTileWidth:  vipsTileWidth,
		VipsTileHeight: vipsTileHeight,
	}
	return cfg, checkRequired(
		requireNonEmpty("DATABASE_URL", cfg.DatabaseURL),
		requireNonEmpty("STORAGE_ROOT", cfg.StorageRoot),
	)
}

// readEnv returns the value of an environment variable, or an empty string if unset.
func readEnv(key string) string {
	return os.Getenv(key)
}

// envWithDefault returns the environment variable value, or defaultValue if the variable is unset.
func envWithDefault(key, defaultValue string) string {
	if rawValue := os.Getenv(key); rawValue != "" {
		return rawValue
	}
	return defaultValue
}

func envDuration(key string, defaultValue time.Duration) (time.Duration, error) {
	rawValue := os.Getenv(key)
	if rawValue == "" {
		return defaultValue, nil
	}
	parsedDuration, err := time.ParseDuration(rawValue)
	if err != nil {
		return 0, fmt.Errorf(errFmtInvalidDuration, key, rawValue, err)
	}
	return parsedDuration, nil
}

func envInt(key string, defaultValue int) (int, error) {
	rawValue := os.Getenv(key)
	if rawValue == "" {
		return defaultValue, nil
	}
	parsedValue, err := strconv.Atoi(rawValue)
	if err != nil {
		return 0, fmt.Errorf(errFmtInvalidInteger, key, rawValue, err)
	}
	return parsedValue, nil
}

func envInt64(key string, defaultValue int64) (int64, error) {
	rawValue := os.Getenv(key)
	if rawValue == "" {
		return defaultValue, nil
	}
	parsedValue, err := strconv.ParseInt(rawValue, 10, 64)
	if err != nil {
		return 0, fmt.Errorf(errFmtInvalidInteger, key, rawValue, err)
	}
	return parsedValue, nil
}

// requireNonEmpty returns an error if the named environment variable has no value.
func requireNonEmpty(key, value string) error {
	if value == "" {
		return fmt.Errorf(errFmtMissingEnvVar, key)
	}
	return nil
}

// checkRequired returns the first non-nil error from the given list, or nil if all pass.
func checkRequired(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
