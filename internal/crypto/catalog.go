package crypto

import (
	"context"
	"errors"
	"fmt"

	"github.com/KDTikkly/Cytisus/internal/crypto/store"
	"github.com/jackc/pgx/v5"
)

func (service *Service) ListAssets(ctx context.Context) ([]Asset, error) {
	queries := store.New(service.database)
	assets, err := queries.ListAssets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list crypto assets: %w", err)
	}
	result := make([]Asset, 0, len(assets))
	for _, stored := range assets {
		networks, networkErr := queries.ListAssetNetworks(ctx, stored.Symbol)
		if networkErr != nil {
			return nil, fmt.Errorf("list %s networks: %w", stored.Symbol, networkErr)
		}
		asset := Asset{Symbol: stored.Symbol, DisplayName: stored.DisplayName, IsStablecoin: stored.IsStablecoin, Precision: stored.Precision}
		for _, network := range networks {
			asset.Networks = append(asset.Networks, Network{
				Code: network.NetworkCode, DisplayName: network.DisplayName,
				DepositEnabled: network.DepositEnabled, WithdrawalEnabled: network.WithdrawalEnabled,
				NativeDeployment: network.NativeDeployment,
			})
		}
		result = append(result, asset)
	}
	return result, nil
}

func (service *Service) validateAsset(ctx context.Context, queries *store.Queries, asset string) error {
	if asset == "USD" {
		return nil
	}
	if _, err := queries.GetAsset(ctx, asset); errors.Is(err, pgx.ErrNoRows) {
		return ErrAssetNotFound
	} else if err != nil {
		return fmt.Errorf("get crypto asset: %w", err)
	}
	return nil
}

func (service *Service) validateAssetNetwork(ctx context.Context, queries *store.Queries, asset, network string, withdrawal bool) error {
	row, err := queries.GetAssetNetwork(ctx, store.GetAssetNetworkParams{AssetSymbol: asset, NetworkCode: network})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAssetNotFound
	}
	if err != nil {
		return fmt.Errorf("get crypto asset network: %w", err)
	}
	if withdrawal && !row.WithdrawalEnabled || !withdrawal && !row.DepositEnabled || !row.NativeDeployment {
		return ErrAssetNotFound
	}
	return nil
}

func (service *Service) Portfolio(ctx context.Context, accessToken string) ([]PortfolioBalance, error) {
	_, profile, err := service.resolveCustomer(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	rows, err := store.New(service.database).ListPortfolioBalances(ctx, profile.CustomerReference)
	if err != nil {
		return nil, fmt.Errorf("list crypto portfolio: %w", err)
	}
	result := make([]PortfolioBalance, 0, len(rows))
	for _, row := range rows {
		result = append(result, PortfolioBalance{Asset: row.AssetSymbol, Settled: row.Settled, Held: row.Held, Frozen: row.Frozen})
	}
	return result, nil
}
