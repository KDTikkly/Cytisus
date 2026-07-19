package securities

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/securities/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ShareReservation is the stable application contract exposed to the RWA
// module. The securities module remains the only writer of its position and
// reservation tables.
type ShareReservation struct {
	ID                    string
	PaperAccountID        string
	InstrumentID          string
	Symbol                string
	Quantity              money.Decimal
	CustomerLedgerAccount string
	LockedLedgerAccount   string
	LockTransactionID     string
	ReleaseTransactionID  string
	Status                string
}

type ReserveSharesCommand struct {
	ReservationID  string
	PaperAccountID string
	InstrumentID   string
	Symbol         string
	Quantity       money.Decimal
	IdempotencyKey string
	Actor          ledger.Actor
	EffectiveAt    time.Time
}

// ReserveSettledShares atomically verifies the projection, reserves whole
// settled shares, and moves them to an RWA_LOCKED Ledger account.
func (service *Service) ReserveSettledShares(ctx context.Context, tx pgx.Tx, command ReserveSharesCommand) (ShareReservation, error) {
	if service == nil || tx == nil || !command.Quantity.IsPositive() || !command.Quantity.IsInteger() ||
		command.Symbol == "" || command.IdempotencyKey == "" || command.EffectiveAt.IsZero() {
		return ShareReservation{}, ErrInvalidCommand
	}
	reservationID, err := parseUUID(command.ReservationID)
	if err != nil {
		return ShareReservation{}, ErrInvalidCommand
	}
	accountID, err := parseUUID(command.PaperAccountID)
	if err != nil {
		return ShareReservation{}, ErrInvalidCommand
	}
	instrumentID, err := parseUUID(command.InstrumentID)
	if err != nil {
		return ShareReservation{}, ErrInvalidCommand
	}
	queries := store.New(tx)
	position, err := queries.GetPositionForUpdate(ctx, store.GetPositionForUpdateParams{
		PaperAccountID: accountID, InstrumentID: instrumentID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ShareReservation{}, ErrInsufficientPosition
	}
	if err != nil {
		return ShareReservation{}, fmt.Errorf("lock settled position: %w", err)
	}
	if position.Symbol != command.Symbol {
		return ShareReservation{}, ErrInvalidCommand
	}
	reserved, err := queries.SumActivePositionReservations(ctx, store.SumActivePositionReservationsParams{
		PaperAccountID: accountID, InstrumentID: instrumentID,
	})
	if err != nil {
		return ShareReservation{}, fmt.Errorf("sum position reservations: %w", err)
	}
	available, err := position.Quantity.Sub(reserved)
	if err != nil || available.Compare(command.Quantity) < 0 {
		return ShareReservation{}, ErrInsufficientPosition
	}
	accounts, err := queries.GetInstrumentLedgerAccounts(ctx, store.GetInstrumentLedgerAccountsParams{
		PaperAccountID: accountID, InstrumentID: instrumentID,
	})
	if err != nil {
		return ShareReservation{}, fmt.Errorf("get settled security ledger account: %w", err)
	}
	ledgerService := ledger.NewService(tx)
	locked, err := ledgerService.OpenAccount(ctx, ledger.AccountCommand{
		AccountKey:  "rwa.locked." + command.PaperAccountID + "." + strings.ToLower(command.Symbol),
		OwnerType:   "USER",
		OwnerID:     command.PaperAccountID,
		AccountType: "RWA_LOCKED",
		Currency:    money.Currency(command.Symbol),
		NormalSide:  ledger.Debit,
		Actor:       command.Actor,
	})
	if err != nil {
		return ShareReservation{}, fmt.Errorf("open RWA locked ledger account: %w", err)
	}
	posting, err := ledgerService.Post(ctx, ledger.PostingCommand{
		Scope:           "rwa.share-lock",
		IdempotencyKey:  command.IdempotencyKey,
		TransactionType: "RWA_UNDERLYING_LOCKED",
		PolicyVersion:   "rwa-sim-v1",
		EffectiveAt:     command.EffectiveAt.UTC(),
		Actor:           command.Actor,
		Entries: []ledger.Entry{
			{AccountID: accounts.CustomerLedgerAccountID.String(), Currency: money.Currency(command.Symbol), Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: command.Quantity},
			{AccountID: locked.ID, Currency: money.Currency(command.Symbol), Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: command.Quantity},
		},
	})
	if err != nil {
		return ShareReservation{}, fmt.Errorf("post RWA share lock: %w", err)
	}
	row, err := queries.InsertPositionReservation(ctx, store.InsertPositionReservationParams{
		ID: reservationID, PaperAccountID: accountID, InstrumentID: instrumentID,
		Symbol: command.Symbol, Quantity: command.Quantity,
		CustomerLedgerAccountID: accounts.CustomerLedgerAccountID,
		LockedLedgerAccountID:   uuidValue(locked.ID),
		LockLedgerTransactionID: uuidValue(posting.TransactionID),
	})
	if err != nil {
		return ShareReservation{}, fmt.Errorf("insert position reservation: %w", err)
	}
	return reservationFromStore(row), nil
}

// ReleaseSettledShares must only be called after the RWA service has proved a
// burn operation final. It creates a new balancing Ledger posting; it never
// mutates the original lock entries.
func (service *Service) ReleaseSettledShares(ctx context.Context, tx pgx.Tx, reservationID, idempotencyKey string, actor ledger.Actor, effectiveAt time.Time) (ShareReservation, error) {
	identifier, err := parseUUID(reservationID)
	if err != nil || idempotencyKey == "" || effectiveAt.IsZero() {
		return ShareReservation{}, ErrInvalidCommand
	}
	queries := store.New(tx)
	row, err := queries.GetPositionReservationForUpdate(ctx, identifier)
	if err != nil {
		return ShareReservation{}, fmt.Errorf("lock share reservation: %w", err)
	}
	if row.Status == "RELEASED" {
		return reservationFromStore(row), nil
	}
	posting, err := ledger.NewService(tx).Post(ctx, ledger.PostingCommand{
		Scope:           "rwa.share-release",
		IdempotencyKey:  idempotencyKey,
		TransactionType: "RWA_UNDERLYING_RELEASED",
		PolicyVersion:   "rwa-sim-v1",
		EffectiveAt:     effectiveAt.UTC(),
		Actor:           actor,
		Entries: []ledger.Entry{
			{AccountID: row.LockedLedgerAccountID.String(), Currency: money.Currency(row.Symbol), Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: row.Quantity},
			{AccountID: row.CustomerLedgerAccountID.String(), Currency: money.Currency(row.Symbol), Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: row.Quantity},
		},
	})
	if err != nil {
		return ShareReservation{}, fmt.Errorf("post RWA share release: %w", err)
	}
	row, err = queries.ReleasePositionReservation(ctx, store.ReleasePositionReservationParams{
		ReleaseLedgerTransactionID: uuidValue(posting.TransactionID), ID: identifier,
	})
	if err != nil {
		return ShareReservation{}, fmt.Errorf("release position reservation: %w", err)
	}
	return reservationFromStore(row), nil
}

func reservationFromStore(row store.SecuritiesPositionReservation) ShareReservation {
	return ShareReservation{
		ID: row.ID.String(), PaperAccountID: row.PaperAccountID.String(), InstrumentID: row.InstrumentID.String(),
		Symbol: row.Symbol, Quantity: row.Quantity, CustomerLedgerAccount: row.CustomerLedgerAccountID.String(),
		LockedLedgerAccount: row.LockedLedgerAccountID.String(), LockTransactionID: row.LockLedgerTransactionID.String(),
		ReleaseTransactionID: row.ReleaseLedgerTransactionID.String(), Status: row.Status,
	}
}

func uuidValue(value string) pgtype.UUID {
	identifier, _ := parseUUID(value)
	return identifier
}
