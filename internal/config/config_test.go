package config

import (
	"strings"
	"testing"
)

func setBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("PORT", "8123")
	t.Setenv("LOG_LEVEL", "info")
	t.Setenv("API_KEY", "sk-test")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("ALLOWED_ORIGINS", "https://one.example,https://two.example")
	t.Setenv("ENABLE_CROSS_REGION_INFERENCE", "true")
	t.Setenv("ENABLE_APP_INFERENCE_PROFILES", "false")
	t.Setenv("ENABLE_PROMPT_CACHING", "true")
	t.Setenv("DEFAULT_MODEL", "anthropic.claude-3-sonnet-20240229-v1:0")
	t.Setenv("DEFAULT_EMBEDDING_MODEL", "cohere.embed-multilingual-v3")
	t.Setenv("MAX_REQUEST_BODY_BYTES", "1048576")
	t.Setenv("RATE_LIMIT_RPM", "120")
	t.Setenv("RATE_LIMIT_BURST", "20")
	t.Setenv("ENABLE_METRICS", "true")
	t.Setenv("ALLOW_PRIVATE_IMAGE_FETCH", "false")
	t.Setenv("IMAGE_MAX_BYTES", "2097152")
	t.Setenv("IMAGE_FETCH_TIMEOUT_SECONDS", "8")
}

func TestLoad_Success(t *testing.T) {
	setBaseEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Port != "8123" {
		t.Fatalf("expected port 8123, got %q", cfg.Port)
	}
	if cfg.APIKey != "sk-test" {
		t.Fatalf("expected api key sk-test, got %q", cfg.APIKey)
	}
	if cfg.AllowAnyOrigin {
		t.Fatalf("expected allow any origin false")
	}
	if len(cfg.AllowedOrigins) != 2 {
		t.Fatalf("expected two allowed origins, got %d", len(cfg.AllowedOrigins))
	}
	if !cfg.EnableMetrics {
		t.Fatalf("expected metrics enabled")
	}
	if cfg.MaxRequestBodyBytes != 1048576 {
		t.Fatalf("unexpected max body bytes: %d", cfg.MaxRequestBodyBytes)
	}
	if cfg.ImageMaxBytes != 2097152 {
		t.Fatalf("unexpected image max bytes: %d", cfg.ImageMaxBytes)
	}
}

func TestLoad_AllowAnyOrigin(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ALLOWED_ORIGINS", "*")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.AllowAnyOrigin {
		t.Fatalf("expected allow any origin true")
	}
}

func TestLoad_MissingAPIKey(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "API_KEY") {
		t.Fatalf("expected API_KEY error, got %v", err)
	}
}

func TestLoad_InvalidLogLevel(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("LOG_LEVEL", "trace")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid LOG_LEVEL") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoad_InvalidNumericBounds(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("MAX_REQUEST_BODY_BYTES", "0")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "MAX_REQUEST_BODY_BYTES") {
		t.Fatalf("unexpected error: %v", err)
	}
}
