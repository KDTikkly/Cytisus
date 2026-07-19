package cardapi

import (
	"net/http"

	"github.com/KDTikkly/Cytisus/internal/card"
	cardprovider "github.com/KDTikkly/Cytisus/internal/card/provider"
)

func (h *handler) capabilities(response http.ResponseWriter, request *http.Request) {
	if err := h.requireSimulator(); err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, capabilitiesDTO(h.service.ProviderCapabilities(request.Context())))
}

func (h *handler) seedCollateral(response http.ResponseWriter, request *http.Request) {
	if err := h.requireSimulator(); err != nil {
		writeError(response, err)
		return
	}
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, card.ErrUnauthorized)
		return
	}
	var body struct {
		Symbol   string `json:"symbol"`
		Quantity string `json:"quantity"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	quantity, err := parseAmount(body.Quantity)
	if err != nil {
		writeError(response, err)
		return
	}
	driver, err := h.service.SeedCollateral(request.Context(), card.SeedCollateralCommand{
		AccessToken: token, IdempotencyKey: request.Header.Get("Idempotency-Key"), Symbol: body.Symbol, Quantity: quantity,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, map[string]any{
		"symbol": driver.Symbol, "quantity": driver.Quantity.String(), "quote_status": driver.QuoteStatus,
		"market_status": driver.MarketStatus, "mode": "SIMULATED",
	})
}

func (h *handler) updateQuote(response http.ResponseWriter, request *http.Request) {
	if err := h.requireSimulator(); err != nil {
		writeError(response, err)
		return
	}
	var body struct {
		Symbol         string `json:"symbol"`
		ReferencePrice string `json:"reference_price_usd"`
		QuoteStatus    string `json:"quote_status"`
		MarketStatus   string `json:"market_status"`
		ObservedAt     string `json:"observed_at"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	price, err := parseAmount(body.ReferencePrice)
	if err != nil {
		writeError(response, err)
		return
	}
	observedAt, err := parseOptionalTime(body.ObservedAt)
	if err != nil {
		writeError(response, err)
		return
	}
	driver, err := h.service.UpdateCollateralQuote(request.Context(), card.UpdateCollateralQuoteCommand{
		Symbol: body.Symbol, ReferencePrice: price, QuoteStatus: body.QuoteStatus,
		MarketStatus: body.MarketStatus, ObservedAt: observedAt,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"status": "UPDATED", "symbol": driver.Symbol, "reference_price_usd": driver.ReferencePrice.String(),
		"quote_status": driver.QuoteStatus, "market_status": driver.MarketStatus, "observed_at": driver.ObservedAt.Format(timeFormat),
		"mode": "SIMULATED",
	})
}

func (h *handler) authorize(response http.ResponseWriter, request *http.Request) {
	if err := h.requireSimulator(); err != nil {
		writeError(response, err)
		return
	}
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, card.ErrUnauthorized)
		return
	}
	var body struct {
		ExternalEventID      string `json:"external_event_id"`
		CardID               string `json:"card_id"`
		MerchantName         string `json:"merchant_name"`
		MerchantCategoryCode string `json:"merchant_category_code"`
		MerchantAmount       string `json:"merchant_amount"`
		MerchantCurrency     string `json:"merchant_currency"`
		EntryMode            string `json:"entry_mode"`
		Offline              bool   `json:"offline"`
		OccurredAt           string `json:"occurred_at"`
		SimulationScenario   string `json:"simulation_scenario"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	amount, err := parseAmount(body.MerchantAmount)
	if err != nil {
		writeError(response, err)
		return
	}
	occurredAt, err := parseOptionalTime(body.OccurredAt)
	if err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.Authorize(request.Context(), card.AuthorizeCommand{
		AccessToken: token, ExternalEventID: body.ExternalEventID, CardID: body.CardID,
		MerchantName: body.MerchantName, MerchantCategoryCode: body.MerchantCategoryCode,
		MerchantAmount: amount, MerchantCurrency: body.MerchantCurrency, EntryMode: body.EntryMode,
		Offline: body.Offline, OccurredAt: occurredAt, SimulationScenario: cardprovider.Scenario(body.SimulationScenario),
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, authorizationDTO(value))
}

func (h *handler) capture(response http.ResponseWriter, request *http.Request) {
	if err := h.requireSimulator(); err != nil {
		writeError(response, err)
		return
	}
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, card.ErrUnauthorized)
		return
	}
	var body struct {
		ExternalEventID    string `json:"external_event_id"`
		AuthorizationID    string `json:"authorization_id"`
		MerchantAmount     string `json:"merchant_amount"`
		MerchantCurrency   string `json:"merchant_currency"`
		Final              bool   `json:"final"`
		OccurredAt         string `json:"occurred_at"`
		SimulationScenario string `json:"simulation_scenario"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	amount, err := parseAmount(body.MerchantAmount)
	if err != nil {
		writeError(response, err)
		return
	}
	occurredAt, err := parseOptionalTime(body.OccurredAt)
	if err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.Capture(request.Context(), card.CaptureCommand{
		AccessToken: token, ExternalEventID: body.ExternalEventID, AuthorizationID: body.AuthorizationID,
		MerchantAmount: amount, MerchantCurrency: body.MerchantCurrency, Final: body.Final,
		OccurredAt: occurredAt, SimulationScenario: cardprovider.Scenario(body.SimulationScenario),
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, captureDTO(value))
}

func (h *handler) reverse(response http.ResponseWriter, request *http.Request) {
	if err := h.requireSimulator(); err != nil {
		writeError(response, err)
		return
	}
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, card.ErrUnauthorized)
		return
	}
	var body struct {
		ExternalEventID string `json:"external_event_id"`
		AuthorizationID string `json:"authorization_id"`
		AmountUSD       string `json:"amount_usd"`
		OccurredAt      string `json:"occurred_at"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	amount, err := parseAmount(body.AmountUSD)
	if err != nil {
		writeError(response, err)
		return
	}
	occurredAt, err := parseOptionalTime(body.OccurredAt)
	if err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.ReverseAuthorization(request.Context(), card.ReversalCommand{
		AccessToken: token, ExternalEventID: body.ExternalEventID, AuthorizationID: body.AuthorizationID,
		AmountUSD: amount, OccurredAt: occurredAt,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, authorizationDTO(value))
}

func (h *handler) refund(response http.ResponseWriter, request *http.Request) {
	if err := h.requireSimulator(); err != nil {
		writeError(response, err)
		return
	}
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, card.ErrUnauthorized)
		return
	}
	var body struct {
		ExternalEventID string `json:"external_event_id"`
		CaptureID       string `json:"capture_id"`
		AmountUSD       string `json:"amount_usd"`
		OccurredAt      string `json:"occurred_at"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	amount, err := parseAmount(body.AmountUSD)
	if err != nil {
		writeError(response, err)
		return
	}
	occurredAt, err := parseOptionalTime(body.OccurredAt)
	if err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.Refund(request.Context(), card.RefundCommand{
		AccessToken: token, ExternalEventID: body.ExternalEventID, CaptureID: body.CaptureID,
		AmountUSD: amount, OccurredAt: occurredAt,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, refundDTO(value))
}
