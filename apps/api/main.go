package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/KDTikkly/Cytisus/internal/banking"
	bankprovider "github.com/KDTikkly/Cytisus/internal/banking/provider"
	"github.com/KDTikkly/Cytisus/internal/bankingapi"
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
	bankProvider, err := bankprovider.NewLocal(cfg.Environment)
	if err != nil {
		log.Fatal(err)
	}
	bankingService, err := banking.NewService(banking.Dependencies{
		Database:    pool,
		Environment: cfg.Environment,
		Resolver:    securitiesSessionResolver{service: paperService},
		Provider:    bankProvider,
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf(`{"level":"info","service":"api","message":"starting","address":%q}`, cfg.APIAddress)
	if err := httpserver.Run(ctx, cfg.APIAddress, newHandler(cfg, applicationServices{paper: paperService, banking: bankingService})); err != nil {
		log.Fatal(err)
	}
}

type applicationServices struct {
	paper   paperapi.Service
	banking bankingapi.Service
}

type securitiesSessionResolver struct {
	service *securities.Service
}

func (resolver securitiesSessionResolver) ResolveSession(ctx context.Context, accessToken string) (banking.CustomerSession, error) {
	account, err := resolver.service.ResolveSession(ctx, accessToken)
	if err != nil {
		return banking.CustomerSession{}, err
	}
	return banking.CustomerSession{
		PaperAccountID: account.ID, CustomerReference: account.CustomerReference, CashLedgerAccountID: account.CashLedgerAccountID,
	}, nil
}

func newHandler(cfg config.Config, services applicationServices) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/healthz", httpserver.HealthHandler("api", string(cfg.Environment), time.Now))
	if services.paper != nil {
		mux.Handle("/v1/", paperapi.New(services.paper, cfg.WebOrigin))
	}
	if services.banking != nil {
		userHandler := bankingapi.NewUser(services.banking, cfg.Environment, cfg.WebOrigin)
		mux.Handle("/v1/banks/", userHandler)
		mux.Handle("/v1/transfers/", userHandler)
		mux.Handle("/internal/v1/admin/", bankingapi.NewAdmin(services.banking, cfg.Environment, cfg.AdminWebOrigin))
		mux.Handle("/internal/v1/simulators/bank/", bankingapi.NewSimulator(services.banking, cfg.Environment))
	}
	return mux
}
