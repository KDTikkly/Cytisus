package banking

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

	"github.com/KDTikkly/Cytisus/internal/banking/provider"
	"github.com/KDTikkly/Cytisus/internal/banking/store"
	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const defaultProviderTimeout = 3 * time.Second

var (
	idempotencyKeyPattern    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,255}$`)
	externalReferencePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{2,127}$`)
)

type database interface {
	store.DBTX
	Begin(context.Context) (pgx.Tx, error)
}

type SessionResolver interface {
	ResolveSession(context.Context, string) (CustomerSession, error)
}

type Service struct {
	database        database
	environment     config.Environment
	resolver        SessionResolver
	provider        provider.Adapter
	providerTimeout time.Duration
	now             func() time.Time
}

type Dependencies struct {
	Database        database
	Environment     config.Environment
	Resolver        SessionResolver
	Provider        provider.Adapter
	ProviderTimeout time.Duration
	Now             func() time.Time
}

func NewService(dependencies Dependencies) (*Service, error) {
	if dependencies.Database == nil || dependencies.Resolver == nil || dependencies.Provider == nil {
		return nil, fmt.Errorf("create banking service: %w: dependencies are required", ErrInvalidCommand)
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
		resolver:        dependencies.Resolver,
		provider:        dependencies.Provider,
		providerTimeout: dependencies.ProviderTimeout,
		now:             dependencies.Now,
	}, nil
}

func (service *Service) resolveCustomer(ctx context.Context, accessToken string) (CustomerSession, store.BankingCustomerProfile, error) {
	session, err := service.resolver.ResolveSession(ctx, accessToken)
	if err != nil || session.CustomerReference == "" {
		return CustomerSession{}, store.BankingCustomerProfile{}, ErrUnauthorized
	}
	profile, err := service.ensureProfile(ctx, session)
	if err != nil {
		return CustomerSession{}, store.BankingCustomerProfile{}, err
	}
	return session, profile, nil
}

func (service *Service) ensureProfile(ctx context.Context, session CustomerSession) (store.BankingCustomerProfile, error) {
	queries := store.New(service.database)
	existing, err := queries.GetCustomerProfile(ctx, session.CustomerReference)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return store.BankingCustomerProfile{}, fmt.Errorf("get banking profile: %w", err)
	}
	paperAccountID, err := parseUUID(session.PaperAccountID)
	if err != nil {
		return store.BankingCustomerProfile{}, ErrUnauthorized
	}
	cashAccountID, err := parseUUID(session.CashLedgerAccountID)
	if err != nil {
		return store.BankingCustomerProfile{}, ErrUnauthorized
	}

	tx, err := service.database.Begin(ctx)
	if err != nil {
		return store.BankingCustomerProfile{}, fmt.Errorf("begin banking profile: %w", err)
	}
	defer tx.Rollback(ctx)
	ledgerService := ledger.NewService(tx)
	clearingAccount, err := ledgerService.OpenAccount(ctx, ledger.AccountCommand{
		AccountKey:  "banking.provider-clearing.usd",
		OwnerType:   "PROVIDER",
		OwnerID:     service.provider.Name(),
		AccountType: "PROVIDER_CLEARING",
		Currency:    money.Currency("USD"),
		NormalSide:  ledger.Credit,
		Actor:       ledger.Actor{Type: "SYSTEM", ID: "banking-profile-bootstrap"},
	})
	if err != nil {
		return store.BankingCustomerProfile{}, err
	}
	clearingAccountID, err := parseUUID(clearingAccount.ID)
	if err != nil {
		return store.BankingCustomerProfile{}, err
	}
	created, err := store.New(tx).CreateCustomerProfile(ctx, store.CreateCustomerProfileParams{
		CustomerReference:               session.CustomerReference,
		PaperAccountID:                  paperAccountID,
		CashLedgerAccountID:             cashAccountID,
		ProviderClearingLedgerAccountID: clearingAccountID,
		PolicyVersion:                   PolicyVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		created, err = store.New(tx).GetCustomerProfile(ctx, session.CustomerReference)
	}
	if err != nil {
		return store.BankingCustomerProfile{}, fmt.Errorf("create banking profile: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return store.BankingCustomerProfile{}, fmt.Errorf("commit banking profile: %w", err)
	}
	return created, nil
}

func (service *Service) callProvider(ctx context.Context, call func(context.Context) (provider.TransferResponse, error)) (provider.TransferResponse, error) {
	providerContext, cancel := context.WithTimeout(ctx, service.providerTimeout)
	defer cancel()
	response, err := call(providerContext)
	if err != nil {
		return provider.TransferResponse{}, err
	}
	if !response.Simulated && (service.environment == config.EnvironmentLocal || service.environment == config.EnvironmentTest) {
		return provider.TransferResponse{}, provider.ErrUnavailable
	}
	return response, nil
}

func dynamicCooling(now time.Time, riskClass string) time.Time {
	duration := 24 * time.Hour
	if riskClass == "ELEVATED" {
		duration = 72 * time.Hour
	}
	return now.UTC().Add(duration)
}

func hashValue(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode banking idempotency data: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func deterministicReference(scope, customerReference, idempotencyKey string) string {
	digest := sha256.Sum256([]byte(scope + ":" + customerReference + ":" + idempotencyKey))
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

func timestamptz(value time.Time) pgtype.Timestamptz {
	if value.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func textValue(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func uuidValue(value string) pgtype.UUID {
	identifier, _ := parseUUID(value)
	return identifier
}
