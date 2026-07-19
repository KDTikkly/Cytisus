package banking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KDTikkly/Cytisus/internal/banking/provider"
	"github.com/KDTikkly/Cytisus/internal/banking/store"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (service *Service) RequestWithdrawal(ctx context.Context, command RequestWithdrawalCommand) (Withdrawal, error) {
	_, profile, err := service.resolveCustomer(ctx, command.AccessToken)
	if err != nil {
		return Withdrawal{}, err
	}
	if !idempotencyKeyPattern.MatchString(command.IdempotencyKey) || !command.Amount.IsPositive() {
		return Withdrawal{}, ErrInvalidCommand
	}
	queries := store.New(service.database)
	var bankAccount store.BankingBankAccount
	if command.BankAccountID == "" {
		bankAccount, err = queries.GetPreferredWithdrawalAccount(ctx, profile.CustomerReference)
		if errors.Is(err, pgx.ErrNoRows) {
			return Withdrawal{}, ErrBankAccountNotFound
		}
	} else {
		bankAccountID, parseErr := parseUUID(command.BankAccountID)
		if parseErr != nil {
			return Withdrawal{}, ErrBankAccountNotFound
		}
		bankAccount, err = queries.GetBankAccount(ctx, bankAccountID)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && bankAccount.CustomerReference != profile.CustomerReference {
			return Withdrawal{}, ErrBankAccountNotFound
		}
	}
	if err != nil {
		return Withdrawal{}, fmt.Errorf("get withdrawal bank account: %w", err)
	}
	if bankAccount.Status != "ACTIVE" || (bankAccount.RailSupport != "ACH" && bankAccount.RailSupport != "WIRE" && bankAccount.RailSupport != "BOTH") {
		return Withdrawal{}, ErrInvalidState
	}
	requestHash, err := hashValue(struct {
		BankAccountID string `json:"bank_account_id"`
		Amount        string `json:"amount"`
	}{bankAccount.ID.String(), command.Amount.String()})
	if err != nil {
		return Withdrawal{}, err
	}
	clientReference := deterministicReference("withdrawal", profile.CustomerReference, command.IdempotencyKey)
	providerResponse, err := service.callProvider(ctx, func(providerContext context.Context) (provider.TransferResponse, error) {
		rail := provider.RailACH
		if bankAccount.RailSupport == "WIRE" {
			rail = provider.RailWire
		}
		return service.provider.PrepareWithdrawal(providerContext, provider.TransferRequest{
			ClientReference:          clientReference,
			ExternalAccountReference: bankAccount.ExternalAccountReference,
			Rail:                     rail,
			Amount:                   command.Amount,
		})
	})
	if err != nil {
		return Withdrawal{}, err
	}

	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Withdrawal{}, fmt.Errorf("begin withdrawal: %w", err)
	}
	defer tx.Rollback(ctx)
	queries = store.New(tx)
	profile, err = queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference)
	if err != nil {
		return Withdrawal{}, fmt.Errorf("lock withdrawal profile: %w", err)
	}
	bankAccount, err = queries.GetBankAccountForUpdate(ctx, bankAccount.ID)
	if err != nil {
		return Withdrawal{}, fmt.Errorf("lock withdrawal bank account: %w", err)
	}
	withdrawalID, err := newUUID()
	if err != nil {
		return Withdrawal{}, err
	}
	insertedID, err := queries.InsertWithdrawalRequest(ctx, store.InsertWithdrawalRequestParams{
		CustomerReference: profile.CustomerReference,
		IdempotencyKey:    command.IdempotencyKey,
		RequestHash:       requestHash,
		WithdrawalID:      withdrawalID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existingRequest, getErr := queries.GetWithdrawalRequest(ctx, store.GetWithdrawalRequestParams{
			CustomerReference: profile.CustomerReference,
			IdempotencyKey:    command.IdempotencyKey,
		})
		if getErr != nil {
			return Withdrawal{}, fmt.Errorf("get withdrawal idempotency: %w", getErr)
		}
		if existingRequest.RequestHash != requestHash {
			return Withdrawal{}, ErrIdempotencyConflict
		}
		existing, getErr := queries.GetWithdrawal(ctx, existingRequest.WithdrawalID)
		if getErr != nil {
			return Withdrawal{}, fmt.Errorf("get replayed withdrawal: %w", getErr)
		}
		if err := tx.Commit(ctx); err != nil {
			return Withdrawal{}, err
		}
		result := withdrawalFromStore(existing)
		result.Replayed = true
		return result, nil
	}
	if err != nil || insertedID != withdrawalID {
		return Withdrawal{}, fmt.Errorf("acquire withdrawal idempotency: %w", err)
	}

	status := "SECURITY_VERIFICATION"
	reasonCode := "OWNERSHIP_VERIFICATION_REQUIRED"
	nextAction := "Complete bank ownership verification, then return to this withdrawal."
	var caseID, reservationID pgtype.UUID
	var coolingUntil = bankAccount.CoolingUntil
	if bankAccount.OwnerRelation == "THIRD_PARTY" {
		status = "REJECTED"
		reasonCode = "THIRD_PARTY_WITHDRAWAL_DISABLED"
		nextAction = "Choose a verified bank account held in your own name."
		complianceCase, caseErr := createCase(ctx, tx, profile.CustomerReference, "BANK_WITHDRAWAL_REVIEW", "bank.withdrawal", withdrawalID.String(), reasonCode, nextAction, "REJECTED")
		if caseErr != nil {
			return Withdrawal{}, caseErr
		}
		caseID = complianceCase.ID
	} else if bankAccount.OwnershipStatus == "VERIFIED" && bankAccount.SuccessfullyFundedAt.Valid && bankAccount.RiskClass == "STANDARD" {
		status = "APPROVED"
		reasonCode = "CLOSED_LOOP_ACCOUNT_APPROVED"
		nextAction = "The withdrawal is approved and ready for bank submission."
		pending := store.BankingWithdrawal{ID: withdrawalID, CustomerReference: profile.CustomerReference, BankAccountID: bankAccount.ID, Amount: command.Amount}
		reservationID, err = service.reserveWithdrawal(ctx, tx, profile, pending, ledger.Actor{Type: "USER", ID: profile.CustomerReference})
		if err != nil {
			return Withdrawal{}, err
		}
	} else if bankAccount.OwnershipStatus == "VERIFIED" {
		if !coolingUntil.Valid {
			coolingUntil = timestamptz(dynamicCooling(service.now(), bankAccount.RiskClass))
		}
		status = "COOLING_OFF"
		reasonCode = "NEW_WITHDRAWAL_ACCOUNT_REVIEW"
		nextAction = "Wait until the displayed cooling period ends while enhanced review is completed."
		if !service.now().UTC().Before(coolingUntil.Time.UTC()) {
			status = "MANUAL_REVIEW"
			nextAction = "Enhanced review is in progress. Monitor the case center for the decision."
		}
		complianceCase, caseErr := createCase(ctx, tx, profile.CustomerReference, "BANK_WITHDRAWAL_REVIEW", "bank.withdrawal", withdrawalID.String(), reasonCode, nextAction, "IN_REVIEW")
		if caseErr != nil {
			return Withdrawal{}, caseErr
		}
		caseID = complianceCase.ID
	}
	created, err := queries.CreateWithdrawal(ctx, store.CreateWithdrawalParams{
		ID: withdrawalID, CustomerReference: profile.CustomerReference, BankAccountID: bankAccount.ID,
		Amount: command.Amount, Status: status, ReasonCode: reasonCode, NextAction: nextAction,
		ComplianceCaseID: caseID, CoolingUntil: coolingUntil, ReservationLedgerTransactionID: reservationID,
		Provider: providerResponse.Provider, ProviderTransferID: providerResponse.ProviderTransferID, PolicyVersion: PolicyVersion,
	})
	if err != nil {
		return Withdrawal{}, fmt.Errorf("create withdrawal: %w", err)
	}
	metadata, _ := json.Marshal(map[string]string{
		"amount": command.Amount.String(), "currency": "USD", "mode": "SIMULATED", "reason_code": reasonCode, "status": status,
	})
	if err := recordMutation(ctx, tx, mutation{
		Action: "bank.withdrawal.requested", ResourceType: "bank.withdrawal", ResourceID: withdrawalID.String(),
		ActorType: "USER", ActorID: profile.CustomerReference, Metadata: metadata,
		AggregateType: "bank.withdrawal", AggregateID: withdrawalID.String(), EventType: "bank.withdrawal.requested", Payload: metadata,
	}); err != nil {
		return Withdrawal{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Withdrawal{}, fmt.Errorf("commit withdrawal: %w", err)
	}
	result := withdrawalFromStore(created)
	if status == "REJECTED" {
		return result, ErrThirdPartyDisabled
	}
	return result, nil
}

func (service *Service) reserveWithdrawal(ctx context.Context, tx pgx.Tx, profile store.BankingCustomerProfile, withdrawal store.BankingWithdrawal, actor ledger.Actor) (pgtype.UUID, error) {
	ledgerService := ledger.NewService(tx)
	settled, err := ledgerService.Balance(ctx, profile.CashLedgerAccountID.String(), money.Currency("USD"), ledger.DimensionSettled)
	if err != nil {
		return pgtype.UUID{}, err
	}
	withdrawable, err := ledgerService.Balance(ctx, profile.CashLedgerAccountID.String(), money.Currency("USD"), ledger.DimensionWithdrawable)
	if err != nil {
		return pgtype.UUID{}, err
	}
	if settled.Compare(withdrawal.Amount) < 0 || withdrawable.Compare(withdrawal.Amount) < 0 {
		return pgtype.UUID{}, ErrInsufficientWithdrawable
	}
	posting, err := ledgerService.Post(ctx, ledger.PostingCommand{
		Scope: "banking.withdrawal.reserve", IdempotencyKey: "withdrawal.reserve:" + withdrawal.ID.String(),
		TransactionType: "BANK_WITHDRAWAL_RESERVED", PolicyVersion: PolicyVersion, EffectiveAt: service.now().UTC(), Actor: actor,
		Entries: []ledger.Entry{
			{AccountID: profile.CashLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionWithdrawable, Direction: ledger.Credit, Amount: withdrawal.Amount},
			{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionWithdrawable, Direction: ledger.Debit, Amount: withdrawal.Amount},
			{AccountID: profile.CashLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionHeld, Direction: ledger.Debit, Amount: withdrawal.Amount},
			{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionHeld, Direction: ledger.Credit, Amount: withdrawal.Amount},
		},
	})
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("reserve bank withdrawal: %w", err)
	}
	return uuidValue(posting.TransactionID), nil
}

func (service *Service) ApplyWithdrawalEvent(ctx context.Context, event provider.Event) (Withdrawal, error) {
	if event.ResourceType != "WITHDRAWAL" || event.Provider == "" || event.ExternalEventID == "" || len(event.Payload) == 0 {
		return Withdrawal{}, ErrInvalidCommand
	}
	withdrawalID, err := parseUUID(event.ResourceID)
	if err != nil {
		return Withdrawal{}, ErrWithdrawalNotFound
	}
	payloadHash, err := hashValue(struct {
		EventType string `json:"event_type"`
		Reason    string `json:"reason"`
		Payload   string `json:"payload"`
	}{event.EventType, event.ReasonCode, string(event.Payload)})
	if err != nil {
		return Withdrawal{}, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Withdrawal{}, fmt.Errorf("begin withdrawal callback: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	_, err = queries.InsertProviderEvent(ctx, store.InsertProviderEventParams{
		Provider: event.Provider, ExternalEventID: event.ExternalEventID, ResourceType: event.ResourceType,
		ResourceID: withdrawalID, EventType: event.EventType, PayloadHash: payloadHash,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existingEvent, getErr := queries.GetProviderEvent(ctx, store.GetProviderEventParams{Provider: event.Provider, ExternalEventID: event.ExternalEventID})
		if getErr != nil {
			return Withdrawal{}, fmt.Errorf("get withdrawal callback: %w", getErr)
		}
		if existingEvent.PayloadHash != payloadHash || existingEvent.ResourceID != withdrawalID {
			return Withdrawal{}, ErrProviderEventConflict
		}
		existing, getErr := queries.GetWithdrawal(ctx, withdrawalID)
		if getErr != nil {
			return Withdrawal{}, ErrWithdrawalNotFound
		}
		if err := tx.Commit(ctx); err != nil {
			return Withdrawal{}, err
		}
		result := withdrawalFromStore(existing)
		result.Replayed = true
		return result, nil
	}
	if err != nil {
		return Withdrawal{}, fmt.Errorf("insert withdrawal callback: %w", err)
	}
	withdrawal, err := queries.GetWithdrawalForUpdate(ctx, withdrawalID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Withdrawal{}, ErrWithdrawalNotFound
	}
	if err != nil {
		return Withdrawal{}, fmt.Errorf("lock withdrawal: %w", err)
	}
	profile, err := queries.GetCustomerProfileForUpdate(ctx, withdrawal.CustomerReference)
	if err != nil {
		return Withdrawal{}, fmt.Errorf("lock withdrawal profile: %w", err)
	}
	params := store.TransitionWithdrawalParams{ID: withdrawalID}
	switch event.EventType {
	case "WITHDRAWAL_SUBMITTED":
		if withdrawal.Status != "APPROVED" || !withdrawal.ReservationLedgerTransactionID.Valid {
			return Withdrawal{}, ErrInvalidState
		}
		posting, postErr := ledger.NewService(tx).Post(ctx, ledger.PostingCommand{
			Scope: "banking.withdrawal.submit", IdempotencyKey: "withdrawal.submit:" + event.ExternalEventID,
			TransactionType: "BANK_WITHDRAWAL_SUBMITTED", PolicyVersion: PolicyVersion, EffectiveAt: service.now().UTC(),
			Actor:         ledger.Actor{Type: "PROVIDER", ID: event.Provider},
			ProviderEvent: &ledger.ProviderEvent{Provider: event.Provider, ExternalEventID: event.ExternalEventID, Payload: event.Payload},
			Entries: []ledger.Entry{
				{AccountID: profile.CashLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: withdrawal.Amount},
				{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: withdrawal.Amount},
				{AccountID: profile.CashLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionHeld, Direction: ledger.Credit, Amount: withdrawal.Amount},
				{AccountID: profile.ProviderClearingLedgerAccountID.String(), Currency: money.Currency("USD"), Dimension: ledger.DimensionHeld, Direction: ledger.Debit, Amount: withdrawal.Amount},
			},
		})
		if postErr != nil {
			return Withdrawal{}, fmt.Errorf("post submitted withdrawal: %w", postErr)
		}
		params.Status = "SUBMITTED_TO_BANK"
		params.ReasonCode = "SUBMITTED_TO_BANK"
		params.NextAction = "The bank simulator accepted the withdrawal. Track it until settlement."
		params.SubmittedLedgerTransactionID = uuidValue(posting.TransactionID)
	case "WITHDRAWAL_PROCESSING":
		params.Status = "PROCESSING"
		params.ReasonCode = "BANK_PROCESSING"
		params.NextAction = "No action is needed while the bank processes the withdrawal."
	case "WITHDRAWAL_SETTLED":
		params.Status = "SETTLED"
		params.ReasonCode = "WITHDRAWAL_SETTLED"
		params.NextAction = "No further action is required."
	case "WITHDRAWAL_RETURNED":
		if !withdrawal.SubmittedLedgerTransactionID.Valid || !withdrawal.ReservationLedgerTransactionID.Valid {
			return Withdrawal{}, ErrInvalidState
		}
		submittedReversal, reverseErr := ledger.NewService(tx).Reverse(ctx, ledger.ReversalCommand{
			Scope: "banking.withdrawal.return-submitted", IdempotencyKey: "withdrawal.return.submitted:" + event.ExternalEventID,
			OriginalTransactionID: withdrawal.SubmittedLedgerTransactionID.String(), ReasonCode: reasonOrDefault(event.ReasonCode, "BANK_RETURN"),
			PolicyVersion: PolicyVersion, EffectiveAt: service.now().UTC(), Actor: ledger.Actor{Type: "PROVIDER", ID: event.Provider},
		})
		if reverseErr != nil {
			return Withdrawal{}, fmt.Errorf("reverse submitted withdrawal: %w", reverseErr)
		}
		reservationReversal, reverseErr := ledger.NewService(tx).Reverse(ctx, ledger.ReversalCommand{
			Scope: "banking.withdrawal.return-reservation", IdempotencyKey: "withdrawal.return.reservation:" + event.ExternalEventID,
			OriginalTransactionID: withdrawal.ReservationLedgerTransactionID.String(), ReasonCode: reasonOrDefault(event.ReasonCode, "BANK_RETURN"),
			PolicyVersion: PolicyVersion, EffectiveAt: service.now().UTC(), Actor: ledger.Actor{Type: "PROVIDER", ID: event.Provider},
		})
		if reverseErr != nil {
			return Withdrawal{}, fmt.Errorf("reverse withdrawal reservation: %w", reverseErr)
		}
		params.Status = "RETURNED"
		params.ReasonCode = reasonOrDefault(event.ReasonCode, "BANK_RETURN")
		params.NextAction = "Review the bank return reason and choose a verified same-name account before retrying."
		params.ReturnSubmittedReversalID = uuidValue(submittedReversal.TransactionID)
		params.ReturnReservationReversalID = uuidValue(reservationReversal.TransactionID)
	default:
		return Withdrawal{}, ErrInvalidCommand
	}
	updated, err := queries.TransitionWithdrawal(ctx, params)
	if err != nil {
		return Withdrawal{}, fmt.Errorf("transition withdrawal: %w", err)
	}
	metadata, _ := json.Marshal(map[string]string{"event_type": event.EventType, "mode": "SIMULATED", "reason_code": params.ReasonCode, "status": updated.Status})
	if err := recordMutation(ctx, tx, mutation{
		Action: "bank.withdrawal." + strings.ToLower(updated.Status), ResourceType: "bank.withdrawal", ResourceID: withdrawalID.String(),
		ActorType: "PROVIDER", ActorID: event.Provider, Metadata: metadata,
		AggregateType: "bank.withdrawal", AggregateID: withdrawalID.String(), EventType: "bank.withdrawal." + strings.ToLower(updated.Status),
		EventVersion: int32(updated.Version), Payload: metadata,
	}); err != nil {
		return Withdrawal{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Withdrawal{}, fmt.Errorf("commit withdrawal callback: %w", err)
	}
	return withdrawalFromStore(updated), nil
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
		return nil, fmt.Errorf("list withdrawals: %w", err)
	}
	result := make([]Withdrawal, 0, len(rows))
	for _, row := range rows {
		result = append(result, withdrawalFromStore(row))
	}
	return result, nil
}

func withdrawalFromStore(withdrawal store.BankingWithdrawal) Withdrawal {
	var coolingUntil *time.Time
	if withdrawal.CoolingUntil.Valid {
		value := withdrawal.CoolingUntil.Time.UTC()
		coolingUntil = &value
	}
	return Withdrawal{
		ID: withdrawal.ID.String(), BankAccountID: withdrawal.BankAccountID.String(), Amount: withdrawal.Amount,
		Status: withdrawal.Status, ReasonCode: withdrawal.ReasonCode, NextAction: withdrawal.NextAction,
		ComplianceCaseID: withdrawal.ComplianceCaseID.String(), CoolingUntil: coolingUntil,
		ProviderTransferID: withdrawal.ProviderTransferID, CreatedAt: withdrawal.CreatedAt.Time.UTC(), UpdatedAt: withdrawal.UpdatedAt.Time.UTC(),
	}
}
