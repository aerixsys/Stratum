package main

import (
	"fmt"
	"log"

	"github.com/stratum/gateway/internal/config"
	"github.com/stratum/gateway/internal/server"
)

var (
	loadConfig = config.Load
	runServer  = server.Run
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("%v", err)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if err := runServer(cfg); err != nil {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}
