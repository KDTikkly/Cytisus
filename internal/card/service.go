package card

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	cardprovider "github.com/KDTikkly/Cytisus/internal/card/provider"
	"github.com/KDTikkly/Cytisus/internal/card/store"
	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/notification"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const defaultProviderTimeout = 3 * time.Second

var idempotencyKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,255}$`)
var reasonCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,63}$`)

var amountPolicy = money.RoundingPolicy{Version: PolicyVersion, DecimalPlaces: 18, Mode: money.RoundHalfEven}

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
	provider        cardprovider.Adapter
	notifications   *notification.Service
	providerTimeout time.Duration
	now             func() time.Time
}

type Dependencies struct {
	Database        database
	Environment     config.Environment
	Resolver        SessionResolver
	Provider        cardprovider.Adapter
	Notifications   *notification.Service
	ProviderTimeout time.Duration
	Now             func() time.Time
}

func NewService(dependencies Dependencies) (*Service, error) {
	if dependencies.Database == nil || dependencies.Resolver == nil || dependencies.Provider == nil || dependencies.Notifications == nil {
		return nil, fmt.Errorf("create card service: %w: dependencies are required", ErrInvalidCommand)
	}
	if dependencies.ProviderTimeout <= 0 {
		dependencies.ProviderTimeout = defaultProviderTimeout
	}
	if dependencies.Now == nil {
		dependencies.Now = time.Now
	}
	return &Service{
		database: dependencies.Database, environment: dependencies.Environment, resolver: dependencies.Resolver,
		provider: dependencies.Provider, notifications: dependencies.Notifications,
		providerTimeout: dependencies.ProviderTimeout, now: dependencies.Now,
	}, nil
}

func (service *Service) resolveCustomer(ctx context.Context, accessToken string) (CustomerSession, store.CardCustomerProfile, error) {
	session, err := service.resolver.ResolveSession(ctx, accessToken)
	if err != nil || session.CustomerReference == "" || session.PaperAccountID == "" || session.CashLedgerAccountID == "" {
		return CustomerSession{}, store.CardCustomerProfile{}, ErrUnauthorized
	}
	profile, err := service.ensureProfile(ctx, session)
	if err != nil {
		return CustomerSession{}, store.CardCustomerProfile{}, err
	}
	return session, profile, nil
}

func (service *Service) ensureProfile(ctx context.Context, session CustomerSession) (store.CardCustomerProfile, error) {
	queries := store.New(service.database)
	existing, err := queries.GetCustomerProfile(ctx, session.CustomerReference)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return store.CardCustomerProfile{}, fmt.Errorf("get card profile: %w", err)
	}
	paperID, err := parseUUID(session.PaperAccountID)
	if err != nil {
		return store.CardCustomerProfile{}, ErrUnauthorized
	}
	cashID, err := parseUUID(session.CashLedgerAccountID)
	if err != nil {
		return store.CardCustomerProfile{}, ErrUnauthorized
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return store.CardCustomerProfile{}, fmt.Errorf("begin card profile: %w", err)
	}
	defer tx.Rollback(ctx)
	policy, err := store.New(tx).GetActivePolicy(ctx)
	if err != nil {
		return store.CardCustomerProfile{}, fmt.Errorf("get active card policy: %w", err)
	}
	ledgerService := ledger.NewService(tx)
	actor := ledger.Actor{Type: "SYSTEM", ID: "card-profile"}
	receivable, err := ledgerService.OpenAccount(ctx, ledger.AccountCommand{
		AccountKey: "card.receivable." + safeKey(session.CustomerReference), OwnerType: "USER",
		OwnerID: session.CustomerReference, AccountType: "CARD_RECEIVABLE", Currency: "USD", NormalSide: ledger.Debit, Actor: actor,
	})
	if err != nil {
		return store.CardCustomerProfile{}, fmt.Errorf("open card receivable account: %w", err)
	}
	clearing, err := ledgerService.OpenAccount(ctx, ledger.AccountCommand{
		AccountKey: "card.provider-clearing.usd", OwnerType: "PROVIDER", OwnerID: service.provider.Name(),
		AccountType: "PROVIDER_CLEARING", Currency: "USD", NormalSide: ledger.Credit, Actor: actor,
	})
	if err != nil {
		return store.CardCustomerProfile{}, fmt.Errorf("open card provider clearing account: %w", err)
	}
	receivableID, _ := parseUUID(receivable.ID)
	clearingID, _ := parseUUID(clearing.ID)
	created, err := store.New(tx).CreateCustomerProfile(ctx, store.CreateCustomerProfileParams{
		CustomerReference: session.CustomerReference, PaperAccountID: paperID, CashLedgerAccountID: cashID,
		ReceivableLedgerAccountID: receivableID, ProviderClearingLedgerAccountID: clearingID,
		PolicyVersion: policy.PolicyVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		created, err = store.New(tx).GetCustomerProfile(ctx, session.CustomerReference)
	}
	if err != nil {
		return store.CardCustomerProfile{}, fmt.Errorf("create card profile: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return store.CardCustomerProfile{}, fmt.Errorf("commit card profile: %w", err)
	}
	return created, nil
}

func (service *Service) localSimulationAllowed() bool {
	return service.environment == config.EnvironmentLocal || service.environment == config.EnvironmentTest
}

func (service *Service) withProviderTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, service.providerTimeout)
}

func hashValue(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode card idempotency data: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func newUUID() (pgtype.UUID, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return pgtype.UUID{}, fmt.Errorf("generate card UUID: %w", err)
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return pgtype.UUID{Bytes: bytes, Valid: true}, nil
}

func parseUUID(value string) (pgtype.UUID, error) {
	var identifier pgtype.UUID
	if err := identifier.Scan(value); err != nil || !identifier.Valid {
		return pgtype.UUID{}, fmt.Errorf("parse card UUID %q", value)
	}
	return identifier, nil
}

func textValue(value string) pgtype.Text { return pgtype.Text{String: value, Valid: value != ""} }

func uuidValue(value string) pgtype.UUID {
	identifier, _ := parseUUID(value)
	return identifier
}

func timestamptz(value time.Time) pgtype.Timestamptz {
	if value.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func safeKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer(":", "-", "/", "-", "@", "-", "_", "-").Replace(value)
	return value
}

func minimum(left, right money.Decimal) money.Decimal {
	if left.Compare(right) <= 0 {
		return left
	}
	return right
}

func positiveRemainder(value money.Decimal) money.Decimal {
	if value.IsPositive() {
		return value
	}
	return money.Zero()
}
