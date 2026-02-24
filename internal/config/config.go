package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all runtime configuration.
type Config struct {
	Port                      string
	LogLevel                  string
	APIKey                    string
	AWSRegion                 string
	AllowedOrigins            []string
	AllowAnyOrigin            bool
	EnableCrossRegion         bool
	EnableAppInferenceProfile bool
	EnablePromptCaching       bool
	DefaultModel              string
	DefaultEmbeddingModel     string
	MaxRequestBodyBytes       int64
	RateLimitRPM              int
	RateLimitBurst            int
	EnableMetrics             bool
	AllowPrivateImageFetch    bool
	ImageMaxBytes             int64
	ImageFetchTimeoutSeconds  int
}

// Load reads configuration from .env and environment variables.
func Load() (*Config, error) {
	_ = godotenv.Load() // ignore if .env missing

	cfg := &Config{
		Port:                      envOr("PORT", "8000"),
		LogLevel:                  strings.ToLower(envOr("LOG_LEVEL", "info")),
		APIKey:                    strings.TrimSpace(os.Getenv("API_KEY")),
		AWSRegion:                 envOr("AWS_REGION", "us-east-1"),
		EnableCrossRegion:         envBool("ENABLE_CROSS_REGION_INFERENCE", true),
		EnableAppInferenceProfile: envBool("ENABLE_APP_INFERENCE_PROFILES", false),
		EnablePromptCaching:       envBool("ENABLE_PROMPT_CACHING", false),
		DefaultModel:              envOr("DEFAULT_MODEL", "anthropic.claude-3-sonnet-20240229-v1:0"),
		DefaultEmbeddingModel:     envOr("DEFAULT_EMBEDDING_MODEL", "cohere.embed-multilingual-v3"),
		MaxRequestBodyBytes:       int64(envInt("MAX_REQUEST_BODY_BYTES", 10*1024*1024)),
		RateLimitRPM:              envInt("RATE_LIMIT_RPM", 120),
		RateLimitBurst:            envInt("RATE_LIMIT_BURST", 20),
		EnableMetrics:             envBool("ENABLE_METRICS", false),
		AllowPrivateImageFetch:    envBool("ALLOW_PRIVATE_IMAGE_FETCH", false),
		ImageMaxBytes:             int64(envInt("IMAGE_MAX_BYTES", 5*1024*1024)),
		ImageFetchTimeoutSeconds:  envInt("IMAGE_FETCH_TIMEOUT_SECONDS", 10),
	}

	// Also check OPENAI_API_KEY for backward compat with Python version
	if cfg.APIKey == "" {
		cfg.APIKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("API_KEY (or OPENAI_API_KEY) is required")
	}

	// CORS
	raw := envOr("ALLOWED_ORIGINS", "*")
	origins := parseCSV(raw)
	cfg.AllowedOrigins = origins
	for _, o := range origins {
		if o == "*" {
			cfg.AllowAnyOrigin = true
		}
	}

	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return nil, fmt.Errorf("invalid LOG_LEVEL %q", cfg.LogLevel)
	}
	if cfg.MaxRequestBodyBytes <= 0 {
		return nil, fmt.Errorf("MAX_REQUEST_BODY_BYTES must be > 0")
	}
	if cfg.RateLimitRPM < 0 {
		return nil, fmt.Errorf("RATE_LIMIT_RPM must be >= 0")
	}
	if cfg.RateLimitBurst < 0 {
		return nil, fmt.Errorf("RATE_LIMIT_BURST must be >= 0")
	}
	if cfg.ImageMaxBytes <= 0 {
		return nil, fmt.Errorf("IMAGE_MAX_BYTES must be > 0")
	}
	if cfg.ImageFetchTimeoutSeconds <= 0 {
		return nil, fmt.Errorf("IMAGE_FETCH_TIMEOUT_SECONDS must be > 0")
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

func parseCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}
