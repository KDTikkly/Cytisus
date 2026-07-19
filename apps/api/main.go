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
	"github.com/KDTikkly/Cytisus/internal/marketdata"
	marketprovider "github.com/KDTikkly/Cytisus/internal/marketdata/provider"
	"github.com/KDTikkly/Cytisus/internal/paperapi"
	"github.com/KDTikkly/Cytisus/internal/securities"
	brokerprovider "github.com/KDTikkly/Cytisus/internal/securities/provider"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if cfg.DatabaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	quoteProvider, err := marketprovider.NewLocal(pool, cfg.Environment)
	if err != nil {
		log.Fatal(err)
	}
	brokerProvider, err := brokerprovider.NewLocal(cfg.Environment)
	if err != nil {
		log.Fatal(err)
	}
	paperService, err := securities.NewService(securities.Dependencies{
		Database:    pool,
		Environment: cfg.Environment,
		Catalog:     marketdata.NewCatalog(pool),
		Quotes:      quoteProvider,
		Broker:      brokerProvider,
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf(`{"level":"info","service":"api","message":"starting","address":%q}`, cfg.APIAddress)
	if err := httpserver.Run(ctx, cfg.APIAddress, newHandler(cfg, paperService)); err != nil {
		log.Fatal(err)
	}
}

func newHandler(cfg config.Config, services ...paperapi.Service) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/healthz", httpserver.HealthHandler("api", string(cfg.Environment), time.Now))
	if len(services) > 0 && services[0] != nil {
		mux.Handle("/v1/", paperapi.New(services[0], cfg.WebOrigin))
	}
	return mux
}
