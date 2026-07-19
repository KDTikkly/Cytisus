package cardapi

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

	"github.com/KDTikkly/Cytisus/internal/card"
	cardprovider "github.com/KDTikkly/Cytisus/internal/card/provider"
	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/notification"
)

const maximumBodyBytes = 64 << 10

type Service interface {
	ProviderCapabilities(context.Context) cardprovider.Capabilities
	Profile(context.Context, string) (card.Profile, error)
	CreateCard(context.Context, card.CreateCardCommand) (card.Card, error)
	ActOnCard(context.Context, card.CardActionCommand) (card.Card, error)
	AdvancePhysicalCard(context.Context, card.AdvancePhysicalCardCommand) (card.Card, error)
	SpendingPower(context.Context, string) (card.SpendingPower, error)
	ConfigureRepayment(context.Context, card.ConfigureRepaymentCommand) (card.Profile, error)
	ConfigureAutoSellMandate(context.Context, card.ConfigureMandateCommand) (card.AutoSellMandate, error)
	GetAutoSellMandate(context.Context, string) (card.AutoSellMandate, error)
	SeedCollateral(context.Context, card.SeedCollateralCommand) (card.CollateralDriver, error)
	UpdateCollateralQuote(context.Context, card.UpdateCollateralQuoteCommand) (card.CollateralDriver, error)
	Authorize(context.Context, card.AuthorizeCommand) (card.Authorization, error)
	ListAuthorizations(context.Context, string, int32) ([]card.Authorization, error)
	Capture(context.Context, card.CaptureCommand) (card.Capture, error)
	ListCaptures(context.Context, string, int32) ([]card.Capture, error)
	ReverseAuthorization(context.Context, card.ReversalCommand) (card.Authorization, error)
	Refund(context.Context, card.RefundCommand) (card.Refund, error)
	OpenDispute(context.Context, card.OpenDisputeCommand) (card.Dispute, error)
	ResolveDispute(context.Context, card.ResolveDisputeCommand) (card.Dispute, error)
	ListDisputes(context.Context, string, int32) ([]card.Dispute, error)
	ListAdminDisputes(context.Context, card.AdminActor, int32) ([]card.Dispute, error)
	GenerateStatement(context.Context, card.GenerateStatementCommand) (card.Statement, error)
	PayStatement(context.Context, card.PayStatementCommand) (card.Statement, error)
	ListStatements(context.Context, string) ([]card.Statement, error)
	Reconcile(context.Context, card.ReconcileCommand) (card.ReconciliationRun, error)
	ListReconciliationRuns(context.Context, card.AdminActor, string, int32) ([]card.ReconciliationRun, error)
	ListNotifications(context.Context, string, int32) ([]notification.InboxItem, error)
}

type handler struct {
	service       Service
	environment   config.Environment
	allowedOrigin string
}

func NewUser(service Service, environment config.Environment, allowedOrigin string) http.Handler {
	h := &handler{service: service, environment: environment, allowedOrigin: strings.TrimRight(allowedOrigin, "/")}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/card", h.profile)
	mux.HandleFunc("POST /v1/card/cards", h.createCard)
	mux.HandleFunc("POST /v1/card/cards/{card_id}/actions", h.cardAction)
	mux.HandleFunc("GET /v1/card/spending-power", h.spendingPower)
	mux.HandleFunc("POST /v1/card/repayment-mode", h.configureRepayment)
	mux.HandleFunc("GET /v1/card/auto-sell-mandate", h.getMandate)
	mux.HandleFunc("PUT /v1/card/auto-sell-mandate", h.configureMandate)
	mux.HandleFunc("GET /v1/card/authorizations", h.listAuthorizations)
	mux.HandleFunc("GET /v1/card/captures", h.listCaptures)
	mux.HandleFunc("GET /v1/card/disputes", h.listDisputes)
	mux.HandleFunc("POST /v1/card/disputes", h.openDispute)
	mux.HandleFunc("GET /v1/card/statements", h.listStatements)
	mux.HandleFunc("POST /v1/card/statements/{statement_id}/pay", h.payStatement)
	mux.HandleFunc("GET /v1/card/notifications", h.listNotifications)
	return h.cors(mux)
}

func NewSimulator(service Service, environment config.Environment) http.Handler {
	h := &handler{service: service, environment: environment}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/v1/simulators/card/capabilities", h.capabilities)
	mux.HandleFunc("POST /internal/v1/simulators/card/collateral", h.seedCollateral)
	mux.HandleFunc("POST /internal/v1/simulators/card/quotes", h.updateQuote)
	mux.HandleFunc("POST /internal/v1/simulators/card/terminal/authorizations", h.authorize)
	mux.HandleFunc("POST /internal/v1/simulators/card/terminal/captures", h.capture)
	mux.HandleFunc("POST /internal/v1/simulators/card/terminal/reversals", h.reverse)
	mux.HandleFunc("POST /internal/v1/simulators/card/terminal/refunds", h.refund)
	return mux
}

func NewAdmin(service Service, environment config.Environment, allowedOrigin string) http.Handler {
	h := &handler{service: service, environment: environment, allowedOrigin: strings.TrimRight(allowedOrigin, "/")}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /internal/v1/admin/card/disputes", h.listAdminDisputes)
	mux.HandleFunc("POST /internal/v1/admin/card/disputes/{dispute_id}/resolve", h.resolveDispute)
	mux.HandleFunc("POST /internal/v1/admin/card/cards/{card_id}/lifecycle", h.advancePhysicalCard)
	mux.HandleFunc("POST /internal/v1/admin/card/statements", h.generateStatement)
	mux.HandleFunc("POST /internal/v1/admin/card/reconciliation", h.reconcile)
	mux.HandleFunc("GET /internal/v1/admin/card/reconciliation", h.listReconciliation)
	return h.cors(mux)
}

func bearerToken(request *http.Request) (string, bool) {
	value := strings.TrimSpace(request.Header.Get("Authorization"))
	if !strings.HasPrefix(value, "Bearer ") || len(value) <= len("Bearer ") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(value, "Bearer ")), true
}

func adminActor(request *http.Request) (card.AdminActor, bool) {
	actor := card.AdminActor{ID: strings.TrimSpace(request.Header.Get("X-Admin-ID")), Role: strings.TrimSpace(request.Header.Get("X-Admin-Role"))}
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
		return fmt.Errorf("%w: invalid JSON body", card.ErrInvalidCommand)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: body must contain one JSON object", card.ErrInvalidCommand)
	}
	return nil
}

func parseAmount(value string) (money.Decimal, error) {
	parsed, err := money.Parse(value)
	if err != nil {
		return money.Decimal{}, card.ErrInvalidCommand
	}
	return parsed, nil
}

func parseOptionalTime(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, card.ErrInvalidCommand
	}
	return parsed.UTC(), nil
}

func (h *handler) requireSimulator() error {
	if h.environment != config.EnvironmentLocal && h.environment != config.EnvironmentTest {
		return card.ErrSimulatorDisabled
	}
	return nil
}

func (h *handler) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		origin := strings.TrimRight(request.Header.Get("Origin"), "/")
		if h.allowedOrigin != "" && origin == h.allowedOrigin {
			response.Header().Set("Access-Control-Allow-Origin", h.allowedOrigin)
			response.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Admin-ID, X-Admin-Role")
			response.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
			response.Header().Set("Vary", "Origin")
		}
		if request.Method == http.MethodOptions {
			if h.allowedOrigin == "" || origin != h.allowedOrigin {
				writeError(response, card.ErrUnauthorized)
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
	case errors.Is(err, card.ErrUnauthorized):
		return http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required.", "Register the local fixture and try again."
	case errors.Is(err, card.ErrAdminUnauthorized):
		return http.StatusForbidden, "ADMIN_PERMISSION_REQUIRED", "This admin role cannot perform the requested action.", "Use an authorized role or ask an administrator for access."
	case errors.Is(err, card.ErrSimulatorDisabled):
		return http.StatusForbidden, "SIMULATOR_DISABLED", "The Card simulator is unavailable in this environment.", "Use a local or test environment."
	case errors.Is(err, card.ErrCardNotFound), errors.Is(err, card.ErrAuthorizationNotFound), errors.Is(err, card.ErrCaptureNotFound), errors.Is(err, card.ErrDisputeNotFound):
		return http.StatusNotFound, "RESOURCE_NOT_FOUND", "The requested Card resource was not found.", "Refresh the page and select an available record."
	case errors.Is(err, card.ErrIdempotencyConflict), errors.Is(err, card.ErrProviderEventConflict):
		return http.StatusConflict, "IDEMPOTENCY_CONFLICT", "This request identifier was already used with different data.", "Retry the original request unchanged or use a new identifier."
	case errors.Is(err, card.ErrInsufficientPower):
		return http.StatusUnprocessableEntity, "INSUFFICIENT_CARD_SPENDING_POWER", "Eligible settled cash and risk-adjusted collateral do not cover this action.", "Lower the amount, settle eligible cash, or review excluded collateral drivers."
	case errors.Is(err, card.ErrFXUnavailable):
		return http.StatusServiceUnavailable, "SIMULATED_FX_UNAVAILABLE", "A usable simulated FX quote is unavailable.", "Retry when the quote status is SIMULATED."
	case errors.Is(err, card.ErrTipExceeded):
		return http.StatusUnprocessableEntity, "TIP_TOLERANCE_EXCEEDED", "The cumulative tip exceeds the configured Card tolerance.", "Lower the capture amount or review the merchant total."
	case errors.Is(err, card.ErrRefundExceeded):
		return http.StatusUnprocessableEntity, "ADJUSTMENT_EXCEEDS_CAPTURE", "The refund or dispute exceeds the remaining captured amount.", "Lower the amount after reviewing prior refunds and accepted disputes."
	case errors.Is(err, card.ErrMandateRequired):
		return http.StatusConflict, "AUTO_SELL_MANDATE_REQUIRED", "An active pre-authorized Auto-Sell mandate is required.", "Configure eligible assets, priorities, limits, and validity before retrying."
	case errors.Is(err, card.ErrProtectedSellFailed):
		return http.StatusUnprocessableEntity, "PROTECTED_AUTO_SELL_NOT_FILLED", "The protected marketable limit did not fill enough quantity.", "Add eligible cash or retry when a protected limit is executable; no market fallback will occur."
	case errors.Is(err, cardprovider.ErrTimeout):
		return http.StatusGatewayTimeout, "CARD_PROVIDER_TIMEOUT", "The simulated Card provider did not respond in time.", "Retry with the same provider event identifier."
	case errors.Is(err, card.ErrInvalidState):
		return http.StatusConflict, "INVALID_STATE", "This action is not available in the current Card state.", "Refresh the status and follow the displayed next action."
	case errors.Is(err, card.ErrInvalidCommand), errors.Is(err, money.ErrInvalidDecimal), errors.Is(err, money.ErrDecimalScale):
		return http.StatusBadRequest, "INVALID_REQUEST", "The request is invalid.", "Review identifiers, decimal strings, dates, and simulator fields."
	default:
		return http.StatusInternalServerError, "INTERNAL_ERROR", "The request could not be completed.", "Try again later or contact support with the request ID."
	}
}
