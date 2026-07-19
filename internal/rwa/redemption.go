package rwa

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/rwa/provider"
	"github.com/KDTikkly/Cytisus/internal/rwa/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (service *Service) Redeem(ctx context.Context, command RedeemCommand) (Redemption, error) {
	session, err := service.resolver.ResolveSession(ctx, command.AccessToken)
	if err != nil {
		return Redemption{}, ErrUnauthorized
	}
	return service.startRedemption(ctx, session.CustomerReference, command.AccessToken, command.IdempotencyKey, command.MintID, false, ledger.Actor{Type: "USER", ID: session.CustomerReference})
}

func (service *Service) ForcedRedeem(ctx context.Context, command ForcedRedeemCommand) (Redemption, error) {
	if !authorizedAdmin(command.Actor) || strings.TrimSpace(command.CustomerReference) == "" || strings.TrimSpace(command.ReasonCode) == "" {
		return Redemption{}, ErrAdminUnauthorized
	}
	return service.startRedemption(ctx, command.CustomerReference, "", command.IdempotencyKey, command.MintID, true, ledger.Actor{Type: "ADMIN", ID: command.Actor.ID})
}

func (service *Service) startRedemption(ctx context.Context, customerReference, accessToken, idempotencyKey, mintIDText string, forced bool, actor ledger.Actor) (Redemption, error) {
	mintID, err := parseUUID(mintIDText)
	if err != nil {
		return Redemption{}, ErrHoldingNotFound
	}
	requestHash, err := hashValue(struct {
		MintID string `json:"mint_id"`
		Forced bool   `json:"forced"`
	}{mintIDText, forced})
	if err != nil {
		return Redemption{}, err
	}
	redemptionID, err := newUUID()
	if err != nil {
		return Redemption{}, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Redemption{}, err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	var profile store.RwaCustomerProfile
	if forced {
		profile, err = queries.GetCustomerProfileForUpdate(ctx, customerReference)
	} else {
		profile, err = service.ensureProfile(ctx, tx, accessToken)
		if err == nil {
			profile, err = queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference)
		}
	}
	if err != nil || profile.Status == "INELIGIBLE" {
		return Redemption{}, ErrInvalidState
	}
	resourceID, acquired, err := service.acquireRequest(ctx, queries, profile.CustomerReference, "rwa.redemption", idempotencyKey, requestHash, redemptionID)
	if err != nil {
		return Redemption{}, err
	}
	if !acquired {
		existing, getErr := queries.GetCustomerRedemptionRequest(ctx, store.GetCustomerRedemptionRequestParams{ID: resourceID, CustomerReference: profile.CustomerReference})
		if getErr != nil {
			return Redemption{}, getErr
		}
		if err := tx.Commit(ctx); err != nil {
			return Redemption{}, err
		}
		return redemptionFromStore(existing), nil
	}
	mint, err := queries.GetCustomerMintRequest(ctx, store.GetCustomerMintRequestParams{ID: mintID, CustomerReference: profile.CustomerReference})
	if err != nil || mint.Status != "MINTED" {
		return Redemption{}, ErrHoldingNotFound
	}
	underlyingLock, err := queries.GetUnderlyingLockForUpdate(ctx, mint.UnderlyingLockID)
	if err != nil || underlyingLock.Status != "LOCKED" || !underlyingLock.Quantity.Equal(mint.Quantity) {
		return Redemption{}, ErrInvalidState
	}
	holding, err := queries.GetBeneficialHoldingForUpdate(ctx, store.GetBeneficialHoldingForUpdateParams{
		CustomerReference: profile.CustomerReference, AssetID: mint.AssetID,
	})
	if err != nil || holding.Quantity.Compare(mint.Quantity) < 0 {
		return Redemption{}, ErrHoldingNotFound
	}
	opType := "BURN"
	if forced {
		opType = "FORCED_REDEMPTION"
	}
	opID := operationID("redemption", redemptionID)
	created, err := queries.CreateRedemptionRequest(ctx, store.CreateRedemptionRequestParams{
		ID: redemptionID, CustomerReference: profile.CustomerReference, AssetID: mint.AssetID,
		UnderlyingLockID: mint.UnderlyingLockID, Quantity: mint.Quantity, SourceAddress: mint.DestinationAddress,
		OperationID: opID, Forced: forced,
	})
	if err != nil {
		return Redemption{}, fmt.Errorf("create RWA redemption: %w", err)
	}
	policy, err := queries.GetActivePolicy(ctx)
	if err != nil {
		return Redemption{}, err
	}
	if _, err := queries.CreateChainOperation(ctx, store.CreateChainOperationParams{
		OperationID: opID, ResourceType: "REDEMPTION", ResourceID: redemptionID,
		OperationType: opType, MaxAttempts: policy.RecoveryMaxAttempts,
	}); err != nil {
		return Redemption{}, err
	}
	if err := service.recordState(ctx, tx, "REDEMPTION", redemptionID, "", created.Status, actor.Type, actor.ID, "", map[string]string{"mint_id": mintIDText}); err != nil {
		return Redemption{}, err
	}
	created, err = queries.UpdateRedemptionState(ctx, store.UpdateRedemptionStateParams{Status: "CHAIN_PENDING", ID: redemptionID})
	if err != nil {
		return Redemption{}, err
	}
	if err := service.recordState(ctx, tx, "REDEMPTION", redemptionID, "REQUESTED", created.Status, "SYSTEM", "rwa-chain-saga", "", nil); err != nil {
		return Redemption{}, err
	}
	asset, err := queries.GetAsset(ctx, mint.AssetID)
	if err != nil {
		return Redemption{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Redemption{}, err
	}

	request := provider.OperationRequest{ContractAddress: asset.ContractAddress, OperationID: opID, Account: mint.DestinationAddress, Quantity: mint.Quantity}
	providerContext, cancel := service.withProviderTimeout(ctx)
	var result provider.OperationResult
	if forced {
		result, err = service.provider.ForcedRedemption(providerContext, request)
	} else {
		result, err = service.provider.Burn(providerContext, request)
	}
	cancel()
	updated, finishErr := service.finishRedemption(ctx, redemptionID, result, err)
	if finishErr != nil {
		return updated, finishErr
	}
	return updated, nil
}

func (service *Service) finishRedemption(ctx context.Context, redemptionID pgtype.UUID, result provider.OperationResult, providerErr error) (Redemption, error) {
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Redemption{}, err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	current, err := queries.GetRedemptionRequestForUpdate(ctx, redemptionID)
	if err != nil {
		return Redemption{}, err
	}
	if current.Status == "COMPLETED" {
		return redemptionFromStore(current), tx.Commit(ctx)
	}
	if providerErr != nil || !result.Confirmed {
		failureCode := "CHAIN_OUTCOME_UNKNOWN"
		if errors.Is(providerErr, provider.ErrChainReverted) {
			failureCode = "CHAIN_REVERTED"
		}
		if _, err := queries.UpdateChainOperation(ctx, store.UpdateChainOperationParams{
			Status: "UNKNOWN", TxHash: textValue(result.TransactionHash), LastErrorCode: textValue(failureCode),
			NextAttemptAt: timestamptz(service.now().UTC()), OperationID: current.OperationID,
		}); err != nil {
			return Redemption{}, err
		}
		updated, err := queries.UpdateRedemptionState(ctx, store.UpdateRedemptionStateParams{
			Status: "RECOVERY_REQUIRED", ChainTxHash: textValue(result.TransactionHash), FailureCode: textValue(failureCode), ID: current.ID,
		})
		if err != nil {
			return Redemption{}, err
		}
		if err := service.recordState(ctx, tx, "REDEMPTION", current.ID, current.Status, updated.Status, "SYSTEM", "rwa-chain-saga", failureCode, nil); err != nil {
			return Redemption{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Redemption{}, err
		}
		return redemptionFromStore(updated), ErrOperationUnknown
	}
	updated, err := service.confirmRedemption(ctx, tx, queries, current, result)
	if err != nil {
		// The chain burn may already be final. Rolling back here intentionally
		// leaves the request recoverable; it must never release shares early.
		return Redemption{}, fmt.Errorf("confirm burn and release underlying: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Redemption{}, err
	}
	return redemptionFromStore(updated), nil
}

func (service *Service) confirmRedemption(ctx context.Context, tx pgx.Tx, queries *store.Queries, current store.RwaRedemptionRequest, result provider.OperationResult) (store.RwaRedemptionRequest, error) {
	asset, err := queries.GetAsset(ctx, current.AssetID)
	if err != nil {
		return store.RwaRedemptionRequest{}, err
	}
	providerContext, cancel := service.withProviderTimeout(ctx)
	status, err := service.provider.Operation(providerContext, asset.ContractAddress, current.OperationID)
	cancel()
	if err != nil || !status.Executed {
		return store.RwaRedemptionRequest{}, ErrRecoveryRequired
	}
	if _, err := queries.UpdateRedemptionState(ctx, store.UpdateRedemptionStateParams{
		Status: "BURN_CONFIRMED", ChainTxHash: textValue(result.TransactionHash), ID: current.ID,
	}); err != nil {
		return store.RwaRedemptionRequest{}, err
	}
	underlyingLock, err := queries.GetUnderlyingLockForUpdate(ctx, current.UnderlyingLockID)
	if err != nil || underlyingLock.Status != "LOCKED" {
		return store.RwaRedemptionRequest{}, ErrInvalidState
	}
	if _, err := queries.ReduceBeneficialHolding(ctx, store.ReduceBeneficialHoldingParams{
		Quantity: current.Quantity, CustomerReference: current.CustomerReference, AssetID: current.AssetID,
	}); err != nil {
		return store.RwaRedemptionRequest{}, fmt.Errorf("reduce beneficial RWA holding: %w", err)
	}
	if _, err := service.custodian.ReleaseSettledShares(ctx, tx, underlyingLock.ReservationID.String(), "rwa-release:"+current.ID.String(), ledger.Actor{Type: "SYSTEM", ID: "rwa-redemption-saga"}, service.now().UTC()); err != nil {
		return store.RwaRedemptionRequest{}, err
	}
	if _, err := queries.ReleaseUnderlyingLock(ctx, underlyingLock.ID); err != nil {
		return store.RwaRedemptionRequest{}, err
	}
	if _, err := queries.UpdateChainOperation(ctx, store.UpdateChainOperationParams{
		Status: "CONFIRMED", TxHash: textValue(result.TransactionHash), NextAttemptAt: timestamptz(service.now().UTC()), OperationID: current.OperationID,
	}); err != nil {
		return store.RwaRedemptionRequest{}, err
	}
	if result.ExternalEventID != "" {
		eventType := "BURN_CONFIRMED"
		if current.Forced {
			eventType = "FORCED_REDEMPTION_CONFIRMED"
		}
		if err := insertProviderEvent(ctx, queries, service.provider.Name(), result.ExternalEventID, eventType, current.OperationID, result.Payload); err != nil {
			return store.RwaRedemptionRequest{}, err
		}
	}
	updated, err := queries.UpdateRedemptionState(ctx, store.UpdateRedemptionStateParams{
		Status: "COMPLETED", ChainTxHash: textValue(result.TransactionHash), ID: current.ID,
	})
	if err != nil {
		return store.RwaRedemptionRequest{}, err
	}
	if err := service.recordState(ctx, tx, "REDEMPTION", current.ID, "BURN_CONFIRMED", updated.Status, "SYSTEM", "rwa-redemption-saga", "", map[string]string{"burn_before_release": "true"}); err != nil {
		return store.RwaRedemptionRequest{}, err
	}
	return updated, nil
}
