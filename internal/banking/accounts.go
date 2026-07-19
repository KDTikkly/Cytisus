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
	"github.com/jackc/pgx/v5"
)

func (service *Service) LinkBankAccount(ctx context.Context, command LinkBankAccountCommand) (BankAccount, error) {
	_, profile, err := service.resolveCustomer(ctx, command.AccessToken)
	if err != nil {
		return BankAccount{}, err
	}
	command.ExternalAccountReference = strings.TrimSpace(command.ExternalAccountReference)
	command.RailSupport = strings.ToUpper(strings.TrimSpace(command.RailSupport))
	command.OwnerRelation = strings.ToUpper(strings.TrimSpace(command.OwnerRelation))
	command.RiskClass = strings.ToUpper(strings.TrimSpace(command.RiskClass))
	if !externalReferencePattern.MatchString(command.ExternalAccountReference) ||
		(command.RailSupport != "ACH" && command.RailSupport != "WIRE" && command.RailSupport != "BOTH") ||
		(command.OwnerRelation != "SAME_NAME" && command.OwnerRelation != "THIRD_PARTY") ||
		(command.RiskClass != "STANDARD" && command.RiskClass != "ELEVATED") {
		return BankAccount{}, ErrInvalidCommand
	}

	tx, err := service.database.Begin(ctx)
	if err != nil {
		return BankAccount{}, fmt.Errorf("begin link bank account: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	identifier, err := newUUID()
	if err != nil {
		return BankAccount{}, err
	}
	ownershipStatus := "PENDING"
	if command.OwnerRelation == "THIRD_PARTY" {
		ownershipStatus = "FAILED"
	}
	created, err := queries.CreateBankAccount(ctx, store.CreateBankAccountParams{
		ID:                       identifier,
		CustomerReference:        profile.CustomerReference,
		Provider:                 service.provider.Name(),
		ExternalAccountReference: command.ExternalAccountReference,
		RailSupport:              command.RailSupport,
		OwnerRelation:            command.OwnerRelation,
		OwnershipStatus:          ownershipStatus,
		Status:                   "ACTIVE",
		RiskClass:                command.RiskClass,
		PolicyVersion:            PolicyVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existing, getErr := queries.GetBankAccountByExternalReference(ctx, store.GetBankAccountByExternalReferenceParams{
			CustomerReference:        profile.CustomerReference,
			Provider:                 service.provider.Name(),
			ExternalAccountReference: command.ExternalAccountReference,
		})
		if getErr != nil {
			return BankAccount{}, fmt.Errorf("get existing bank account: %w", getErr)
		}
		if existing.RailSupport != command.RailSupport || existing.OwnerRelation != command.OwnerRelation || existing.RiskClass != command.RiskClass {
			return BankAccount{}, ErrIdempotencyConflict
		}
		if err := tx.Commit(ctx); err != nil {
			return BankAccount{}, fmt.Errorf("commit replayed bank account: %w", err)
		}
		return bankAccountFromStore(existing), nil
	}
	if err != nil {
		return BankAccount{}, fmt.Errorf("create bank account: %w", err)
	}
	metadata, _ := json.Marshal(map[string]string{
		"mode":             "SIMULATED",
		"rail_support":     command.RailSupport,
		"owner_relation":   command.OwnerRelation,
		"ownership_status": ownershipStatus,
	})
	if _, err := queries.InsertBankAccountEvent(ctx, store.InsertBankAccountEventParams{
		BankAccountID: identifier,
		EventType:     "LINKED",
		ActorType:     "USER",
		ActorID:       profile.CustomerReference,
		Metadata:      metadata,
	}); err != nil {
		return BankAccount{}, fmt.Errorf("record bank account link event: %w", err)
	}
	if err := recordMutation(ctx, tx, mutation{
		Action:        "bank.account.linked",
		ResourceType:  "bank.account",
		ResourceID:    identifier.String(),
		ActorType:     "USER",
		ActorID:       profile.CustomerReference,
		Metadata:      metadata,
		AggregateType: "bank.account",
		AggregateID:   identifier.String(),
		EventType:     "bank.account.linked",
		Payload:       metadata,
	}); err != nil {
		return BankAccount{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return BankAccount{}, fmt.Errorf("commit bank account: %w", err)
	}
	return bankAccountFromStore(created), nil
}

func (service *Service) VerifyBankAccountOwnership(ctx context.Context, event provider.Event) (BankAccount, bool, error) {
	if event.ResourceType != "BANK_ACCOUNT" || (event.EventType != "OWNERSHIP_VERIFIED" && event.EventType != "OWNERSHIP_FAILED") {
		return BankAccount{}, false, ErrInvalidCommand
	}
	accountID, err := parseUUID(event.ResourceID)
	if err != nil || event.Provider == "" || event.ExternalEventID == "" || len(event.Payload) == 0 {
		return BankAccount{}, false, ErrInvalidCommand
	}
	payloadHash, err := hashValue(struct {
		EventType  string `json:"event_type"`
		ReasonCode string `json:"reason_code"`
		Payload    string `json:"payload"`
	}{event.EventType, event.ReasonCode, string(event.Payload)})
	if err != nil {
		return BankAccount{}, false, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return BankAccount{}, false, fmt.Errorf("begin ownership callback: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	_, err = queries.InsertProviderEvent(ctx, store.InsertProviderEventParams{
		Provider:        event.Provider,
		ExternalEventID: event.ExternalEventID,
		ResourceType:    event.ResourceType,
		ResourceID:      accountID,
		EventType:       event.EventType,
		PayloadHash:     payloadHash,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existingEvent, getErr := queries.GetProviderEvent(ctx, store.GetProviderEventParams{Provider: event.Provider, ExternalEventID: event.ExternalEventID})
		if getErr != nil {
			return BankAccount{}, false, fmt.Errorf("get ownership callback: %w", getErr)
		}
		if existingEvent.PayloadHash != payloadHash || existingEvent.ResourceID != accountID {
			return BankAccount{}, false, ErrProviderEventConflict
		}
		account, getErr := queries.GetBankAccount(ctx, accountID)
		if getErr != nil {
			return BankAccount{}, false, ErrBankAccountNotFound
		}
		if err := tx.Commit(ctx); err != nil {
			return BankAccount{}, false, err
		}
		return bankAccountFromStore(account), true, nil
	}
	if err != nil {
		return BankAccount{}, false, fmt.Errorf("insert ownership callback: %w", err)
	}
	account, err := queries.GetBankAccountForUpdate(ctx, accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return BankAccount{}, false, ErrBankAccountNotFound
	}
	if err != nil {
		return BankAccount{}, false, fmt.Errorf("lock bank account: %w", err)
	}
	if account.OwnershipStatus != "PENDING" {
		if err := tx.Commit(ctx); err != nil {
			return BankAccount{}, false, err
		}
		return bankAccountFromStore(account), true, nil
	}
	status := "FAILED"
	eventType := "OWNERSHIP_FAILED"
	var coolingUntil = account.CoolingUntil
	if event.EventType == "OWNERSHIP_VERIFIED" && account.OwnerRelation == "SAME_NAME" {
		status = "VERIFIED"
		eventType = "OWNERSHIP_VERIFIED"
		coolingUntil = timestamptz(dynamicCooling(service.now(), account.RiskClass))
	}
	updated, err := queries.UpdateBankAccountOwnership(ctx, store.UpdateBankAccountOwnershipParams{
		OwnershipStatus: status,
		CoolingUntil:    coolingUntil,
		ID:              accountID,
	})
	if err != nil {
		return BankAccount{}, false, fmt.Errorf("update ownership: %w", err)
	}
	metadata, _ := json.Marshal(map[string]string{"mode": "SIMULATED", "ownership_status": status})
	if _, err := queries.InsertBankAccountEvent(ctx, store.InsertBankAccountEventParams{
		BankAccountID: accountID,
		EventType:     eventType,
		ActorType:     "PROVIDER",
		ActorID:       event.Provider,
		Metadata:      metadata,
	}); err != nil {
		return BankAccount{}, false, fmt.Errorf("record ownership event: %w", err)
	}
	if err := recordMutation(ctx, tx, mutation{
		Action:        "bank.account.ownership_" + strings.ToLower(status),
		ResourceType:  "bank.account",
		ResourceID:    accountID.String(),
		ActorType:     "PROVIDER",
		ActorID:       event.Provider,
		Metadata:      metadata,
		AggregateType: "bank.account",
		AggregateID:   accountID.String(),
		EventType:     "bank.account.ownership_" + strings.ToLower(status),
		EventVersion:  int32(updated.Version),
		Payload:       metadata,
	}); err != nil {
		return BankAccount{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return BankAccount{}, false, fmt.Errorf("commit ownership callback: %w", err)
	}
	return bankAccountFromStore(updated), false, nil
}

func (service *Service) ListBankAccounts(ctx context.Context, accessToken string) ([]BankAccount, error) {
	_, profile, err := service.resolveCustomer(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	rows, err := store.New(service.database).ListBankAccounts(ctx, profile.CustomerReference)
	if err != nil {
		return nil, fmt.Errorf("list bank accounts: %w", err)
	}
	result := make([]BankAccount, 0, len(rows))
	for _, row := range rows {
		result = append(result, bankAccountFromStore(row))
	}
	return result, nil
}

func bankAccountFromStore(account store.BankingBankAccount) BankAccount {
	var fundedAt, coolingUntil *time.Time
	if account.SuccessfullyFundedAt.Valid {
		value := account.SuccessfullyFundedAt.Time.UTC()
		fundedAt = &value
	}
	if account.CoolingUntil.Valid {
		value := account.CoolingUntil.Time.UTC()
		coolingUntil = &value
	}
	return BankAccount{
		ID:                       account.ID.String(),
		Provider:                 account.Provider,
		ExternalAccountReference: account.ExternalAccountReference,
		RailSupport:              account.RailSupport,
		OwnerRelation:            account.OwnerRelation,
		OwnershipStatus:          account.OwnershipStatus,
		Status:                   account.Status,
		PreferredForWithdrawal:   account.SuccessfullyFundedAt.Valid && account.OwnerRelation == "SAME_NAME" && account.OwnershipStatus == "VERIFIED",
		SuccessfullyFundedAt:     fundedAt,
		CoolingUntil:             coolingUntil,
		CreatedAt:                account.CreatedAt.Time.UTC(),
	}
}
