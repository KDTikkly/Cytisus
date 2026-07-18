package httpserver

import (
	"encoding/json"
	"net/http"
	"time"
)

type HealthResponse struct {
	Service     string `json:"service"`
	Status      string `json:"status"`
	Environment string `json:"environment"`
	Time        string `json:"time"`
}

func HealthHandler(service, environment string, now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(HealthResponse{
			Service:     service,
			Status:      "ok",
			Environment: environment,
			Time:        now().UTC().Format(time.RFC3339),
		})
	}
}
