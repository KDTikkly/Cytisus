package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/foundation/httpserver"
)

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf(`{"level":"info","service":"api","message":"starting","address":%q}`, cfg.APIAddress)
	if err := httpserver.Run(ctx, cfg.APIAddress, newHandler(cfg)); err != nil {
		log.Fatal(err)
	}
}

func newHandler(cfg config.Config) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/healthz", httpserver.HealthHandler("api", string(cfg.Environment), time.Now))
	return mux
}
