package ledger

import (
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/KDTikkly/Cytisus/internal/money"
)

type Direction string

const (
	Debit  Direction = "DEBIT"
	Credit Direction = "CREDIT"
)

type BalanceDimension string

const (
	DimensionPending           BalanceDimension = "PENDING"
	DimensionHeld              BalanceDimension = "HELD"
	DimensionSettled           BalanceDimension = "SETTLED"
	DimensionWithdrawable      BalanceDimension = "WITHDRAWABLE"
	DimensionProvisionalBuying BalanceDimension = "PROVISIONAL_BUYING_POWER"
	DimensionCardSpendable     BalanceDimension = "CARD_SPENDABLE"
	DimensionFrozen            BalanceDimension = "FROZEN"
	DimensionReceivable        BalanceDimension = "RECEIVABLE"
)

type Actor struct {
	Type string
	ID   string
}

type Entry struct {
	AccountID string
	Currency  money.Currency
	Dimension BalanceDimension
	Direction Direction
	Amount    money.Decimal
}

type ProviderEvent struct {
	Provider        string
	ExternalEventID string
	Payload         []byte
}

type PostingCommand struct {
	Scope           string
	IdempotencyKey  string
	TransactionType string
	PolicyVersion   string
	EffectiveAt     time.Time
	Actor           Actor
	Entries         []Entry
	ProviderEvent   *ProviderEvent
}

type ReversalCommand struct {
	Scope                 string
	IdempotencyKey        string
	OriginalTransactionID string
	ReasonCode            string
	PolicyVersion         string
	EffectiveAt           time.Time
	Actor                 Actor
}

type PostingResult struct {
	TransactionID string
	Replayed      bool
}

type AccountCommand struct {
	AccountKey  string
	OwnerType   string
	OwnerID     string
	AccountType string
	Currency    money.Currency
	NormalSide  Direction
	Actor       Actor
}

type Account struct {
	ID          string
	AccountKey  string
	OwnerType   string
	OwnerID     string
	AccountType string
	Currency    money.Currency
	NormalSide  Direction
}

var (
	ErrInvalidCommand        = errors.New("invalid ledger command")
	ErrUnbalancedPosting     = errors.New("ledger posting is not balanced")
	ErrIdempotencyConflict   = errors.New("idempotency key was reused with different request data")
	ErrProviderEventConflict = errors.New("provider event was reused with different payload data")
	ErrAlreadyReversed       = errors.New("ledger transaction has already been reversed")
	ErrAccountConflict       = errors.New("ledger account key was reused with different account data")
	codePattern              = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,63}$`)
	scopePattern             = regexp.MustCompile(`^[a-z][a-z0-9._:-]{2,127}$`)
	keyPattern               = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,255}$`)
	providerPattern          = regexp.MustCompile(`^[a-z][a-z0-9._:-]{1,63}$`)
)

func (command PostingCommand) validate() error {
	if !scopePattern.MatchString(command.Scope) || !keyPattern.MatchString(command.IdempotencyKey) {
		return fmt.Errorf("%w: invalid idempotency scope or key", ErrInvalidCommand)
	}
	if !codePattern.MatchString(command.TransactionType) || command.PolicyVersion == "" || command.EffectiveAt.IsZero() {
		return fmt.Errorf("%w: invalid transaction metadata", ErrInvalidCommand)
	}
	if err := command.Actor.validate(); err != nil {
		return err
	}
	if len(command.Entries) < 2 || len(command.Entries) > 32767 {
		return fmt.Errorf("%w: a posting requires 2..32767 entries", ErrInvalidCommand)
	}

	type totals struct {
		debit  money.Decimal
		credit money.Decimal
	}
	byCurrency := make(map[money.Currency]totals)
	for _, entry := range command.Entries {
		if err := entry.validate(); err != nil {
			return err
		}
		currencyTotals := byCurrency[entry.Currency]
		var err error
		switch entry.Direction {
		case Debit:
			currencyTotals.debit, err = currencyTotals.debit.Add(entry.Amount)
		case Credit:
			currencyTotals.credit, err = currencyTotals.credit.Add(entry.Amount)
		}
		if err != nil {
			return fmt.Errorf("%w: total precision: %v", ErrInvalidCommand, err)
		}
		byCurrency[entry.Currency] = currencyTotals
	}
	for _, currencyTotals := range byCurrency {
		if currencyTotals.debit.IsZero() || currencyTotals.credit.IsZero() || !currencyTotals.debit.Equal(currencyTotals.credit) {
			return ErrUnbalancedPosting
		}
	}
	if command.ProviderEvent != nil {
		if err := command.ProviderEvent.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (command ReversalCommand) validate() error {
	if !scopePattern.MatchString(command.Scope) || !keyPattern.MatchString(command.IdempotencyKey) {
		return fmt.Errorf("%w: invalid idempotency scope or key", ErrInvalidCommand)
	}
	if _, err := parseUUID(command.OriginalTransactionID); err != nil {
		return fmt.Errorf("%w: invalid original transaction ID", ErrInvalidCommand)
	}
	if !codePattern.MatchString(command.ReasonCode) || command.PolicyVersion == "" || command.EffectiveAt.IsZero() {
		return fmt.Errorf("%w: invalid reversal metadata", ErrInvalidCommand)
	}
	return command.Actor.validate()
}

func (command AccountCommand) validate() error {
	if !scopePattern.MatchString(command.AccountKey) || command.OwnerID == "" {
		return fmt.Errorf("%w: invalid account identity", ErrInvalidCommand)
	}
	switch command.OwnerType {
	case "USER", "PLATFORM", "PROVIDER", "SYSTEM":
	default:
		return fmt.Errorf("%w: invalid account owner type", ErrInvalidCommand)
	}
	switch command.AccountType {
	case "CRYPTO", "CASH", "SECURITIES", "CARD_RECEIVABLE", "PLATFORM_FEE", "PROVIDER_CLEARING", "SUSPENSE", "FROZEN_FUNDS", "RWA_LOCKED":
	default:
		return fmt.Errorf("%w: invalid account type", ErrInvalidCommand)
	}
	if err := command.Currency.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCommand, err)
	}
	if command.NormalSide != Debit && command.NormalSide != Credit {
		return fmt.Errorf("%w: invalid normal side", ErrInvalidCommand)
	}
	return command.Actor.validate()
}

func (actor Actor) validate() error {
	switch actor.Type {
	case "USER", "ADMIN", "SYSTEM", "PROVIDER":
	default:
		return fmt.Errorf("%w: invalid actor type", ErrInvalidCommand)
	}
	if actor.ID == "" {
		return fmt.Errorf("%w: actor ID is required", ErrInvalidCommand)
	}
	return nil
}

func (entry Entry) validate() error {
	if _, err := parseUUID(entry.AccountID); err != nil {
		return fmt.Errorf("%w: invalid account ID", ErrInvalidCommand)
	}
	if err := entry.Currency.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCommand, err)
	}
	switch entry.Dimension {
	case DimensionPending, DimensionHeld, DimensionSettled, DimensionWithdrawable,
		DimensionProvisionalBuying, DimensionCardSpendable, DimensionFrozen, DimensionReceivable:
	default:
		return fmt.Errorf("%w: invalid balance dimension", ErrInvalidCommand)
	}
	if entry.Direction != Debit && entry.Direction != Credit {
		return fmt.Errorf("%w: invalid entry direction", ErrInvalidCommand)
	}
	if !entry.Amount.IsPositive() {
		return fmt.Errorf("%w: entry amount must be positive", ErrInvalidCommand)
	}
	return nil
}

func (event ProviderEvent) validate() error {
	if !providerPattern.MatchString(event.Provider) || event.ExternalEventID == "" || len(event.ExternalEventID) > 255 {
		return fmt.Errorf("%w: invalid provider event identity", ErrInvalidCommand)
	}
	if len(event.Payload) == 0 {
		return fmt.Errorf("%w: provider payload is required", ErrInvalidCommand)
	}
	return nil
}
