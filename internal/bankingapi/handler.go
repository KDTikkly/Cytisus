package bankingapi

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

	"github.com/KDTikkly/Cytisus/internal/banking"
	"github.com/KDTikkly/Cytisus/internal/banking/provider"
	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/money"
)

const maximumBodyBytes = 64 << 10

type Service interface {
	LinkBankAccount(context.Context, banking.LinkBankAccountCommand) (banking.BankAccount, error)
	ListBankAccounts(context.Context, string) ([]banking.BankAccount, error)
	VerifyBankAccountOwnership(context.Context, provider.Event) (banking.BankAccount, bool, error)
	InitiateFunding(context.Context, banking.InitiateFundingCommand) (banking.FundingTransfer, error)
	ListFundingTransfers(context.Context, string, int32) ([]banking.FundingTransfer, error)
	ApplyFundingEvent(context.Context, provider.Event) (banking.FundingTransfer, error)
	RequestWithdrawal(context.Context, banking.RequestWithdrawalCommand) (banking.Withdrawal, error)
	ListWithdrawals(context.Context, string, int32) ([]banking.Withdrawal, error)
	ApplyWithdrawalEvent(context.Context, provider.Event) (banking.Withdrawal, error)
	ListCases(context.Context, banking.AdminActor, string, int32) ([]banking.ComplianceCase, error)
	ProposeReview(context.Context, banking.ProposeReviewCommand) (banking.ReviewProposal, error)
	DecideReview(context.Context, banking.DecideReviewCommand) (banking.ReviewProposal, error)
	ReconcileCustomer(context.Context, banking.ReconcileCustomerCommand) (banking.ReconciliationRun, error)
}

type handler struct {
	service       Service
	environment   config.Environment
	allowedOrigin string
}

func NewUser(service Service, environment config.Environment, allowedOrigin string) http.Handler {
	h := &handler{service: service, environment: environment, allowedOrigin: strings.TrimRight(allowedOrigin, "/")}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/banks/accounts", h.linkBankAccount)
	mux.HandleFunc("GET /v1/banks/accounts", h.listBankAccounts)
	mux.HandleFunc("POST /v1/transfers/funding", h.initiateFunding)
	mux.HandleFunc("GET /v1/transfers/funding", h.listFunding)
	mux.HandleFunc("POST /v1/transfers/withdrawals", h.requestWithdrawal)
	mux.HandleFunc("GET /v1/transfers/withdrawals", h.listWithdrawals)
	return h.cors(mux)
}

func NewAdmin(service Service, environment config.Environment) http.Handler {
	h := &handler{service: service, environment: environment}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/v1/admin/compliance/cases", h.listCases)
	mux.HandleFunc("POST /internal/v1/admin/compliance/cases/{case_id}/proposals", h.proposeReview)
	mux.HandleFunc("POST /internal/v1/admin/compliance/proposals/{proposal_id}/decisions", h.decideReview)
	mux.HandleFunc("POST /internal/v1/admin/reconciliation/banks", h.reconcile)
	return mux
}

func NewSimulator(service Service, environment config.Environment) http.Handler {
	h := &handler{service: service, environment: environment}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/v1/simulators/bank/events", h.simulatorEvent)
	return mux
}

func (h *handler) linkBankAccount(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, banking.ErrUnauthorized)
		return
	}
	var body struct {
		ExternalAccountReference string `json:"external_account_reference"`
		RailSupport              string `json:"rail_support"`
		OwnerRelation            string `json:"owner_relation"`
		RiskClass                string `json:"risk_class"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	account, err := h.service.LinkBankAccount(request.Context(), banking.LinkBankAccountCommand{
		AccessToken: token, ExternalAccountReference: body.ExternalAccountReference,
		RailSupport: body.RailSupport, OwnerRelation: body.OwnerRelation, RiskClass: body.RiskClass,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, bankAccountDTO(account))
}

func (h *handler) listBankAccounts(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, banking.ErrUnauthorized)
		return
	}
	accounts, err := h.service.ListBankAccounts(request.Context(), token)
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]any, 0, len(accounts))
	for _, account := range accounts {
		items = append(items, bankAccountDTO(account))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "mode": "SIMULATED"})
}

func (h *handler) initiateFunding(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, banking.ErrUnauthorized)
		return
	}
	var body struct {
		BankAccountID string `json:"bank_account_id"`
		Rail          string `json:"rail"`
		Amount        string `json:"amount"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	amount, err := money.Parse(body.Amount)
	if err != nil {
		writeError(response, banking.ErrInvalidCommand)
		return
	}
	transfer, err := h.service.InitiateFunding(request.Context(), banking.InitiateFundingCommand{
		AccessToken: token, IdempotencyKey: request.Header.Get("Idempotency-Key"),
		BankAccountID: body.BankAccountID, Rail: body.Rail, Amount: amount,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, fundingDTO(transfer))
}

func (h *handler) listFunding(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, banking.ErrUnauthorized)
		return
	}
	transfers, err := h.service.ListFundingTransfers(request.Context(), token, pageSize(request))
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]any, 0, len(transfers))
	for _, transfer := range transfers {
		items = append(items, fundingDTO(transfer))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "mode": "SIMULATED"})
}

func (h *handler) requestWithdrawal(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, banking.ErrUnauthorized)
		return
	}
	var body struct {
		BankAccountID string `json:"bank_account_id"`
		Amount        string `json:"amount"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	amount, err := money.Parse(body.Amount)
	if err != nil {
		writeError(response, banking.ErrInvalidCommand)
		return
	}
	withdrawal, err := h.service.RequestWithdrawal(request.Context(), banking.RequestWithdrawalCommand{
		AccessToken: token, IdempotencyKey: request.Header.Get("Idempotency-Key"), BankAccountID: body.BankAccountID, Amount: amount,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, withdrawalDTO(withdrawal))
}

func (h *handler) listWithdrawals(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, banking.ErrUnauthorized)
		return
	}
	withdrawals, err := h.service.ListWithdrawals(request.Context(), token, pageSize(request))
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]any, 0, len(withdrawals))
	for _, withdrawal := range withdrawals {
		items = append(items, withdrawalDTO(withdrawal))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "mode": "SIMULATED"})
}

func (h *handler) simulatorEvent(response http.ResponseWriter, request *http.Request) {
	if h.environment != config.EnvironmentLocal && h.environment != config.EnvironmentTest {
		writeError(response, banking.ErrAdminUnauthorized)
		return
	}
	var body struct {
		Mode            string `json:"mode"`
		ExternalEventID string `json:"external_event_id"`
		ResourceType    string `json:"resource_type"`
		ResourceID      string `json:"resource_id"`
		EventType       string `json:"event_type"`
		ReasonCode      string `json:"reason_code"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	if body.Mode != "SIMULATED" {
		writeError(response, banking.ErrInvalidCommand)
		return
	}
	payload, _ := json.Marshal(body)
	event := provider.Event{
		Provider: "local-bank-simulator", ExternalEventID: body.ExternalEventID, ResourceType: body.ResourceType,
		ResourceID: body.ResourceID, EventType: body.EventType, ReasonCode: body.ReasonCode, Payload: payload, OccurredAt: time.Now().UTC(),
	}
	switch body.ResourceType {
	case "BANK_ACCOUNT":
		account, replayed, err := h.service.VerifyBankAccountOwnership(request.Context(), event)
		if err != nil {
			writeError(response, err)
			return
		}
		result := bankAccountDTO(account)
		result["replayed"] = replayed
		writeJSON(response, http.StatusOK, result)
	case "FUNDING":
		transfer, err := h.service.ApplyFundingEvent(request.Context(), event)
		if err != nil {
			writeError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, fundingDTO(transfer))
	case "WITHDRAWAL":
		withdrawal, err := h.service.ApplyWithdrawalEvent(request.Context(), event)
		if err != nil {
			writeError(response, err)
			return
		}
		writeJSON(response, http.StatusOK, withdrawalDTO(withdrawal))
	default:
		writeError(response, banking.ErrInvalidCommand)
	}
}

func (h *handler) listCases(response http.ResponseWriter, request *http.Request) {
	actor, ok := adminActor(request)
	if !ok {
		writeError(response, banking.ErrAdminUnauthorized)
		return
	}
	cases, err := h.service.ListCases(request.Context(), actor, request.URL.Query().Get("status"), pageSize(request))
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]any, 0, len(cases))
	for _, value := range cases {
		items = append(items, caseDTO(value))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (h *handler) proposeReview(response http.ResponseWriter, request *http.Request) {
	actor, ok := adminActor(request)
	if !ok {
		writeError(response, banking.ErrAdminUnauthorized)
		return
	}
	var body struct {
		Action            string `json:"action"`
		ReasonCode        string `json:"reason_code"`
		TicketReference   string `json:"ticket_reference"`
		EvidenceReference string `json:"evidence_reference"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	proposal, err := h.service.ProposeReview(request.Context(), banking.ProposeReviewCommand{
		Actor: actor, CaseID: request.PathValue("case_id"), Action: body.Action, ReasonCode: body.ReasonCode,
		TicketReference: body.TicketReference, EvidenceReference: body.EvidenceReference,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, proposalDTO(proposal))
}

func (h *handler) decideReview(response http.ResponseWriter, request *http.Request) {
	actor, ok := adminActor(request)
	if !ok {
		writeError(response, banking.ErrAdminUnauthorized)
		return
	}
	var body struct {
		Approve        bool   `json:"approve"`
		DecisionReason string `json:"decision_reason"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	proposal, err := h.service.DecideReview(request.Context(), banking.DecideReviewCommand{
		Actor: actor, ProposalID: request.PathValue("proposal_id"), Approve: body.Approve, DecisionReason: body.DecisionReason,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, proposalDTO(proposal))
}

func (h *handler) reconcile(response http.ResponseWriter, request *http.Request) {
	actor, ok := adminActor(request)
	if !ok {
		writeError(response, banking.ErrAdminUnauthorized)
		return
	}
	var body struct {
		CustomerReference string  `json:"customer_reference"`
		ProviderAmount    *string `json:"provider_amount"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	var providerAmount *money.Decimal
	if body.ProviderAmount != nil {
		parsed, err := money.Parse(*body.ProviderAmount)
		if err != nil {
			writeError(response, banking.ErrInvalidCommand)
			return
		}
		providerAmount = &parsed
	}
	run, err := h.service.ReconcileCustomer(request.Context(), banking.ReconcileCustomerCommand{
		Actor: actor, CustomerReference: body.CustomerReference, ProviderAmount: providerAmount,
	})
	if err != nil && !errors.Is(err, banking.ErrReconciliationDifference) {
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

func adminActor(request *http.Request) (banking.AdminActor, bool) {
	actor := banking.AdminActor{ID: strings.TrimSpace(request.Header.Get("X-Admin-ID")), Role: strings.TrimSpace(request.Header.Get("X-Admin-Role"))}
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
		return fmt.Errorf("%w: invalid JSON body", banking.ErrInvalidCommand)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: body must contain one JSON object", banking.ErrInvalidCommand)
	}
	return nil
}

func (h *handler) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		origin := strings.TrimRight(request.Header.Get("Origin"), "/")
		if h.allowedOrigin != "" && origin == h.allowedOrigin {
			response.Header().Set("Access-Control-Allow-Origin", h.allowedOrigin)
			response.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key")
			response.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			response.Header().Set("Vary", "Origin")
		}
		if request.Method == http.MethodOptions {
			if origin != h.allowedOrigin {
				writeError(response, banking.ErrUnauthorized)
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
	writeJSON(response, status, map[string]any{"error": map[string]string{
		"code": code, "message": message, "next_action": nextAction,
	}})
}

func errorDetails(err error) (int, string, string, string) {
	switch {
	case errors.Is(err, banking.ErrUnauthorized):
		return http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required.", "Register the local fixture and try again."
	case errors.Is(err, banking.ErrAdminUnauthorized):
		return http.StatusForbidden, "ADMIN_PERMISSION_REQUIRED", "This admin role cannot perform the requested action.", "Use an authorized role or ask an administrator for access."
	case errors.Is(err, banking.ErrThirdPartyDisabled):
		return http.StatusUnprocessableEntity, "THIRD_PARTY_WITHDRAWAL_DISABLED", "MVP withdrawals cannot be sent to a third-party bank account.", "Choose a verified bank account held in your own name."
	case errors.Is(err, banking.ErrOwnershipVerification):
		return http.StatusUnprocessableEntity, "OWNERSHIP_VERIFICATION_REQUIRED", "Bank ownership verification is required before this action.", "Complete ownership verification and try again."
	case errors.Is(err, banking.ErrCoolingOff):
		return http.StatusConflict, "COOLING_OFF_ACTIVE", "The security cooling period is still active.", "Wait until the displayed time or request an independently approved review."
	case errors.Is(err, banking.ErrEnhancedReview):
		return http.StatusConflict, "ENHANCED_REVIEW_REQUIRED", "Enhanced review is required for this bank account.", "Monitor the case center and provide any requested information."
	case errors.Is(err, banking.ErrInsufficientWithdrawable):
		return http.StatusUnprocessableEntity, "INSUFFICIENT_WITHDRAWABLE_CASH", "Only settled and withdrawable USD can be withdrawn.", "Lower the amount or wait for eligible funds to settle."
	case errors.Is(err, banking.ErrIdempotencyConflict), errors.Is(err, banking.ErrProviderEventConflict):
		return http.StatusConflict, "IDEMPOTENCY_CONFLICT", "This request identifier was already used with different data.", "Retry the original request unchanged or use a new identifier."
	case errors.Is(err, banking.ErrBankAccountNotFound), errors.Is(err, banking.ErrTransferNotFound), errors.Is(err, banking.ErrWithdrawalNotFound), errors.Is(err, banking.ErrCaseNotFound), errors.Is(err, banking.ErrReviewNotFound):
		return http.StatusNotFound, "RESOURCE_NOT_FOUND", "The requested resource was not found.", "Refresh the page and select an available record."
	case errors.Is(err, banking.ErrMakerCheckerConflict):
		return http.StatusConflict, "INDEPENDENT_APPROVAL_REQUIRED", "The person who proposed this action cannot approve it.", "Ask a different authorized administrator to review the proposal."
	case errors.Is(err, banking.ErrInvalidState):
		return http.StatusConflict, "INVALID_STATE", "This action is not available in the current state.", "Refresh the status and follow the displayed next action."
	case errors.Is(err, provider.ErrTimeout):
		return http.StatusGatewayTimeout, "PROVIDER_TIMEOUT", "The simulated bank provider did not respond in time.", "Retry with the same idempotency key."
	case errors.Is(err, banking.ErrInvalidCommand), errors.Is(err, money.ErrInvalidDecimal), errors.Is(err, money.ErrDecimalScale):
		return http.StatusBadRequest, "INVALID_REQUEST", "The request is invalid.", "Review the fields and try again."
	default:
		return http.StatusInternalServerError, "INTERNAL_ERROR", "The request could not be completed.", "Try again later or contact support with the request ID."
	}
}
