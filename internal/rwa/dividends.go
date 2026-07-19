package rwa

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/rwa/store"
	"github.com/jackc/pgx/v5"
)

var dividendPolicy = money.RoundingPolicy{Version: "rwa-dividend-usd-v1", DecimalPlaces: 2, Mode: money.RoundHalfEven}

func (service *Service) AnnounceDividend(ctx context.Context, command AnnounceDividendCommand) (Dividend, error) {
	if !authorizedAdmin(command.Actor) || strings.TrimSpace(command.ExternalReference) == "" ||
		!command.USDPerShare.IsPositive() || command.RecordAt.IsZero() || command.PayableAt.Before(command.RecordAt) {
		return Dividend{}, ErrInvalidCommand
	}
	assetID, err := parseUUID(command.AssetID)
	if err != nil {
		return Dividend{}, ErrAssetNotFound
	}
	identifier, err := newUUID()
	if err != nil {
		return Dividend{}, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Dividend{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := store.New(tx).GetAsset(ctx, assetID); err != nil {
		return Dividend{}, ErrAssetNotFound
	}
	created, err := store.New(tx).CreateDividend(ctx, store.CreateDividendParams{
		ID: identifier, AssetID: assetID, ExternalReference: command.ExternalReference,
		UsdPerShare: command.USDPerShare, RecordAt: timestamptz(command.RecordAt),
		PayableAt: timestamptz(command.PayableAt), PolicyVersion: PolicyVersion,
	})
	if err != nil {
		return Dividend{}, err
	}
	if err := service.recordState(ctx, tx, "DIVIDEND", identifier, "", created.Status, "ADMIN", command.Actor.ID, "", map[string]string{
		"usd_per_share": command.USDPerShare.String(), "drip": "false",
	}); err != nil {
		return Dividend{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Dividend{}, err
	}
	return dividendFromStore(created), nil
}

func (service *Service) ProcessDividend(ctx context.Context, command ProcessDividendCommand) (Dividend, []DividendEntitlement, error) {
	if !authorizedAdmin(command.Actor) {
		return Dividend{}, nil, ErrAdminUnauthorized
	}
	identifier, err := parseUUID(command.DividendID)
	if err != nil {
		return Dividend{}, nil, ErrInvalidCommand
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Dividend{}, nil, err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	dividend, err := queries.GetDividendForUpdate(ctx, identifier)
	if err != nil || dividend.Status != "ANNOUNCED" || dividend.PayableAt.Time.After(service.now().UTC()) {
		return Dividend{}, nil, ErrInvalidState
	}
	dividend, err = service.advanceDividend(ctx, tx, dividend, "RECORD_DATE_LOCKED", command.Actor)
	if err != nil {
		return Dividend{}, nil, err
	}
	holdings, err := queries.ListPositiveHoldingsForAsset(ctx, dividend.AssetID)
	if err != nil {
		return Dividend{}, nil, err
	}
	for _, holding := range holdings {
		gross, multiplyErr := holding.Quantity.Multiply(dividend.UsdPerShare, dividendPolicy)
		if multiplyErr != nil || !gross.IsPositive() {
			return Dividend{}, nil, fmt.Errorf("calculate RWA dividend entitlement: %w", multiplyErr)
		}
		entitlementID, idErr := newUUID()
		if idErr != nil {
			return Dividend{}, nil, idErr
		}
		if _, err := queries.InsertDividendEntitlement(ctx, store.InsertDividendEntitlementParams{
			ID: entitlementID, DividendID: dividend.ID, CustomerReference: holding.CustomerReference,
			WholeShares: holding.Quantity, GrossUsd: gross, WithholdingUsd: money.Zero(), NetUsd: gross,
		}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Dividend{}, nil, err
		}
	}
	for _, status := range []string{"ENTITLEMENT_CALCULATED", "PAYMENT_RECEIVED", "WITHHOLDING_APPLIED"} {
		dividend, err = service.advanceDividend(ctx, tx, dividend, status, command.Actor)
		if err != nil {
			return Dividend{}, nil, err
		}
	}
	entitlements, err := queries.ListDividendEntitlements(ctx, dividend.ID)
	if err != nil {
		return Dividend{}, nil, err
	}
	ledgerService := ledger.NewService(tx)
	funding, err := ledgerService.OpenAccount(ctx, ledger.AccountCommand{
		AccountKey: "rwa.dividend-funding.usd", OwnerType: "SYSTEM", OwnerID: "rwa-dividend-simulator",
		AccountType: "PROVIDER_CLEARING", Currency: money.Currency("USD"), NormalSide: ledger.Credit,
		Actor: ledger.Actor{Type: "SYSTEM", ID: "rwa-dividend-simulator"},
	})
	if err != nil {
		return Dividend{}, nil, err
	}
	for _, entitlement := range entitlements {
		profile, err := queries.GetCustomerProfile(ctx, entitlement.CustomerReference)
		if err != nil {
			return Dividend{}, nil, err
		}
		posting, err := ledgerService.Post(ctx, ledger.PostingCommand{
			Scope: "rwa.dividend", IdempotencyKey: "rwa-dividend:" + entitlement.ID.String(),
			TransactionType: "RWA_CASH_DIVIDEND", PolicyVersion: PolicyVersion,
			EffectiveAt: service.now().UTC(), Actor: ledger.Actor{Type: "SYSTEM", ID: "rwa-dividend-simulator"},
			Entries: []ledger.Entry{
				{AccountID: profile.CashLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: entitlement.NetUsd},
				{AccountID: funding.ID, Currency: money.Currency("USD"), Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: entitlement.NetUsd},
			},
		})
		if err != nil {
			return Dividend{}, nil, err
		}
		if _, err := queries.MarkDividendEntitlementCredited(ctx, store.MarkDividendEntitlementCreditedParams{
			CashLedgerTransactionID: uuidValue(posting.TransactionID), ID: entitlement.ID,
		}); err != nil {
			return Dividend{}, nil, err
		}
	}
	for _, status := range []string{"USD_CASH_CREDITED", "RECONCILED"} {
		dividend, err = service.advanceDividend(ctx, tx, dividend, status, command.Actor)
		if err != nil {
			return Dividend{}, nil, err
		}
	}
	entitlements, err = queries.ListDividendEntitlements(ctx, dividend.ID)
	if err != nil {
		return Dividend{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Dividend{}, nil, err
	}
	result := make([]DividendEntitlement, 0, len(entitlements))
	for _, entitlement := range entitlements {
		result = append(result, entitlementFromStore(entitlement))
	}
	return dividendFromStore(dividend), result, nil
}

func (service *Service) advanceDividend(ctx context.Context, tx pgx.Tx, current store.RwaDividend, status string, actor AdminActor) (store.RwaDividend, error) {
	updated, err := store.New(tx).UpdateDividendStatus(ctx, store.UpdateDividendStatusParams{Status: status, ID: current.ID})
	if err != nil {
		return store.RwaDividend{}, err
	}
	if err := service.recordState(ctx, tx, "DIVIDEND", current.ID, current.Status, updated.Status, "ADMIN", actor.ID, "", map[string]string{"cash_only": "true", "drip": "false"}); err != nil {
		return store.RwaDividend{}, err
	}
	return updated, nil
}

func dividendFromStore(value store.RwaDividend) Dividend {
	return Dividend{ID: value.ID.String(), AssetID: value.AssetID.String(), ExternalReference: value.ExternalReference, USDPerShare: value.UsdPerShare, RecordAt: value.RecordAt.Time.UTC(), PayableAt: value.PayableAt.Time.UTC(), Status: value.Status, UpdatedAt: value.UpdatedAt.Time.UTC()}
}

func entitlementFromStore(value store.RwaDividendEntitlement) DividendEntitlement {
	return DividendEntitlement{ID: value.ID.String(), CustomerReference: value.CustomerReference, WholeShares: value.WholeShares, GrossUSD: value.GrossUsd, WithholdingUSD: value.WithholdingUsd, NetUSD: value.NetUsd, Status: value.Status}
}
