package config

import (
	"fmt"
	"os"
	"strconv"
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
	ModelPolicyPath     string
}

// Load reads configuration from .env and environment variables.
func Load() (*Config, error) {
	_ = godotenv.Load() // ignore if .env missing

	maxRequestBodyBytes, err := envInt64("MAX_REQUEST_BODY_BYTES", 10*1024*1024)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Port:                envOr("PORT", "8000"),
		LogLevel:            strings.ToLower(envOr("LOG_LEVEL", "info")),
		APIKey:              strings.TrimSpace(os.Getenv("API_KEY")),
		AWSRegion:           envOr("AWS_REGION", "us-east-1"),
		MaxRequestBodyBytes: maxRequestBodyBytes,
		EnableMetrics:       envBool("ENABLE_METRICS", false),
		ModelPolicyPath:     strings.TrimSpace(os.Getenv("MODEL_POLICY_PATH")),
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

func envInt64(key string, fallback int64) (int64, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback, nil
	}
	out, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: must be an integer", key, v)
	}
	return out, nil
}
