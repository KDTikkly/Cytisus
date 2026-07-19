package crypto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/KDTikkly/Cytisus/internal/crypto/provider"
	"github.com/KDTikkly/Cytisus/internal/crypto/store"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (service *Service) CreateDepositAddress(ctx context.Context, command CreateDepositAddressCommand) (DepositAddress, error) {
	command.Asset = normalizedAsset(command.Asset)
	command.Network = normalizedAsset(command.Network)
	if !idempotencyKeyPattern.MatchString(command.IdempotencyKey) || command.Asset == "" || command.Network == "" {
		return DepositAddress{}, ErrInvalidCommand
	}
	_, profile, err := service.resolveCustomer(ctx, command.AccessToken)
	if err != nil {
		return DepositAddress{}, err
	}
	if err := service.validateAssetNetwork(ctx, store.New(service.database), command.Asset, command.Network, false); err != nil {
		return DepositAddress{}, err
	}
	providerContext, cancel := context.WithTimeout(ctx, service.providerTimeout)
	generated, err := service.custody.GenerateDepositAddress(providerContext, provider.DepositAddressRequest{
		CustomerReference: profile.CustomerReference, Asset: command.Asset, Network: command.Network,
	})
	cancel()
	if err != nil {
		return DepositAddress{}, fmt.Errorf("generate custody deposit address: %w", err)
	}
	if generated.Provider != service.custody.Name() || generated.ExternalAddress == "" ||
		generated.Simulated && service.environment == "production" {
		return DepositAddress{}, provider.ErrUnavailable
	}
	requestHash, err := hashValue(struct{ Asset, Network string }{command.Asset, command.Network})
	if err != nil {
		return DepositAddress{}, err
	}
	addressID, err := newUUID()
	if err != nil {
		return DepositAddress{}, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return DepositAddress{}, fmt.Errorf("begin deposit address: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	if _, err := queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference); err != nil {
		return DepositAddress{}, err
	}
	created, err := queries.CreateDepositAddress(ctx, store.CreateDepositAddressParams{
		ID: addressID, CustomerReference: profile.CustomerReference, AssetSymbol: command.Asset,
		NetworkCode: command.Network, Provider: generated.Provider, ExternalAddress: generated.ExternalAddress,
		Memo: textValue(generated.Memo),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		created, err = queries.GetDepositAddressByAssetNetwork(ctx, store.GetDepositAddressByAssetNetworkParams{
			CustomerReference: profile.CustomerReference, AssetSymbol: command.Asset, NetworkCode: command.Network,
		})
	}
	if err != nil {
		return DepositAddress{}, fmt.Errorf("create crypto deposit address: %w", err)
	}
	inserted, err := queries.InsertDepositAddressRequest(ctx, store.InsertDepositAddressRequestParams{
		CustomerReference: profile.CustomerReference, IdempotencyKey: command.IdempotencyKey,
		RequestHash: requestHash, DepositAddressID: created.ID,
	})
	replayed := false
	if errors.Is(err, pgx.ErrNoRows) {
		existingRequest, getErr := queries.GetDepositAddressRequest(ctx, store.GetDepositAddressRequestParams{
			CustomerReference: profile.CustomerReference, IdempotencyKey: command.IdempotencyKey,
		})
		if getErr != nil {
			return DepositAddress{}, getErr
		}
		if existingRequest.RequestHash != requestHash {
			return DepositAddress{}, ErrIdempotencyConflict
		}
		created, err = queries.GetDepositAddress(ctx, existingRequest.DepositAddressID)
		replayed = true
	} else if err == nil && inserted != created.ID {
		err = ErrIdempotencyConflict
	}
	if err != nil {
		return DepositAddress{}, fmt.Errorf("record deposit address request: %w", err)
	}
	if !replayed {
		payload, _ := json.Marshal(map[string]string{"address_id": created.ID.String(), "asset": created.AssetSymbol, "network": created.NetworkCode, "mode": "SIMULATED"})
		if err := recordMutation(ctx, tx, mutation{
			Action: "crypto.deposit_address.created", ResourceType: "crypto.deposit_address", ResourceID: created.ID.String(),
			ActorType: "USER", ActorID: profile.CustomerReference, CorrelationID: created.ID,
			Metadata: payload, AggregateType: "crypto.deposit_address", AggregateID: created.ID.String(),
			EventType: "crypto.deposit_address.created", Payload: payload,
		}); err != nil {
			return DepositAddress{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return DepositAddress{}, fmt.Errorf("commit deposit address: %w", err)
	}
	return depositAddressFromStore(created, replayed), nil
}

func (service *Service) ListDepositAddresses(ctx context.Context, accessToken string) ([]DepositAddress, error) {
	_, profile, err := service.resolveCustomer(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	rows, err := store.New(service.database).ListDepositAddresses(ctx, profile.CustomerReference)
	if err != nil {
		return nil, fmt.Errorf("list crypto deposit addresses: %w", err)
	}
	result := make([]DepositAddress, 0, len(rows))
	for _, row := range rows {
		result = append(result, depositAddressFromStore(row, false))
	}
	return result, nil
}

func (service *Service) ListDeposits(ctx context.Context, accessToken string, pageSize int32) ([]Deposit, error) {
	_, profile, err := service.resolveCustomer(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	rows, err := store.New(service.database).ListDeposits(ctx, store.ListDepositsParams{
		CustomerReference: profile.CustomerReference, PageSize: pageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("list crypto deposits: %w", err)
	}
	result := make([]Deposit, 0, len(rows))
	for _, row := range rows {
		result = append(result, depositFromStore(row, false))
	}
	return result, nil
}

func (service *Service) ApplyDepositEvent(ctx context.Context, event DepositEvent) (Deposit, error) {
	event.Asset = normalizedAsset(event.Asset)
	event.Network = normalizedAsset(event.Network)
	if event.Provider != service.custody.Name() || event.ExternalEventID == "" || event.ProviderTransactionID == "" ||
		!event.Quantity.IsPositive() || event.OccurredAt.IsZero() || len(event.Payload) == 0 ||
		(event.EventType != "DEPOSIT_CONFIRMED" && event.EventType != "DEPOSIT_REJECTED") {
		return Deposit{}, ErrInvalidCommand
	}
	addressID, err := parseUUID(event.DepositAddressID)
	if err != nil {
		return Deposit{}, ErrAddressNotFound
	}
	depositID, err := newUUID()
	if err != nil {
		return Deposit{}, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Deposit{}, fmt.Errorf("begin crypto deposit event: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	payloadHash := sha256Hex(event.Payload)
	insertedEvent, err := queries.InsertProviderEvent(ctx, store.InsertProviderEventParams{
		Provider: event.Provider, ExternalEventID: event.ExternalEventID, ResourceType: "DEPOSIT",
		ResourceID: addressID, EventType: event.EventType, PayloadHash: payloadHash, DomainRecordID: depositID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := queries.GetProviderEvent(ctx, store.GetProviderEventParams{Provider: event.Provider, ExternalEventID: event.ExternalEventID})
		if getErr != nil {
			return Deposit{}, fmt.Errorf("get duplicate crypto deposit event: %w", getErr)
		}
		if existing.ResourceType != "DEPOSIT" || existing.ResourceID != addressID || existing.EventType != event.EventType || existing.PayloadHash != payloadHash {
			return Deposit{}, ErrProviderEventConflict
		}
		stored, getErr := queries.GetDeposit(ctx, existing.DomainRecordID)
		if getErr != nil {
			return Deposit{}, fmt.Errorf("get replayed crypto deposit: %w", getErr)
		}
		return depositFromStore(stored, true), nil
	}
	if err != nil || insertedEvent.DomainRecordID != depositID {
		return Deposit{}, fmt.Errorf("insert crypto deposit event: %w", err)
	}
	address, err := queries.GetDepositAddress(ctx, addressID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Deposit{}, ErrAddressNotFound
	}
	if err != nil {
		return Deposit{}, err
	}
	if address.Status != "ACTIVE" || address.AssetSymbol != event.Asset || address.NetworkCode != event.Network {
		return Deposit{}, ErrInvalidState
	}
	profile, err := queries.GetCustomerProfileForUpdate(ctx, address.CustomerReference)
	if err != nil {
		return Deposit{}, err
	}
	ledgerTransactionID := pgtype.UUID{}
	reasonCode := event.ReasonCode
	if event.EventType == "DEPOSIT_CONFIRMED" {
		assetAccounts, accountErr := service.ensureAssetLedgerAccounts(ctx, tx, profile, event.Asset)
		if accountErr != nil {
			return Deposit{}, accountErr
		}
		posting, postErr := ledger.NewService(tx).Post(ctx, ledger.PostingCommand{
			Scope: "crypto.deposit", IdempotencyKey: "deposit." + event.Provider + "." + event.ExternalEventID,
			TransactionType: "CRYPTO_DEPOSIT", PolicyVersion: profile.PolicyVersion, EffectiveAt: event.OccurredAt.UTC(),
			Actor: ledger.Actor{Type: "PROVIDER", ID: event.Provider},
			Entries: []ledger.Entry{
				{AccountID: assetAccounts.CustomerLedgerAccountID.String(), Currency: money.Currency(event.Asset), Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: event.Quantity},
				{AccountID: assetAccounts.CustodyInventoryLedgerAccountID.String(), Currency: money.Currency(event.Asset), Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: event.Quantity},
			},
			ProviderEvent: &ledger.ProviderEvent{Provider: event.Provider, ExternalEventID: event.ExternalEventID, Payload: event.Payload},
		})
		if postErr != nil {
			return Deposit{}, fmt.Errorf("post crypto deposit to ledger: %w", postErr)
		}
		ledgerTransactionID, err = parseUUID(posting.TransactionID)
		if err != nil {
			return Deposit{}, err
		}
	} else if reasonCode == "" {
		reasonCode = "PROVIDER_DEPOSIT_REJECTED"
	}
	status := "CONFIRMED"
	if event.EventType == "DEPOSIT_REJECTED" {
		status = "REJECTED"
	}
	created, err := queries.CreateDeposit(ctx, store.CreateDepositParams{
		ID: depositID, CustomerReference: address.CustomerReference, DepositAddressID: addressID,
		AssetSymbol: event.Asset, NetworkCode: event.Network, Quantity: event.Quantity, Status: status,
		Provider: event.Provider, ProviderTransactionID: event.ProviderTransactionID,
		LedgerTransactionID: ledgerTransactionID, ReasonCode: textValue(reasonCode),
	})
	if err != nil {
		return Deposit{}, fmt.Errorf("create crypto deposit: %w", err)
	}
	payload, _ := json.Marshal(map[string]string{"deposit_id": depositID.String(), "status": status, "asset": event.Asset, "quantity": event.Quantity.String()})
	if err := recordMutation(ctx, tx, mutation{
		Action: "crypto.deposit." + statusName(status), ResourceType: "crypto.deposit", ResourceID: depositID.String(),
		ActorType: "PROVIDER", ActorID: event.Provider, CorrelationID: depositID,
		Metadata: payload, AggregateType: "crypto.deposit", AggregateID: depositID.String(),
		EventType: "crypto.deposit." + statusName(status), Payload: payload,
	}); err != nil {
		return Deposit{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Deposit{}, fmt.Errorf("commit crypto deposit event: %w", err)
	}
	return depositFromStore(created, false), nil
}

func depositAddressFromStore(stored store.CryptoDepositAddress, replayed bool) DepositAddress {
	return DepositAddress{
		ID: stored.ID.String(), Asset: stored.AssetSymbol, Network: stored.NetworkCode,
		Provider: stored.Provider, ExternalAddress: stored.ExternalAddress, Memo: stored.Memo.String,
		Status: stored.Status, CreatedAt: stored.CreatedAt.Time.UTC(), Replayed: replayed,
	}
}

func depositFromStore(stored store.CryptoDeposit, replayed bool) Deposit {
	return Deposit{
		ID: stored.ID.String(), DepositAddressID: stored.DepositAddressID.String(), Asset: stored.AssetSymbol,
		Network: stored.NetworkCode, Quantity: stored.Quantity, Status: stored.Status, Provider: stored.Provider,
		ProviderTransactionID: stored.ProviderTransactionID, LedgerTransactionID: stored.LedgerTransactionID.String(),
		ReasonCode: stored.ReasonCode.String, CreatedAt: stored.CreatedAt.Time.UTC(), Replayed: replayed,
	}
}

func statusName(status string) string {
	if status == "CONFIRMED" {
		return "confirmed"
	}
	return "rejected"
}
