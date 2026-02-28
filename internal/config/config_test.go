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
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("MAX_REQUEST_BODY_BYTES", "1048576")
	t.Setenv("ENABLE_METRICS", "true")
	t.Setenv("MODEL_POLICY_PATH", "")
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
	if !cfg.EnableMetrics {
		t.Fatalf("expected metrics enabled")
	}
	if cfg.MaxRequestBodyBytes != 1048576 {
		t.Fatalf("unexpected max body bytes: %d", cfg.MaxRequestBodyBytes)
	}
}

func TestLoad_MissingAPIKey(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("API_KEY", "")

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

func TestLoad_InvalidNumericFormat(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("MAX_REQUEST_BODY_BYTES", "abc")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "MAX_REQUEST_BODY_BYTES") {
		t.Fatalf("unexpected error: %v", err)
	}
}
