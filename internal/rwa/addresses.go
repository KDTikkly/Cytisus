package rwa

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	compliancestore "github.com/KDTikkly/Cytisus/internal/compliance/store"
	"github.com/KDTikkly/Cytisus/internal/rwa/provider"
	"github.com/KDTikkly/Cytisus/internal/rwa/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (service *Service) RegisterExternalAddress(ctx context.Context, command RegisterAddressCommand) (ExternalAddress, error) {
	command.Address = strings.TrimSpace(command.Address)
	command.ProofReference = strings.TrimSpace(command.ProofReference)
	requestHash, err := hashValue(struct {
		Address string `json:"address"`
		Proof   string `json:"proof_reference"`
	}{command.Address, command.ProofReference})
	if err != nil {
		return ExternalAddress{}, err
	}
	identifier, err := newUUID()
	if err != nil {
		return ExternalAddress{}, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return ExternalAddress{}, err
	}
	defer tx.Rollback(ctx)
	profile, err := service.ensureProfile(ctx, tx, command.AccessToken)
	if err != nil {
		return ExternalAddress{}, err
	}
	resourceID, acquired, err := service.acquireRequest(ctx, store.New(tx), profile.CustomerReference, "rwa.address.register", command.IdempotencyKey, requestHash, identifier)
	if err != nil {
		return ExternalAddress{}, err
	}
	if !acquired {
		existing, getErr := store.New(tx).GetExternalAddress(ctx, resourceID)
		if getErr != nil {
			return ExternalAddress{}, getErr
		}
		if err := tx.Commit(ctx); err != nil {
			return ExternalAddress{}, err
		}
		if existing.Status == "PENDING_PROOF" {
			return service.verifyAndCompleteAddressProof(ctx, existing, command.ProofReference)
		}
		return addressFromStore(existing), nil
	}
	created, err := store.New(tx).CreateExternalAddress(ctx, store.CreateExternalAddressParams{
		ID: identifier, CustomerReference: profile.CustomerReference, Address: command.Address,
	})
	if err != nil {
		return ExternalAddress{}, fmt.Errorf("create RWA external address: %w", err)
	}
	if err := service.recordState(ctx, tx, "ADDRESS", identifier, "", created.Status, "USER", profile.CustomerReference, "", map[string]string{"address": command.Address}); err != nil {
		return ExternalAddress{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ExternalAddress{}, err
	}

	return service.verifyAndCompleteAddressProof(ctx, created, command.ProofReference)
}

func (service *Service) verifyAndCompleteAddressProof(ctx context.Context, address store.RwaExternalAddress, proofReference string) (ExternalAddress, error) {
	providerContext, cancel := service.withProviderTimeout(ctx)
	proof, err := service.addressVerifier.VerifyControl(providerContext, provider.AddressProofRequest{Address: address.Address, ProofReference: proofReference})
	cancel()
	if err != nil || !proof.Verified {
		return addressFromStore(address), fmt.Errorf("verify RWA address control: %w", ErrInvalidCommand)
	}
	return service.completeAddressProof(ctx, address.ID, proofReference)
}

func (service *Service) completeAddressProof(ctx context.Context, identifier pgtype.UUID, proofReference string) (ExternalAddress, error) {
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return ExternalAddress{}, err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	current, err := queries.GetExternalAddress(ctx, identifier)
	if err != nil {
		return ExternalAddress{}, err
	}
	if current.Status != "PENDING_PROOF" {
		return addressFromStore(current), nil
	}
	verified, err := queries.MarkAddressProofVerified(ctx, store.MarkAddressProofVerifiedParams{ProofReference: textValue(proofReference), ID: identifier})
	if err != nil {
		return ExternalAddress{}, err
	}
	if err := service.recordState(ctx, tx, "ADDRESS", identifier, current.Status, verified.Status, "PROVIDER", "local-address-proof-simulator", "", nil); err != nil {
		return ExternalAddress{}, err
	}
	underReview, err := queries.MarkAddressRiskReview(ctx, identifier)
	if err != nil {
		return ExternalAddress{}, err
	}
	if err := service.recordState(ctx, tx, "ADDRESS", identifier, verified.Status, underReview.Status, "SYSTEM", "rwa-address-policy", "", nil); err != nil {
		return ExternalAddress{}, err
	}
	caseID, err := newUUID()
	if err != nil {
		return ExternalAddress{}, err
	}
	complianceCase, err := compliancestore.New(tx).CreateCase(ctx, compliancestore.CreateCaseParams{
		ID: caseID, CustomerReference: current.CustomerReference, CaseType: "RWA_ADDRESS_REVIEW",
		ResourceType: "rwa.address", ResourceID: identifier.String(), Status: "OPEN",
		ReasonCode: "RWA_EXTERNAL_ADDRESS_REVIEW", NextAction: "An authorized reviewer must complete the simulated risk review before cooling begins.",
		PolicyVersion: PolicyVersion,
	})
	if err != nil {
		return ExternalAddress{}, fmt.Errorf("create RWA address compliance case: %w", err)
	}
	if _, err := compliancestore.New(tx).InsertCaseEvent(ctx, compliancestore.InsertCaseEventParams{
		CaseID: complianceCase.ID, ToStatus: "OPEN", ActorType: "SYSTEM", ActorID: "rwa-address-policy",
		ReasonCode: "RWA_EXTERNAL_ADDRESS_REVIEW", Metadata: []byte(`{"simulation":true}`),
	}); err != nil {
		return ExternalAddress{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ExternalAddress{}, err
	}
	return addressFromStore(underReview), nil
}

func (service *Service) ReviewExternalAddress(ctx context.Context, command ReviewAddressCommand) (ExternalAddress, error) {
	if !authorizedAdmin(command.Actor) || strings.TrimSpace(command.ReasonCode) == "" {
		return ExternalAddress{}, ErrAdminUnauthorized
	}
	identifier, err := parseUUID(command.AddressID)
	if err != nil {
		return ExternalAddress{}, err
	}
	current, err := store.New(service.database).GetExternalAddress(ctx, identifier)
	if err != nil || current.Status != "RISK_REVIEW" {
		return ExternalAddress{}, ErrInvalidState
	}
	providerContext, cancel := service.withProviderTimeout(ctx)
	risk, riskErr := service.addressVerifier.ReviewRisk(providerContext, current.Address)
	cancel()
	if riskErr != nil {
		return ExternalAddress{}, fmt.Errorf("review external address risk: %w", riskErr)
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return ExternalAddress{}, err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	current, err = queries.GetCustomerExternalAddressForUpdate(ctx, store.GetCustomerExternalAddressForUpdateParams{
		ID: identifier, CustomerReference: current.CustomerReference,
	})
	if err != nil || current.Status != "RISK_REVIEW" {
		return ExternalAddress{}, ErrInvalidState
	}
	approved := command.Approve && risk.Approved
	var updated store.RwaExternalAddress
	if approved {
		policy, err := queries.GetActivePolicy(ctx)
		if err != nil {
			return ExternalAddress{}, err
		}
		updated, err = queries.MarkAddressCooling(ctx, store.MarkAddressCoolingParams{
			RiskReasonCode: textValue(risk.ReasonCode), CoolingEndsAt: timestamptz(service.now().UTC().Add(time.Duration(policy.AddressCoolingHours) * time.Hour)), ID: identifier,
		})
		if err != nil {
			return ExternalAddress{}, err
		}
	} else {
		reason := command.ReasonCode
		if !risk.Approved {
			reason = risk.ReasonCode
		}
		updated, err = queries.RejectExternalAddress(ctx, store.RejectExternalAddressParams{RiskReasonCode: textValue(reason), ID: identifier})
		if err != nil {
			return ExternalAddress{}, err
		}
	}
	if err := service.recordState(ctx, tx, "ADDRESS", identifier, current.Status, updated.Status, "ADMIN", command.Actor.ID, command.ReasonCode, map[string]string{"risk_provider": risk.Provider}); err != nil {
		return ExternalAddress{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ExternalAddress{}, err
	}
	return addressFromStore(updated), nil
}

func (service *Service) ActivateExternalAddress(ctx context.Context, accessToken, addressID string) (ExternalAddress, error) {
	identifier, err := parseUUID(addressID)
	if err != nil {
		return ExternalAddress{}, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return ExternalAddress{}, err
	}
	defer tx.Rollback(ctx)
	profile, err := service.ensureProfile(ctx, tx, accessToken)
	if err != nil {
		return ExternalAddress{}, err
	}
	current, err := store.New(tx).GetCustomerExternalAddressForUpdate(ctx, store.GetCustomerExternalAddressForUpdateParams{ID: identifier, CustomerReference: profile.CustomerReference})
	if err != nil || current.Status != "COOLING" || current.CoolingEndsAt.Time.After(service.now().UTC()) {
		return ExternalAddress{}, ErrInvalidState
	}
	assets, err := store.New(tx).ListAssets(ctx)
	if err != nil {
		return ExternalAddress{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ExternalAddress{}, err
	}
	for _, asset := range assets {
		providerContext, cancel := service.withProviderTimeout(ctx)
		_, permissionErr := service.provider.SetPermission(providerContext, asset.ContractAddress, current.Address, true)
		cancel()
		if permissionErr != nil {
			return ExternalAddress{}, fmt.Errorf("permission external RWA address: %w", permissionErr)
		}
	}
	tx, err = service.database.Begin(ctx)
	if err != nil {
		return ExternalAddress{}, err
	}
	defer tx.Rollback(ctx)
	current, err = store.New(tx).GetCustomerExternalAddressForUpdate(ctx, store.GetCustomerExternalAddressForUpdateParams{ID: identifier, CustomerReference: profile.CustomerReference})
	if err != nil {
		return ExternalAddress{}, err
	}
	if current.Status == "ACTIVE" {
		return addressFromStore(current), tx.Commit(ctx)
	}
	updated, err := store.New(tx).ActivateExternalAddress(ctx, identifier)
	if err != nil {
		return ExternalAddress{}, ErrInvalidState
	}
	if err := service.recordState(ctx, tx, "ADDRESS", identifier, current.Status, updated.Status, "USER", profile.CustomerReference, "", nil); err != nil {
		return ExternalAddress{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ExternalAddress{}, err
	}
	return addressFromStore(updated), nil
}

func (service *Service) ListExternalAddresses(ctx context.Context, accessToken string) ([]ExternalAddress, error) {
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	profile, err := service.ensureProfile(ctx, tx, accessToken)
	if err != nil {
		return nil, err
	}
	rows, err := store.New(tx).ListExternalAddresses(ctx, profile.CustomerReference)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	result := make([]ExternalAddress, 0, len(rows))
	for _, row := range rows {
		result = append(result, addressFromStore(row))
	}
	return result, nil
}

func (service *Service) recordState(ctx context.Context, tx pgx.Tx, resourceType string, resourceID pgtype.UUID, fromStatus, toStatus, actorType, actorID, reasonCode string, metadata map[string]string) error {
	payload, _ := json.Marshal(metadata)
	if len(payload) == 0 || string(payload) == "null" {
		payload = []byte(`{}`)
	}
	if _, err := store.New(tx).InsertStateEvent(ctx, store.InsertStateEventParams{
		ResourceType: resourceType, ResourceID: resourceID, FromStatus: textValue(fromStatus), ToStatus: toStatus,
		ActorType: actorType, ActorID: actorID, ReasonCode: textValue(reasonCode), Metadata: payload,
	}); err != nil {
		return fmt.Errorf("insert RWA state event: %w", err)
	}
	action := "rwa." + strings.ToLower(resourceType) + "." + strings.ToLower(toStatus)
	return recordMutation(ctx, tx, mutation{
		Action: action, ResourceType: "rwa." + strings.ToLower(resourceType), ResourceID: resourceID.String(),
		ActorType: actorType, ActorID: actorID, CorrelationID: resourceID, Metadata: payload,
		AggregateType: "rwa." + strings.ToLower(resourceType), AggregateID: resourceID.String(), EventType: action, Payload: payload,
	})
}

func addressFromStore(value store.RwaExternalAddress) ExternalAddress {
	return ExternalAddress{
		ID: value.ID.String(), Address: value.Address, Status: value.Status,
		ProofReference: value.ProofReference.String, RiskReasonCode: value.RiskReasonCode.String,
		CoolingEndsAt: value.CoolingEndsAt.Time.UTC(), ActivatedAt: value.ActivatedAt.Time.UTC(),
		SuspendedAt: value.SuspendedAt.Time.UTC(), UpdatedAt: value.UpdatedAt.Time.UTC(),
	}
}
