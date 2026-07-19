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
)

func (service *Service) RequestWithdrawal(ctx context.Context, command RequestWithdrawalCommand) (Withdrawal, error) {
	if !idempotencyKeyPattern.MatchString(command.IdempotencyKey) || !command.Quantity.IsPositive() {
		return Withdrawal{}, ErrInvalidCommand
	}
	_, profile, err := service.resolveCustomer(ctx, command.AccessToken)
	if err != nil {
		return Withdrawal{}, err
	}
	addressID, err := parseUUID(command.AddressID)
	if err != nil {
		return Withdrawal{}, ErrAddressNotFound
	}
	address, err := store.New(service.database).GetWithdrawalAddress(ctx, addressID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && address.CustomerReference != profile.CustomerReference {
		return Withdrawal{}, ErrAddressNotFound
	}
	if err != nil {
		return Withdrawal{}, err
	}
	if address.Status != "ACTIVE" {
		if address.Status == "COOLING_OFF" {
			return Withdrawal{}, ErrCoolingOff
		}
		return Withdrawal{}, ErrInvalidState
	}
	risk, err := service.analyzeAddress(ctx, address.AssetSymbol, address.NetworkCode, address.ExternalAddress)
	if err != nil {
		return Withdrawal{}, err
	}
	if risk.Decision == "BLOCK" {
		return Withdrawal{}, ErrAddressRiskBlocked
	}
	clientReference := deterministicReference("crypto-withdrawal", profile.CustomerReference, command.IdempotencyKey)
	providerContext, cancel := context.WithTimeout(ctx, service.providerTimeout)
	prepared, err := service.custody.PrepareWithdrawal(providerContext, provider.WithdrawalRequest{
		ClientReference: clientReference, Asset: address.AssetSymbol, Network: address.NetworkCode,
		ExternalAddress: address.ExternalAddress, Quantity: command.Quantity,
	})
	cancel()
	if err != nil {
		return Withdrawal{}, fmt.Errorf("prepare crypto withdrawal: %w", err)
	}
	if prepared.Provider != service.custody.Name() || prepared.InitialStatus != "APPROVED" ||
		prepared.ProviderWithdrawalID == "" || prepared.Simulated && service.environment == "production" {
		return Withdrawal{}, provider.ErrUnavailable
	}
	requestHash, err := hashValue(struct {
		AddressID, Quantity string
	}{command.AddressID, command.Quantity.String()})
	if err != nil {
		return Withdrawal{}, err
	}
	withdrawalID, err := newUUID()
	if err != nil {
		return Withdrawal{}, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Withdrawal{}, fmt.Errorf("begin crypto withdrawal: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	inserted, err := queries.InsertWithdrawalRequest(ctx, store.InsertWithdrawalRequestParams{
		CustomerReference: profile.CustomerReference, IdempotencyKey: command.IdempotencyKey,
		RequestHash: requestHash, WithdrawalID: withdrawalID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existingRequest, getErr := queries.GetWithdrawalRequest(ctx, store.GetWithdrawalRequestParams{
			CustomerReference: profile.CustomerReference, IdempotencyKey: command.IdempotencyKey,
		})
		if getErr != nil {
			return Withdrawal{}, getErr
		}
		if existingRequest.RequestHash != requestHash {
			return Withdrawal{}, ErrIdempotencyConflict
		}
		existing, getErr := queries.GetWithdrawal(ctx, existingRequest.WithdrawalID)
		if getErr != nil {
			return Withdrawal{}, getErr
		}
		return withdrawalFromStore(existing, true), nil
	}
	if err != nil || inserted != withdrawalID {
		return Withdrawal{}, fmt.Errorf("insert crypto withdrawal request: %w", err)
	}
	lockedProfile, err := queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference)
	if err != nil {
		return Withdrawal{}, err
	}
	address, err = queries.GetWithdrawalAddressForUpdate(ctx, addressID)
	if err != nil || address.CustomerReference != profile.CustomerReference {
		return Withdrawal{}, ErrAddressNotFound
	}
	if address.Status != "ACTIVE" {
		return Withdrawal{}, ErrInvalidState
	}
	accounts, err := service.ensureAssetLedgerAccounts(ctx, tx, lockedProfile, address.AssetSymbol)
	if err != nil {
		return Withdrawal{}, err
	}
	ledgerService := ledger.NewService(tx)
	settled, err := ledgerService.Balance(ctx, accounts.CustomerLedgerAccountID.String(), money.Currency(address.AssetSymbol), ledger.DimensionSettled)
	if err != nil {
		return Withdrawal{}, err
	}
	if settled.Compare(command.Quantity) < 0 {
		return Withdrawal{}, ErrInsufficientAsset
	}
	reservation, err := ledgerService.Post(ctx, ledger.PostingCommand{
		Scope: "crypto.withdrawal.reserve", IdempotencyKey: "withdrawal.reserve." + withdrawalID.String(),
		TransactionType: "CRYPTO_WITHDRAWAL_RESERVATION", PolicyVersion: profile.PolicyVersion, EffectiveAt: service.now().UTC(),
		Actor: ledger.Actor{Type: "USER", ID: profile.CustomerReference},
		Entries: []ledger.Entry{
			{AccountID: accounts.CustomerLedgerAccountID.String(), Currency: money.Currency(address.AssetSymbol), Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: command.Quantity},
			{AccountID: accounts.CustodyInventoryLedgerAccountID.String(), Currency: money.Currency(address.AssetSymbol), Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: command.Quantity},
			{AccountID: accounts.CustomerLedgerAccountID.String(), Currency: money.Currency(address.AssetSymbol), Dimension: ledger.DimensionHeld, Direction: ledger.Debit, Amount: command.Quantity},
			{AccountID: accounts.CustodyInventoryLedgerAccountID.String(), Currency: money.Currency(address.AssetSymbol), Dimension: ledger.DimensionHeld, Direction: ledger.Credit, Amount: command.Quantity},
		},
	})
	if err != nil {
		return Withdrawal{}, fmt.Errorf("reserve crypto withdrawal: %w", err)
	}
	reservationID, err := parseUUID(reservation.TransactionID)
	if err != nil {
		return Withdrawal{}, err
	}
	created, err := queries.CreateWithdrawal(ctx, store.CreateWithdrawalParams{
		ID: withdrawalID, CustomerReference: profile.CustomerReference, WithdrawalAddressID: addressID,
		AssetSymbol: address.AssetSymbol, NetworkCode: address.NetworkCode, Quantity: command.Quantity,
		Status: "APPROVED", ReasonCode: "CUSTODY_WITHDRAWAL_APPROVED",
		NextAction: "The simulated custody provider is preparing the network broadcast.",
		Provider:   prepared.Provider, ProviderWithdrawalID: prepared.ProviderWithdrawalID,
		ReservationLedgerTransactionID: reservationID, PolicyVersion: profile.PolicyVersion,
	})
	if err != nil {
		return Withdrawal{}, fmt.Errorf("create crypto withdrawal: %w", err)
	}
	metadata, _ := json.Marshal(map[string]string{"risk_provider": risk.Provider, "risk_decision": risk.Decision, "mode": "SIMULATED"})
	payload, _ := json.Marshal(map[string]string{"withdrawal_id": withdrawalID.String(), "status": created.Status, "asset": created.AssetSymbol, "quantity": created.Quantity.String()})
	if err := recordMutation(ctx, tx, mutation{
		Action: "crypto.withdrawal.approved", ResourceType: "crypto.withdrawal", ResourceID: withdrawalID.String(),
		ActorType: "USER", ActorID: profile.CustomerReference, CorrelationID: withdrawalID,
		Metadata: metadata, AggregateType: "crypto.withdrawal", AggregateID: withdrawalID.String(),
		EventType: "crypto.withdrawal.approved", Payload: payload,
	}); err != nil {
		return Withdrawal{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Withdrawal{}, fmt.Errorf("commit crypto withdrawal: %w", err)
	}
	return withdrawalFromStore(created, false), nil
}

func (service *Service) ApplyWithdrawalEvent(ctx context.Context, event WithdrawalEvent) (Withdrawal, error) {
	if event.Provider != service.custody.Name() || event.ExternalEventID == "" || event.WithdrawalID == "" ||
		event.OccurredAt.IsZero() || len(event.Payload) == 0 ||
		(event.EventType != "WITHDRAWAL_BROADCAST" && event.EventType != "WITHDRAWAL_CONFIRMED" && event.EventType != "WITHDRAWAL_FAILED") {
		return Withdrawal{}, ErrInvalidCommand
	}
	withdrawalID, err := parseUUID(event.WithdrawalID)
	if err != nil {
		return Withdrawal{}, ErrWithdrawalNotFound
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Withdrawal{}, fmt.Errorf("begin crypto withdrawal event: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	payloadHash := sha256Hex(event.Payload)
	_, err = queries.InsertProviderEvent(ctx, store.InsertProviderEventParams{
		Provider: event.Provider, ExternalEventID: event.ExternalEventID, ResourceType: "WITHDRAWAL",
		ResourceID: withdrawalID, EventType: event.EventType, PayloadHash: payloadHash, DomainRecordID: withdrawalID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := queries.GetProviderEvent(ctx, store.GetProviderEventParams{Provider: event.Provider, ExternalEventID: event.ExternalEventID})
		if getErr != nil {
			return Withdrawal{}, getErr
		}
		if existing.ResourceType != "WITHDRAWAL" || existing.ResourceID != withdrawalID || existing.EventType != event.EventType || existing.PayloadHash != payloadHash {
			return Withdrawal{}, ErrProviderEventConflict
		}
		stored, getErr := queries.GetWithdrawal(ctx, existing.DomainRecordID)
		if getErr != nil {
			return Withdrawal{}, getErr
		}
		return withdrawalFromStore(stored, true), nil
	}
	if err != nil {
		return Withdrawal{}, fmt.Errorf("insert crypto withdrawal event: %w", err)
	}
	current, err := queries.GetWithdrawalForUpdate(ctx, withdrawalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Withdrawal{}, ErrWithdrawalNotFound
	}
	if err != nil {
		return Withdrawal{}, err
	}
	profile, err := queries.GetCustomerProfileForUpdate(ctx, current.CustomerReference)
	if err != nil {
		return Withdrawal{}, err
	}
	updated, err := service.transitionWithdrawalEvent(ctx, tx, profile, current, event)
	if err != nil {
		return Withdrawal{}, err
	}
	metadata, _ := json.Marshal(map[string]string{"provider": event.Provider, "external_event_id": event.ExternalEventID, "reason_code": updated.ReasonCode})
	payload, _ := json.Marshal(map[string]string{"withdrawal_id": withdrawalID.String(), "status": updated.Status})
	action := withdrawalEventAction(updated.Status)
	if err := recordMutation(ctx, tx, mutation{
		Action: "crypto.withdrawal." + action, ResourceType: "crypto.withdrawal", ResourceID: withdrawalID.String(),
		ActorType: "PROVIDER", ActorID: event.Provider, CorrelationID: withdrawalID,
		Metadata: metadata, AggregateType: "crypto.withdrawal", AggregateID: withdrawalID.String(),
		EventType: "crypto.withdrawal." + action, EventVersion: int32(updated.Version), Payload: payload,
	}); err != nil {
		return Withdrawal{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Withdrawal{}, fmt.Errorf("commit crypto withdrawal event: %w", err)
	}
	return withdrawalFromStore(updated, false), nil
}

func (service *Service) transitionWithdrawalEvent(ctx context.Context, tx pgx.Tx, profile store.CryptoCustomerProfile, current store.CryptoWithdrawal, event WithdrawalEvent) (store.CryptoWithdrawal, error) {
	params := store.TransitionWithdrawalParams{
		ID: current.ID, BroadcastLedgerTransactionID: current.BroadcastLedgerTransactionID,
		ReservationReversalTransactionID: current.ReservationReversalTransactionID,
		BroadcastReversalTransactionID:   current.BroadcastReversalTransactionID,
	}
	ledgerService := ledger.NewService(tx)
	switch event.EventType {
	case "WITHDRAWAL_BROADCAST":
		if current.Status != "APPROVED" {
			return store.CryptoWithdrawal{}, ErrInvalidState
		}
		accounts, err := service.ensureAssetLedgerAccounts(ctx, tx, profile, current.AssetSymbol)
		if err != nil {
			return store.CryptoWithdrawal{}, err
		}
		posting, err := ledgerService.Post(ctx, ledger.PostingCommand{
			Scope: "crypto.withdrawal.broadcast", IdempotencyKey: "withdrawal.broadcast." + current.ID.String(),
			TransactionType: "CRYPTO_WITHDRAWAL_BROADCAST", PolicyVersion: current.PolicyVersion, EffectiveAt: event.OccurredAt.UTC(),
			Actor: ledger.Actor{Type: "PROVIDER", ID: event.Provider},
			Entries: []ledger.Entry{
				{AccountID: accounts.CustomerLedgerAccountID.String(), Currency: money.Currency(current.AssetSymbol), Dimension: ledger.DimensionHeld, Direction: ledger.Credit, Amount: current.Quantity},
				{AccountID: accounts.CustodyInventoryLedgerAccountID.String(), Currency: money.Currency(current.AssetSymbol), Dimension: ledger.DimensionHeld, Direction: ledger.Debit, Amount: current.Quantity},
			},
			ProviderEvent: &ledger.ProviderEvent{Provider: event.Provider, ExternalEventID: event.ExternalEventID, Payload: event.Payload},
		})
		if err != nil {
			return store.CryptoWithdrawal{}, fmt.Errorf("post crypto withdrawal broadcast: %w", err)
		}
		params.Status = "BROADCAST"
		params.ReasonCode = "CUSTODY_WITHDRAWAL_BROADCAST"
		params.NextAction = "The simulated network transfer is awaiting confirmation."
		params.BroadcastLedgerTransactionID, err = parseUUID(posting.TransactionID)
		if err != nil {
			return store.CryptoWithdrawal{}, err
		}
	case "WITHDRAWAL_CONFIRMED":
		if current.Status != "BROADCAST" {
			return store.CryptoWithdrawal{}, ErrInvalidState
		}
		params.Status = "CONFIRMED"
		params.ReasonCode = "CUSTODY_WITHDRAWAL_CONFIRMED"
		params.NextAction = "No further action is required."
	case "WITHDRAWAL_FAILED":
		if current.Status != "APPROVED" && current.Status != "BROADCAST" {
			return store.CryptoWithdrawal{}, ErrInvalidState
		}
		if current.Status == "BROADCAST" {
			reversal, err := ledgerService.Reverse(ctx, ledger.ReversalCommand{
				Scope: "crypto.withdrawal.broadcast-reversal", IdempotencyKey: "withdrawal.broadcast-reversal." + current.ID.String(),
				OriginalTransactionID: current.BroadcastLedgerTransactionID.String(), ReasonCode: "PROVIDER_WITHDRAWAL_FAILED",
				PolicyVersion: current.PolicyVersion, EffectiveAt: event.OccurredAt.UTC(), Actor: ledger.Actor{Type: "PROVIDER", ID: event.Provider},
			})
			if err != nil {
				return store.CryptoWithdrawal{}, fmt.Errorf("reverse crypto withdrawal broadcast: %w", err)
			}
			params.BroadcastReversalTransactionID, err = parseUUID(reversal.TransactionID)
			if err != nil {
				return store.CryptoWithdrawal{}, err
			}
		}
		reversal, err := ledgerService.Reverse(ctx, ledger.ReversalCommand{
			Scope: "crypto.withdrawal.reservation-reversal", IdempotencyKey: "withdrawal.reservation-reversal." + current.ID.String(),
			OriginalTransactionID: current.ReservationLedgerTransactionID.String(), ReasonCode: "PROVIDER_WITHDRAWAL_FAILED",
			PolicyVersion: current.PolicyVersion, EffectiveAt: event.OccurredAt.UTC(), Actor: ledger.Actor{Type: "PROVIDER", ID: event.Provider},
		})
		if err != nil {
			return store.CryptoWithdrawal{}, fmt.Errorf("reverse crypto withdrawal reservation: %w", err)
		}
		params.ReservationReversalTransactionID, err = parseUUID(reversal.TransactionID)
		if err != nil {
			return store.CryptoWithdrawal{}, err
		}
		params.Status = "FAILED"
		params.ReasonCode = event.ReasonCode
		if params.ReasonCode == "" {
			params.ReasonCode = "PROVIDER_WITHDRAWAL_FAILED"
		}
		params.NextAction = "The withdrawal failed and the reserved asset was restored; retry or contact support."
	}
	updated, err := store.New(tx).TransitionWithdrawal(ctx, params)
	if err != nil {
		return store.CryptoWithdrawal{}, fmt.Errorf("transition crypto withdrawal: %w", err)
	}
	return updated, nil
}

func (service *Service) ListWithdrawals(ctx context.Context, accessToken string, pageSize int32) ([]Withdrawal, error) {
	_, profile, err := service.resolveCustomer(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	rows, err := store.New(service.database).ListWithdrawals(ctx, store.ListWithdrawalsParams{CustomerReference: profile.CustomerReference, PageSize: pageSize})
	if err != nil {
		return nil, fmt.Errorf("list crypto withdrawals: %w", err)
	}
	result := make([]Withdrawal, 0, len(rows))
	for _, row := range rows {
		result = append(result, withdrawalFromStore(row, false))
	}
	return result, nil
}

func withdrawalFromStore(stored store.CryptoWithdrawal, replayed bool) Withdrawal {
	return Withdrawal{
		ID: stored.ID.String(), WithdrawalAddressID: stored.WithdrawalAddressID.String(), Asset: stored.AssetSymbol,
		Network: stored.NetworkCode, Quantity: stored.Quantity, Status: stored.Status, ReasonCode: stored.ReasonCode,
		NextAction: stored.NextAction, Provider: stored.Provider, ProviderWithdrawalID: stored.ProviderWithdrawalID,
		CreatedAt: stored.CreatedAt.Time.UTC(), UpdatedAt: stored.UpdatedAt.Time.UTC(), Replayed: replayed,
	}
}

func withdrawalEventAction(status string) string {
	switch status {
	case "BROADCAST":
		return "broadcast"
	case "CONFIRMED":
		return "confirmed"
	default:
		return "failed"
	}
}
