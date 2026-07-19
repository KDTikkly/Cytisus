package rwa

import (
	"context"
	"fmt"
	"time"

	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/KDTikkly/Cytisus/internal/rwa/provider"
	"github.com/KDTikkly/Cytisus/internal/rwa/store"
)

func (service *Service) RecoverOperation(ctx context.Context, command RecoverCommand) error {
	if !authorizedAdmin(command.Actor) || len(command.OperationID) != 64 {
		return ErrAdminUnauthorized
	}
	return service.recoverOperation(ctx, command.OperationID, ledger.Actor{Type: "ADMIN", ID: command.Actor.ID})
}

func (service *Service) RecoverDueOperations(ctx context.Context, workerID string, batchSize int32) (int, error) {
	if workerID == "" {
		return 0, ErrInvalidCommand
	}
	if batchSize <= 0 || batchSize > 100 {
		batchSize = 20
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	operations, err := store.New(tx).ClaimChainOperations(ctx, store.ClaimChainOperationsParams{BatchSize: batchSize, WorkerID: textValue(workerID)})
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	completed := 0
	for _, operation := range operations {
		if err := service.recoverOperation(ctx, operation.OperationID, ledger.Actor{Type: "SYSTEM", ID: workerID}); err == nil {
			completed++
		} else {
			_ = service.deferRecovery(ctx, operation.OperationID, "RECOVERY_ATTEMPT_FAILED")
		}
	}
	_, _ = store.New(service.database).MoveExhaustedOperationsToDLQ(ctx)
	return completed, nil
}

func (service *Service) recoverOperation(ctx context.Context, operationID string, actor ledger.Actor) error {
	operation, err := store.New(service.database).GetChainOperation(ctx, operationID)
	if err != nil {
		return ErrInvalidCommand
	}
	var asset store.RwaAsset
	var account string
	var quantity money.Decimal
	if operation.ResourceType == "MINT" {
		mint, err := store.New(service.database).GetMintRequest(ctx, operation.ResourceID)
		if err != nil {
			return err
		}
		asset, err = store.New(service.database).GetAsset(ctx, mint.AssetID)
		if err != nil {
			return err
		}
		account, quantity = mint.DestinationAddress, mint.Quantity
	} else {
		redemption, err := store.New(service.database).GetRedemptionRequest(ctx, operation.ResourceID)
		if err != nil {
			return err
		}
		asset, err = store.New(service.database).GetAsset(ctx, redemption.AssetID)
		if err != nil {
			return err
		}
		account, quantity = redemption.SourceAddress, redemption.Quantity
	}
	providerContext, cancel := service.withProviderTimeout(ctx)
	status, err := service.provider.Operation(providerContext, asset.ContractAddress, operation.OperationID)
	cancel()
	if err != nil {
		return fmt.Errorf("query RWA operation for recovery: %w", err)
	}
	if status.Executed {
		externalEventID := operation.TxHash.String
		if !operation.TxHash.Valid {
			// Recovery may observe a finalized operation after the original caller
			// lost the transaction hash. Keep the provider-event replay key stable
			// so repeated recovery cannot duplicate financial effects.
			externalEventID = "recovery:" + operation.OperationID
		}
		result := provider.OperationResult{
			OperationID: operation.OperationID, TransactionHash: operation.TxHash.String,
			ExternalEventID: externalEventID, Confirmed: true,
			Payload: providerPayload(operation.OperationID, operation.TxHash.String),
		}
		if operation.ResourceType == "MINT" {
			_, err = service.finishMint(ctx, operation.ResourceID, result, nil)
		} else {
			_, err = service.finishRedemption(ctx, operation.ResourceID, result, nil)
		}
		return err
	}
	if operation.ResourceType == "MINT" {
		return service.recoverFailedMint(ctx, operation, actor)
	}
	request := provider.OperationRequest{ContractAddress: asset.ContractAddress, OperationID: operation.OperationID, Account: account, Quantity: quantity}
	providerContext, cancel = service.withProviderTimeout(ctx)
	var result provider.OperationResult
	if operation.OperationType == "FORCED_REDEMPTION" {
		result, err = service.provider.ForcedRedemption(providerContext, request)
	} else {
		result, err = service.provider.Burn(providerContext, request)
	}
	cancel()
	_, finishErr := service.finishRedemption(ctx, operation.ResourceID, result, err)
	return finishErr
}

func (service *Service) recoverFailedMint(ctx context.Context, operation store.RwaChainOperation, actor ledger.Actor) error {
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	mint, err := queries.GetMintRequestForUpdate(ctx, operation.ResourceID)
	if err != nil {
		return err
	}
	if mint.Status == "FAILED_RECOVERED" {
		return tx.Commit(ctx)
	}
	underlyingLock, err := queries.GetUnderlyingLockForUpdate(ctx, mint.UnderlyingLockID)
	if err != nil {
		return err
	}
	if underlyingLock.Status == "LOCKED" {
		if _, err := service.custodian.ReleaseSettledShares(ctx, tx, underlyingLock.ReservationID.String(), "rwa-mint-failure-release:"+mint.ID.String(), actor, service.now().UTC()); err != nil {
			return err
		}
		if _, err := queries.ReleaseUnderlyingLock(ctx, underlyingLock.ID); err != nil {
			return err
		}
	}
	if _, err := queries.UpdateChainOperation(ctx, store.UpdateChainOperationParams{
		Status: "FAILED", LastErrorCode: textValue("MINT_NOT_EXECUTED"),
		NextAttemptAt: timestamptz(service.now().UTC()), OperationID: operation.OperationID,
	}); err != nil {
		return err
	}
	updated, err := queries.UpdateMintState(ctx, store.UpdateMintStateParams{
		Status: "FAILED_RECOVERED", FailureCode: textValue("MINT_NOT_EXECUTED"), ID: mint.ID,
	})
	if err != nil {
		return err
	}
	if err := service.recordState(ctx, tx, "MINT", mint.ID, mint.Status, updated.Status, actor.Type, actor.ID, "MINT_NOT_EXECUTED", map[string]string{"underlying_released": "true"}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (service *Service) deferRecovery(ctx context.Context, operationID, reasonCode string) error {
	operation, err := store.New(service.database).GetChainOperation(ctx, operationID)
	if err != nil {
		return err
	}
	delay := time.Second * time.Duration(1<<min(operation.AttemptCount, 8))
	_, err = store.New(service.database).UpdateChainOperation(ctx, store.UpdateChainOperationParams{
		Status: "UNKNOWN", TxHash: operation.TxHash, LastErrorCode: textValue(reasonCode),
		NextAttemptAt: timestamptz(service.now().UTC().Add(delay)), OperationID: operationID,
	})
	return err
}
