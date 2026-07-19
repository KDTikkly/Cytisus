package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
)

func TestAPIHealthRoute(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	newHandler(config.Config{Environment: config.EnvironmentTest}, applicationServices{}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"status":"ok"`) {
		t.Fatalf("unexpected health response: %s", recorder.Body.String())
	}
}
