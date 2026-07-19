package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/marketdata"
	marketprovider "github.com/KDTikkly/Cytisus/internal/marketdata/provider"
	rwaservice "github.com/KDTikkly/Cytisus/internal/rwa"
	rwaprovider "github.com/KDTikkly/Cytisus/internal/rwa/provider"
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
	quotes, err := marketprovider.NewLocal(pool, cfg.Environment)
	if err != nil {
		log.Fatal(err)
	}
	broker, err := brokerprovider.NewLocal(cfg.Environment)
	if err != nil {
		log.Fatal(err)
	}
	securitiesService, err := securities.NewService(securities.Dependencies{
		Database: pool, Environment: cfg.Environment, Catalog: marketdata.NewCatalog(pool), Quotes: quotes, Broker: broker,
	})
	if err != nil {
		log.Fatal(err)
	}
	chain, err := rwaprovider.NewAnvilAdapter(cfg.Environment, rwaprovider.AnvilConfig{RPCURL: cfg.RWARPCURL, AdminAddress: cfg.RWAAdminAddress})
	if err != nil {
		log.Fatal(err)
	}
	verifier, err := rwaprovider.NewLocalAddressVerifier(cfg.Environment)
	if err != nil {
		log.Fatal(err)
	}
	rwaService, err := rwaservice.NewService(rwaservice.Dependencies{
		Database: pool, Environment: cfg.Environment, Resolver: workerSessionResolver{service: securitiesService},
		Custodian: securitiesService, Provider: chain, AddressVerifier: verifier,
	})
	if err != nil {
		log.Fatal(err)
	}

	log.Printf(`{"level":"info","service":"worker","environment":%q,"message":"ready"}`, cfg.Environment)
	runRWAJobs(ctx, rwaService)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Print(`{"level":"info","service":"worker","message":"stopped"}`)
			return
		case <-ticker.C:
			runRWAJobs(ctx, rwaService)
		}
	}
}

type workerSessionResolver struct{ service *securities.Service }

func (resolver workerSessionResolver) ResolveSession(ctx context.Context, accessToken string) (rwaservice.CustomerSession, error) {
	account, err := resolver.service.ResolveSession(ctx, accessToken)
	if err != nil {
		return rwaservice.CustomerSession{}, err
	}
	return rwaservice.CustomerSession{PaperAccountID: account.ID, CustomerReference: account.CustomerReference, CashLedgerAccountID: account.CashLedgerAccountID}, nil
}

func runRWAJobs(ctx context.Context, service *rwaservice.Service) {
	recovered, err := service.RecoverDueOperations(ctx, "rwa-chain-worker", 20)
	if err != nil {
		log.Printf(`{"level":"error","service":"worker","job":"rwa_recovery","error_code":"RWA_RECOVERY_FAILED"}`)
	} else if recovered > 0 {
		log.Printf(`{"level":"info","service":"worker","job":"rwa_recovery","completed":%d}`, recovered)
	}
	reconciled, err := service.RunDailyReconciliation(ctx)
	if err != nil {
		log.Printf(`{"level":"error","service":"worker","job":"rwa_daily_reconciliation","error_code":"RWA_RECONCILIATION_FAILED"}`)
	} else if reconciled > 0 {
		log.Printf(`{"level":"info","service":"worker","job":"rwa_daily_reconciliation","completed":%d}`, reconciled)
	}
}
