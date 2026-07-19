package cardapi

import (
	"net/http"
	"time"

	"github.com/KDTikkly/Cytisus/internal/card"
	"github.com/KDTikkly/Cytisus/internal/money"
)

func (h *handler) listAdminDisputes(response http.ResponseWriter, request *http.Request) {
	actor, ok := adminActor(request)
	if !ok {
		writeError(response, card.ErrAdminUnauthorized)
		return
	}
	rows, err := h.service.ListAdminDisputes(request.Context(), actor, pageSize(request))
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

func (h *handler) resolveDispute(response http.ResponseWriter, request *http.Request) {
	actor, ok := adminActor(request)
	if !ok {
		writeError(response, card.ErrAdminUnauthorized)
		return
	}
	var body struct {
		Accept     bool   `json:"accept"`
		ReasonCode string `json:"reason_code"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.ResolveDispute(request.Context(), card.ResolveDisputeCommand{
		Actor: actor, DisputeID: request.PathValue("dispute_id"), Accept: body.Accept, ReasonCode: body.ReasonCode,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, disputeDTO(value))
}

func (h *handler) advancePhysicalCard(response http.ResponseWriter, request *http.Request) {
	actor, ok := adminActor(request)
	if !ok {
		writeError(response, card.ErrAdminUnauthorized)
		return
	}
	var body struct {
		NextStatus string `json:"next_status"`
		ReasonCode string `json:"reason_code"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.AdvancePhysicalCard(request.Context(), card.AdvancePhysicalCardCommand{
		Actor: actor, CardID: request.PathValue("card_id"), NextStatus: body.NextStatus, ReasonCode: body.ReasonCode,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, cardDTO(value))
}

func (h *handler) generateStatement(response http.ResponseWriter, request *http.Request) {
	actor, ok := adminActor(request)
	if !ok {
		writeError(response, card.ErrAdminUnauthorized)
		return
	}
	var body struct {
		CustomerReference string `json:"customer_reference"`
		PeriodStart       string `json:"period_start"`
		PeriodEnd         string `json:"period_end"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	periodStart, err := time.Parse("2006-01-02", body.PeriodStart)
	if err != nil {
		writeError(response, card.ErrInvalidCommand)
		return
	}
	periodEnd, err := time.Parse("2006-01-02", body.PeriodEnd)
	if err != nil {
		writeError(response, card.ErrInvalidCommand)
		return
	}
	value, err := h.service.GenerateStatement(request.Context(), card.GenerateStatementCommand{
		Actor: actor, CustomerReference: body.CustomerReference, PeriodStart: periodStart, PeriodEnd: periodEnd,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusCreated, statementDTO(value))
}

func (h *handler) reconcile(response http.ResponseWriter, request *http.Request) {
	actor, ok := adminActor(request)
	if !ok {
		writeError(response, card.ErrAdminUnauthorized)
		return
	}
	var body struct {
		CustomerReference     string  `json:"customer_reference"`
		ProviderHoldUSD       *string `json:"provider_hold_usd"`
		ProviderReceivableUSD *string `json:"provider_receivable_usd"`
	}
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	providerHold, err := optionalAmount(body.ProviderHoldUSD)
	if err != nil {
		writeError(response, err)
		return
	}
	providerReceivable, err := optionalAmount(body.ProviderReceivableUSD)
	if err != nil {
		writeError(response, err)
		return
	}
	value, err := h.service.Reconcile(request.Context(), card.ReconcileCommand{
		Actor: actor, CustomerReference: body.CustomerReference,
		ProviderHoldUSD: providerHold, ProviderReceivableUSD: providerReceivable,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeJSON(response, http.StatusOK, reconciliationDTO(value))
}

func (h *handler) listReconciliation(response http.ResponseWriter, request *http.Request) {
	actor, ok := adminActor(request)
	if !ok {
		writeError(response, card.ErrAdminUnauthorized)
		return
	}
	rows, err := h.service.ListReconciliationRuns(request.Context(), actor, request.URL.Query().Get("customer_reference"), pageSize(request))
	if err != nil {
		writeError(response, err)
		return
	}
	items := make([]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, reconciliationDTO(row))
	}
	writeJSON(response, http.StatusOK, map[string]any{"items": items})
}

func optionalAmount(value *string) (*money.Decimal, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := parseAmount(*value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
