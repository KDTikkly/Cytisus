package rwa

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/rwa/provider"
	"github.com/KDTikkly/Cytisus/internal/rwa/store"
	"github.com/KDTikkly/Cytisus/internal/securities"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const defaultProviderTimeout = 4 * time.Second

type database interface {
	store.DBTX
	Begin(context.Context) (pgx.Tx, error)
}

type ShareCustodian interface {
	ReserveSettledShares(context.Context, pgx.Tx, securities.ReserveSharesCommand) (securities.ShareReservation, error)
	ReleaseSettledShares(context.Context, pgx.Tx, string, string, securitiesLedgerActor, time.Time) (securities.ShareReservation, error)
}

// securitiesLedgerActor is an alias declared below through the concrete
// Ledger actor type; keeping it named makes the application boundary readable.
type securitiesLedgerActor = ledger.Actor

type Service struct {
	database        database
	environment     config.Environment
	resolver        SessionResolver
	custodian       ShareCustodian
	provider        provider.Adapter
	addressVerifier provider.AddressVerifier
	providerTimeout time.Duration
	now             func() time.Time
}

type Dependencies struct {
	Database        database
	Environment     config.Environment
	Resolver        SessionResolver
	Custodian       ShareCustodian
	Provider        provider.Adapter
	AddressVerifier provider.AddressVerifier
	ProviderTimeout time.Duration
	Now             func() time.Time
}

func NewService(dependencies Dependencies) (*Service, error) {
	if dependencies.Database == nil || dependencies.Resolver == nil || dependencies.Custodian == nil ||
		dependencies.Provider == nil || dependencies.AddressVerifier == nil {
		return nil, fmt.Errorf("create RWA service: %w: dependencies are required", ErrInvalidCommand)
	}
	if dependencies.Environment == config.EnvironmentProduction {
		return nil, provider.ErrProductionMode
	}
	if dependencies.ProviderTimeout <= 0 {
		dependencies.ProviderTimeout = defaultProviderTimeout
	}
	if dependencies.Now == nil {
		dependencies.Now = time.Now
	}
	capability, err := dependencies.Provider.Capabilities(context.Background())
	if err != nil {
		return nil, fmt.Errorf("read RWA provider capability: %w", err)
	}
	if capability.ChainID != provider.BaseAnvilChainID || capability.ChainName != "BASE_ANVIL" ||
		!capability.Simulated || !capability.Permissioned || !capability.WholeSharesOnly || capability.BridgeSupported {
		return nil, fmt.Errorf("create RWA service: %w: incompatible provider capability", ErrInvalidCommand)
	}
	return &Service{
		database: dependencies.Database, environment: dependencies.Environment, resolver: dependencies.Resolver,
		custodian: dependencies.Custodian, provider: dependencies.Provider, addressVerifier: dependencies.AddressVerifier,
		providerTimeout: dependencies.ProviderTimeout, now: dependencies.Now,
	}, nil
}

func (service *Service) ensureProfile(ctx context.Context, tx pgx.Tx, accessToken string) (store.RwaCustomerProfile, error) {
	session, err := service.resolver.ResolveSession(ctx, accessToken)
	if err != nil {
		return store.RwaCustomerProfile{}, ErrUnauthorized
	}
	queries := store.New(tx)
	profile, err := queries.GetCustomerProfile(ctx, session.CustomerReference)
	if err == nil {
		if profile.PaperAccountID.String() != session.PaperAccountID || profile.CashLedgerAccountID.String() != session.CashLedgerAccountID {
			return store.RwaCustomerProfile{}, ErrUnauthorized
		}
		return profile, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return store.RwaCustomerProfile{}, fmt.Errorf("get RWA profile: %w", err)
	}
	policy, err := queries.GetActivePolicy(ctx)
	if err != nil {
		return store.RwaCustomerProfile{}, fmt.Errorf("get RWA policy: %w", err)
	}
	created, err := queries.CreateCustomerProfile(ctx, store.CreateCustomerProfileParams{
		CustomerReference: session.CustomerReference, PaperAccountID: uuidValue(session.PaperAccountID),
		CashLedgerAccountID: uuidValue(session.CashLedgerAccountID), PolicyVersion: policy.PolicyVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return queries.GetCustomerProfile(ctx, session.CustomerReference)
	}
	if err != nil {
		return store.RwaCustomerProfile{}, fmt.Errorf("create RWA profile: %w", err)
	}
	return created, nil
}

func (service *Service) acquireRequest(ctx context.Context, queries *store.Queries, customerReference, scope, key, requestHash string, resourceID pgtype.UUID) (pgtype.UUID, bool, error) {
	if len(key) < 8 || len(key) > 256 || strings.TrimSpace(key) != key {
		return pgtype.UUID{}, false, ErrInvalidCommand
	}
	created, err := queries.InsertCommandRequest(ctx, store.InsertCommandRequestParams{
		CustomerReference: customerReference, Scope: scope, IdempotencyKey: key,
		RequestHash: requestHash, ResourceID: resourceID,
	})
	if err == nil {
		return created.ResourceID, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, false, fmt.Errorf("acquire RWA command request: %w", err)
	}
	existing, err := queries.GetCommandRequest(ctx, store.GetCommandRequestParams{
		CustomerReference: customerReference, Scope: scope, IdempotencyKey: key,
	})
	if err != nil {
		return pgtype.UUID{}, false, fmt.Errorf("get RWA command request: %w", err)
	}
	if existing.RequestHash != requestHash {
		return pgtype.UUID{}, false, ErrIdempotencyConflict
	}
	return existing.ResourceID, false, nil
}

func (service *Service) withProviderTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, service.providerTimeout)
}

func (service *Service) ProviderCapabilities(ctx context.Context) (provider.Capability, error) {
	return service.provider.Capabilities(ctx)
}

func (service *Service) ProviderHealth(ctx context.Context) (provider.Health, error) {
	providerContext, cancel := service.withProviderTimeout(ctx)
	defer cancel()
	return service.provider.Health(providerContext)
}

func hashValue(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode RWA idempotency data: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func operationID(resourceType string, identifier pgtype.UUID) string {
	digest := sha256.Sum256([]byte("cytisus:rwa:" + resourceType + ":" + identifier.String()))
	return hex.EncodeToString(digest[:])
}

func newUUID() (pgtype.UUID, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return pgtype.UUID{}, fmt.Errorf("generate RWA UUID: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return pgtype.UUID{Bytes: value, Valid: true}, nil
}

func parseUUID(value string) (pgtype.UUID, error) {
	var identifier pgtype.UUID
	if err := identifier.Scan(value); err != nil || !identifier.Valid {
		return pgtype.UUID{}, ErrInvalidCommand
	}
	return identifier, nil
}

func uuidValue(value string) pgtype.UUID {
	identifier, _ := parseUUID(value)
	return identifier
}

func textValue(value string) pgtype.Text { return pgtype.Text{String: value, Valid: value != ""} }

func timestamptz(value time.Time) pgtype.Timestamptz {
	if value.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func authorizedAdmin(actor AdminActor) bool {
	if strings.TrimSpace(actor.ID) == "" {
		return false
	}
	switch actor.Role {
	case AdminRoleOps, AdminRoleRisk, AdminRoleAdmin, AdminRoleAuditor:
		return true
	default:
		return false
	}
}
