package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/stratum/gateway/internal/config"
)

func TestRun_Success(t *testing.T) {
	oldLoadConfig := loadConfig
	oldRunServer := runServer
	defer func() {
		loadConfig = oldLoadConfig
		runServer = oldRunServer
	}()

	loadConfig = func() (*config.Config, error) {
		return &config.Config{Port: "8000"}, nil
	}

	called := false
	runServer = func(cfg *config.Config) error {
		called = true
		if cfg == nil {
			t.Fatal("expected non-nil config")
		}
		return nil
	}

	if err := run(); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !called {
		t.Fatal("expected runServer to be called")
	}
}

func TestRun_ConfigError(t *testing.T) {
	oldLoadConfig := loadConfig
	oldRunServer := runServer
	defer func() {
		loadConfig = oldLoadConfig
		runServer = oldRunServer
	}()

	loadConfig = func() (*config.Config, error) {
		return nil, errors.New("bad config")
	}
	runServer = func(cfg *config.Config) error {
		t.Fatal("runServer should not be called when config fails")
		return nil
	}

	err := run()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "config") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_ServerError(t *testing.T) {
	oldLoadConfig := loadConfig
	oldRunServer := runServer
	defer func() {
		loadConfig = oldLoadConfig
		runServer = oldRunServer
	}()

	loadConfig = func() (*config.Config, error) {
		return &config.Config{Port: "8000"}, nil
	}
	runServer = func(cfg *config.Config) error {
		return errors.New("boom")
	}

	err := run()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "server") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_LoggingConfigError(t *testing.T) {
	oldLoadConfig := loadConfig
	oldRunServer := runServer
	defer func() {
		loadConfig = oldLoadConfig
		runServer = oldRunServer
	}()

	loadConfig = func() (*config.Config, error) {
		return &config.Config{
			Port:     "8000",
			LogLevel: "invalid",
		}, nil
	}
	runServer = func(cfg *config.Config) error {
		t.Fatal("runServer should not be called when logging config fails")
		return nil
	}

	err := run()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "logging") {
		t.Fatalf("unexpected error: %v", err)
	}
}
