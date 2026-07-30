package config

import (
	"flag"
	"os"
	"strconv"
	"time"
)

const (
	DefaultMaxUploadBytes = 256 << 20
	DefaultProxyTimeout   = 60 * time.Second
)

type Config struct {
	DevMode      bool
	Port         int
	DatabasePath string
	StorageDir   string

	MaxUploadBytes int64
	// MaxBlobBytes limits a container layer, which routinely runs past whatever
	// is a sane ceiling for a jar or a tarball. Zero means unlimited.
	MaxBlobBytes int64

	ProxyTimeout time.Duration
	ViteURL      string
}

func Parse(arguments []string) (Config, error) {
	flags := flag.NewFlagSet("arca", flag.ContinueOnError)

	config := Config{}
	var proxyTimeoutSeconds int64

	flags.BoolVar(&config.DevMode, "dev", envBool("DEV_MODE", false), "proxy the frontend to a running vite dev server instead of serving the embedded bundle")
	flags.Int64Var(&proxyTimeoutSeconds, "proxy-timeout", envInt64("PROXY_TIMEOUT_SECONDS", int64(DefaultProxyTimeout/time.Second)), "how long a proxy repository waits for its upstream, in seconds")
	flags.IntVar(&config.Port, "port", envInt("PORT", 8080), "port to listen on")
	flags.StringVar(&config.DatabasePath, "db", envString("DATABASE_PATH", "./data/arca.db"), "path to the SQLite database file")
	flags.StringVar(&config.StorageDir, "storage", envString("STORAGE_DIR", "./data/storage"), "directory holding uploaded artifacts")
	flags.Int64Var(&config.MaxUploadBytes, "max-upload", envInt64("MAX_UPLOAD_BYTES", DefaultMaxUploadBytes), "largest artifact accepted, in bytes")
	flags.Int64Var(&config.MaxBlobBytes, "max-blob", envInt64("MAX_BLOB_BYTES", 0), "largest container image layer accepted, in bytes, or 0 for no limit")
	flags.StringVar(&config.ViteURL, "vite-url", envString("VITE_URL", "http://localhost:5173"), "vite dev server to proxy in dev mode")

	if err := flags.Parse(arguments); err != nil {
		return Config{}, err
	}
	if config.MaxUploadBytes <= 0 {
		config.MaxUploadBytes = DefaultMaxUploadBytes
	}
	if config.MaxBlobBytes < 0 {
		config.MaxBlobBytes = 0
	}

	config.ProxyTimeout = time.Duration(proxyTimeoutSeconds) * time.Second
	if config.ProxyTimeout <= 0 {
		config.ProxyTimeout = DefaultProxyTimeout
	}
	return config, nil
}

func envString(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	return int(envInt64(key, int64(fallback)))
}

func envInt64(key string, fallback int64) int64 {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBool(key string, fallback bool) bool {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
