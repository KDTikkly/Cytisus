package rwaapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/rwa"
	"github.com/KDTikkly/Cytisus/internal/rwa/provider"
)

type stubService struct {
	Service
	capability   provider.Capability
	forcedCalled *bool
}

func (service stubService) ProviderCapabilities(context.Context) (provider.Capability, error) {
	return service.capability, nil
}

func (service stubService) ForcedRedeem(_ context.Context, command rwa.ForcedRedeemCommand) (rwa.Redemption, error) {
	*service.forcedCalled = true
	return rwa.Redemption{
		ID: "0190f860-913a-7cc0-b7fd-0719e3776990", AssetID: "0190f860-913a-7cc0-b7fd-0719e3776991",
		UnderlyingLockID: "0190f860-913a-7cc0-b7fd-0719e3776992", Quantity: money.MustParse("1"),
		SourceAddress: "0x70997970C51812dc3A010C7d01b50e0d17dc79C8", OperationID: strings.Repeat("a", 64),
		Forced: true, Status: "COMPLETED",
	}, nil
}

func TestUserErrorAndCORSContract(t *testing.T) {
	handler := NewUser(nil, config.EnvironmentTest, "http://localhost:3000")

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/v1/rwa/assets", nil))
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
	request := httptest.NewRequest(http.MethodOptions, "/v1/rwa/assets", nil)
	request.Header.Set("Origin", "http://localhost:3000")
	handler.ServeHTTP(preflight, request)
	if preflight.Code != http.StatusNoContent || preflight.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatalf("unexpected allowed preflight: status=%d headers=%v", preflight.Code, preflight.Header())
	}
}

func TestSimulatorCapabilityContractAndProductionGuard(t *testing.T) {
	production := NewSimulator(nil, config.EnvironmentProduction)
	response := httptest.NewRecorder()
	production.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/internal/v1/simulators/rwa/capabilities", nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("expected production simulator to be forbidden, got %d", response.Code)
	}

	local := NewSimulator(stubService{capability: provider.Capability{
		Provider: "LOCAL_BASE_ANVIL", ChainName: "BASE_ANVIL", ChainID: provider.BaseAnvilChainID,
		Simulated: true, Permissioned: true, WholeSharesOnly: true, SimulationMessage: rwa.SimulationOnly,
	}}, config.EnvironmentTest)
	response = httptest.NewRecorder()
	local.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/internal/v1/simulators/rwa/capabilities", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected local capability response, got %d", response.Code)
	}
	var capability map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &capability); err != nil {
		t.Fatal(err)
	}
	if capability["chain_id"] != float64(provider.BaseAnvilChainID) || capability["simulation_message"] != rwa.SimulationOnly {
		t.Fatalf("unexpected capability contract: %+v", capability)
	}
	if _, leakedGoFieldName := capability["ChainID"]; leakedGoFieldName {
		t.Fatal("capability JSON must use OpenAPI snake_case field names")
	}
}

func TestForcedRedemptionAdminRoute(t *testing.T) {
	called := false
	handler := NewAdmin(stubService{forcedCalled: &called}, config.EnvironmentTest, "")
	request := httptest.NewRequest(http.MethodPost, "/internal/v1/admin/rwa/forced-redemptions", strings.NewReader(`{
		"customer_reference":"customer-fixture", "mint_id":"0190f860-913a-7cc0-b7fd-0719e3776990", "reason_code":"COURT_ORDER"
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "forced-redemption-001")
	request.Header.Set("X-Admin-ID", "ops-1")
	request.Header.Set("X-Admin-Role", rwa.AdminRoleOps)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || !called {
		t.Fatalf("forced redemption route was not dispatched: status=%d body=%s", response.Code, response.Body.String())
	}
}
