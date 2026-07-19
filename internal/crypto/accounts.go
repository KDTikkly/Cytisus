package crypto

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/KDTikkly/Cytisus/internal/crypto/store"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/jackc/pgx/v5"
)

type usdLedgerAccounts struct {
	RoutingClearing string
	PlatformFees    string
	VenueFees       string
}

func (service *Service) ensureAssetLedgerAccounts(
	ctx context.Context,
	tx pgx.Tx,
	profile store.CryptoCustomerProfile,
	asset string,
) (store.CryptoAssetLedgerAccount, error) {
	queries := store.New(tx)
	existing, err := queries.GetAssetLedgerAccounts(ctx, store.GetAssetLedgerAccountsParams{
		CustomerReference: profile.CustomerReference, AssetSymbol: asset,
	})
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return store.CryptoAssetLedgerAccount{}, fmt.Errorf("get crypto ledger accounts: %w", err)
	}
	ledgerService := ledger.NewService(tx)
	actor := ledger.Actor{Type: "SYSTEM", ID: "crypto-custody"}
	customer, err := ledgerService.OpenAccount(ctx, ledger.AccountCommand{
		AccountKey: "crypto.asset." + strings.ToLower(profile.CustomerReference) + "." + strings.ToLower(asset),
		OwnerType:  "USER", OwnerID: profile.CustomerReference, AccountType: "CRYPTO",
		Currency: money.Currency(asset), NormalSide: ledger.Debit, Actor: actor,
	})
	if err != nil {
		return store.CryptoAssetLedgerAccount{}, fmt.Errorf("open customer crypto ledger: %w", err)
	}
	inventory, err := ledgerService.OpenAccount(ctx, ledger.AccountCommand{
		AccountKey: "crypto.custody-inventory." + strings.ToLower(asset),
		OwnerType:  "PROVIDER", OwnerID: service.custody.Name(), AccountType: "CRYPTO",
		Currency: money.Currency(asset), NormalSide: ledger.Credit, Actor: actor,
	})
	if err != nil {
		return store.CryptoAssetLedgerAccount{}, fmt.Errorf("open custody inventory ledger: %w", err)
	}
	customerID, err := parseUUID(customer.ID)
	if err != nil {
		return store.CryptoAssetLedgerAccount{}, err
	}
	inventoryID, err := parseUUID(inventory.ID)
	if err != nil {
		return store.CryptoAssetLedgerAccount{}, err
	}
	created, err := queries.InsertAssetLedgerAccounts(ctx, store.InsertAssetLedgerAccountsParams{
		CustomerReference: profile.CustomerReference, AssetSymbol: asset,
		CustomerLedgerAccountID: customerID, CustodyInventoryLedgerAccountID: inventoryID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return queries.GetAssetLedgerAccounts(ctx, store.GetAssetLedgerAccountsParams{
			CustomerReference: profile.CustomerReference, AssetSymbol: asset,
		})
	}
	if err != nil {
		return store.CryptoAssetLedgerAccount{}, fmt.Errorf("insert crypto ledger accounts: %w", err)
	}
	return created, nil
}

func (service *Service) ensureUSDLedgerAccounts(ctx context.Context, tx pgx.Tx) (usdLedgerAccounts, error) {
	ledgerService := ledger.NewService(tx)
	actor := ledger.Actor{Type: "SYSTEM", ID: "crypto-routing"}
	commands := []ledger.AccountCommand{
		{AccountKey: "crypto.routing-clearing.usd", OwnerType: "PROVIDER", OwnerID: "simulated-liquidity-venues", AccountType: "PROVIDER_CLEARING", Currency: "USD", NormalSide: ledger.Credit, Actor: actor},
		{AccountKey: "crypto.platform-fees.usd", OwnerType: "PLATFORM", OwnerID: "cytisus", AccountType: "PLATFORM_FEE", Currency: "USD", NormalSide: ledger.Debit, Actor: actor},
		{AccountKey: "crypto.venue-fees.usd", OwnerType: "PROVIDER", OwnerID: "simulated-liquidity-venues", AccountType: "PLATFORM_FEE", Currency: "USD", NormalSide: ledger.Debit, Actor: actor},
	}
	identifiers := make([]string, 0, len(commands))
	for _, command := range commands {
		account, err := ledgerService.OpenAccount(ctx, command)
		if err != nil {
			return usdLedgerAccounts{}, fmt.Errorf("open crypto USD ledger account: %w", err)
		}
		identifiers = append(identifiers, account.ID)
	}
	return usdLedgerAccounts{RoutingClearing: identifiers[0], PlatformFees: identifiers[1], VenueFees: identifiers[2]}, nil
}
