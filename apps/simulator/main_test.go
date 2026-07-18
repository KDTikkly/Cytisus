package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
)

func TestDemoIdentityHandlerMarksResponseSimulated(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/demo/identity", nil)
	demoIdentityHandler(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	if recorder.Header().Get("X-Cytisus-Data-Classification") != "SIMULATED" {
		t.Fatal("expected explicit simulated classification header")
	}
	if !strings.Contains(recorder.Body.String(), `"classification":"SIMULATED"`) {
		t.Fatalf("response is not explicitly simulated: %s", recorder.Body.String())
	}
}

func TestSimulatorHealthRoute(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	newHandler(config.Config{Environment: config.EnvironmentTest}, "providers").ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"service":"simulator-providers"`) {
		t.Fatalf("unexpected simulator health response: %s", recorder.Body.String())
	}
}
