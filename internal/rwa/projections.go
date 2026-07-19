package rwa

import (
	"context"
	"fmt"

	"github.com/KDTikkly/Cytisus/internal/rwa/store"
)

func (service *Service) ListAssets(ctx context.Context, accessToken string) ([]Asset, error) {
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := service.ensureProfile(ctx, tx, accessToken); err != nil {
		return nil, err
	}
	policy, err := store.New(tx).GetActivePolicy(ctx)
	if err != nil {
		return nil, fmt.Errorf("get active RWA policy: %w", err)
	}
	rows, err := store.New(tx).ListAssets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list RWA assets: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	result := make([]Asset, 0, len(rows))
	for _, row := range rows {
		result = append(result, assetFromStore(row, policy))
	}
	return result, nil
}

func (service *Service) Portfolio(ctx context.Context, accessToken string) ([]Holding, error) {
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	profile, err := service.ensureProfile(ctx, tx, accessToken)
	if err != nil {
		return nil, err
	}
	rows, err := store.New(tx).ListBeneficialHoldings(ctx, profile.CustomerReference)
	if err != nil {
		return nil, fmt.Errorf("list RWA holdings: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	result := make([]Holding, 0, len(rows))
	for _, row := range rows {
		result = append(result, holdingFromStore(row))
	}
	return result, nil
}

func (service *Service) ListMints(ctx context.Context, accessToken string) ([]Mint, error) {
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	profile, err := service.ensureProfile(ctx, tx, accessToken)
	if err != nil {
		return nil, err
	}
	rows, err := store.New(tx).ListMintRequests(ctx, profile.CustomerReference)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	result := make([]Mint, 0, len(rows))
	for _, row := range rows {
		result = append(result, mintFromStore(row))
	}
	return result, nil
}

func (service *Service) ListRedemptions(ctx context.Context, accessToken string) ([]Redemption, error) {
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	profile, err := service.ensureProfile(ctx, tx, accessToken)
	if err != nil {
		return nil, err
	}
	rows, err := store.New(tx).ListRedemptionRequests(ctx, profile.CustomerReference)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	result := make([]Redemption, 0, len(rows))
	for _, row := range rows {
		result = append(result, redemptionFromStore(row))
	}
	return result, nil
}

func assetFromStore(value store.RwaAsset, policy store.RwaPolicy) Asset {
	return Asset{
		ID: value.ID.String(), InstrumentID: value.InstrumentID.String(), UnderlyingSymbol: value.UnderlyingSymbol,
		TokenName: value.TokenName, TokenSymbol: value.TokenSymbol, TokenDecimals: value.TokenDecimals,
		ContractAddress: value.ContractAddress, PlatformVaultAddress: value.PlatformVaultAddress,
		ChainName: policy.ChainName, ChainID: policy.ChainID, SimulationDisclaimer: value.SimulationDisclaimer,
	}
}

func holdingFromStore(value store.RwaBeneficialHolding) Holding {
	return Holding{AssetID: value.AssetID.String(), CustodyMode: value.CustodyMode, DestinationAddress: value.DestinationAddress, Quantity: value.Quantity, UpdatedAt: value.UpdatedAt.Time.UTC()}
}

func mintFromStore(value store.RwaMintRequest) Mint {
	return Mint{
		ID: value.ID.String(), AssetID: value.AssetID.String(), UnderlyingLockID: value.UnderlyingLockID.String(),
		Quantity: value.Quantity, CustodyMode: value.CustodyMode, DestinationAddress: value.DestinationAddress,
		OperationID: value.OperationID, Status: value.Status, ChainTransactionHash: value.ChainTxHash.String,
		FailureCode: value.FailureCode.String, CreatedAt: value.CreatedAt.Time.UTC(), UpdatedAt: value.UpdatedAt.Time.UTC(),
	}
}

func redemptionFromStore(value store.RwaRedemptionRequest) Redemption {
	return Redemption{
		ID: value.ID.String(), AssetID: value.AssetID.String(), UnderlyingLockID: value.UnderlyingLockID.String(),
		Quantity: value.Quantity, SourceAddress: value.SourceAddress, OperationID: value.OperationID,
		Forced: value.Forced, Status: value.Status, ChainTransactionHash: value.ChainTxHash.String,
		FailureCode: value.FailureCode.String, CompletedAt: value.CompletedAt.Time.UTC(),
	}
}
