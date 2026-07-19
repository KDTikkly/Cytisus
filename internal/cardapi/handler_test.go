package cardapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
)

func TestUserErrorAndCORSContract(t *testing.T) {
	handler := NewUser(nil, config.EnvironmentTest, "http://localhost:3000")

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/card", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized status, got %d", unauthorized.Code)
	}
	var body struct {
		Error struct {
			Code       string `json:"code"`
			NextAction string `json:"next_action"`
		} `json:"error"`
	}
	if err := json.Unmarshal(unauthorized.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "AUTHENTICATION_REQUIRED" || body.Error.NextAction == "" {
		t.Fatalf("unexpected stable error contract: %+v", body.Error)
	}

	preflight := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, "/v1/card", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	handler.ServeHTTP(preflight, request)
	if preflight.Code != http.StatusNoContent || preflight.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatalf("unexpected allowed preflight: status=%d headers=%v", preflight.Code, preflight.Header())
	}

	denied := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodOptions, "/v1/card", nil)
	request.Header.Set("Origin", "https://untrusted.example")
	handler.ServeHTTP(denied, request)
	if denied.Code != http.StatusUnauthorized || denied.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unexpected denied preflight: status=%d headers=%v", denied.Code, denied.Header())
	}
}

func TestSimulatorIsUnavailableInProduction(t *testing.T) {
	handler := NewSimulator(nil, config.EnvironmentProduction)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/internal/v1/simulators/card/capabilities", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected production simulator to be forbidden, got %d", response.Code)
	}
	var body map[string]map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"]["code"] != "SIMULATOR_DISABLED" {
		t.Fatalf("unexpected simulator error: %+v", body)
	}
}
