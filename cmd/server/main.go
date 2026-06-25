// Package main is the entry point for the DigitalFX API server.
//
//	@title          DigitalFX API
//	@version        1.0
//	@description    DigitalFX is a multi-currency fintech platform for Central/West Africa.
//	@description    It combines fiat accounts (XAF, XOF, USD, GBP, EUR), HD crypto wallets (BIP-44 via Rach WaaS),
//	@description    and stablecoin P2P transfers (USDC/USDT via Rach CaaS ERC-4337 SCWs).
//	@description    Mobile Money deposits and withdrawals are handled via HUB2 (MTN, Orange).
//
//	@contact.name   Rach Finance Engineering
//	@contact.email  engineering@rach.finance
//
//	@host           localhost:8080
//	@BasePath       /api/v1
//
//	@securityDefinitions.apikey  BearerAuth
//	@in                          header
//	@name                        Authorization
//	@description                 JWT access token — prefix with "Bearer ": Bearer eyJhbGci...
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/rachfinance/digitalfx/docs"

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
	log.Printf("Swagger UI: http://localhost:%s/swagger/index.html", cfg.Server.Port)

	if err := server.Start(ctx); err != nil {
		log.Fatalf("server: %v", err)
	}
}
