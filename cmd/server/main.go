package main

import (
	"fmt"
	"os"

	"github.com/stratum/gateway/internal/config"
	"github.com/stratum/gateway/internal/logging"
	"github.com/stratum/gateway/internal/server"
)

var (
	loadConfig = config.Load
	runServer  = server.Run
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if err := logging.Configure(cfg.LogLevel); err != nil {
		return fmt.Errorf("logging: %w", err)
	}
	if err := runServer(cfg); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}
