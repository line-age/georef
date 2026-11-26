package config

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// ServerConfig holds all configuration for the HTTP server process.
// Loaded once at startup via LoadServer; never read from env after that.
type ServerConfig struct {
	DatabaseURL             string
	StorageBackend          string // "filesystem" (default) or "s3"
	StorageRoot             string // filesystem root; required when StorageBackend = "filesystem"
	S3Bucket                string // required when StorageBackend = "s3"
	S3Region                string // S3 region; default "us-east-1"
	S3Endpoint              string // optional custom endpoint for S3-compatible services
	BaseURL                 string
	ImageServerURL          string        // internal URL of the IIIF image server, not reachable from outside
	IIIFImageAPIVersion     int           // IIIF Image API version served by the image server; default 2
	IIIFImageAPILevel       int           // IIIF Image API compliance level; default 2
	RetryAfterSeconds       int           // Retry-After header value sent on 503 responses; default 5
	MultipartMemoryLimit    int64         // bytes kept in RAM when parsing multipart form; excess spills to disk; default 32 MB
	ProxyMaxIdleConns        int           // total idle connections in the proxy transport pool; default 100
	ProxyMaxIdleConnsPerHost int           // per-host idle connections in the proxy transport pool; default 20
	ProxyIdleConnTimeout     time.Duration // how long an idle proxy connection stays open; default 90s
	ProxyRequestTimeout      time.Duration // per-request timeout for proxy calls to the image server; default 2m
	ProxyMaxInfoBodySize     int64         // maximum bytes read from an info.json response body; default 1 MB
	ShutdownTimeout          time.Duration // graceful shutdown window; default 30s
	Addr                    string
	MaxUploadSize           int64         // bytes; default 1 GB
	MultipartFormOverhead   int64         // bytes added to MaxUploadSize when limiting the full multipart body; default 1024
	IIIFStatusCacheTTL      time.Duration // how long IIIF sheet status lookups are cached in memory; default 5s

	// Auth — optional. Leave GitHubClientID empty to disable authentication.
	AuthRequired       bool          // when false, all auth middleware is a passthrough; default true
	GitHubClientID     string
	GitHubClientSecret string
	AllowedTeams       []string      // parsed from GITHUB_ALLOWED_TEAMS (comma-separated "org/team" pairs)
	SessionKey         []byte        // parsed from SESSION_KEY (base64); at least 32 bytes when auth is enabled
	SecureCookies      bool          // set false for local HTTP development; default true
	SessionMaxAge      int           // session lifetime in seconds; default 604800 (7 days)
	IIIFAuthRequired   bool          // gate IIIF endpoints globally; default false
	ServiceCredential  string        // bearer token for machine-to-machine IIIF access

	// CORS
	CORSAllowedOrigins []string // parsed from CORS_ALLOWED_ORIGINS; default ["*"]
}

// WorkerConfig holds all configuration for the conversion worker process.
// Loaded once at startup via LoadWorker; never read from env after that.
type WorkerConfig struct {
	DatabaseURL          string
	StorageBackend       string // "filesystem" (default) or "s3"
	StorageRoot          string // filesystem root; required when StorageBackend = "filesystem"
	S3Bucket             string // required when StorageBackend = "s3"
	S3Region             string // S3 region; default "us-east-1"
	S3Endpoint           string // optional custom endpoint for S3-compatible services
	PollInterval         time.Duration
	VipsTileWidth        int
	VipsTileHeight       int
	MaxConversionRetries int
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
	multipartFormOverhead, err := envInt64("MULTIPART_FORM_OVERHEAD", 8*1024)
	if err != nil {
		return nil, err
	}
	iiifImageAPIVersion, err := envInt("IIIF_IMAGE_API_VERSION", 2)
	if err != nil {
		return nil, err
	}
	iiifImageAPILevel, err := envInt("IIIF_IMAGE_API_LEVEL", 2)
	if err != nil {
		return nil, err
	}
	retryAfterSeconds, err := envInt("RETRY_AFTER_SECONDS", 5)
	if err != nil {
		return nil, err
	}
	multipartMemoryLimit, err := envInt64("MULTIPART_MEMORY_LIMIT", 32<<20)
	if err != nil {
		return nil, err
	}
	proxyMaxIdleConns, err := envInt("PROXY_MAX_IDLE_CONNS", 100)
	if err != nil {
		return nil, err
	}
	proxyMaxIdleConnsPerHost, err := envInt("PROXY_MAX_IDLE_CONNS_PER_HOST", 20)
	if err != nil {
		return nil, err
	}
	proxyIdleConnTimeout, err := envDuration("PROXY_IDLE_CONN_TIMEOUT", 90*time.Second)
	if err != nil {
		return nil, err
	}
	proxyRequestTimeout, err := envDuration("PROXY_REQUEST_TIMEOUT", 2*time.Minute)
	if err != nil {
		return nil, err
	}
	proxyMaxInfoBodySize, err := envInt64("PROXY_MAX_INFO_BODY_SIZE", 1<<20)
	if err != nil {
		return nil, err
	}
	shutdownTimeout, err := envDuration("SHUTDOWN_TIMEOUT", 30*time.Second)
	if err != nil {
		return nil, err
	}
	sessionKey, err := loadSessionKey()
	if err != nil {
		return nil, err
	}
	sessionMaxAge, err := envInt("SESSION_MAX_AGE", 86400*7)
	if err != nil {
		return nil, err
	}
	iiifStatusCacheTTL, err := envDuration("IIIF_STATUS_CACHE_TTL", 5*time.Second)
	if err != nil {
		return nil, err
	}

	dbName := readEnv("DB_NAME")
	dbUser := readEnv("DB_USER")
	dbPassword := readEnv("DB_PASSWORD")

	cfg := &ServerConfig{
		DatabaseURL:              buildDatabaseURL(
			envWithDefault("DB_HOST", "localhost"),
			envWithDefault("DB_PORT", "5432"),
			dbName,
			dbUser,
			dbPassword,
			envWithDefault("DB_SSLMODE", "disable"),
		),
		StorageBackend:           envWithDefault("STORAGE_BACKEND", "filesystem"),
		StorageRoot:              readEnv("STORAGE_ROOT"),
		S3Bucket:                 readEnv("S3_BUCKET"),
		S3Region:                 envWithDefault("S3_REGION", "us-east-1"),
		S3Endpoint:               readEnv("S3_ENDPOINT"),
		BaseURL:                  readEnv("BASE_URL"),
		ImageServerURL:           readEnv("IMAGE_SERVER_URL"),
		IIIFImageAPIVersion:      iiifImageAPIVersion,
		IIIFImageAPILevel:        iiifImageAPILevel,
		RetryAfterSeconds:        retryAfterSeconds,
		MultipartMemoryLimit:     multipartMemoryLimit,
		ProxyMaxIdleConns:        proxyMaxIdleConns,
		ProxyMaxIdleConnsPerHost: proxyMaxIdleConnsPerHost,
		ProxyIdleConnTimeout:     proxyIdleConnTimeout,
		ProxyRequestTimeout:      proxyRequestTimeout,
		ProxyMaxInfoBodySize:     proxyMaxInfoBodySize,
		ShutdownTimeout:          shutdownTimeout,
		Addr:                     envWithDefault("ADDR", ":8080"),
		MaxUploadSize:            maxUploadSize,
		MultipartFormOverhead:    multipartFormOverhead,
		AuthRequired:             envWithDefault("AUTH_REQUIRED", "true") != "false",
		GitHubClientID:           readEnv("GITHUB_CLIENT_ID"),
		GitHubClientSecret:       readEnv("GITHUB_CLIENT_SECRET"),
		AllowedTeams:             parseTeams(readEnv("GITHUB_ALLOWED_TEAMS")),
		SessionKey:               sessionKey,
		SecureCookies:            envWithDefault("SECURE_COOKIES", "true") != "false",
		SessionMaxAge:            sessionMaxAge,
		IIIFStatusCacheTTL:       iiifStatusCacheTTL,
		IIIFAuthRequired:         envWithDefault("IIIF_AUTH_REQUIRED", "false") == "true",
		ServiceCredential:        readEnv("SERVICE_CREDENTIAL"),
		CORSAllowedOrigins:       parseCORSOrigins(readEnv("CORS_ALLOWED_ORIGINS")),
	}
	required := []error{
		requireNonEmpty("DB_NAME", dbName),
		requireNonEmpty("DB_USER", dbUser),
		requireNonEmpty("DB_PASSWORD", dbPassword),
		requireNonEmpty("BASE_URL", cfg.BaseURL),
		requireNonEmpty("IMAGE_SERVER_URL", cfg.ImageServerURL),
	}
	if cfg.StorageBackend == "s3" {
		required = append(required, requireNonEmpty("S3_BUCKET", cfg.S3Bucket))
	} else {
		required = append(required, requireNonEmpty("STORAGE_ROOT", cfg.StorageRoot))
	}
	if cfg.AuthRequired && cfg.GitHubClientID != "" {
		required = append(required, requireNonEmpty("GITHUB_CLIENT_SECRET", cfg.GitHubClientSecret))
		if len(cfg.SessionKey) == 0 {
			required = append(required, fmt.Errorf(errFmtMissingEnvVar, "SESSION_KEY"))
		}
	}
	return cfg, checkRequired(required...)
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
	maxConversionRetries, err := envInt("MAX_CONVERSION_RETRIES", 3)
	if err != nil {
		return nil, err
	}

	dbName := readEnv("DB_NAME")
	dbUser := readEnv("DB_USER")
	dbPassword := readEnv("DB_PASSWORD")

	cfg := &WorkerConfig{
		DatabaseURL: buildDatabaseURL(
			envWithDefault("DB_HOST", "localhost"),
			envWithDefault("DB_PORT", "5432"),
			dbName,
			dbUser,
			dbPassword,
			envWithDefault("DB_SSLMODE", "disable"),
		),
		StorageBackend:       envWithDefault("STORAGE_BACKEND", "filesystem"),
		StorageRoot:          readEnv("STORAGE_ROOT"),
		S3Bucket:             readEnv("S3_BUCKET"),
		S3Region:             envWithDefault("S3_REGION", "us-east-1"),
		S3Endpoint:           readEnv("S3_ENDPOINT"),
		PollInterval:         pollInterval,
		VipsTileWidth:        vipsTileWidth,
		VipsTileHeight:       vipsTileHeight,
		MaxConversionRetries: maxConversionRetries,
	}
	required := []error{
		requireNonEmpty("DB_NAME", dbName),
		requireNonEmpty("DB_USER", dbUser),
		requireNonEmpty("DB_PASSWORD", dbPassword),
	}
	if cfg.StorageBackend == "s3" {
		required = append(required, requireNonEmpty("S3_BUCKET", cfg.S3Bucket))
	} else {
		required = append(required, requireNonEmpty("STORAGE_ROOT", cfg.StorageRoot))
	}
	return cfg, checkRequired(required...)
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

// parseCSV splits a comma-separated string, trims whitespace, and skips empty entries.
func parseCSV(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// parseCORSOrigins splits a comma-separated list of allowed origins.
// When the env var is unset or resolves to an empty list, it returns ["*"]
// so IIIF endpoints are open by default.
func parseCORSOrigins(raw string) []string {
	origins := parseCSV(raw)
	if len(origins) == 0 {
		return []string{"*"}
	}
	return origins
}

// parseTeams splits a comma-separated list of "org/team" pairs, skipping empty entries.
func parseTeams(raw string) []string {
	return parseCSV(raw)
}

// loadSessionKey decodes the base64 SESSION_KEY env var.
// Returns nil when the variable is unset (auth is disabled in that case).
func loadSessionKey() ([]byte, error) {
	raw := readEnv("SESSION_KEY")
	if raw == "" {
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("env SESSION_KEY: invalid base64: %w", err)
	}
	if len(key) < 32 {
		return nil, fmt.Errorf("env SESSION_KEY: must be at least 32 bytes, got %d", len(key))
	}
	return key, nil
}

// buildDatabaseURL assembles a postgres:// DSN from its component parts.
// url.UserPassword handles correct percent-encoding of passwords that contain
// special characters such as @, /, or spaces.
func buildDatabaseURL(host, port, name, user, password, sslMode string) string {
	dbURL := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, password),
		Host:   host + ":" + port,
		Path:   "/" + name,
	}
	query := dbURL.Query()
	query.Set("sslmode", sslMode)
	dbURL.RawQuery = query.Encode()
	return dbURL.String()
}
