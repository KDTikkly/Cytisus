package card

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	cardprovider "github.com/KDTikkly/Cytisus/internal/card/provider"
	"github.com/KDTikkly/Cytisus/internal/card/store"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/notification"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (service *Service) Authorize(ctx context.Context, command AuthorizeCommand) (Authorization, error) {
	if !service.localSimulationAllowed() {
		return Authorization{}, ErrSimulatorDisabled
	}
	command.MerchantCurrency = strings.ToUpper(strings.TrimSpace(command.MerchantCurrency))
	command.MerchantName = strings.TrimSpace(command.MerchantName)
	command.EntryMode = strings.ToUpper(strings.TrimSpace(command.EntryMode))
	if command.EntryMode == "" {
		command.EntryMode = "NFC_SIMULATOR"
	}
	if command.Offline {
		command.EntryMode = "OFFLINE_SIMULATOR"
	}
	if command.ExternalEventID == "" || command.CardID == "" || command.MerchantName == "" ||
		len(command.MerchantCategoryCode) != 4 || !command.MerchantAmount.IsPositive() || len(command.MerchantCurrency) != 3 ||
		!validSimulatorEntryMode(command.EntryMode) {
		return Authorization{}, ErrInvalidCommand
	}
	if command.OccurredAt.IsZero() {
		command.OccurredAt = service.now().UTC()
	}
	_, profile, err := service.resolveCustomer(ctx, command.AccessToken)
	if err != nil {
		return Authorization{}, err
	}
	cardID, err := parseUUID(command.CardID)
	if err != nil {
		return Authorization{}, ErrCardNotFound
	}
	payloadHash, _ := hashValue(struct {
		CardID, MerchantName, MCC, Amount, Currency, EntryMode string
		Offline                                                bool
		OccurredAt                                             string
		Scenario                                               cardprovider.Scenario
	}{command.CardID, command.MerchantName, command.MerchantCategoryCode, command.MerchantAmount.String(), command.MerchantCurrency, command.EntryMode, command.Offline, command.OccurredAt.UTC().Format(time.RFC3339Nano), command.SimulationScenario})
	authorizationID, err := newUUID()
	if err != nil {
		return Authorization{}, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Authorization{}, err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	locked, err := queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference)
	if err != nil {
		return Authorization{}, err
	}
	inserted, err := queries.InsertProviderEvent(ctx, store.InsertProviderEventParams{
		Provider: service.provider.Name(), ExternalEventID: command.ExternalEventID, EventType: "AUTHORIZATION",
		PayloadHash: payloadHash, ResourceType: "card.authorization", ResourceID: authorizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existingEvent, getErr := queries.GetProviderEvent(ctx, store.GetProviderEventParams{Provider: service.provider.Name(), ExternalEventID: command.ExternalEventID})
		if getErr != nil || existingEvent.PayloadHash != payloadHash || existingEvent.EventType != "AUTHORIZATION" {
			return Authorization{}, ErrProviderEventConflict
		}
		existing, getErr := queries.GetAuthorization(ctx, existingEvent.ResourceID)
		if getErr != nil {
			return Authorization{}, getErr
		}
		if err := tx.Commit(ctx); err != nil {
			return Authorization{}, err
		}
		return authorizationFromStore(existing, true), nil
	}
	if err != nil || inserted != authorizationID {
		return Authorization{}, err
	}
	cardRow, err := queries.GetCustomerCardForUpdate(ctx, store.GetCustomerCardForUpdateParams{ID: cardID, CustomerReference: profile.CustomerReference})
	if errors.Is(err, pgx.ErrNoRows) {
		return Authorization{}, ErrCardNotFound
	}
	if err != nil {
		return Authorization{}, err
	}
	policy, err := queries.GetPolicy(ctx, locked.PolicyVersion)
	if err != nil {
		return Authorization{}, err
	}
	providerCtx, cancel := service.withProviderTimeout(ctx)
	defer cancel()
	fx, err := service.provider.FX(providerCtx, command.MerchantCurrency, command.SimulationScenario)
	if err != nil {
		return Authorization{}, fmt.Errorf("get authorization FX: %w", err)
	}
	if fx.Status != "SIMULATED" {
		return Authorization{}, ErrFXUnavailable
	}
	baseUSD, err := command.MerchantAmount.Multiply(fx.USDPerUnit, amountPolicy)
	if err != nil {
		return Authorization{}, err
	}
	markupMultiplier, err := money.MustParse("1").Add(policy.AuthFxMarkupRate)
	if err != nil {
		return Authorization{}, err
	}
	authorizedUSD, err := baseUSD.Multiply(markupMultiplier, amountPolicy)
	if err != nil {
		return Authorization{}, err
	}
	status := "APPROVED"
	declineCode := ""
	if command.SimulationScenario == cardprovider.ScenarioDecline {
		status, declineCode = "DECLINED", "SIMULATED_DECLINE"
	} else if cardRow.Status != "ACTIVE" || locked.SpendingStatus != "ACTIVE" {
		status, declineCode = "DECLINED", "CARD_NOT_AVAILABLE"
	} else {
		power, powerErr := service.calculateSpendingPower(ctx, tx, locked)
		if powerErr != nil {
			return Authorization{}, powerErr
		}
		if power.AvailableUSD.Compare(authorizedUSD) < 0 {
			status, declineCode = "DECLINED", "INSUFFICIENT_SPENDING_POWER"
		}
	}
	holdID := pgtype.UUID{}
	if status == "APPROVED" {
		posting, postErr := service.postAuthorizationHold(ctx, tx, locked, authorizationID.String(), authorizedUSD)
		if postErr != nil {
			return Authorization{}, postErr
		}
		holdID = uuidValue(posting.TransactionID)
	}
	created, err := queries.CreateAuthorization(ctx, store.CreateAuthorizationParams{
		ID: authorizationID, CustomerReference: profile.CustomerReference, CardID: cardID,
		Provider: service.provider.Name(), ExternalAuthorizationID: command.ExternalEventID,
		MerchantName: command.MerchantName, MerchantCategoryCode: command.MerchantCategoryCode,
		MerchantAmount: command.MerchantAmount, MerchantCurrency: command.MerchantCurrency,
		AuthorizationFxRate: fx.USDPerUnit, FxMarkupRate: policy.AuthFxMarkupRate, AuthorizedUsd: authorizedUSD,
		Status: status, Offline: command.Offline, EntryMode: command.EntryMode, OccurredAt: timestamptz(command.OccurredAt),
		DeclineCode: textValue(declineCode), HoldLedgerTransactionID: holdID, PolicyVersion: locked.PolicyVersion,
	})
	if err != nil {
		return Authorization{}, fmt.Errorf("create card authorization: %w", err)
	}
	payload, _ := json.Marshal(map[string]string{"authorization_id": authorizationID.String(), "status": status, "authorized_usd": authorizedUSD.String()})
	if err := recordMutation(ctx, tx, mutation{
		Action: "card.authorization." + strings.ToLower(status), ResourceType: "card.authorization", ResourceID: authorizationID.String(),
		ActorType: "PROVIDER", ActorID: service.provider.Name(), CorrelationID: authorizationID, Metadata: payload,
		AggregateType: "card.authorization", AggregateID: authorizationID.String(),
		EventType: "card.authorization." + strings.ToLower(status), Payload: payload,
	}); err != nil {
		return Authorization{}, err
	}
	if _, err := service.notifications.Record(ctx, tx, notification.Event{
		NotificationEventID: "card.authorization." + command.ExternalEventID,
		CustomerReference:   profile.CustomerReference, Category: "CARD", EventType: "card.authorization." + strings.ToLower(status),
		TitleKey: "notification.card.authorization.title", BodyKey: "notification.card.authorization." + strings.ToLower(status),
		ResourceType: "card.authorization", ResourceID: authorizationID.String(), ActionPath: "/card",
	}); err != nil {
		return Authorization{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Authorization{}, err
	}
	return authorizationFromStore(created, false), nil
}

func (service *Service) ListAuthorizations(ctx context.Context, accessToken string, pageSize int32) ([]Authorization, error) {
	_, profile, err := service.resolveCustomer(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	rows, err := store.New(service.database).ListAuthorizations(ctx, store.ListAuthorizationsParams{CustomerReference: profile.CustomerReference, PageSize: pageSize})
	if err != nil {
		return nil, err
	}
	result := make([]Authorization, 0, len(rows))
	for _, row := range rows {
		result = append(result, authorizationFromStore(row, false))
	}
	return result, nil
}

func authorizationFromStore(value store.CardAuthorization, replayed bool) Authorization {
	return Authorization{
		ID: value.ID.String(), CardID: value.CardID.String(), ExternalAuthorizationID: value.ExternalAuthorizationID,
		MerchantName: value.MerchantName, MerchantCategoryCode: value.MerchantCategoryCode,
		MerchantAmount: value.MerchantAmount, MerchantCurrency: value.MerchantCurrency,
		AuthorizationFXRate: value.AuthorizationFxRate, FXMarkupRate: value.FxMarkupRate, AuthorizedUSD: value.AuthorizedUsd,
		Status: value.Status, Offline: value.Offline, EntryMode: value.EntryMode, DeclineCode: value.DeclineCode.String,
		HoldLedgerTransactionID: value.HoldLedgerTransactionID.String(), CapturedUSD: value.CapturedUsd,
		ReversedUSD: value.ReversedUsd, PolicyVersion: value.PolicyVersion, Replayed: replayed,
		OccurredAt: value.OccurredAt.Time.UTC(), CreatedAt: value.CreatedAt.Time.UTC(),
	}
}

func validSimulatorEntryMode(value string) bool {
	switch value {
	case "NFC_SIMULATOR", "ECOMMERCE_SIMULATOR", "MANUAL_SIMULATOR", "OFFLINE_SIMULATOR":
		return true
	default:
		return false
	}
}
