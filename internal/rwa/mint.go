package rwa

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/rwa/provider"
	"github.com/KDTikkly/Cytisus/internal/rwa/store"
	"github.com/KDTikkly/Cytisus/internal/securities"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (service *Service) Mint(ctx context.Context, command MintCommand) (Mint, error) {
	command.CustodyMode = strings.ToUpper(strings.TrimSpace(command.CustodyMode))
	if command.CustodyMode == "" {
		command.CustodyMode = CustodyVault
	}
	if !command.Quantity.IsPositive() || !command.Quantity.IsInteger() ||
		(command.CustodyMode != CustodyVault && command.CustodyMode != CustodyExternal) {
		return Mint{}, ErrInvalidCommand
	}
	assetID, err := parseUUID(command.AssetID)
	if err != nil {
		return Mint{}, ErrAssetNotFound
	}
	requestHash, err := hashValue(struct {
		AssetID         string `json:"asset_id"`
		Quantity        string `json:"quantity"`
		CustodyMode     string `json:"custody_mode"`
		ExternalAddress string `json:"external_address_id"`
	}{command.AssetID, command.Quantity.String(), command.CustodyMode, command.ExternalAddressID})
	if err != nil {
		return Mint{}, err
	}
	mintID, err := newUUID()
	if err != nil {
		return Mint{}, err
	}
	lockID, err := newUUID()
	if err != nil {
		return Mint{}, err
	}
	reservationID, err := newUUID()
	if err != nil {
		return Mint{}, err
	}

	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Mint{}, err
	}
	defer tx.Rollback(ctx)
	profile, err := service.ensureProfile(ctx, tx, command.AccessToken)
	if err != nil {
		return Mint{}, err
	}
	queries := store.New(tx)
	if profile, err = queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference); err != nil || profile.Status != "ELIGIBLE" {
		return Mint{}, ErrInvalidState
	}
	resourceID, acquired, err := service.acquireRequest(ctx, queries, profile.CustomerReference, "rwa.mint", command.IdempotencyKey, requestHash, mintID)
	if err != nil {
		return Mint{}, err
	}
	if !acquired {
		existing, getErr := queries.GetCustomerMintRequest(ctx, store.GetCustomerMintRequestParams{ID: resourceID, CustomerReference: profile.CustomerReference})
		if getErr != nil {
			return Mint{}, getErr
		}
		if err := tx.Commit(ctx); err != nil {
			return Mint{}, err
		}
		return mintFromStore(existing), nil
	}
	asset, err := queries.GetAsset(ctx, assetID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Mint{}, ErrAssetNotFound
	}
	if err != nil {
		return Mint{}, err
	}
	destinationAddress := asset.PlatformVaultAddress
	destinationAddressID := pgtype.UUID{}
	if command.CustodyMode == CustodyExternal {
		destinationAddressID, err = parseUUID(command.ExternalAddressID)
		if err != nil {
			return Mint{}, ErrAddressNotActive
		}
		address, getErr := queries.GetCustomerExternalAddressForUpdate(ctx, store.GetCustomerExternalAddressForUpdateParams{
			ID: destinationAddressID, CustomerReference: profile.CustomerReference,
		})
		if getErr != nil || address.Status != "ACTIVE" {
			return Mint{}, ErrAddressNotActive
		}
		destinationAddress = address.Address
	}
	reservation, err := service.custodian.ReserveSettledShares(ctx, tx, securities.ReserveSharesCommand{
		ReservationID: reservationID.String(), PaperAccountID: profile.PaperAccountID.String(),
		InstrumentID: asset.InstrumentID.String(), Symbol: asset.UnderlyingSymbol, Quantity: command.Quantity,
		IdempotencyKey: "rwa-lock:" + mintID.String(), Actor: ledger.Actor{Type: "USER", ID: profile.CustomerReference},
		EffectiveAt: service.now().UTC(),
	})
	if errors.Is(err, securities.ErrInsufficientPosition) {
		return Mint{}, ErrInsufficientShares
	}
	if err != nil {
		return Mint{}, err
	}
	underlyingLock, err := queries.CreateUnderlyingLock(ctx, store.CreateUnderlyingLockParams{
		ID: lockID, CustomerReference: profile.CustomerReference, AssetID: asset.ID,
		ReservationID: uuidValue(reservation.ID), Quantity: command.Quantity,
	})
	if err != nil {
		return Mint{}, fmt.Errorf("create RWA underlying lock: %w", err)
	}
	opID := operationID("mint", mintID)
	created, err := queries.CreateMintRequest(ctx, store.CreateMintRequestParams{
		ID: mintID, CustomerReference: profile.CustomerReference, AssetID: asset.ID,
		UnderlyingLockID: underlyingLock.ID, Quantity: command.Quantity, CustodyMode: command.CustodyMode,
		DestinationAddressID: destinationAddressID, DestinationAddress: destinationAddress, OperationID: opID,
	})
	if err != nil {
		return Mint{}, fmt.Errorf("create RWA mint: %w", err)
	}
	policy, err := queries.GetActivePolicy(ctx)
	if err != nil {
		return Mint{}, err
	}
	if _, err := queries.CreateChainOperation(ctx, store.CreateChainOperationParams{
		OperationID: opID, ResourceType: "MINT", ResourceID: mintID, OperationType: "MINT", MaxAttempts: policy.RecoveryMaxAttempts,
	}); err != nil {
		return Mint{}, fmt.Errorf("create mint chain operation: %w", err)
	}
	if err := service.recordState(ctx, tx, "MINT", mintID, "", created.Status, "USER", profile.CustomerReference, "", map[string]string{
		"quantity": command.Quantity.String(), "underlying_lock_id": lockID.String(),
	}); err != nil {
		return Mint{}, err
	}
	created, err = queries.UpdateMintState(ctx, store.UpdateMintStateParams{Status: "CHAIN_PENDING", ID: mintID})
	if err != nil {
		return Mint{}, err
	}
	if err := service.recordState(ctx, tx, "MINT", mintID, "UNDERLYING_LOCKED", created.Status, "SYSTEM", "rwa-chain-saga", "", nil); err != nil {
		return Mint{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Mint{}, err
	}

	providerContext, cancel := service.withProviderTimeout(ctx)
	result, providerErr := service.provider.Mint(providerContext, provider.OperationRequest{
		ContractAddress: asset.ContractAddress, OperationID: opID, Account: destinationAddress, Quantity: command.Quantity,
	})
	cancel()
	updated, finalErr := service.finishMint(ctx, mintID, result, providerErr)
	if finalErr != nil {
		return updated, finalErr
	}
	return updated, nil
}

func (service *Service) finishMint(ctx context.Context, mintID pgtype.UUID, result provider.OperationResult, providerErr error) (Mint, error) {
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return Mint{}, err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	current, err := queries.GetMintRequestForUpdate(ctx, mintID)
	if err != nil {
		return Mint{}, err
	}
	if current.Status == "MINTED" || current.Status == "FAILED_RECOVERED" {
		return mintFromStore(current), tx.Commit(ctx)
	}
	if providerErr != nil || !result.Confirmed {
		failureCode := "CHAIN_OUTCOME_UNKNOWN"
		if errors.Is(providerErr, provider.ErrChainReverted) {
			failureCode = "CHAIN_REVERTED"
		}
		_, err = queries.UpdateChainOperation(ctx, store.UpdateChainOperationParams{
			Status: "UNKNOWN", TxHash: textValue(result.TransactionHash), LastErrorCode: textValue(failureCode),
			NextAttemptAt: timestamptz(service.now().UTC()), OperationID: current.OperationID,
		})
		if err != nil {
			return Mint{}, err
		}
		updated, err := queries.UpdateMintState(ctx, store.UpdateMintStateParams{
			Status: "RECOVERY_REQUIRED", ChainTxHash: textValue(result.TransactionHash), FailureCode: textValue(failureCode), ID: mintID,
		})
		if err != nil {
			return Mint{}, err
		}
		if err := service.recordState(ctx, tx, "MINT", mintID, current.Status, updated.Status, "SYSTEM", "rwa-chain-saga", failureCode, nil); err != nil {
			return Mint{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Mint{}, err
		}
		return mintFromStore(updated), ErrOperationUnknown
	}
	updated, err := service.confirmMint(ctx, tx, queries, current, result)
	if err != nil {
		return Mint{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Mint{}, err
	}
	return mintFromStore(updated), nil
}

func (service *Service) confirmMint(ctx context.Context, tx pgx.Tx, queries *store.Queries, current store.RwaMintRequest, result provider.OperationResult) (store.RwaMintRequest, error) {
	if _, err := queries.UpdateChainOperation(ctx, store.UpdateChainOperationParams{
		Status: "CONFIRMED", TxHash: textValue(result.TransactionHash), NextAttemptAt: timestamptz(service.now().UTC()), OperationID: current.OperationID,
	}); err != nil {
		return store.RwaMintRequest{}, err
	}
	if result.ExternalEventID != "" {
		if err := insertProviderEvent(ctx, queries, service.provider.Name(), result.ExternalEventID, "MINT_CONFIRMED", current.OperationID, result.Payload); err != nil {
			return store.RwaMintRequest{}, err
		}
	}
	if _, err := queries.UpsertBeneficialHolding(ctx, store.UpsertBeneficialHoldingParams{
		CustomerReference: current.CustomerReference, AssetID: current.AssetID, CustodyMode: current.CustodyMode,
		DestinationAddress: current.DestinationAddress, Quantity: current.Quantity,
	}); err != nil {
		return store.RwaMintRequest{}, fmt.Errorf("record beneficial RWA holding: %w", err)
	}
	updated, err := queries.UpdateMintState(ctx, store.UpdateMintStateParams{
		Status: "MINTED", ChainTxHash: textValue(result.TransactionHash), ID: current.ID,
	})
	if err != nil {
		return store.RwaMintRequest{}, err
	}
	if err := service.recordState(ctx, tx, "MINT", current.ID, current.Status, updated.Status, "PROVIDER", service.provider.Name(), "", map[string]string{"chain_tx_hash": result.TransactionHash}); err != nil {
		return store.RwaMintRequest{}, err
	}
	return updated, nil
}

func insertProviderEvent(ctx context.Context, queries *store.Queries, providerName, externalID, eventType, operationID string, payload []byte) error {
	digest := sha256.Sum256(payload)
	payloadHash := hex.EncodeToString(digest[:])
	_, err := queries.InsertProviderEvent(ctx, store.InsertProviderEventParams{
		Provider: providerName, ExternalEventID: externalID, EventType: eventType, PayloadHash: payloadHash, OperationID: operationID,
	})
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	existing, err := queries.GetProviderEvent(ctx, store.GetProviderEventParams{Provider: providerName, ExternalEventID: externalID})
	if err != nil {
		return err
	}
	if existing.PayloadHash != payloadHash || existing.OperationID != operationID || existing.EventType != eventType {
		return ledger.ErrProviderEventConflict
	}
	return nil
}

func providerPayload(operationID, transactionHash string) []byte {
	payload, _ := json.Marshal(map[string]string{"operation_id": operationID, "transaction_hash": transactionHash})
	return payload
}
