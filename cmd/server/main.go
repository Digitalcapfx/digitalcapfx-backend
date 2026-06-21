package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/rachfinance/digitalfx/internal/api"
	"github.com/rachfinance/digitalfx/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	server, err := api.NewServer(cfg)
	if err != nil {
		log.Fatalf("server init: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("DigitalFX starting on :%s [%s]", cfg.Server.Port, cfg.Server.Env)

	if err := server.Start(ctx); err != nil {
		log.Fatalf("server: %v", err)
	}
}
