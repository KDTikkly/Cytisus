package securities

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/KDTikkly/Cytisus/internal/audit"
	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/marketdata"
	"github.com/KDTikkly/Cytisus/internal/money"
	outboxstore "github.com/KDTikkly/Cytisus/internal/outbox/store"
	"github.com/KDTikkly/Cytisus/internal/securities/broker"
	"github.com/KDTikkly/Cytisus/internal/securities/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const defaultProviderTimeout = 3 * time.Second

var fixturePattern = regexp.MustCompile(`^[a-z][a-z0-9._:-]{2,127}$`)

type database interface {
	store.DBTX
	Begin(context.Context) (pgx.Tx, error)
}

type Service struct {
	database        database
	environment     config.Environment
	catalog         *marketdata.Catalog
	quotes          marketdata.Provider
	broker          broker.Adapter
	initialCash     money.Decimal
	providerTimeout time.Duration
	now             func() time.Time
}

type Dependencies struct {
	Database        database
	Environment     config.Environment
	Catalog         *marketdata.Catalog
	Quotes          marketdata.Provider
	Broker          broker.Adapter
	InitialCash     money.Decimal
	ProviderTimeout time.Duration
	Now             func() time.Time
}

func NewService(dependencies Dependencies) (*Service, error) {
	if dependencies.Database == nil || dependencies.Catalog == nil || dependencies.Quotes == nil || dependencies.Broker == nil {
		return nil, fmt.Errorf("create securities service: %w: dependencies are required", ErrInvalidCommand)
	}
	if dependencies.Environment != config.EnvironmentLocal && dependencies.Environment != config.EnvironmentTest &&
		dependencies.Environment != config.EnvironmentStaging && dependencies.Environment != config.EnvironmentProduction {
		return nil, fmt.Errorf("create securities service: %w: invalid environment", ErrInvalidCommand)
	}
	if !dependencies.InitialCash.IsPositive() {
		dependencies.InitialCash = money.MustParse("100000")
	}
	if dependencies.ProviderTimeout <= 0 {
		dependencies.ProviderTimeout = defaultProviderTimeout
	}
	if dependencies.Now == nil {
		dependencies.Now = time.Now
	}
	return &Service{
		database:        dependencies.Database,
		environment:     dependencies.Environment,
		catalog:         dependencies.Catalog,
		quotes:          dependencies.Quotes,
		broker:          dependencies.Broker,
		initialCash:     dependencies.InitialCash,
		providerTimeout: dependencies.ProviderTimeout,
		now:             dependencies.Now,
	}, nil
}

func (service *Service) RegisterFixture(ctx context.Context, fixtureID string) (Registration, error) {
	if service.environment != config.EnvironmentLocal && service.environment != config.EnvironmentTest {
		return Registration{}, ErrFixtureDisabled
	}
	if !fixturePattern.MatchString(fixtureID) {
		return Registration{}, fmt.Errorf("register fixture: %w: invalid fixture ID", ErrInvalidCommand)
	}
	token := fixtureToken(fixtureID)
	tokenHash := sha256Hex([]byte(token))

	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Registration{}, fmt.Errorf("begin registration: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	if err := queries.LockRegistrationFixture(ctx, fixtureID); err != nil {
		return Registration{}, fmt.Errorf("lock registration fixture: %w", err)
	}
	existing, err := queries.GetPaperAccountByFixture(ctx, fixtureID)
	if err == nil {
		if existing.SessionTokenHash != tokenHash {
			return Registration{}, ErrUnauthorized
		}
		if err := tx.Commit(ctx); err != nil {
			return Registration{}, fmt.Errorf("commit replayed registration: %w", err)
		}
		return Registration{Account: accountFromStore(existing), AccessToken: token, Replayed: true}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Registration{}, fmt.Errorf("get registration fixture: %w", err)
	}

	accountID, err := newUUID()
	if err != nil {
		return Registration{}, err
	}
	accountIDText := accountID.String()
	actor := ledger.Actor{Type: "SYSTEM", ID: "paper-registration-fixture"}
	ledgerService := ledger.NewService(tx)
	cashAccount, err := ledgerService.OpenAccount(ctx, ledger.AccountCommand{
		AccountKey:  "paper.cash." + accountIDText,
		OwnerType:   "USER",
		OwnerID:     accountIDText,
		AccountType: "CASH",
		Currency:    money.Currency("USD"),
		NormalSide:  ledger.Debit,
		Actor:       actor,
	})
	if err != nil {
		return Registration{}, fmt.Errorf("open paper cash account: %w", err)
	}
	fundingAccount, err := ledgerService.OpenAccount(ctx, ledger.AccountCommand{
		AccountKey:  "paper.system-funding.usd",
		OwnerType:   "SYSTEM",
		OwnerID:     "paper-system",
		AccountType: "PROVIDER_CLEARING",
		Currency:    money.Currency("USD"),
		NormalSide:  ledger.Credit,
		Actor:       actor,
	})
	if err != nil {
		return Registration{}, fmt.Errorf("open paper funding account: %w", err)
	}
	cashID, err := parseUUID(cashAccount.ID)
	if err != nil {
		return Registration{}, err
	}
	fundingID, err := parseUUID(fundingAccount.ID)
	if err != nil {
		return Registration{}, err
	}
	created, err := queries.InsertPaperAccount(ctx, store.InsertPaperAccountParams{
		ID:                     accountID,
		FixtureID:              fixtureID,
		CustomerReference:      "fixture:" + fixtureID,
		SessionTokenHash:       tokenHash,
		CashLedgerAccountID:    cashID,
		FundingLedgerAccountID: fundingID,
		InitialCash:            service.initialCash,
	})
	if err != nil {
		return Registration{}, fmt.Errorf("insert paper account: %w", err)
	}
	if _, err := ledgerService.Post(ctx, ledger.PostingCommand{
		Scope:           "paper.registration",
		IdempotencyKey:  "register.seed:" + fixtureID,
		TransactionType: "PAPER_ACCOUNT_FUNDED",
		PolicyVersion:   PolicyVersion,
		EffectiveAt:     service.now().UTC(),
		Actor:           actor,
		Entries: []ledger.Entry{
			{AccountID: cashAccount.ID, Currency: money.Currency("USD"), Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: service.initialCash},
			{AccountID: fundingAccount.ID, Currency: money.Currency("USD"), Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: service.initialCash},
			{AccountID: cashAccount.ID, Currency: money.Currency("USD"), Dimension: ledger.DimensionWithdrawable, Direction: ledger.Debit, Amount: service.initialCash},
			{AccountID: fundingAccount.ID, Currency: money.Currency("USD"), Dimension: ledger.DimensionWithdrawable, Direction: ledger.Credit, Amount: service.initialCash},
		},
	}); err != nil {
		return Registration{}, fmt.Errorf("fund paper account: %w", err)
	}
	payload, _ := json.Marshal(struct {
		PaperAccountID string `json:"paper_account_id"`
		FixtureID      string `json:"fixture_id"`
		InitialCash    string `json:"initial_cash"`
	}{accountIDText, fixtureID, service.initialCash.String()})
	if err := recordMutation(ctx, tx, mutation{
		Action:        "paper.account.registered",
		ResourceType:  "paper.account",
		ResourceID:    accountIDText,
		ActorType:     actor.Type,
		ActorID:       actor.ID,
		Metadata:      payload,
		AggregateType: "paper.account",
		AggregateID:   accountIDText,
		EventType:     "paper.account.registered",
		Payload:       payload,
	}); err != nil {
		return Registration{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Registration{}, fmt.Errorf("commit registration: %w", err)
	}
	return Registration{Account: accountFromStore(created), AccessToken: token}, nil
}

func (service *Service) authenticate(ctx context.Context, accessToken string) (store.SecuritiesPaperAccount, error) {
	if accessToken == "" {
		return store.SecuritiesPaperAccount{}, ErrUnauthorized
	}
	account, err := store.New(service.database).GetPaperAccountBySessionHash(ctx, sha256Hex([]byte(accessToken)))
	if errors.Is(err, pgx.ErrNoRows) {
		return store.SecuritiesPaperAccount{}, ErrUnauthorized
	}
	if err != nil {
		return store.SecuritiesPaperAccount{}, fmt.Errorf("authenticate paper account: %w", err)
	}
	return account, nil
}

// ResolveSession exposes the stable customer and Ledger account contract needed
// by adjacent application services without allowing them to read securities tables.
func (service *Service) ResolveSession(ctx context.Context, accessToken string) (PaperAccount, error) {
	account, err := service.authenticate(ctx, accessToken)
	if err != nil {
		return PaperAccount{}, err
	}
	return accountFromStore(account), nil
}

func accountFromStore(account store.SecuritiesPaperAccount) PaperAccount {
	return PaperAccount{
		ID:                     account.ID.String(),
		FixtureID:              account.FixtureID,
		CustomerReference:      account.CustomerReference,
		CashLedgerAccountID:    account.CashLedgerAccountID.String(),
		FundingLedgerAccountID: account.FundingLedgerAccountID.String(),
		InitialCash:            account.InitialCash,
		CreatedAt:              account.CreatedAt.Time.UTC(),
	}
}

type mutation struct {
	Action        string
	ResourceType  string
	ResourceID    string
	ActorType     string
	ActorID       string
	CorrelationID pgtype.UUID
	Metadata      []byte
	AggregateType string
	AggregateID   string
	EventType     string
	EventVersion  int32
	Payload       []byte
}

func recordMutation(ctx context.Context, tx pgx.Tx, event mutation) error {
	if err := audit.Record(ctx, tx, audit.Event{
		Action:        event.Action,
		ResourceType:  event.ResourceType,
		ResourceID:    event.ResourceID,
		ActorType:     event.ActorType,
		ActorID:       event.ActorID,
		CorrelationID: event.CorrelationID,
		Metadata:      event.Metadata,
	}); err != nil {
		return err
	}
	eventVersion := event.EventVersion
	if eventVersion <= 0 {
		eventVersion = 1
	}
	if _, err := outboxstore.New(tx).InsertOutboxEvent(ctx, outboxstore.InsertOutboxEventParams{
		AggregateType: event.AggregateType,
		AggregateID:   event.AggregateID,
		EventType:     event.EventType,
		EventVersion:  eventVersion,
		Payload:       event.Payload,
		MaxAttempts:   8,
	}); err != nil {
		return fmt.Errorf("insert securities outbox event: %w", err)
	}
	return nil
}

func fixtureToken(fixtureID string) string {
	digest := sha256.Sum256([]byte("cytisus-paper-fixture-v1:" + fixtureID))
	return "paper_fixture_" + hex.EncodeToString(digest[:])
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func newUUID() (pgtype.UUID, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return pgtype.UUID{}, fmt.Errorf("generate UUID: %w", err)
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return pgtype.UUID{Bytes: bytes, Valid: true}, nil
}

func parseUUID(value string) (pgtype.UUID, error) {
	var identifier pgtype.UUID
	if err := identifier.Scan(value); err != nil || !identifier.Valid {
		return pgtype.UUID{}, fmt.Errorf("parse UUID %q", value)
	}
	return identifier, nil
}
