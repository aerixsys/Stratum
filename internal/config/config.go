package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration.
type Config struct {
	Port                string
	LogLevel            string
	APIKey              string
	AWSRegion           string
	MaxRequestBodyBytes int64
	EnableMetrics       bool
}

// Load reads configuration from .env and environment variables.
func Load() (*Config, error) {
	_ = godotenv.Load() // ignore if .env missing

	cfg := &Config{
		Port:                envOr("PORT", "8000"),
		LogLevel:            strings.ToLower(envOr("LOG_LEVEL", "info")),
		APIKey:              strings.TrimSpace(os.Getenv("API_KEY")),
		AWSRegion:           envOr("AWS_REGION", "us-east-1"),
		MaxRequestBodyBytes: int64(envInt("MAX_REQUEST_BODY_BYTES", 10*1024*1024)),
		EnableMetrics:       envBool("ENABLE_METRICS", false),
	}

	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API_KEY is required")
	}

	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return nil, fmt.Errorf("invalid LOG_LEVEL %q", cfg.LogLevel)
	}
	if cfg.MaxRequestBodyBytes <= 0 {
		return nil, fmt.Errorf("MAX_REQUEST_BODY_BYTES must be > 0")
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}

func envBool(key string, fallback bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return fallback
	}
	return v == "true" || v == "1" || v == "yes"
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	var out int
	_, err := fmt.Sscanf(v, "%d", &out)
	if err != nil {
		return fallback
	}
	return out
}
