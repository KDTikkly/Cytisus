package cryptoapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/KDTikkly/Cytisus/internal/crypto"
	"github.com/KDTikkly/Cytisus/internal/crypto/provider"
	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/money"
)

const maximumBodyBytes = 64 << 10

type Service interface {
	ListAssets(context.Context) ([]crypto.Asset, error)
	Portfolio(context.Context, string) ([]crypto.PortfolioBalance, error)
	Convert(context.Context, crypto.ConvertCommand) (crypto.Conversion, error)
	GetConversion(context.Context, string, string) (crypto.Conversion, error)
	ListConversions(context.Context, string, int32) ([]crypto.Conversion, error)
	CreateDepositAddress(context.Context, crypto.CreateDepositAddressCommand) (crypto.DepositAddress, error)
	ListDepositAddresses(context.Context, string) ([]crypto.DepositAddress, error)
	ListDeposits(context.Context, string, int32) ([]crypto.Deposit, error)
	ApplyDepositEvent(context.Context, crypto.DepositEvent) (crypto.Deposit, error)
	AddWithdrawalAddress(context.Context, crypto.AddWithdrawalAddressCommand) (crypto.WithdrawalAddress, error)
	ActivateWithdrawalAddress(context.Context, crypto.ActivateWithdrawalAddressCommand) (crypto.WithdrawalAddress, error)
	ListWithdrawalAddresses(context.Context, string) ([]crypto.WithdrawalAddress, error)
	RequestWithdrawal(context.Context, crypto.RequestWithdrawalCommand) (crypto.Withdrawal, error)
	ApplyWithdrawalEvent(context.Context, crypto.WithdrawalEvent) (crypto.Withdrawal, error)
	ListWithdrawals(context.Context, string, int32) ([]crypto.Withdrawal, error)
	Reconcile(context.Context, crypto.ReconcileCommand) (crypto.ReconciliationRun, error)
}

type handler struct {
	service       Service
	environment   config.Environment
	allowedOrigin string
}

func NewUser(service Service, environment config.Environment, allowedOrigin string) http.Handler {
	h := &handler{service: service, environment: environment, allowedOrigin: strings.TrimRight(allowedOrigin, "/")}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/crypto/assets", h.listAssets)
	mux.HandleFunc("GET /v1/crypto/portfolio", h.portfolio)
	mux.HandleFunc("POST /v1/crypto/conversions", h.convert)
	mux.HandleFunc("GET /v1/crypto/conversions", h.listConversions)
	mux.HandleFunc("GET /v1/crypto/conversions/{conversion_id}", h.getConversion)
	mux.HandleFunc("POST /v1/crypto/deposit-addresses", h.createDepositAddress)
	mux.HandleFunc("GET /v1/crypto/deposit-addresses", h.listDepositAddresses)
	mux.HandleFunc("GET /v1/crypto/deposits", h.listDeposits)
	mux.HandleFunc("POST /v1/crypto/withdrawal-addresses", h.addWithdrawalAddress)
	mux.HandleFunc("GET /v1/crypto/withdrawal-addresses", h.listWithdrawalAddresses)
	mux.HandleFunc("POST /v1/crypto/withdrawal-addresses/{address_id}/activate", h.activateWithdrawalAddress)
	mux.HandleFunc("POST /v1/crypto/withdrawals", h.requestWithdrawal)
	mux.HandleFunc("GET /v1/crypto/withdrawals", h.listWithdrawals)
	return h.cors(mux)
}

func NewSimulator(service Service, environment config.Environment) http.Handler {
	h := &handler{service: service, environment: environment}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/v1/simulators/crypto/events", h.simulatorEvent)
	return mux
}

func NewAdmin(service Service, environment config.Environment, allowedOrigin string) http.Handler {
	h := &handler{service: service, environment: environment, allowedOrigin: strings.TrimRight(allowedOrigin, "/")}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/v1/admin/reconciliation/crypto", h.reconcile)
	return h.cors(mux)
}

func (h *handler) listAssets(response http.ResponseWriter, request *http.Request) {
	assets, err := h.service.ListAssets(request.Context())
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]any, 0, len(assets))
	for _, asset := range assets {
		items = append(items, assetDTO(asset))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "quote_currency": "USD", "mode": "SIMULATED"})
}

func (h *handler) portfolio(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, crypto.ErrUnauthorized)
		return
	}
	balances, err := h.service.Portfolio(request.Context(), token)
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]any, 0, len(balances))
	for _, balance := range balances {
		items = append(items, portfolioDTO(balance))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "custody_model": "CUSTODIAL", "mode": "SIMULATED"})
}

func (h *handler) convert(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, crypto.ErrUnauthorized)
		return
	}
	var body struct {
		SourceAsset        string `json:"source_asset"`
		DestinationAsset   string `json:"destination_asset"`
		SourceAmount       string `json:"source_amount"`
		SimulationScenario string `json:"simulation_scenario"`
		SimulationRiskFlag bool   `json:"simulation_risk_flag"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	amount, err := money.Parse(body.SourceAmount)
	if err != nil {
		writeError(response, crypto.ErrInvalidCommand)
		return
	}
	converted, err := h.service.Convert(request.Context(), crypto.ConvertCommand{
		AccessToken: token, IdempotencyKey: request.Header.Get("Idempotency-Key"),
		SourceAsset: body.SourceAsset, DestinationAsset: body.DestinationAsset, SourceAmount: amount,
		SimulationScenario: provider.Scenario(body.SimulationScenario), SimulationRiskFlag: body.SimulationRiskFlag,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	status := http.StatusCreated
	if converted.Replayed {
		status = http.StatusOK
	}
	writeJSON(response, status, conversionDTO(converted))
}

func (h *handler) getConversion(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, crypto.ErrUnauthorized)
		return
	}
	converted, err := h.service.GetConversion(request.Context(), token, request.PathValue("conversion_id"))
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, conversionDTO(converted))
}

func (h *handler) listConversions(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, crypto.ErrUnauthorized)
		return
	}
	rows, err := h.service.ListConversions(request.Context(), token, pageSize(request))
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, conversionDTO(row))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "mode": "SIMULATED"})
}

func (h *handler) createDepositAddress(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, crypto.ErrUnauthorized)
		return
	}
	var body struct {
		Asset   string `json:"asset"`
		Network string `json:"network"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	address, err := h.service.CreateDepositAddress(request.Context(), crypto.CreateDepositAddressCommand{
		AccessToken: token, IdempotencyKey: request.Header.Get("Idempotency-Key"), Asset: body.Asset, Network: body.Network,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	status := http.StatusCreated
	if address.Replayed {
		status = http.StatusOK
	}
	writeJSON(response, status, depositAddressDTO(address))
}

func (h *handler) listDepositAddresses(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, crypto.ErrUnauthorized)
		return
	}
	rows, err := h.service.ListDepositAddresses(request.Context(), token)
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, depositAddressDTO(row))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "mode": "SIMULATED"})
}

func (h *handler) listDeposits(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, crypto.ErrUnauthorized)
		return
	}
	rows, err := h.service.ListDeposits(request.Context(), token, pageSize(request))
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, depositDTO(row))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "mode": "SIMULATED"})
}

func (h *handler) addWithdrawalAddress(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, crypto.ErrUnauthorized)
		return
	}
	var body struct {
		Asset                string `json:"asset"`
		Network              string `json:"network"`
		ExternalAddress      string `json:"external_address"`
		Label                string `json:"label"`
		UntrustedDevice      bool   `json:"untrusted_device"`
		RecentSecurityChange bool   `json:"recent_security_change"`
		RecentRecovery       bool   `json:"recent_recovery"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	address, err := h.service.AddWithdrawalAddress(request.Context(), crypto.AddWithdrawalAddressCommand{
		AccessToken: token, IdempotencyKey: request.Header.Get("Idempotency-Key"), Asset: body.Asset,
		Network: body.Network, ExternalAddress: body.ExternalAddress, Label: body.Label,
		RiskContext: crypto.AddressRiskContext{UntrustedDevice: body.UntrustedDevice, RecentSecurityChange: body.RecentSecurityChange, RecentRecovery: body.RecentRecovery},
	})
	if err != nil {
		writeError(response, err)
		return
	}
	status := http.StatusCreated
	if address.Replayed {
		status = http.StatusOK
	}
	writeJSON(response, status, withdrawalAddressDTO(address))
}

func (h *handler) activateWithdrawalAddress(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, crypto.ErrUnauthorized)
		return
	}
	address, err := h.service.ActivateWithdrawalAddress(request.Context(), crypto.ActivateWithdrawalAddressCommand{
		AccessToken: token, AddressID: request.PathValue("address_id"),
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, withdrawalAddressDTO(address))
}

func (h *handler) listWithdrawalAddresses(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, crypto.ErrUnauthorized)
		return
	}
	rows, err := h.service.ListWithdrawalAddresses(request.Context(), token)
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, withdrawalAddressDTO(row))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "mode": "SIMULATED"})
}

func (h *handler) requestWithdrawal(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, crypto.ErrUnauthorized)
		return
	}
	var body struct {
		AddressID string `json:"address_id"`
		Quantity  string `json:"quantity"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	quantity, err := money.Parse(body.Quantity)
	if err != nil {
		writeError(response, crypto.ErrInvalidCommand)
		return
	}
	withdrawal, err := h.service.RequestWithdrawal(request.Context(), crypto.RequestWithdrawalCommand{
		AccessToken: token, IdempotencyKey: request.Header.Get("Idempotency-Key"), AddressID: body.AddressID, Quantity: quantity,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	status := http.StatusCreated
	if withdrawal.Replayed {
		status = http.StatusOK
	}
	writeJSON(response, status, withdrawalDTO(withdrawal))
}

func (h *handler) listWithdrawals(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, crypto.ErrUnauthorized)
		return
	}
	rows, err := h.service.ListWithdrawals(request.Context(), token, pageSize(request))
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, withdrawalDTO(row))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "mode": "SIMULATED"})
}

func (h *handler) simulatorEvent(response http.ResponseWriter, request *http.Request) {
	if h.environment != config.EnvironmentLocal && h.environment != config.EnvironmentTest {
		writeError(response, crypto.ErrAdminUnauthorized)
		return
	}
	var body struct {
		Mode                  string `json:"mode"`
		ExternalEventID       string `json:"external_event_id"`
		ResourceType          string `json:"resource_type"`
		ResourceID            string `json:"resource_id"`
		EventType             string `json:"event_type"`
		Asset                 string `json:"asset"`
		Network               string `json:"network"`
		Quantity              string `json:"quantity"`
		ProviderTransactionID string `json:"provider_transaction_id"`
		ReasonCode            string `json:"reason_code"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	if body.Mode != "SIMULATED" {
		writeError(response, crypto.ErrInvalidCommand)
		return
	}
	payload, _ := json.Marshal(body)
	switch body.ResourceType {
	case "DEPOSIT":
		quantity, err := money.Parse(body.Quantity)
		if err != nil {
			writeError(response, crypto.ErrInvalidCommand)
			return
		}
		deposit, err := h.service.ApplyDepositEvent(request.Context(), crypto.DepositEvent{
			Provider: "local-custody-simulator", ExternalEventID: body.ExternalEventID,
			DepositAddressID: body.ResourceID, Asset: body.Asset, Network: body.Network, Quantity: quantity,
			EventType: body.EventType, ProviderTransactionID: body.ProviderTransactionID,
			ReasonCode: body.ReasonCode, OccurredAt: time.Now().UTC(), Payload: payload,
		})
		if err != nil {
			writeError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, depositDTO(deposit))
	case "WITHDRAWAL":
		withdrawal, err := h.service.ApplyWithdrawalEvent(request.Context(), crypto.WithdrawalEvent{
			Provider: "local-custody-simulator", ExternalEventID: body.ExternalEventID,
			WithdrawalID: body.ResourceID, EventType: body.EventType, ReasonCode: body.ReasonCode,
			OccurredAt: time.Now().UTC(), Payload: payload,
		})
		if err != nil {
			writeError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, withdrawalDTO(withdrawal))
	default:
		writeError(response, crypto.ErrInvalidCommand)
	}
}

func (h *handler) reconcile(response http.ResponseWriter, request *http.Request) {
	actor, ok := adminActor(request)
	if !ok {
		writeError(response, crypto.ErrAdminUnauthorized)
		return
	}
	var body struct {
		CustomerReference string  `json:"customer_reference"`
		Asset             string  `json:"asset"`
		CustodyAmount     *string `json:"custody_amount"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	var custodyAmount *money.Decimal
	if body.CustodyAmount != nil {
		parsed, err := money.Parse(*body.CustodyAmount)
		if err != nil {
			writeError(response, crypto.ErrInvalidCommand)
			return
		}
		custodyAmount = &parsed
	}
	run, err := h.service.Reconcile(request.Context(), crypto.ReconcileCommand{
		Actor: actor, CustomerReference: body.CustomerReference, Asset: body.Asset, CustodyAmount: custodyAmount,
	})
	if err != nil && !errors.Is(err, crypto.ErrReconciliationDifference) {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, reconciliationDTO(run))
}

func bearerToken(request *http.Request) (string, bool) {
	value := strings.TrimSpace(request.Header.Get("Authorization"))
	if !strings.HasPrefix(value, "Bearer ") || len(value) <= len("Bearer ") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(value, "Bearer ")), true
}

func adminActor(request *http.Request) (crypto.AdminActor, bool) {
	actor := crypto.AdminActor{ID: strings.TrimSpace(request.Header.Get("X-Admin-ID")), Role: strings.TrimSpace(request.Header.Get("X-Admin-Role"))}
	return actor, actor.ID != "" && actor.Role != ""
}

func pageSize(request *http.Request) int32 {
	parsed, err := strconv.ParseInt(request.URL.Query().Get("page_size"), 10, 32)
	if err != nil || parsed <= 0 {
		return 50
	}
	return int32(parsed)
}

func decodeJSON(response http.ResponseWriter, request *http.Request, destination any) error {
	request.Body = http.MaxBytesReader(response, request.Body, maximumBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("%w: invalid JSON body", crypto.ErrInvalidCommand)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: body must contain one JSON object", crypto.ErrInvalidCommand)
	}
	return nil
}

func (h *handler) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		origin := strings.TrimRight(request.Header.Get("Origin"), "/")
		if h.allowedOrigin != "" && origin == h.allowedOrigin {
			response.Header().Set("Access-Control-Allow-Origin", h.allowedOrigin)
			response.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Admin-ID, X-Admin-Role")
			response.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			response.Header().Set("Vary", "Origin")
		}
		if request.Method == http.MethodOptions {
			if origin != h.allowedOrigin {
				writeError(response, crypto.ErrUnauthorized)
				return
			}
			response.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}

func writeError(response http.ResponseWriter, err error) {
	status, code, message, nextAction := errorDetails(err)
	writeJSON(response, status, map[string]any{"error": map[string]string{"code": code, "message": message, "next_action": nextAction}})
}

func errorDetails(err error) (int, string, string, string) {
	switch {
	case errors.Is(err, crypto.ErrUnauthorized):
		return http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required.", "Register the local fixture and try again."
	case errors.Is(err, crypto.ErrAdminUnauthorized):
		return http.StatusForbidden, "ADMIN_PERMISSION_REQUIRED", "This admin role cannot perform the requested action.", "Use an authorized role or ask an administrator for access."
	case errors.Is(err, crypto.ErrAssetNotFound):
		return http.StatusNotFound, "ASSET_OR_NETWORK_NOT_FOUND", "The requested crypto asset or native network is unavailable.", "Refresh the supported asset and network list."
	case errors.Is(err, crypto.ErrConversionNotFound), errors.Is(err, crypto.ErrAddressNotFound), errors.Is(err, crypto.ErrWithdrawalNotFound):
		return http.StatusNotFound, "RESOURCE_NOT_FOUND", "The requested crypto resource was not found.", "Refresh the page and select an available record."
	case errors.Is(err, crypto.ErrInsufficientCash):
		return http.StatusUnprocessableEntity, "INSUFFICIENT_ELIGIBLE_USD", "Only settled and withdrawable USD can fund crypto purchases.", "Lower the amount or wait for eligible USD to settle."
	case errors.Is(err, crypto.ErrInsufficientAsset):
		return http.StatusUnprocessableEntity, "INSUFFICIENT_SETTLED_ASSET", "The settled crypto quantity is insufficient.", "Lower the quantity or wait for a pending deposit to confirm."
	case errors.Is(err, crypto.ErrCoolingOff):
		return http.StatusConflict, "ADDRESS_COOLING_ACTIVE", "The withdrawal address cooling period is still active.", "Wait until the displayed activation time and try again."
	case errors.Is(err, crypto.ErrAddressRiskBlocked):
		return http.StatusUnprocessableEntity, "ADDRESS_RISK_REVIEW_FAILED", "This address cannot be used after risk review.", "Add a different address or contact support."
	case errors.Is(err, crypto.ErrReviewRequired):
		return http.StatusConflict, "CRYPTO_TO_USD_REVIEW_REQUIRED", "The crypto-to-USD conversion requires review.", "Monitor the case status and provide any requested information."
	case errors.Is(err, crypto.ErrIdempotencyConflict), errors.Is(err, crypto.ErrProviderEventConflict):
		return http.StatusConflict, "IDEMPOTENCY_CONFLICT", "This request identifier was already used with different data.", "Retry the original request unchanged or use a new identifier."
	case errors.Is(err, crypto.ErrInvalidState):
		return http.StatusConflict, "INVALID_STATE", "This action is not available in the current state.", "Refresh the status and follow the displayed next action."
	case errors.Is(err, provider.ErrTimeout):
		return http.StatusGatewayTimeout, "PROVIDER_TIMEOUT", "A simulated crypto provider did not respond in time.", "Retry with the same idempotency key."
	case errors.Is(err, crypto.ErrInvalidCommand), errors.Is(err, crypto.ErrUnsupportedPair), errors.Is(err, money.ErrInvalidDecimal), errors.Is(err, money.ErrDecimalScale):
		return http.StatusBadRequest, "INVALID_REQUEST", "The request is invalid.", "Review the asset, amount, network, and request identifiers."
	default:
		return http.StatusInternalServerError, "INTERNAL_ERROR", "The request could not be completed.", "Try again later or contact support with the request ID."
	}
}
