package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/foundation/httpserver"
	identityprovider "github.com/KDTikkly/Cytisus/internal/identity/provider"
)

func main() {
	cfg := config.Load()
	if err := cfg.ValidateSimulator(); err != nil {
		log.Fatal(err)
	}

	mode := os.Getenv("CYTISUS_SIMULATOR_MODE")
	if mode == "" {
		mode = "providers"
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf(`{"level":"info","service":"simulator","mode":%q,"classification":"SIMULATED","message":"starting"}`, mode)
	if err := httpserver.Run(ctx, cfg.SimulatorAddress, newHandler(cfg, mode)); err != nil {
		log.Fatal(err)
	}
}

func newHandler(cfg config.Config, mode string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/healthz", httpserver.HealthHandler("simulator-"+mode, string(cfg.Environment), time.Now))
	mux.HandleFunc("/demo/identity", demoIdentityHandler)
	return mux
}

func demoIdentityHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Cytisus-Data-Classification", identityprovider.SimulatedClassification)
	_ = json.NewEncoder(w).Encode(identityprovider.SyntheticDemoIdentity())
}
