package cardapi

import (
	"net/http"

	"github.com/KDTikkly/Cytisus/internal/card"
)

func (h *handler) profile(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, card.ErrUnauthorized)
		return
	}
	value, err := h.service.Profile(request.Context(), token)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, profileDTO(value))
}

func (h *handler) createCard(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, card.ErrUnauthorized)
		return
	}
	var body struct {
		CardType string `json:"card_type"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	created, err := h.service.CreateCard(request.Context(), card.CreateCardCommand{
		AccessToken: token, IdempotencyKey: request.Header.Get("Idempotency-Key"), Type: card.CardType(body.CardType),
	})
	if err != nil {
		writeError(response, err)
		return
	}
	status := http.StatusCreated
	if created.Replayed {
		status = http.StatusOK
	}
	writeJSON(response, status, cardDTO(created))
}

func (h *handler) cardAction(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, card.ErrUnauthorized)
		return
	}
	var body struct {
		Action     string `json:"action"`
		ReasonCode string `json:"reason_code"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	updated, err := h.service.ActOnCard(request.Context(), card.CardActionCommand{
		AccessToken: token, IdempotencyKey: request.Header.Get("Idempotency-Key"), CardID: request.PathValue("card_id"),
		Action: body.Action, ReasonCode: body.ReasonCode,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, cardDTO(updated))
}

func (h *handler) spendingPower(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, card.ErrUnauthorized)
		return
	}
	value, err := h.service.SpendingPower(request.Context(), token)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, spendingPowerDTO(value))
}

func (h *handler) configureRepayment(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, card.ErrUnauthorized)
		return
	}
	var body struct {
		RepaymentMode string `json:"repayment_mode"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.ConfigureRepayment(request.Context(), card.ConfigureRepaymentCommand{
		AccessToken: token, IdempotencyKey: request.Header.Get("Idempotency-Key"), Mode: card.RepaymentMode(body.RepaymentMode),
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, profileDTO(value))
}

func (h *handler) getMandate(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, card.ErrUnauthorized)
		return
	}
	value, err := h.service.GetAutoSellMandate(request.Context(), token)
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, mandateDTO(value))
}

func (h *handler) configureMandate(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, card.ErrUnauthorized)
		return
	}
	var body struct {
		Enabled         bool   `json:"enabled"`
		AllowFractional bool   `json:"allow_fractional"`
		DailyMaxUSD     string `json:"daily_max_usd"`
		ValidUntil      string `json:"valid_until"`
		Assets          []struct {
			Priority              int16  `json:"priority"`
			Symbol                string `json:"symbol"`
			MinimumRetainQuantity string `json:"minimum_retain_quantity"`
		} `json:"assets"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	dailyMax, err := parseAmount(body.DailyMaxUSD)
	if err != nil {
		writeError(response, err)
		return
	}
	validUntil, err := parseOptionalTime(body.ValidUntil)
	if err != nil {
		writeError(response, err)
		return
	}
	assets := make([]card.AutoSellMandateAsset, 0, len(body.Assets))
	for _, item := range body.Assets {
		minimumRetain, parseErr := parseAmount(item.MinimumRetainQuantity)
		if parseErr != nil {
			writeError(response, parseErr)
			return
		}
		assets = append(assets, card.AutoSellMandateAsset{
			Priority: item.Priority, Symbol: item.Symbol, MinimumRetainQuantity: minimumRetain,
		})
	}
	value, err := h.service.ConfigureAutoSellMandate(request.Context(), card.ConfigureMandateCommand{
		AccessToken: token, IdempotencyKey: request.Header.Get("Idempotency-Key"), Enabled: body.Enabled,
		AllowFractional: body.AllowFractional, DailyMaxUSD: dailyMax, ValidUntil: validUntil, Assets: assets,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, mandateDTO(value))
}

func (h *handler) listAuthorizations(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, card.ErrUnauthorized)
		return
	}
	rows, err := h.service.ListAuthorizations(request.Context(), token, pageSize(request))
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, authorizationDTO(row))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "mode": "SIMULATED"})
}

func (h *handler) listCaptures(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, card.ErrUnauthorized)
		return
	}
	rows, err := h.service.ListCaptures(request.Context(), token, pageSize(request))
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, captureDTO(row))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items, "mode": "SIMULATED"})
}

func (h *handler) listDisputes(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, card.ErrUnauthorized)
		return
	}
	rows, err := h.service.ListDisputes(request.Context(), token, pageSize(request))
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, disputeDTO(row))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (h *handler) openDispute(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, card.ErrUnauthorized)
		return
	}
	var body struct {
		CaptureID  string `json:"capture_id"`
		AmountUSD  string `json:"amount_usd"`
		ReasonCode string `json:"reason_code"`
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
	value, err := h.service.OpenDispute(request.Context(), card.OpenDisputeCommand{
		AccessToken: token, IdempotencyKey: request.Header.Get("Idempotency-Key"), CaptureID: body.CaptureID,
		AmountUSD: amount, ReasonCode: body.ReasonCode,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, disputeDTO(value))
}

func (h *handler) listStatements(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, card.ErrUnauthorized)
		return
	}
	rows, err := h.service.ListStatements(request.Context(), token)
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, statementDTO(row))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func (h *handler) payStatement(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, card.ErrUnauthorized)
		return
	}
	value, err := h.service.PayStatement(request.Context(), card.PayStatementCommand{
		AccessToken: token, IdempotencyKey: request.Header.Get("Idempotency-Key"), StatementID: request.PathValue("statement_id"),
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, statementDTO(value))
}

func (h *handler) listNotifications(response http.ResponseWriter, request *http.Request) {
	token, ok := bearerToken(request)
	if !ok {
		writeError(response, card.ErrUnauthorized)
		return
	}
	rows, err := h.service.ListNotifications(request.Context(), token, pageSize(request))
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, notificationDTO(row))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}
