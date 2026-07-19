package card

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KDTikkly/Cytisus/internal/card/store"
	"github.com/KDTikkly/Cytisus/internal/notification"
	"github.com/jackc/pgx/v5"
)

func (service *Service) ConfigureRepayment(ctx context.Context, command ConfigureRepaymentCommand) (Profile, error) {
	if !idempotencyKeyPattern.MatchString(command.IdempotencyKey) ||
		(command.Mode != RepaymentCashOnly && command.Mode != RepaymentCashThenAutoSell && command.Mode != RepaymentMonthlyStatement) {
		return Profile{}, ErrInvalidCommand
	}
	_, profile, err := service.resolveCustomer(ctx, command.AccessToken)
	if err != nil {
		return Profile{}, err
	}
	requestHash, _ := hashValue(struct{ Mode RepaymentMode }{command.Mode})
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Profile{}, err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	locked, err := queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference)
	if err != nil {
		return Profile{}, err
	}
	resourceID := locked.PaperAccountID
	_, err = queries.InsertCommandRequest(ctx, store.InsertCommandRequestParams{
		CustomerReference: profile.CustomerReference, Scope: "card.repayment-mode", IdempotencyKey: command.IdempotencyKey,
		RequestHash: requestHash, ResourceID: resourceID,
	})
	replayed := false
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := queries.GetCommandRequest(ctx, store.GetCommandRequestParams{
			CustomerReference: profile.CustomerReference, Scope: "card.repayment-mode", IdempotencyKey: command.IdempotencyKey,
		})
		if getErr != nil || existing.RequestHash != requestHash {
			return Profile{}, ErrIdempotencyConflict
		}
		replayed = true
	} else if err != nil {
		return Profile{}, err
	}
	if !replayed {
		locked, err = queries.UpdateRepaymentMode(ctx, store.UpdateRepaymentModeParams{
			CustomerReference: profile.CustomerReference, RepaymentMode: string(command.Mode),
		})
		if err != nil {
			return Profile{}, err
		}
		payload, _ := json.Marshal(map[string]string{"repayment_mode": locked.RepaymentMode})
		if err := recordMutation(ctx, tx, mutation{
			Action: "card.repayment-mode.configured", ResourceType: "card.profile", ResourceID: profile.CustomerReference,
			ActorType: "USER", ActorID: profile.CustomerReference, CorrelationID: resourceID, Metadata: payload,
			AggregateType: "card.profile", AggregateID: profile.CustomerReference,
			EventType: "card.repayment-mode.configured", Payload: payload,
		}); err != nil {
			return Profile{}, err
		}
		if _, err := service.notifications.Record(ctx, tx, notification.Event{
			NotificationEventID: "card.repayment-mode." + profile.CustomerReference + "." + fmt.Sprint(locked.Version),
			CustomerReference:   profile.CustomerReference, Category: "CARD", EventType: "card.repayment-mode.configured",
			TitleKey: "notification.card.repayment.title", BodyKey: "notification.card.repayment.body",
			ResourceType: "card.profile", ResourceID: profile.CustomerReference, ActionPath: "/card",
		}); err != nil {
			return Profile{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Profile{}, err
	}
	return service.Profile(ctx, command.AccessToken)
}

func (service *Service) ConfigureAutoSellMandate(ctx context.Context, command ConfigureMandateCommand) (AutoSellMandate, error) {
	if !idempotencyKeyPattern.MatchString(command.IdempotencyKey) || !command.DailyMaxUSD.IsPositive() ||
		command.ValidUntil.Before(service.now().UTC()) || len(command.Assets) == 0 || len(command.Assets) > 100 {
		return AutoSellMandate{}, ErrInvalidCommand
	}
	_, profile, err := service.resolveCustomer(ctx, command.AccessToken)
	if err != nil {
		return AutoSellMandate{}, err
	}
	seen := map[string]bool{}
	for index := range command.Assets {
		command.Assets[index].Symbol = strings.ToUpper(strings.TrimSpace(command.Assets[index].Symbol))
		if command.Assets[index].Priority != int16(index+1) || seen[command.Assets[index].Symbol] ||
			command.Assets[index].MinimumRetainQuantity.Sign() < 0 {
			return AutoSellMandate{}, ErrInvalidCommand
		}
		seen[command.Assets[index].Symbol] = true
	}
	requestHash, _ := hashValue(commandForMandateHash(command))
	mandateID, err := newUUID()
	if err != nil {
		return AutoSellMandate{}, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return AutoSellMandate{}, err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	locked, err := queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference)
	if err != nil {
		return AutoSellMandate{}, err
	}
	policy, err := queries.GetPolicy(ctx, locked.PolicyVersion)
	if err != nil || command.DailyMaxUSD.Compare(policy.DefaultDailyAutoSellUsd) > 0 {
		return AutoSellMandate{}, ErrInvalidCommand
	}
	for _, asset := range command.Assets {
		if _, err := queries.GetCollateralAsset(ctx, asset.Symbol); err != nil {
			return AutoSellMandate{}, ErrInvalidCommand
		}
	}
	_, err = queries.InsertCommandRequest(ctx, store.InsertCommandRequestParams{
		CustomerReference: profile.CustomerReference, Scope: "card.auto-sell-mandate", IdempotencyKey: command.IdempotencyKey,
		RequestHash: requestHash, ResourceID: mandateID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existingRequest, getErr := queries.GetCommandRequest(ctx, store.GetCommandRequestParams{
			CustomerReference: profile.CustomerReference, Scope: "card.auto-sell-mandate", IdempotencyKey: command.IdempotencyKey,
		})
		if getErr != nil || existingRequest.RequestHash != requestHash {
			return AutoSellMandate{}, ErrIdempotencyConflict
		}
		existing, getErr := service.getMandateWith(ctx, tx, profile.CustomerReference)
		if getErr != nil {
			return AutoSellMandate{}, getErr
		}
		if err := tx.Commit(ctx); err != nil {
			return AutoSellMandate{}, err
		}
		return existing, nil
	}
	if err != nil {
		return AutoSellMandate{}, err
	}
	mandate, err := queries.UpsertAutoSellMandate(ctx, store.UpsertAutoSellMandateParams{
		ID: mandateID, CustomerReference: profile.CustomerReference, Enabled: command.Enabled,
		AllowFractional: command.AllowFractional, DailyMaxUsd: command.DailyMaxUSD,
		ValidUntil: timestamptz(command.ValidUntil), PolicyVersion: locked.PolicyVersion,
	})
	if err != nil {
		return AutoSellMandate{}, err
	}
	if err := queries.DeleteAutoSellMandateAssets(ctx, mandate.ID); err != nil {
		return AutoSellMandate{}, err
	}
	for _, asset := range command.Assets {
		if _, err := queries.InsertAutoSellMandateAsset(ctx, store.InsertAutoSellMandateAssetParams{
			MandateID: mandate.ID, Priority: asset.Priority, Symbol: asset.Symbol,
			MinimumRetainQuantity: asset.MinimumRetainQuantity,
		}); err != nil {
			return AutoSellMandate{}, err
		}
	}
	payload, _ := json.Marshal(map[string]any{"mandate_id": mandate.ID.String(), "asset_count": len(command.Assets), "enabled": mandate.Enabled})
	if err := recordMutation(ctx, tx, mutation{
		Action: "card.auto-sell-mandate.configured", ResourceType: "card.auto-sell-mandate", ResourceID: mandate.ID.String(),
		ActorType: "USER", ActorID: profile.CustomerReference, CorrelationID: mandate.ID, Metadata: payload,
		AggregateType: "card.auto-sell-mandate", AggregateID: mandate.ID.String(),
		EventType: "card.auto-sell-mandate.configured", Payload: payload,
	}); err != nil {
		return AutoSellMandate{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return AutoSellMandate{}, err
	}
	return service.GetAutoSellMandate(ctx, command.AccessToken)
}

func commandForMandateHash(command ConfigureMandateCommand) any {
	return struct {
		Enabled         bool
		AllowFractional bool
		DailyMaxUSD     string
		ValidUntil      string
		Assets          []AutoSellMandateAsset
	}{command.Enabled, command.AllowFractional, command.DailyMaxUSD.String(), command.ValidUntil.UTC().Format(time.RFC3339Nano), command.Assets}
}

func (service *Service) GetAutoSellMandate(ctx context.Context, accessToken string) (AutoSellMandate, error) {
	_, profile, err := service.resolveCustomer(ctx, accessToken)
	if err != nil {
		return AutoSellMandate{}, err
	}
	return service.getMandateWith(ctx, service.database, profile.CustomerReference)
}

func (service *Service) getMandateWith(ctx context.Context, database store.DBTX, customerReference string) (AutoSellMandate, error) {
	queries := store.New(database)
	mandate, err := queries.GetAutoSellMandate(ctx, customerReference)
	if errors.Is(err, pgx.ErrNoRows) {
		return AutoSellMandate{}, ErrMandateRequired
	}
	if err != nil {
		return AutoSellMandate{}, err
	}
	rows, err := queries.ListAutoSellMandateAssets(ctx, mandate.ID)
	if err != nil {
		return AutoSellMandate{}, err
	}
	assets := make([]AutoSellMandateAsset, 0, len(rows))
	for _, row := range rows {
		assets = append(assets, AutoSellMandateAsset{Priority: row.Priority, Symbol: row.Symbol, MinimumRetainQuantity: row.MinimumRetainQuantity})
	}
	return AutoSellMandate{
		ID: mandate.ID.String(), Enabled: mandate.Enabled, AllowFractional: mandate.AllowFractional,
		DailyMaxUSD: mandate.DailyMaxUsd, ValidUntil: mandate.ValidUntil.Time.UTC(),
		PolicyVersion: mandate.PolicyVersion, Assets: assets, UpdatedAt: mandate.UpdatedAt.Time.UTC(),
	}, nil
}
