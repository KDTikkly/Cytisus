package rwaapi

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

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/rwa"
	"github.com/KDTikkly/Cytisus/internal/rwa/provider"
)

const maximumBodyBytes = 64 << 10

type Service interface {
	ProviderCapabilities(context.Context) (provider.Capability, error)
	ProviderHealth(context.Context) (provider.Health, error)
	ListAssets(context.Context, string) ([]rwa.Asset, error)
	Portfolio(context.Context, string) ([]rwa.Holding, error)
	Mint(context.Context, rwa.MintCommand) (rwa.Mint, error)
	ListMints(context.Context, string) ([]rwa.Mint, error)
	Redeem(context.Context, rwa.RedeemCommand) (rwa.Redemption, error)
	ForcedRedeem(context.Context, rwa.ForcedRedeemCommand) (rwa.Redemption, error)
	ListRedemptions(context.Context, string) ([]rwa.Redemption, error)
	RegisterExternalAddress(context.Context, rwa.RegisterAddressCommand) (rwa.ExternalAddress, error)
	ReviewExternalAddress(context.Context, rwa.ReviewAddressCommand) (rwa.ExternalAddress, error)
	ActivateExternalAddress(context.Context, string, string) (rwa.ExternalAddress, error)
	ListExternalAddresses(context.Context, string) ([]rwa.ExternalAddress, error)
	RecoverOperation(context.Context, rwa.RecoverCommand) error
	Reconcile(context.Context, rwa.ReconcileCommand) (rwa.Reconciliation, error)
	ListReconciliations(context.Context, rwa.AdminActor, string, int32) ([]rwa.Reconciliation, error)
	AnnounceDividend(context.Context, rwa.AnnounceDividendCommand) (rwa.Dividend, error)
	ProcessDividend(context.Context, rwa.ProcessDividendCommand) (rwa.Dividend, []rwa.DividendEntitlement, error)
}

type handler struct {
	service       Service
	environment   config.Environment
	allowedOrigin string
}

func NewUser(service Service, environment config.Environment, allowedOrigin string) http.Handler {
	h := &handler{service: service, environment: environment, allowedOrigin: strings.TrimRight(allowedOrigin, "/")}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/rwa/assets", h.listAssets)
	mux.HandleFunc("GET /v1/rwa/portfolio", h.portfolio)
	mux.HandleFunc("POST /v1/rwa/mints", h.mint)
	mux.HandleFunc("GET /v1/rwa/mints", h.listMints)
	mux.HandleFunc("POST /v1/rwa/redemptions", h.redeem)
	mux.HandleFunc("GET /v1/rwa/redemptions", h.listRedemptions)
	mux.HandleFunc("POST /v1/rwa/addresses", h.registerAddress)
	mux.HandleFunc("GET /v1/rwa/addresses", h.listAddresses)
	mux.HandleFunc("POST /v1/rwa/addresses/{address_id}/activate", h.activateAddress)
	return h.cors(mux)
}

func NewAdmin(service Service, environment config.Environment, allowedOrigin string) http.Handler {
	h := &handler{service: service, environment: environment, allowedOrigin: strings.TrimRight(allowedOrigin, "/")}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/v1/admin/rwa/addresses/{address_id}/review", h.reviewAddress)
	mux.HandleFunc("POST /internal/v1/admin/rwa/forced-redemptions", h.forcedRedemption)
	mux.HandleFunc("POST /internal/v1/admin/rwa/recoveries/{operation_id}", h.recover)
	mux.HandleFunc("POST /internal/v1/admin/rwa/reconciliation", h.reconcile)
	mux.HandleFunc("GET /internal/v1/admin/rwa/reconciliation", h.listReconciliation)
	mux.HandleFunc("POST /internal/v1/admin/rwa/dividends", h.announceDividend)
	mux.HandleFunc("POST /internal/v1/admin/rwa/dividends/{dividend_id}/process", h.processDividend)
	return h.cors(mux)
}

func NewSimulator(service Service, environment config.Environment) http.Handler {
	h := &handler{service: service, environment: environment}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/v1/simulators/rwa/capabilities", h.capabilities)
	mux.HandleFunc("GET /internal/v1/simulators/rwa/health", h.health)
	return mux
}

func (h *handler) listAssets(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		writeError(w, rwa.ErrUnauthorized)
		return
	}
	items, err := h.service.ListAssets(r.Context(), token)
	if err != nil {
		writeError(w, err)
		return
	}
	values := make([]any, 0, len(items))
	for _, item := range items {
		values = append(values, assetDTO(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values, "mode": "SIMULATED", "chain": "BASE_ANVIL", "bridge_supported": false})
}

func (h *handler) portfolio(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		writeError(w, rwa.ErrUnauthorized)
		return
	}
	items, err := h.service.Portfolio(r.Context(), token)
	if err != nil {
		writeError(w, err)
		return
	}
	values := make([]any, 0, len(items))
	for _, item := range items {
		values = append(values, holdingDTO(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values, "custody_default": rwa.CustodyVault, "mode": "SIMULATED"})
}

func (h *handler) mint(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		writeError(w, rwa.ErrUnauthorized)
		return
	}
	var body struct {
		AssetID           string `json:"asset_id"`
		Quantity          string `json:"quantity"`
		CustodyMode       string `json:"custody_mode"`
		ExternalAddressID string `json:"external_address_id"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, err)
		return
	}
	quantity, err := money.Parse(body.Quantity)
	if err != nil {
		writeError(w, rwa.ErrInvalidCommand)
		return
	}
	value, err := h.service.Mint(r.Context(), rwa.MintCommand{AccessToken: token, IdempotencyKey: r.Header.Get("Idempotency-Key"), AssetID: body.AssetID, Quantity: quantity, CustodyMode: body.CustodyMode, ExternalAddressID: body.ExternalAddressID})
	if err != nil && !errors.Is(err, rwa.ErrOperationUnknown) {
		writeError(w, err)
		return
	}
	status := http.StatusCreated
	if errors.Is(err, rwa.ErrOperationUnknown) {
		status = http.StatusAccepted
	}
	writeJSON(w, status, mintDTO(value))
}

func (h *handler) listMints(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		writeError(w, rwa.ErrUnauthorized)
		return
	}
	items, err := h.service.ListMints(r.Context(), token)
	if err != nil {
		writeError(w, err)
		return
	}
	values := make([]any, 0, len(items))
	for _, item := range items {
		values = append(values, mintDTO(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values})
}

func (h *handler) redeem(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		writeError(w, rwa.ErrUnauthorized)
		return
	}
	var body struct {
		MintID string `json:"mint_id"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, err)
		return
	}
	value, err := h.service.Redeem(r.Context(), rwa.RedeemCommand{AccessToken: token, IdempotencyKey: r.Header.Get("Idempotency-Key"), MintID: body.MintID})
	if err != nil && !errors.Is(err, rwa.ErrOperationUnknown) {
		writeError(w, err)
		return
	}
	status := http.StatusCreated
	if errors.Is(err, rwa.ErrOperationUnknown) {
		status = http.StatusAccepted
	}
	writeJSON(w, status, redemptionDTO(value))
}

func (h *handler) listRedemptions(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		writeError(w, rwa.ErrUnauthorized)
		return
	}
	items, err := h.service.ListRedemptions(r.Context(), token)
	if err != nil {
		writeError(w, err)
		return
	}
	values := make([]any, 0, len(items))
	for _, item := range items {
		values = append(values, redemptionDTO(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values})
}

func (h *handler) registerAddress(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		writeError(w, rwa.ErrUnauthorized)
		return
	}
	var body struct {
		Address        string `json:"address"`
		ProofReference string `json:"proof_reference"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, err)
		return
	}
	value, err := h.service.RegisterExternalAddress(r.Context(), rwa.RegisterAddressCommand{AccessToken: token, IdempotencyKey: r.Header.Get("Idempotency-Key"), Address: body.Address, ProofReference: body.ProofReference})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, addressDTO(value))
}

func (h *handler) listAddresses(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		writeError(w, rwa.ErrUnauthorized)
		return
	}
	items, err := h.service.ListExternalAddresses(r.Context(), token)
	if err != nil {
		writeError(w, err)
		return
	}
	values := make([]any, 0, len(items))
	for _, item := range items {
		values = append(values, addressDTO(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values})
}

func (h *handler) activateAddress(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		writeError(w, rwa.ErrUnauthorized)
		return
	}
	value, err := h.service.ActivateExternalAddress(r.Context(), token, r.PathValue("address_id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, addressDTO(value))
}

func (h *handler) reviewAddress(w http.ResponseWriter, r *http.Request) {
	actor, ok := adminActor(r)
	if !ok {
		writeError(w, rwa.ErrAdminUnauthorized)
		return
	}
	var body struct {
		Approve    bool   `json:"approve"`
		ReasonCode string `json:"reason_code"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, err)
		return
	}
	value, err := h.service.ReviewExternalAddress(r.Context(), rwa.ReviewAddressCommand{AddressID: r.PathValue("address_id"), Approve: body.Approve, ReasonCode: body.ReasonCode, Actor: actor})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, addressDTO(value))
}

func (h *handler) forcedRedemption(w http.ResponseWriter, r *http.Request) {
	actor, ok := adminActor(r)
	if !ok {
		writeError(w, rwa.ErrAdminUnauthorized)
		return
	}
	var body struct {
		CustomerReference string `json:"customer_reference"`
		MintID            string `json:"mint_id"`
		ReasonCode        string `json:"reason_code"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, err)
		return
	}
	value, err := h.service.ForcedRedeem(r.Context(), rwa.ForcedRedeemCommand{CustomerReference: body.CustomerReference, IdempotencyKey: r.Header.Get("Idempotency-Key"), MintID: body.MintID, ReasonCode: body.ReasonCode, Actor: actor})
	if err != nil && !errors.Is(err, rwa.ErrOperationUnknown) {
		writeError(w, err)
		return
	}
	status := http.StatusCreated
	if errors.Is(err, rwa.ErrOperationUnknown) {
		status = http.StatusAccepted
	}
	writeJSON(w, status, redemptionDTO(value))
}

func (h *handler) recover(w http.ResponseWriter, r *http.Request) {
	actor, ok := adminActor(r)
	if !ok {
		writeError(w, rwa.ErrAdminUnauthorized)
		return
	}
	if err := h.service.RecoverOperation(r.Context(), rwa.RecoverCommand{OperationID: r.PathValue("operation_id"), Actor: actor}); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "RECOVERED_OR_REQUEUED"})
}

func (h *handler) reconcile(w http.ResponseWriter, r *http.Request) {
	actor, ok := adminActor(r)
	if !ok {
		writeError(w, rwa.ErrAdminUnauthorized)
		return
	}
	var body struct {
		AssetID string `json:"asset_id"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, err)
		return
	}
	value, err := h.service.Reconcile(r.Context(), rwa.ReconcileCommand{AssetID: body.AssetID, Actor: actor})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, reconciliationDTO(value))
}

func (h *handler) listReconciliation(w http.ResponseWriter, r *http.Request) {
	actor, ok := adminActor(r)
	if !ok {
		writeError(w, rwa.ErrAdminUnauthorized)
		return
	}
	items, err := h.service.ListReconciliations(r.Context(), actor, r.URL.Query().Get("asset_id"), pageSize(r))
	if err != nil {
		writeError(w, err)
		return
	}
	values := make([]any, 0, len(items))
	for _, item := range items {
		values = append(values, reconciliationDTO(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": values})
}

func (h *handler) announceDividend(w http.ResponseWriter, r *http.Request) {
	actor, ok := adminActor(r)
	if !ok {
		writeError(w, rwa.ErrAdminUnauthorized)
		return
	}
	var body struct {
		AssetID           string `json:"asset_id"`
		ExternalReference string `json:"external_reference"`
		USDPerShare       string `json:"usd_per_share"`
		RecordAt          string `json:"record_at"`
		PayableAt         string `json:"payable_at"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeError(w, err)
		return
	}
	amount, err := money.Parse(body.USDPerShare)
	if err != nil {
		writeError(w, rwa.ErrInvalidCommand)
		return
	}
	recordAt, err := time.Parse(time.RFC3339Nano, body.RecordAt)
	if err != nil {
		writeError(w, rwa.ErrInvalidCommand)
		return
	}
	payableAt, err := time.Parse(time.RFC3339Nano, body.PayableAt)
	if err != nil {
		writeError(w, rwa.ErrInvalidCommand)
		return
	}
	value, err := h.service.AnnounceDividend(r.Context(), rwa.AnnounceDividendCommand{AssetID: body.AssetID, ExternalReference: body.ExternalReference, USDPerShare: amount, RecordAt: recordAt, PayableAt: payableAt, Actor: actor})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, dividendDTO(value))
}

func (h *handler) processDividend(w http.ResponseWriter, r *http.Request) {
	actor, ok := adminActor(r)
	if !ok {
		writeError(w, rwa.ErrAdminUnauthorized)
		return
	}
	value, entitlements, err := h.service.ProcessDividend(r.Context(), rwa.ProcessDividendCommand{DividendID: r.PathValue("dividend_id"), Actor: actor})
	if err != nil {
		writeError(w, err)
		return
	}
	items := make([]any, 0, len(entitlements))
	for _, item := range entitlements {
		items = append(items, entitlementDTO(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"dividend": dividendDTO(value), "entitlements": items})
}

func (h *handler) capabilities(w http.ResponseWriter, r *http.Request) {
	if h.environment != config.EnvironmentLocal && h.environment != config.EnvironmentTest {
		writeError(w, provider.ErrProductionMode)
		return
	}
	value, err := h.service.ProviderCapabilities(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	if h.environment != config.EnvironmentLocal && h.environment != config.EnvironmentTest {
		writeError(w, provider.ErrProductionMode)
		return
	}
	value, err := h.service.ProviderHealth(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func bearerToken(r *http.Request) (string, bool) {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(value, "Bearer ") {
		return "", false
	}
	value = strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
	return value, value != ""
}
func adminActor(r *http.Request) (rwa.AdminActor, bool) {
	actor := rwa.AdminActor{ID: strings.TrimSpace(r.Header.Get("X-Admin-ID")), Role: strings.TrimSpace(r.Header.Get("X-Admin-Role"))}
	return actor, actor.ID != "" && actor.Role != ""
}
func pageSize(r *http.Request) int32 {
	value, err := strconv.ParseInt(r.URL.Query().Get("page_size"), 10, 32)
	if err != nil || value <= 0 {
		return 50
	}
	return int32(value)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maximumBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: invalid JSON body", rwa.ErrInvalidCommand)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: one JSON object is required", rwa.ErrInvalidCommand)
	}
	return nil
}

func (h *handler) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimRight(r.Header.Get("Origin"), "/")
		if h.allowedOrigin != "" && origin == h.allowedOrigin {
			w.Header().Set("Access-Control-Allow-Origin", h.allowedOrigin)
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Admin-ID, X-Admin-Role")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			if h.allowedOrigin == "" || origin != h.allowedOrigin {
				writeError(w, rwa.ErrUnauthorized)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
func writeError(w http.ResponseWriter, err error) {
	status, code, message, next := errorDetails(err)
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message, "next_action": next}})
}
func errorDetails(err error) (int, string, string, string) {
	switch {
	case errors.Is(err, rwa.ErrUnauthorized):
		return 401, "AUTHENTICATION_REQUIRED", "Authentication is required.", "Register the local fixture and retry."
	case errors.Is(err, rwa.ErrAdminUnauthorized):
		return 403, "ADMIN_PERMISSION_REQUIRED", "This admin role cannot perform the requested action.", "Use an authorized operations, risk, audit, or admin role."
	case errors.Is(err, provider.ErrProductionMode):
		return 403, "SIMULATOR_DISABLED", "The RWA simulator is unavailable in this environment.", "Use a local or test environment."
	case errors.Is(err, rwa.ErrAssetNotFound), errors.Is(err, rwa.ErrHoldingNotFound):
		return 404, "RWA_RESOURCE_NOT_FOUND", "The requested simulated RWA resource was not found.", "Refresh the data and select an available record."
	case errors.Is(err, rwa.ErrIdempotencyConflict):
		return 409, "IDEMPOTENCY_CONFLICT", "This request identifier was already used with different data.", "Retry unchanged or use a new identifier."
	case errors.Is(err, rwa.ErrAddressNotActive):
		return 409, "RWA_ADDRESS_NOT_ACTIVE", "The external address has not completed proof, review, and cooling.", "Complete the displayed verification and cooling steps, or use the platform Vault."
	case errors.Is(err, rwa.ErrInsufficientShares):
		return 422, "INSUFFICIENT_WHOLE_SETTLED_SHARES", "There are not enough eligible whole settled shares.", "Wait for settlement, reduce the whole-share quantity, or buy eligible shares."
	case errors.Is(err, rwa.ErrInvalidState):
		return 409, "INVALID_STATE", "This action is unavailable in the current RWA state.", "Refresh status and follow the displayed next action."
	case errors.Is(err, provider.ErrUnavailable), errors.Is(err, provider.ErrOutcomeUnknown):
		return 503, "RWA_CHAIN_UNAVAILABLE", "The local chain result is not yet known.", "Keep the underlying locked and retry recovery with the same operation ID."
	case errors.Is(err, rwa.ErrInvalidCommand), errors.Is(err, money.ErrInvalidDecimal):
		return 400, "INVALID_REQUEST", "The request is invalid.", "Review identifiers, whole-share decimal strings, and timestamps."
	default:
		return 500, "INTERNAL_ERROR", "The request could not be completed.", "Retry later or contact support with the request ID."
	}
}
