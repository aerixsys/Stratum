package main

import (
	"log"

	"github.com/stratum/gateway/internal/config"
	"github.com/stratum/gateway/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Config error: %v", err)
	}

	if err := server.Run(cfg); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
