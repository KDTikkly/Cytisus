package paperapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/KDTikkly/Cytisus/internal/marketdata"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/securities"
	"github.com/KDTikkly/Cytisus/internal/securities/broker"
)

const maximumBodyBytes = 64 << 10

type Service interface {
	RegisterFixture(context.Context, string) (securities.Registration, error)
	SearchInstruments(context.Context, string, int32) ([]marketdata.Instrument, error)
	Quote(context.Context, string, int32) (marketdata.Quote, error)
	SubmitOrder(context.Context, securities.SubmitOrderCommand) (securities.Order, error)
	AdvanceReplay(context.Context, securities.ActionCommand) (securities.Order, error)
	CancelOrder(context.Context, securities.ActionCommand) (securities.Order, error)
	ListOrders(context.Context, string, int32) ([]securities.Order, error)
	ListPositions(context.Context, string) ([]securities.Position, error)
	Portfolio(context.Context, string) (securities.Portfolio, error)
}

type handler struct {
	service       Service
	allowedOrigin string
}

func New(service Service, allowedOrigin string) http.Handler {
	h := &handler{service: service, allowedOrigin: strings.TrimRight(allowedOrigin, "/")}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/paper/registrations", h.register)
	mux.HandleFunc("GET /v1/instruments", h.searchInstruments)
	mux.HandleFunc("GET /v1/market-data/quotes/{symbol}", h.quote)
	mux.HandleFunc("POST /v1/orders", h.submitOrder)
	mux.HandleFunc("GET /v1/orders", h.listOrders)
	mux.HandleFunc("POST /v1/orders/{order_id}/replay", h.advanceReplay)
	mux.HandleFunc("POST /v1/orders/{order_id}/cancel", h.cancelOrder)
	mux.HandleFunc("GET /v1/positions", h.listPositions)
	mux.HandleFunc("GET /v1/portfolio", h.portfolio)
	return h.cors(mux)
}

func (h *handler) register(response http.ResponseWriter, request *http.Request) {
	var body struct {
		FixtureID string `json:"fixture_id"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	registration, err := h.service.RegisterFixture(request.Context(), body.FixtureID)
	if err != nil {
		writeError(response, err)
		return
	}
	status := http.StatusCreated
	if registration.Replayed {
		status = http.StatusOK
	}
	writeJSON(response, status, map[string]any{
		"account":      accountDTO(registration.Account),
		"access_token": registration.AccessToken,
		"mode":         "SIMULATED",
		"replayed":     registration.Replayed,
	})
}

func (h *handler) searchInstruments(response http.ResponseWriter, request *http.Request) {
	pageSize := parsePageSize(request.URL.Query().Get("page_size"))
	instruments, err := h.service.SearchInstruments(request.Context(), request.URL.Query().Get("q"), pageSize)
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]any, 0, len(instruments))
	for _, instrument := range instruments {
		items = append(items, instrumentDTO(instrument))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (h *handler) quote(response http.ResponseWriter, request *http.Request) {
	cursor := int32(0)
	if value := request.URL.Query().Get("replay_cursor"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil || parsed < 0 {
			writeError(response, fmt.Errorf("%w: replay_cursor", securities.ErrInvalidCommand))
			return
		}
		cursor = int32(parsed)
	}
	quote, err := h.service.Quote(request.Context(), request.PathValue("symbol"), cursor)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, quoteDTO(quote))
}

func (h *handler) submitOrder(response http.ResponseWriter, request *http.Request) {
	accessToken, ok := bearerToken(request)
	if !ok {
		writeError(response, securities.ErrUnauthorized)
		return
	}
	var body struct {
		Symbol      string  `json:"symbol"`
		Side        string  `json:"side"`
		OrderType   string  `json:"order_type"`
		TimeInForce string  `json:"time_in_force"`
		Quantity    string  `json:"quantity"`
		LimitPrice  *string `json:"limit_price"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	quantity, err := money.Parse(body.Quantity)
	if err != nil {
		writeError(response, fmt.Errorf("%w: quantity", securities.ErrInvalidCommand))
		return
	}
	var limitPrice *money.Decimal
	if body.LimitPrice != nil {
		parsed, parseErr := money.Parse(*body.LimitPrice)
		if parseErr != nil {
			writeError(response, fmt.Errorf("%w: limit_price", securities.ErrInvalidCommand))
			return
		}
		limitPrice = &parsed
	}
	order, err := h.service.SubmitOrder(request.Context(), securities.SubmitOrderCommand{
		AccessToken:    accessToken,
		IdempotencyKey: request.Header.Get("Idempotency-Key"),
		Symbol:         body.Symbol,
		Side:           broker.Side(body.Side),
		OrderType:      broker.OrderType(body.OrderType),
		TimeInForce:    broker.TimeInForce(body.TimeInForce),
		Quantity:       quantity,
		LimitPrice:     limitPrice,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, orderDTO(order))
}

func (h *handler) listOrders(response http.ResponseWriter, request *http.Request) {
	accessToken, ok := bearerToken(request)
	if !ok {
		writeError(response, securities.ErrUnauthorized)
		return
	}
	orders, err := h.service.ListOrders(request.Context(), accessToken, parsePageSize(request.URL.Query().Get("page_size")))
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]any, 0, len(orders))
	for _, order := range orders {
		items = append(items, orderDTO(order))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (h *handler) advanceReplay(response http.ResponseWriter, request *http.Request) {
	h.orderAction(response, request, "ADVANCE_REPLAY")
}

func (h *handler) cancelOrder(response http.ResponseWriter, request *http.Request) {
	h.orderAction(response, request, "CANCEL")
}

func (h *handler) orderAction(response http.ResponseWriter, request *http.Request, action string) {
	accessToken, ok := bearerToken(request)
	if !ok {
		writeError(response, securities.ErrUnauthorized)
		return
	}
	command := securities.ActionCommand{
		AccessToken:    accessToken,
		IdempotencyKey: request.Header.Get("Idempotency-Key"),
		OrderID:        request.PathValue("order_id"),
	}
	var order securities.Order
	var err error
	if action == "CANCEL" {
		order, err = h.service.CancelOrder(request.Context(), command)
	} else {
		order, err = h.service.AdvanceReplay(request.Context(), command)
	}
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, orderDTO(order))
}

func (h *handler) listPositions(response http.ResponseWriter, request *http.Request) {
	accessToken, ok := bearerToken(request)
	if !ok {
		writeError(response, securities.ErrUnauthorized)
		return
	}
	positions, err := h.service.ListPositions(request.Context(), accessToken)
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]any, 0, len(positions))
	for _, position := range positions {
		items = append(items, positionDTO(position))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (h *handler) portfolio(response http.ResponseWriter, request *http.Request) {
	accessToken, ok := bearerToken(request)
	if !ok {
		writeError(response, securities.ErrUnauthorized)
		return
	}
	portfolio, err := h.service.Portfolio(request.Context(), accessToken)
	if err != nil {
		writeError(response, err)
		return
	}
	positions := make([]any, 0, len(portfolio.Positions))
	for _, position := range portfolio.Positions {
		positions = append(positions, positionDTO(position))
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"cash": map[string]string{
			"settled":                  portfolio.Cash.Settled.String(),
			"withdrawable":             portfolio.Cash.Withdrawable.String(),
			"provisional_buying_power": portfolio.Cash.ProvisionalBuyingPower.String(),
			"total_buying_power":       portfolio.Cash.TotalBuyingPower.String(),
		},
		"positions": positions,
	})
}

func decodeJSON(response http.ResponseWriter, request *http.Request, destination any) error {
	request.Body = http.MaxBytesReader(response, request.Body, maximumBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("%w: invalid JSON body", securities.ErrInvalidCommand)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: body must contain one JSON object", securities.ErrInvalidCommand)
	}
	return nil
}

func bearerToken(request *http.Request) (string, bool) {
	value := strings.TrimSpace(request.Header.Get("Authorization"))
	prefix := "Bearer "
	if !strings.HasPrefix(value, prefix) || len(value) == len(prefix) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(value, prefix)), true
}

func parsePageSize(value string) int32 {
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil || parsed <= 0 {
		return 20
	}
	return int32(parsed)
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
				writeError(response, securities.ErrUnauthorized)
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
	status, code, message := errorDetails(err)
	writeJSON(response, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func errorDetails(err error) (int, string, string) {
	switch {
	case errors.Is(err, securities.ErrUnauthorized):
		return http.StatusUnauthorized, "AUTHENTICATION_REQUIRED", "Authentication is required. Register a paper fixture and try again."
	case errors.Is(err, securities.ErrFixtureDisabled):
		return http.StatusForbidden, "FIXTURE_DISABLED", "Paper registration fixtures are disabled in this environment."
	case errors.Is(err, securities.ErrIdempotencyConflict):
		return http.StatusConflict, "IDEMPOTENCY_CONFLICT", "This idempotency key was already used for a different request."
	case errors.Is(err, securities.ErrInstrumentViewOnly):
		return http.StatusUnprocessableEntity, "INSTRUMENT_VIEW_ONLY", "This instrument can be viewed but is not eligible for paper trading."
	case errors.Is(err, securities.ErrFractionalUnsupported):
		return http.StatusUnprocessableEntity, "FRACTIONAL_UNSUPPORTED", "This instrument does not accept fractional quantities."
	case errors.Is(err, securities.ErrQuoteStale):
		return http.StatusConflict, "QUOTE_STALE", "The quote is stale. Refresh market data before placing an order."
	case errors.Is(err, securities.ErrQuoteUnavailable):
		return http.StatusServiceUnavailable, "QUOTE_UNAVAILABLE", "A simulated quote is currently unavailable."
	case errors.Is(err, securities.ErrInsufficientCash):
		return http.StatusUnprocessableEntity, "INSUFFICIENT_BUYING_POWER", "The paper account does not have enough buying power."
	case errors.Is(err, securities.ErrInsufficientPosition):
		return http.StatusUnprocessableEntity, "INSUFFICIENT_POSITION", "The paper account does not hold enough of this instrument."
	case errors.Is(err, securities.ErrOrderNotFound):
		return http.StatusNotFound, "ORDER_NOT_FOUND", "The order was not found."
	case errors.Is(err, securities.ErrInvalidOrderState):
		return http.StatusConflict, "INVALID_ORDER_STATE", "The order can no longer perform this action."
	case errors.Is(err, securities.ErrReplayExhausted):
		return http.StatusConflict, "REPLAY_EXHAUSTED", "No additional deterministic replay step is available."
	case errors.Is(err, broker.ErrProviderTimeout):
		return http.StatusGatewayTimeout, "PROVIDER_TIMEOUT", "The paper broker timed out. Retry with the same idempotency key."
	case errors.Is(err, marketdata.ErrInstrumentNotFound):
		return http.StatusNotFound, "INSTRUMENT_NOT_FOUND", "The instrument was not found."
	case errors.Is(err, securities.ErrInvalidCommand), errors.Is(err, money.ErrInvalidDecimal), errors.Is(err, money.ErrDecimalScale):
		return http.StatusBadRequest, "INVALID_REQUEST", "The request is invalid. Review the fields and try again."
	default:
		return http.StatusInternalServerError, "INTERNAL_ERROR", "The request could not be completed."
	}
}
