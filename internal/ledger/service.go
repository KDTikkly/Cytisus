package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/KDTikkly/Cytisus/internal/ledger/store"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const defaultOutboxMaxAttempts = 8

type database interface {
	store.DBTX
	Begin(context.Context) (pgx.Tx, error)
}

type Service struct {
	database database
}

func NewService(database database) *Service {
	return &Service{database: database}
}

func (service *Service) OpenAccount(ctx context.Context, command AccountCommand) (Account, error) {
	if service == nil || service.database == nil {
		return Account{}, fmt.Errorf("open account: %w: database is required", ErrInvalidCommand)
	}
	if err := command.validate(); err != nil {
		return Account{}, err
	}

	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Account{}, fmt.Errorf("begin account transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	created, err := queries.CreateLedgerAccount(ctx, store.CreateLedgerAccountParams{
		AccountKey:  command.AccountKey,
		OwnerType:   command.OwnerType,
		OwnerID:     command.OwnerID,
		AccountType: command.AccountType,
		Currency:    string(command.Currency),
		NormalSide:  string(command.NormalSide),
	})
	if err != nil {
		return Account{}, fmt.Errorf("create ledger account: %w", err)
	}
	accountID := created.ID.String()
	metadata, err := json.Marshal(struct {
		AccountType string `json:"account_type"`
		Currency    string `json:"currency"`
	}{command.AccountType, string(command.Currency)})
	if err != nil {
		return Account{}, fmt.Errorf("encode account audit metadata: %w", err)
	}
	if err := writeAuditAndOutbox(ctx, queries, mutationRecord{
		Action:        "ledger.account.created",
		ResourceType:  "ledger.account",
		ResourceID:    accountID,
		Actor:         command.Actor,
		Metadata:      metadata,
		AggregateType: "ledger.account",
		AggregateID:   accountID,
		EventType:     "ledger.account.created",
		Payload:       metadata,
	}); err != nil {
		return Account{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Account{}, fmt.Errorf("commit account transaction: %w", err)
	}
	return Account{
		ID:          accountID,
		AccountKey:  created.AccountKey,
		OwnerType:   created.OwnerType,
		OwnerID:     created.OwnerID,
		AccountType: created.AccountType,
		Currency:    money.Currency(created.Currency),
		NormalSide:  Direction(created.NormalSide),
	}, nil
}

func (service *Service) Post(ctx context.Context, command PostingCommand) (PostingResult, error) {
	if service == nil || service.database == nil {
		return PostingResult{}, fmt.Errorf("post ledger transaction: %w: database is required", ErrInvalidCommand)
	}
	if err := command.validate(); err != nil {
		return PostingResult{}, err
	}
	requestHash, err := hashPostingCommand(command)
	if err != nil {
		return PostingResult{}, err
	}
	transactionID, err := newUUID()
	if err != nil {
		return PostingResult{}, err
	}

	tx, err := service.database.Begin(ctx)
	if err != nil {
		return PostingResult{}, fmt.Errorf("begin ledger transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)

	existing, acquired, err := acquireRequest(ctx, queries, command.Scope, command.IdempotencyKey, requestHash, transactionID)
	if err != nil {
		return PostingResult{}, err
	}
	if !acquired {
		return PostingResult{TransactionID: existing.String(), Replayed: true}, nil
	}
	if command.ProviderEvent != nil {
		existing, acquired, err = acquireProviderEvent(ctx, queries, *command.ProviderEvent, transactionID)
		if err != nil {
			return PostingResult{}, err
		}
		if !acquired {
			return PostingResult{TransactionID: existing.String(), Replayed: true}, nil
		}
	}

	if _, err := queries.InsertLedgerTransaction(ctx, store.InsertLedgerTransactionParams{
		ID:              transactionID,
		TransactionType: command.TransactionType,
		PolicyVersion:   command.PolicyVersion,
		EffectiveAt:     pgtype.Timestamptz{Time: command.EffectiveAt.UTC(), Valid: true},
	}); err != nil {
		return PostingResult{}, fmt.Errorf("insert ledger transaction: %w", err)
	}
	for index, entry := range command.Entries {
		accountID, parseErr := parseUUID(entry.AccountID)
		if parseErr != nil {
			return PostingResult{}, fmt.Errorf("parse account ID: %w", parseErr)
		}
		if _, err := queries.InsertLedgerEntry(ctx, store.InsertLedgerEntryParams{
			TransactionID:    transactionID,
			EntrySequence:    int16(index + 1),
			AccountID:        accountID,
			Currency:         string(entry.Currency),
			BalanceDimension: string(entry.Dimension),
			Direction:        string(entry.Direction),
			Amount:           entry.Amount,
		}); err != nil {
			return PostingResult{}, fmt.Errorf("insert ledger entry %d: %w", index+1, err)
		}
	}

	transactionIDText := transactionID.String()
	metadata, err := json.Marshal(struct {
		EntryCount      int    `json:"entry_count"`
		PolicyVersion   string `json:"policy_version"`
		TransactionType string `json:"transaction_type"`
	}{len(command.Entries), command.PolicyVersion, command.TransactionType})
	if err != nil {
		return PostingResult{}, fmt.Errorf("encode posting audit metadata: %w", err)
	}
	payload, err := json.Marshal(struct {
		LedgerTransactionID string `json:"ledger_transaction_id"`
		TransactionType     string `json:"transaction_type"`
	}{transactionIDText, command.TransactionType})
	if err != nil {
		return PostingResult{}, fmt.Errorf("encode posting event: %w", err)
	}
	if err := writeAuditAndOutbox(ctx, queries, mutationRecord{
		Action:        "ledger.transaction.posted",
		ResourceType:  "ledger.transaction",
		ResourceID:    transactionIDText,
		Actor:         command.Actor,
		CorrelationID: transactionID,
		Metadata:      metadata,
		AggregateType: "ledger.transaction",
		AggregateID:   transactionIDText,
		EventType:     "ledger.transaction.posted",
		Payload:       payload,
	}); err != nil {
		return PostingResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PostingResult{}, fmt.Errorf("commit ledger transaction: %w", err)
	}
	return PostingResult{TransactionID: transactionIDText}, nil
}

func (service *Service) Reverse(ctx context.Context, command ReversalCommand) (PostingResult, error) {
	if service == nil || service.database == nil {
		return PostingResult{}, fmt.Errorf("reverse ledger transaction: %w: database is required", ErrInvalidCommand)
	}
	if err := command.validate(); err != nil {
		return PostingResult{}, err
	}
	requestHash, err := hashReversalCommand(command)
	if err != nil {
		return PostingResult{}, err
	}
	reversalID, err := newUUID()
	if err != nil {
		return PostingResult{}, err
	}
	originalID, _ := parseUUID(command.OriginalTransactionID)

	tx, err := service.database.Begin(ctx)
	if err != nil {
		return PostingResult{}, fmt.Errorf("begin reversal transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)

	existing, acquired, err := acquireRequest(ctx, queries, command.Scope, command.IdempotencyKey, requestHash, reversalID)
	if err != nil {
		return PostingResult{}, err
	}
	if !acquired {
		return PostingResult{TransactionID: existing.String(), Replayed: true}, nil
	}
	if _, err := queries.GetLedgerTransaction(ctx, originalID); err != nil {
		return PostingResult{}, fmt.Errorf("get original ledger transaction: %w", err)
	}
	originalEntries, err := queries.LockLedgerEntriesForTransaction(ctx, originalID)
	if err != nil {
		return PostingResult{}, fmt.Errorf("lock original ledger entries: %w", err)
	}
	insertedReversal, err := queries.InsertReversal(ctx, store.InsertReversalParams{
		OriginalTransactionID: originalID,
		ReversalTransactionID: reversalID,
		ReasonCode:            command.ReasonCode,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return PostingResult{}, ErrAlreadyReversed
	}
	if err != nil {
		return PostingResult{}, fmt.Errorf("reserve reversal: %w", err)
	}
	if insertedReversal != reversalID {
		return PostingResult{}, ErrAlreadyReversed
	}
	if _, err := queries.InsertLedgerTransaction(ctx, store.InsertLedgerTransactionParams{
		ID:              reversalID,
		TransactionType: "REVERSAL",
		PolicyVersion:   command.PolicyVersion,
		EffectiveAt:     pgtype.Timestamptz{Time: command.EffectiveAt.UTC(), Valid: true},
	}); err != nil {
		return PostingResult{}, fmt.Errorf("insert reversal transaction: %w", err)
	}
	for index, entry := range originalEntries {
		if _, err := queries.InsertLedgerEntry(ctx, store.InsertLedgerEntryParams{
			TransactionID:    reversalID,
			EntrySequence:    int16(index + 1),
			AccountID:        entry.AccountID,
			Currency:         entry.Currency,
			BalanceDimension: entry.BalanceDimension,
			Direction:        string(opposite(Direction(entry.Direction))),
			Amount:           entry.Amount,
		}); err != nil {
			return PostingResult{}, fmt.Errorf("insert reversal entry %d: %w", index+1, err)
		}
	}

	reversalIDText := reversalID.String()
	metadata, err := json.Marshal(struct {
		OriginalTransactionID string `json:"original_transaction_id"`
		ReasonCode            string `json:"reason_code"`
	}{command.OriginalTransactionID, command.ReasonCode})
	if err != nil {
		return PostingResult{}, fmt.Errorf("encode reversal audit metadata: %w", err)
	}
	payload, err := json.Marshal(struct {
		LedgerTransactionID   string `json:"ledger_transaction_id"`
		OriginalTransactionID string `json:"original_transaction_id"`
	}{reversalIDText, command.OriginalTransactionID})
	if err != nil {
		return PostingResult{}, fmt.Errorf("encode reversal event: %w", err)
	}
	if err := writeAuditAndOutbox(ctx, queries, mutationRecord{
		Action:        "ledger.transaction.reversed",
		ResourceType:  "ledger.transaction",
		ResourceID:    command.OriginalTransactionID,
		Actor:         command.Actor,
		CorrelationID: reversalID,
		Metadata:      metadata,
		AggregateType: "ledger.transaction",
		AggregateID:   reversalIDText,
		EventType:     "ledger.transaction.reversed",
		Payload:       payload,
	}); err != nil {
		return PostingResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PostingResult{}, fmt.Errorf("commit reversal transaction: %w", err)
	}
	return PostingResult{TransactionID: reversalIDText}, nil
}

func (service *Service) Balance(ctx context.Context, accountID string, currency money.Currency, dimension BalanceDimension) (money.Decimal, error) {
	identifier, err := parseUUID(accountID)
	if err != nil {
		return money.Decimal{}, fmt.Errorf("get account balance: %w", err)
	}
	if err := currency.Validate(); err != nil {
		return money.Decimal{}, err
	}
	queries := store.New(service.database)
	balance, err := queries.GetAccountBalance(ctx, store.GetAccountBalanceParams{
		AccountID:        identifier,
		Currency:         string(currency),
		BalanceDimension: string(dimension),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return money.Zero(), nil
	}
	if err != nil {
		return money.Decimal{}, fmt.Errorf("get account balance: %w", err)
	}
	return balance.DebitBalance, nil
}

type mutationRecord struct {
	Action        string
	ResourceType  string
	ResourceID    string
	Actor         Actor
	CorrelationID pgtype.UUID
	Metadata      []byte
	AggregateType string
	AggregateID   string
	EventType     string
	Payload       []byte
}

func writeAuditAndOutbox(ctx context.Context, queries *store.Queries, record mutationRecord) error {
	if _, err := queries.InsertAuditEvent(ctx, store.InsertAuditEventParams{
		Action:        record.Action,
		ResourceType:  record.ResourceType,
		ResourceID:    record.ResourceID,
		ActorType:     record.Actor.Type,
		ActorID:       record.Actor.ID,
		CorrelationID: record.CorrelationID,
		Metadata:      record.Metadata,
	}); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	if _, err := queries.InsertOutboxEvent(ctx, store.InsertOutboxEventParams{
		AggregateType: record.AggregateType,
		AggregateID:   record.AggregateID,
		EventType:     record.EventType,
		EventVersion:  1,
		Payload:       record.Payload,
		MaxAttempts:   defaultOutboxMaxAttempts,
	}); err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}

func acquireRequest(ctx context.Context, queries *store.Queries, scope, key, requestHash string, proposedID pgtype.UUID) (pgtype.UUID, bool, error) {
	inserted, err := queries.InsertRequestIdempotency(ctx, store.InsertRequestIdempotencyParams{
		Scope:          scope,
		IdempotencyKey: key,
		RequestHash:    requestHash,
		TransactionID:  proposedID,
	})
	if err == nil {
		return inserted, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, false, fmt.Errorf("acquire request idempotency: %w", err)
	}
	existing, err := queries.GetRequestIdempotency(ctx, store.GetRequestIdempotencyParams{
		Scope:          scope,
		IdempotencyKey: key,
	})
	if err != nil {
		return pgtype.UUID{}, false, fmt.Errorf("get request idempotency: %w", err)
	}
	if existing.RequestHash != requestHash {
		return pgtype.UUID{}, false, ErrIdempotencyConflict
	}
	return existing.TransactionID, false, nil
}

func acquireProviderEvent(ctx context.Context, queries *store.Queries, event ProviderEvent, proposedID pgtype.UUID) (pgtype.UUID, bool, error) {
	payloadHash := sha256Hex(event.Payload)
	inserted, err := queries.InsertProviderEvent(ctx, store.InsertProviderEventParams{
		Provider:        event.Provider,
		ExternalEventID: event.ExternalEventID,
		PayloadHash:     payloadHash,
		TransactionID:   proposedID,
	})
	if err == nil {
		return inserted, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, false, fmt.Errorf("acquire provider event: %w", err)
	}
	existing, err := queries.GetProviderEvent(ctx, store.GetProviderEventParams{
		Provider:        event.Provider,
		ExternalEventID: event.ExternalEventID,
	})
	if err != nil {
		return pgtype.UUID{}, false, fmt.Errorf("get provider event: %w", err)
	}
	if existing.PayloadHash != payloadHash {
		return pgtype.UUID{}, false, ErrProviderEventConflict
	}
	return existing.TransactionID, false, nil
}

func hashPostingCommand(command PostingCommand) (string, error) {
	type hashEntry struct {
		AccountID string `json:"account_id"`
		Amount    string `json:"amount"`
		Currency  string `json:"currency"`
		Dimension string `json:"dimension"`
		Direction string `json:"direction"`
	}
	type hashProviderEvent struct {
		ExternalEventID string `json:"external_event_id"`
		PayloadHash     string `json:"payload_hash"`
		Provider        string `json:"provider"`
	}
	payload := struct {
		Actor           Actor              `json:"actor"`
		EffectiveAt     string             `json:"effective_at"`
		Entries         []hashEntry        `json:"entries"`
		PolicyVersion   string             `json:"policy_version"`
		ProviderEvent   *hashProviderEvent `json:"provider_event,omitempty"`
		TransactionType string             `json:"transaction_type"`
	}{
		Actor:           command.Actor,
		EffectiveAt:     command.EffectiveAt.UTC().Format(time.RFC3339Nano),
		PolicyVersion:   command.PolicyVersion,
		TransactionType: command.TransactionType,
		Entries:         make([]hashEntry, 0, len(command.Entries)),
	}
	for _, entry := range command.Entries {
		payload.Entries = append(payload.Entries, hashEntry{
			AccountID: entry.AccountID,
			Amount:    entry.Amount.String(),
			Currency:  string(entry.Currency),
			Dimension: string(entry.Dimension),
			Direction: string(entry.Direction),
		})
	}
	if command.ProviderEvent != nil {
		payload.ProviderEvent = &hashProviderEvent{
			ExternalEventID: command.ProviderEvent.ExternalEventID,
			PayloadHash:     sha256Hex(command.ProviderEvent.Payload),
			Provider:        command.ProviderEvent.Provider,
		}
	}
	return hashJSON(payload)
}

func hashReversalCommand(command ReversalCommand) (string, error) {
	return hashJSON(struct {
		Actor                 Actor  `json:"actor"`
		EffectiveAt           string `json:"effective_at"`
		OriginalTransactionID string `json:"original_transaction_id"`
		PolicyVersion         string `json:"policy_version"`
		ReasonCode            string `json:"reason_code"`
	}{
		Actor:                 command.Actor,
		EffectiveAt:           command.EffectiveAt.UTC().Format(time.RFC3339Nano),
		OriginalTransactionID: command.OriginalTransactionID,
		PolicyVersion:         command.PolicyVersion,
		ReasonCode:            command.ReasonCode,
	})
}

func hashJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode idempotency hash: %w", err)
	}
	return sha256Hex(encoded), nil
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func opposite(direction Direction) Direction {
	if direction == Debit {
		return Credit
	}
	return Debit
}
