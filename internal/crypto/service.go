package crypto

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

	"github.com/KDTikkly/Cytisus/internal/crypto/provider"
	"github.com/KDTikkly/Cytisus/internal/crypto/store"
	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const defaultProviderTimeout = 3 * time.Second

var idempotencyKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{7,255}$`)

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
	venues          []provider.VenueAdapter
	custody         provider.CustodyAdapter
	chainAnalytics  provider.ChainAnalyticsAdapter
	providerTimeout time.Duration
	now             func() time.Time
}

type Dependencies struct {
	Database        database
	Environment     config.Environment
	Resolver        SessionResolver
	Venues          []provider.VenueAdapter
	Custody         provider.CustodyAdapter
	ChainAnalytics  provider.ChainAnalyticsAdapter
	ProviderTimeout time.Duration
	Now             func() time.Time
}

func NewService(dependencies Dependencies) (*Service, error) {
	if dependencies.Database == nil || dependencies.Resolver == nil || len(dependencies.Venues) != 3 ||
		dependencies.Custody == nil || dependencies.ChainAnalytics == nil {
		return nil, fmt.Errorf("create crypto service: %w: dependencies are required", ErrInvalidCommand)
	}
	if dependencies.ProviderTimeout <= 0 {
		dependencies.ProviderTimeout = defaultProviderTimeout
	}
	if dependencies.Now == nil {
		dependencies.Now = time.Now
	}
	return &Service{
		database: dependencies.Database, environment: dependencies.Environment, resolver: dependencies.Resolver,
		venues: dependencies.Venues, custody: dependencies.Custody, chainAnalytics: dependencies.ChainAnalytics,
		providerTimeout: dependencies.ProviderTimeout, now: dependencies.Now,
	}, nil
}

func (service *Service) resolveCustomer(ctx context.Context, accessToken string) (CustomerSession, store.CryptoCustomerProfile, error) {
	session, err := service.resolver.ResolveSession(ctx, accessToken)
	if err != nil || session.CustomerReference == "" {
		return CustomerSession{}, store.CryptoCustomerProfile{}, ErrUnauthorized
	}
	profile, err := service.ensureProfile(ctx, session)
	if err != nil {
		return CustomerSession{}, store.CryptoCustomerProfile{}, err
	}
	return session, profile, nil
}

func (service *Service) ensureProfile(ctx context.Context, session CustomerSession) (store.CryptoCustomerProfile, error) {
	queries := store.New(service.database)
	existing, err := queries.GetCustomerProfile(ctx, session.CustomerReference)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return store.CryptoCustomerProfile{}, fmt.Errorf("get crypto profile: %w", err)
	}
	policy, err := queries.GetActivePolicy(ctx)
	if err != nil {
		return store.CryptoCustomerProfile{}, fmt.Errorf("get crypto policy: %w", err)
	}
	paperAccountID, err := parseUUID(session.PaperAccountID)
	if err != nil {
		return store.CryptoCustomerProfile{}, ErrUnauthorized
	}
	cashAccountID, err := parseUUID(session.CashLedgerAccountID)
	if err != nil {
		return store.CryptoCustomerProfile{}, ErrUnauthorized
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return store.CryptoCustomerProfile{}, fmt.Errorf("begin crypto profile: %w", err)
	}
	defer tx.Rollback(ctx)
	created, err := store.New(tx).CreateCustomerProfile(ctx, store.CreateCustomerProfileParams{
		CustomerReference: session.CustomerReference, PaperAccountID: paperAccountID,
		CashLedgerAccountID: cashAccountID, PolicyVersion: policy.PolicyVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		created, err = store.New(tx).GetCustomerProfile(ctx, session.CustomerReference)
	}
	if err != nil {
		return store.CryptoCustomerProfile{}, fmt.Errorf("create crypto profile: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return store.CryptoCustomerProfile{}, fmt.Errorf("commit crypto profile: %w", err)
	}
	return created, nil
}

func (service *Service) localSimulationAllowed() bool {
	return service.environment == config.EnvironmentLocal || service.environment == config.EnvironmentTest
}

func hashValue(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode crypto idempotency data: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
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

func textValue(value string) pgtype.Text { return pgtype.Text{String: value, Valid: value != ""} }

func timestamptz(value time.Time) pgtype.Timestamptz {
	if value.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func normalizedAsset(value string) string { return strings.ToUpper(strings.TrimSpace(value)) }

func timePointer(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	timestamp := value.Time.UTC()
	return &timestamp
}
