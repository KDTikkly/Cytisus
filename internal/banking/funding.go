package banking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/KDTikkly/Cytisus/internal/banking/provider"
	"github.com/KDTikkly/Cytisus/internal/banking/store"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (service *Service) InitiateFunding(ctx context.Context, command InitiateFundingCommand) (FundingTransfer, error) {
	_, profile, err := service.resolveCustomer(ctx, command.AccessToken)
	if err != nil {
		return FundingTransfer{}, err
	}
	command.Rail = strings.ToUpper(strings.TrimSpace(command.Rail))
	if !idempotencyKeyPattern.MatchString(command.IdempotencyKey) || !command.Amount.IsPositive() ||
		(command.Rail != "ACH" && command.Rail != "WIRE") {
		return FundingTransfer{}, ErrInvalidCommand
	}
	bankAccountID, err := parseUUID(command.BankAccountID)
	if err != nil {
		return FundingTransfer{}, ErrBankAccountNotFound
	}
	bankAccount, err := store.New(service.database).GetBankAccount(ctx, bankAccountID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && bankAccount.CustomerReference != profile.CustomerReference {
		return FundingTransfer{}, ErrBankAccountNotFound
	}
	if err != nil {
		return FundingTransfer{}, fmt.Errorf("get funding bank account: %w", err)
	}
	if bankAccount.Status != "ACTIVE" || (bankAccount.RailSupport != command.Rail && bankAccount.RailSupport != "BOTH") {
		return FundingTransfer{}, ErrInvalidState
	}
	if command.Rail == "ACH" && (bankAccount.OwnerRelation != "SAME_NAME" || bankAccount.OwnershipStatus != "VERIFIED") {
		return FundingTransfer{}, ErrOwnershipVerification
	}
	requestHash, err := hashValue(struct {
		BankAccountID string `json:"bank_account_id"`
		Rail          string `json:"rail"`
		Amount        string `json:"amount"`
	}{command.BankAccountID, command.Rail, command.Amount.String()})
	if err != nil {
		return FundingTransfer{}, err
	}
	clientReference := deterministicReference("funding", profile.CustomerReference, command.IdempotencyKey)
	providerResponse, err := service.callProvider(ctx, func(providerContext context.Context) (provider.TransferResponse, error) {
		return service.provider.InitiateFunding(providerContext, provider.TransferRequest{
			ClientReference:          clientReference,
			ExternalAccountReference: bankAccount.ExternalAccountReference,
			Rail:                     provider.Rail(command.Rail),
			Amount:                   command.Amount,
		})
	})
	if err != nil {
		return FundingTransfer{}, err
	}

	tx, err := service.database.Begin(ctx)
	if err != nil {
		return FundingTransfer{}, fmt.Errorf("begin funding: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	if _, err := queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference); err != nil {
		return FundingTransfer{}, fmt.Errorf("lock banking profile: %w", err)
	}
	transferID, err := newUUID()
	if err != nil {
		return FundingTransfer{}, err
	}
	insertedID, err := queries.InsertFundingRequest(ctx, store.InsertFundingRequestParams{
		CustomerReference: profile.CustomerReference,
		IdempotencyKey:    command.IdempotencyKey,
		RequestHash:       requestHash,
		TransferID:        transferID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existingRequest, getErr := queries.GetFundingRequest(ctx, store.GetFundingRequestParams{
			CustomerReference: profile.CustomerReference,
			IdempotencyKey:    command.IdempotencyKey,
		})
		if getErr != nil {
			return FundingTransfer{}, fmt.Errorf("get funding idempotency: %w", getErr)
		}
		if existingRequest.RequestHash != requestHash {
			return FundingTransfer{}, ErrIdempotencyConflict
		}
		existing, getErr := queries.GetFundingTransfer(ctx, existingRequest.TransferID)
		if getErr != nil {
			return FundingTransfer{}, fmt.Errorf("get replayed funding: %w", getErr)
		}
		if err := tx.Commit(ctx); err != nil {
			return FundingTransfer{}, err
		}
		result := fundingFromStore(existing)
		result.Replayed = true
		return result, nil
	}
	if err != nil || insertedID != transferID {
		return FundingTransfer{}, fmt.Errorf("acquire funding idempotency: %w", err)
	}

	var pendingTransactionID pgtype.UUID
	if command.Rail == "ACH" {
		posting, postErr := ledger.NewService(tx).Post(ctx, ledger.PostingCommand{
			Scope:           "banking.ach.initiation",
			IdempotencyKey:  "ach.pending:" + clientReference,
			TransactionType: "ACH_FUNDING_PENDING",
			PolicyVersion:   PolicyVersion,
			EffectiveAt:     service.now().UTC(),
			Actor:           ledger.Actor{Type: "USER", ID: profile.CustomerReference},
			Entries: []ledger.Entry{
				{AccountID: profile.CashLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionPending, Direction: ledger.Debit, Amount: command.Amount},
				{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionPending, Direction: ledger.Credit, Amount: command.Amount},
			},
		})
		if postErr != nil {
			return FundingTransfer{}, fmt.Errorf("post ACH pending funds: %w", postErr)
		}
		pendingTransactionID = uuidValue(posting.TransactionID)
	}
	created, err := queries.CreateFundingTransfer(ctx, store.CreateFundingTransferParams{
		ID:                         transferID,
		CustomerReference:          profile.CustomerReference,
		BankAccountID:              bankAccountID,
		Rail:                       command.Rail,
		Amount:                     command.Amount,
		Status:                     providerResponse.InitialStatus,
		Provider:                   providerResponse.Provider,
		ProviderTransferID:         providerResponse.ProviderTransferID,
		PendingLedgerTransactionID: pendingTransactionID,
		PolicyVersion:              PolicyVersion,
	})
	if err != nil {
		return FundingTransfer{}, fmt.Errorf("create funding transfer: %w", err)
	}
	metadata, _ := json.Marshal(map[string]string{
		"amount": command.Amount.String(), "currency": "USD", "mode": "SIMULATED", "rail": command.Rail, "status": created.Status,
	})
	if err := recordMutation(ctx, tx, mutation{
		Action:        "bank.funding.initiated",
		ResourceType:  "bank.funding",
		ResourceID:    transferID.String(),
		ActorType:     "USER",
		ActorID:       profile.CustomerReference,
		Metadata:      metadata,
		AggregateType: "bank.funding",
		AggregateID:   transferID.String(),
		EventType:     "bank.funding.initiated",
		Payload:       metadata,
	}); err != nil {
		return FundingTransfer{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return FundingTransfer{}, fmt.Errorf("commit funding: %w", err)
	}
	return fundingFromStore(created), nil
}

func (service *Service) ApplyFundingEvent(ctx context.Context, event provider.Event) (FundingTransfer, error) {
	if event.ResourceType != "FUNDING" || event.Provider == "" || event.ExternalEventID == "" || len(event.Payload) == 0 {
		return FundingTransfer{}, ErrInvalidCommand
	}
	transferID, err := parseUUID(event.ResourceID)
	if err != nil {
		return FundingTransfer{}, ErrTransferNotFound
	}
	payloadHash, err := hashValue(struct {
		EventType string `json:"event_type"`
		Reason    string `json:"reason"`
		Payload   string `json:"payload"`
	}{event.EventType, event.ReasonCode, string(event.Payload)})
	if err != nil {
		return FundingTransfer{}, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return FundingTransfer{}, fmt.Errorf("begin funding callback: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	_, err = queries.InsertProviderEvent(ctx, store.InsertProviderEventParams{
		Provider:        event.Provider,
		ExternalEventID: event.ExternalEventID,
		ResourceType:    event.ResourceType,
		ResourceID:      transferID,
		EventType:       event.EventType,
		PayloadHash:     payloadHash,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existingEvent, getErr := queries.GetProviderEvent(ctx, store.GetProviderEventParams{Provider: event.Provider, ExternalEventID: event.ExternalEventID})
		if getErr != nil {
			return FundingTransfer{}, fmt.Errorf("get funding callback: %w", getErr)
		}
		if existingEvent.PayloadHash != payloadHash || existingEvent.ResourceID != transferID {
			return FundingTransfer{}, ErrProviderEventConflict
		}
		existing, getErr := queries.GetFundingTransfer(ctx, transferID)
		if getErr != nil {
			return FundingTransfer{}, ErrTransferNotFound
		}
		if err := tx.Commit(ctx); err != nil {
			return FundingTransfer{}, err
		}
		result := fundingFromStore(existing)
		result.Replayed = true
		return result, nil
	}
	if err != nil {
		return FundingTransfer{}, fmt.Errorf("insert funding callback: %w", err)
	}
	transfer, err := queries.GetFundingTransferForUpdate(ctx, transferID)
	if errors.Is(err, pgx.ErrNoRows) {
		return FundingTransfer{}, ErrTransferNotFound
	}
	if err != nil {
		return FundingTransfer{}, fmt.Errorf("lock funding transfer: %w", err)
	}
	profile, err := queries.GetCustomerProfileForUpdate(ctx, transfer.CustomerReference)
	if err != nil {
		return FundingTransfer{}, fmt.Errorf("lock funding profile: %w", err)
	}
	bankAccount, err := queries.GetBankAccountForUpdate(ctx, transfer.BankAccountID)
	if err != nil {
		return FundingTransfer{}, fmt.Errorf("lock funding bank account: %w", err)
	}
	updated, ledgerTransactionID, err := service.applyFundingTransition(ctx, tx, queries, profile, bankAccount, transfer, event)
	if err != nil {
		return FundingTransfer{}, err
	}
	if ledgerTransactionID.Valid {
		// The immutable banking callback row is the application inbox. Ledger has
		// its own provider-event uniqueness constraint for money-impacting callbacks.
		// The reference is retained here for reconciliation and incident review.
		_ = ledgerTransactionID
	}
	metadata, _ := json.Marshal(map[string]string{
		"event_type": event.EventType, "mode": "SIMULATED", "reason_code": event.ReasonCode, "status": updated.Status,
	})
	if err := recordMutation(ctx, tx, mutation{
		Action:        "bank.funding." + strings.ToLower(updated.Status),
		ResourceType:  "bank.funding",
		ResourceID:    transferID.String(),
		ActorType:     "PROVIDER",
		ActorID:       event.Provider,
		Metadata:      metadata,
		AggregateType: "bank.funding",
		AggregateID:   transferID.String(),
		EventType:     "bank.funding." + strings.ToLower(updated.Status),
		EventVersion:  int32(updated.Version),
		Payload:       metadata,
	}); err != nil {
		return FundingTransfer{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return FundingTransfer{}, fmt.Errorf("commit funding callback: %w", err)
	}
	return fundingFromStore(updated), nil
}

func (service *Service) applyFundingTransition(
	ctx context.Context,
	tx pgx.Tx,
	queries *store.Queries,
	profile store.BankingCustomerProfile,
	bankAccount store.BankingBankAccount,
	transfer store.BankingFundingTransfer,
	event provider.Event,
) (store.BankingFundingTransfer, pgtype.UUID, error) {
	params := store.TransitionFundingTransferParams{ID: transfer.ID, ReasonCode: textValue(event.ReasonCode)}
	var financialID pgtype.UUID
	switch event.EventType {
	case "ACH_PROCESSING":
		if transfer.Rail != "ACH" {
			return transfer, financialID, ErrInvalidState
		}
		params.Status = "PROCESSING"
	case "ACH_SETTLED":
		if transfer.Rail != "ACH" || !transfer.PendingLedgerTransactionID.Valid {
			return transfer, financialID, ErrInvalidState
		}
		release, err := service.postPendingRelease(ctx, tx, profile, transfer, event)
		if err != nil {
			return transfer, financialID, err
		}
		settled, err := service.postFundingSettled(ctx, tx, profile, transfer, event, "ACH_FUNDING_SETTLED")
		if err != nil {
			return transfer, financialID, err
		}
		params.Status = "SETTLED"
		params.PendingReleaseTransactionID = uuidValue(release.TransactionID)
		params.SettledLedgerTransactionID = uuidValue(settled.TransactionID)
		financialID = uuidValue(settled.TransactionID)
		if _, err := queries.MarkBankAccountFunded(ctx, bankAccount.ID); err != nil {
			return transfer, financialID, fmt.Errorf("mark ACH account funded: %w", err)
		}
	case "ACH_RETURNED", "ACH_CANCELED":
		if transfer.Rail != "ACH" {
			return transfer, financialID, ErrInvalidState
		}
		original := transfer.PendingLedgerTransactionID
		if transfer.Status == "SETTLED" {
			original = transfer.SettledLedgerTransactionID
		}
		if !original.Valid {
			return transfer, financialID, ErrInvalidState
		}
		reversal, err := ledger.NewService(tx).Reverse(ctx, ledger.ReversalCommand{
			Scope:                 "banking.ach.return",
			IdempotencyKey:        "ach.return:" + event.ExternalEventID,
			OriginalTransactionID: original.String(),
			ReasonCode:            reasonOrDefault(event.ReasonCode, "ACH_RETURN"),
			PolicyVersion:         PolicyVersion,
			EffectiveAt:           service.now().UTC(),
			Actor:                 ledger.Actor{Type: "PROVIDER", ID: event.Provider},
		})
		if err != nil {
			return transfer, financialID, fmt.Errorf("reverse ACH funding: %w", err)
		}
		params.Status = "RETURNED"
		if event.EventType == "ACH_CANCELED" {
			params.Status = "CANCELED"
		}
		params.ReturnReversalTransactionID = uuidValue(reversal.TransactionID)
		financialID = uuidValue(reversal.TransactionID)
		if _, err := queries.ClearBankAccountFunding(ctx, bankAccount.ID); err != nil {
			return transfer, financialID, fmt.Errorf("clear returned funding preference: %w", err)
		}
	case "WIRE_FUNDS_DETECTED":
		if transfer.Rail != "WIRE" {
			return transfer, financialID, ErrInvalidState
		}
		params.Status = "FUNDS_DETECTED"
	case "WIRE_OWNERSHIP_REVIEW":
		if transfer.Rail != "WIRE" {
			return transfer, financialID, ErrInvalidState
		}
		params.Status = "OWNERSHIP_REVIEW"
	case "WIRE_CREDITED":
		if transfer.Rail != "WIRE" || bankAccount.OwnerRelation != "SAME_NAME" || bankAccount.OwnershipStatus != "VERIFIED" {
			return transfer, financialID, ErrOwnershipVerification
		}
		settled, err := service.postFundingSettled(ctx, tx, profile, transfer, event, "WIRE_FUNDING_CREDITED")
		if err != nil {
			return transfer, financialID, err
		}
		params.Status = "CREDITED"
		params.SettledLedgerTransactionID = uuidValue(settled.TransactionID)
		financialID = uuidValue(settled.TransactionID)
		if _, err := queries.MarkBankAccountFunded(ctx, bankAccount.ID); err != nil {
			return transfer, financialID, fmt.Errorf("mark Wire account funded: %w", err)
		}
	case "WIRE_NAME_MISMATCH", "WIRE_THIRD_PARTY_FUNDS", "WIRE_MISSING_REFERENCE", "WIRE_MANUAL_REVIEW":
		if transfer.Rail != "WIRE" {
			return transfer, financialID, ErrInvalidState
		}
		params.Status = map[string]string{
			"WIRE_NAME_MISMATCH": "NAME_MISMATCH", "WIRE_THIRD_PARTY_FUNDS": "THIRD_PARTY_FUNDS",
			"WIRE_MISSING_REFERENCE": "MISSING_REFERENCE", "WIRE_MANUAL_REVIEW": "MANUAL_REVIEW",
		}[event.EventType]
		if _, err := createCase(ctx, tx, transfer.CustomerReference, "TRANSACTION_MONITORING_ALERT", "bank.funding", transfer.ID.String(), reasonOrDefault(event.ReasonCode, params.Status), "Review the requested bank information in the secure case center.", "IN_REVIEW"); err != nil {
			return transfer, financialID, err
		}
	case "WIRE_RETURN_REQUIRED":
		if transfer.Rail != "WIRE" {
			return transfer, financialID, ErrInvalidState
		}
		params.Status = "RETURN_REQUIRED"
	default:
		return transfer, financialID, ErrInvalidCommand
	}
	updated, err := queries.TransitionFundingTransfer(ctx, params)
	if err != nil {
		return transfer, financialID, fmt.Errorf("transition funding transfer: %w", err)
	}
	return updated, financialID, nil
}

func (service *Service) postPendingRelease(ctx context.Context, tx pgx.Tx, profile store.BankingCustomerProfile, transfer store.BankingFundingTransfer, event provider.Event) (ledger.PostingResult, error) {
	return ledger.NewService(tx).Post(ctx, ledger.PostingCommand{
		Scope:           "banking.ach.pending-release",
		IdempotencyKey:  "ach.release:" + event.ExternalEventID,
		TransactionType: "ACH_PENDING_RELEASED",
		PolicyVersion:   PolicyVersion,
		EffectiveAt:     service.now().UTC(),
		Actor:           ledger.Actor{Type: "PROVIDER", ID: event.Provider},
		Entries: []ledger.Entry{
			{AccountID: profile.CashLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionPending, Direction: ledger.Credit, Amount: transfer.Amount},
			{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionPending, Direction: ledger.Debit, Amount: transfer.Amount},
		},
	})
}

func (service *Service) postFundingSettled(ctx context.Context, tx pgx.Tx, profile store.BankingCustomerProfile, transfer store.BankingFundingTransfer, event provider.Event, transactionType string) (ledger.PostingResult, error) {
	return ledger.NewService(tx).Post(ctx, ledger.PostingCommand{
		Scope:           "banking.funding.settled",
		IdempotencyKey:  "funding.settled:" + event.ExternalEventID,
		TransactionType: transactionType,
		PolicyVersion:   PolicyVersion,
		EffectiveAt:     service.now().UTC(),
		Actor:           ledger.Actor{Type: "PROVIDER", ID: event.Provider},
		ProviderEvent:   &ledger.ProviderEvent{Provider: event.Provider, ExternalEventID: event.ExternalEventID, Payload: event.Payload},
		Entries: []ledger.Entry{
			{AccountID: profile.CashLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: transfer.Amount},
			{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: transfer.Amount},
			{AccountID: profile.CashLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionWithdrawable, Direction: ledger.Debit, Amount: transfer.Amount},
			{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionWithdrawable, Direction: ledger.Credit, Amount: transfer.Amount},
		},
	})
}

func (service *Service) ListFundingTransfers(ctx context.Context, accessToken string, pageSize int32) ([]FundingTransfer, error) {
	_, profile, err := service.resolveCustomer(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	rows, err := store.New(service.database).ListFundingTransfers(ctx, store.ListFundingTransfersParams{CustomerReference: profile.CustomerReference, PageSize: pageSize})
	if err != nil {
		return nil, fmt.Errorf("list funding transfers: %w", err)
	}
	result := make([]FundingTransfer, 0, len(rows))
	for _, row := range rows {
		result = append(result, fundingFromStore(row))
	}
	return result, nil
}

func fundingFromStore(transfer store.BankingFundingTransfer) FundingTransfer {
	return FundingTransfer{
		ID:                 transfer.ID.String(),
		BankAccountID:      transfer.BankAccountID.String(),
		Rail:               transfer.Rail,
		Amount:             transfer.Amount,
		Status:             transfer.Status,
		ProviderTransferID: transfer.ProviderTransferID,
		ReasonCode:         transfer.ReasonCode.String,
		Pending:            transfer.Rail == "ACH" && (transfer.Status == "INITIATED" || transfer.Status == "PROCESSING"),
		Settled:            transfer.Status == "SETTLED" || transfer.Status == "CREDITED",
		CreatedAt:          transfer.CreatedAt.Time.UTC(),
		UpdatedAt:          transfer.UpdatedAt.Time.UTC(),
	}
}

func reasonOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
