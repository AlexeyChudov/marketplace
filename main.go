package main

import (
	"context"
	"embed"
	"os"
	"os/signal"
	"syscall"

	"marketplace-api/internal/app"
	"marketplace-api/internal/config"
	httpapi "marketplace-api/internal/handlers/http"
	"marketplace-api/internal/logger"
	"marketplace-api/internal/security"
	"marketplace-api/internal/storage/memory"
)

//go:embed web/index.html
var webFS embed.FS

func main() {
	args := os.Args[1:]
	log := logger.NewDefault()
	cfg, err := config.Load(args)
	if err != nil {
		log.Error("loading config: ", "err", err)
		os.Exit(1)
	}

	tokens, err := security.NewTokenService(cfg.JWTSecret)
	if err != nil {
		log.Error("create token service: ", "err", err)
		os.Exit(1)
	}

	frontend, err := webFS.ReadFile("web/index.html")
	if err != nil {
		log.Error("read frontend file: ", "err", err)
	}

	repository := memory.NewRepository()
	auth := app.NewAuthService(repository, security.PasswordHasher{}, tokens, nil)
	listings := app.NewListingService(repository, nil, nil)
	server := httpapi.NewServer(cfg, log, auth, listings, tokens, frontend)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := server.Run(ctx); err != nil {
		log.Error("run HTTP server: ", "err", err)
	}
}
