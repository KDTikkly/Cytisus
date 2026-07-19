package card

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KDTikkly/Cytisus/internal/card/store"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/jackc/pgx/v5"
)

func (service *Service) SpendingPower(ctx context.Context, accessToken string) (SpendingPower, error) {
	_, profile, err := service.resolveCustomer(ctx, accessToken)
	if err != nil {
		return SpendingPower{}, err
	}
	return service.calculateSpendingPower(ctx, service.database, profile)
}

func (service *Service) calculateSpendingPower(ctx context.Context, database database, profile store.CardCustomerProfile) (SpendingPower, error) {
	queries := store.New(database)
	policy, err := queries.GetPolicy(ctx, profile.PolicyVersion)
	if err != nil {
		return SpendingPower{}, fmt.Errorf("get card spending policy: %w", err)
	}
	ledgerService := ledger.NewService(database)
	settled, err := ledgerService.Balance(ctx, profile.CashLedgerAccountID.String(), "USD", ledger.DimensionSettled)
	if err != nil {
		return SpendingPower{}, err
	}
	withdrawable, err := ledgerService.Balance(ctx, profile.CashLedgerAccountID.String(), "USD", ledger.DimensionWithdrawable)
	if err != nil {
		return SpendingPower{}, err
	}
	cashEligible := minimum(positiveRemainder(settled), positiveRemainder(withdrawable))
	holds, err := ledgerService.Balance(ctx, profile.ReceivableLedgerAccountID.String(), "USD", ledger.DimensionHeld)
	if err != nil {
		return SpendingPower{}, err
	}
	receivable, err := ledgerService.Balance(ctx, profile.ReceivableLedgerAccountID.String(), "USD", ledger.DimensionReceivable)
	if err != nil {
		return SpendingPower{}, err
	}
	rows, err := queries.ListCollateralAccounts(ctx, profile.CustomerReference)
	if err != nil {
		return SpendingPower{}, fmt.Errorf("list card collateral: %w", err)
	}
	drivers := make([]CollateralDriver, 0, len(rows))
	collateralEligible := money.Zero()
	eligibleCount := 0
	maxEligible := money.Zero()
	for _, row := range rows {
		quantity, balanceErr := ledgerService.Balance(ctx, row.CustomerLedgerAccountID.String(), money.Currency(row.Symbol), ledger.DimensionSettled)
		if balanceErr != nil {
			return SpendingPower{}, balanceErr
		}
		marketValue, multiplyErr := quantity.Multiply(row.ReferencePriceUsd, amountPolicy)
		if multiplyErr != nil {
			return SpendingPower{}, multiplyErr
		}
		eligibleValue, multiplyErr := marketValue.Multiply(row.HaircutRate, amountPolicy)
		if multiplyErr != nil {
			return SpendingPower{}, multiplyErr
		}
		eligible := quantity.IsPositive() && !row.Frozen && !row.Transferring && row.QuoteStatus == "SIMULATED" && row.MarketStatus == "OPEN" &&
			service.now().UTC().Sub(row.ObservedAt.Time.UTC()) <= time.Duration(policy.QuoteMaxAgeSeconds)*time.Second
		reason := ""
		if !eligible {
			eligibleValue = money.Zero()
			switch {
			case row.Frozen:
				reason = "FROZEN"
			case row.Transferring:
				reason = "TRANSFERRING"
			case row.QuoteStatus != "SIMULATED" || service.now().UTC().Sub(row.ObservedAt.Time.UTC()) > time.Duration(policy.QuoteMaxAgeSeconds)*time.Second:
				reason = "STALE_PRICE"
			case row.MarketStatus != "OPEN":
				reason = "MARKET_UNAVAILABLE"
			default:
				reason = "NO_SETTLED_QUANTITY"
			}
		} else {
			eligibleCount++
			collateralEligible, err = collateralEligible.Add(eligibleValue)
			if err != nil {
				return SpendingPower{}, err
			}
			if eligibleValue.Compare(maxEligible) > 0 {
				maxEligible = eligibleValue
			}
		}
		drivers = append(drivers, CollateralDriver{
			Symbol: row.Symbol, DisplayName: row.DisplayName, AssetClass: row.AssetClass, Quantity: quantity,
			ReferencePrice: row.ReferencePriceUsd, MarketValueUSD: marketValue, EligibleValueUSD: eligibleValue,
			HaircutRate: row.HaircutRate, QuoteStatus: row.QuoteStatus, MarketStatus: row.MarketStatus,
			Eligible: eligible, ExclusionReason: reason, ObservedAt: row.ObservedAt.Time.UTC(),
		})
	}
	// A single or highly concentrated position receives an additional documented risk buffer.
	if collateralEligible.IsPositive() {
		multiplier := money.MustParse("1")
		if eligibleCount == 1 {
			multiplier = money.MustParse("0.85")
		} else {
			concentration, divideErr := maxEligible.Divide(collateralEligible, amountPolicy)
			if divideErr != nil {
				return SpendingPower{}, divideErr
			}
			if concentration.Compare(money.MustParse("0.70")) > 0 {
				multiplier = money.MustParse("0.90")
			}
		}
		collateralEligible, err = collateralEligible.Multiply(multiplier, amountPolicy)
		if err != nil {
			return SpendingPower{}, err
		}
	}
	gross := cashEligible
	if profile.RepaymentMode != string(RepaymentCashOnly) {
		gross, err = gross.Add(collateralEligible)
		if err != nil {
			return SpendingPower{}, err
		}
	}
	gross = minimum(gross, minimum(policy.AbsoluteSpendingCapUsd, policy.ProviderSpendingCapUsd))
	used, err := positiveRemainder(holds).Add(positiveRemainder(receivable))
	if err != nil {
		return SpendingPower{}, err
	}
	available, err := gross.Sub(used)
	if err != nil {
		return SpendingPower{}, err
	}
	available = positiveRemainder(available)
	if profile.SpendingStatus != "ACTIVE" || profile.KycStatus != "ELIGIBLE" {
		available = money.Zero()
	}
	explanation := "Eligible settled cash is the primary spending-power driver."
	if profile.RepaymentMode != string(RepaymentCashOnly) && collateralEligible.IsPositive() {
		explanation = "Eligible settled cash and risk-adjusted simulated live securities support spending power."
	}
	return SpendingPower{
		CashEligibleUSD: cashEligible, CollateralEligibleUSD: collateralEligible, GrossUSD: gross,
		OutstandingHoldsUSD: positiveRemainder(holds), ReceivableUSD: positiveRemainder(receivable), AvailableUSD: available,
		AbsoluteCapUSD: policy.AbsoluteSpendingCapUsd, ProviderCapUSD: policy.ProviderSpendingCapUsd,
		RepaymentMode: RepaymentMode(profile.RepaymentMode), SpendingStatus: profile.SpendingStatus,
		PolicyVersion: profile.PolicyVersion, Drivers: drivers, PrimaryExplanation: explanation, CalculatedAt: service.now().UTC(),
	}, nil
}

func (service *Service) SeedCollateral(ctx context.Context, command SeedCollateralCommand) (CollateralDriver, error) {
	if !service.localSimulationAllowed() {
		return CollateralDriver{}, ErrSimulatorDisabled
	}
	command.Symbol = strings.ToUpper(strings.TrimSpace(command.Symbol))
	if !idempotencyKeyPattern.MatchString(command.IdempotencyKey) || !command.Quantity.IsPositive() || command.Symbol == "" {
		return CollateralDriver{}, ErrInvalidCommand
	}
	_, profile, err := service.resolveCustomer(ctx, command.AccessToken)
	if err != nil {
		return CollateralDriver{}, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return CollateralDriver{}, err
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	locked, err := queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference)
	if err != nil {
		return CollateralDriver{}, err
	}
	asset, err := queries.GetCollateralAsset(ctx, command.Symbol)
	if err != nil {
		return CollateralDriver{}, ErrInvalidCommand
	}
	accounts, err := service.ensureCollateralAccounts(ctx, tx, locked, asset)
	if err != nil {
		return CollateralDriver{}, err
	}
	ledgerService := ledger.NewService(tx)
	posting, err := ledgerService.Post(ctx, ledger.PostingCommand{
		Scope: "card.collateral.seed", IdempotencyKey: command.IdempotencyKey,
		TransactionType: "CARD_COLLATERAL_SIMULATOR_SEED", PolicyVersion: locked.PolicyVersion,
		EffectiveAt: service.now().UTC(), Actor: ledger.Actor{Type: "SYSTEM", ID: "card-collateral-simulator"},
		Entries: []ledger.Entry{
			{AccountID: accounts.CustomerLedgerAccountID.String(), Currency: money.Currency(command.Symbol), Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: command.Quantity},
			{AccountID: accounts.ProviderInventoryLedgerAccountID.String(), Currency: money.Currency(command.Symbol), Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: command.Quantity},
		},
	})
	if err != nil {
		return CollateralDriver{}, fmt.Errorf("seed simulated live collateral: %w", err)
	}
	payload, _ := json.Marshal(map[string]string{"symbol": command.Symbol, "quantity": command.Quantity.String(), "ledger_transaction_id": posting.TransactionID})
	if err := recordMutation(ctx, tx, mutation{
		Action: "card.collateral.seeded", ResourceType: "card.collateral", ResourceID: profile.CustomerReference + ":" + command.Symbol,
		ActorType: "SYSTEM", ActorID: "card-collateral-simulator", CorrelationID: uuidValue(posting.TransactionID), Metadata: payload,
		AggregateType: "card.collateral", AggregateID: profile.CustomerReference + ":" + command.Symbol,
		EventType: "card.collateral.seeded", Payload: payload,
	}); err != nil {
		return CollateralDriver{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CollateralDriver{}, err
	}
	power, err := service.SpendingPower(ctx, command.AccessToken)
	if err != nil {
		return CollateralDriver{}, err
	}
	for _, driver := range power.Drivers {
		if driver.Symbol == command.Symbol {
			return driver, nil
		}
	}
	return CollateralDriver{}, ErrInvalidCommand
}

func (service *Service) ensureCollateralAccounts(ctx context.Context, tx pgx.Tx, profile store.CardCustomerProfile, asset store.CardCollateralAsset) (store.CardCollateralAccount, error) {
	queries := store.New(tx)
	existing, err := queries.GetCollateralAccount(ctx, store.GetCollateralAccountParams{CustomerReference: profile.CustomerReference, Symbol: asset.Symbol})
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return store.CardCollateralAccount{}, err
	}
	ledgerService := ledger.NewService(tx)
	actor := ledger.Actor{Type: "SYSTEM", ID: "card-collateral-simulator"}
	customer, err := ledgerService.OpenAccount(ctx, ledger.AccountCommand{
		AccountKey: "card.collateral." + safeKey(profile.CustomerReference) + "." + strings.ToLower(asset.Symbol),
		OwnerType:  "USER", OwnerID: profile.CustomerReference, AccountType: "SECURITIES",
		Currency: money.Currency(asset.Symbol), NormalSide: ledger.Debit, Actor: actor,
	})
	if err != nil {
		return store.CardCollateralAccount{}, err
	}
	inventory, err := ledgerService.OpenAccount(ctx, ledger.AccountCommand{
		AccountKey: "card.collateral-inventory." + strings.ToLower(asset.Symbol), OwnerType: "PROVIDER",
		OwnerID: "simulated-live-collateral", AccountType: "SECURITIES", Currency: money.Currency(asset.Symbol),
		NormalSide: ledger.Credit, Actor: actor,
	})
	if err != nil {
		return store.CardCollateralAccount{}, err
	}
	created, err := queries.InsertCollateralAccount(ctx, store.InsertCollateralAccountParams{
		CustomerReference: profile.CustomerReference, Symbol: asset.Symbol,
		CustomerLedgerAccountID: uuidValue(customer.ID), ProviderInventoryLedgerAccountID: uuidValue(inventory.ID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return queries.GetCollateralAccount(ctx, store.GetCollateralAccountParams{CustomerReference: profile.CustomerReference, Symbol: asset.Symbol})
	}
	return created, err
}

func (service *Service) UpdateCollateralQuote(ctx context.Context, command UpdateCollateralQuoteCommand) (CollateralDriver, error) {
	if !service.localSimulationAllowed() {
		return CollateralDriver{}, ErrSimulatorDisabled
	}
	command.Symbol = strings.ToUpper(strings.TrimSpace(command.Symbol))
	if command.Symbol == "" || !command.ReferencePrice.IsPositive() {
		return CollateralDriver{}, ErrInvalidCommand
	}
	if command.ObservedAt.IsZero() {
		command.ObservedAt = service.now().UTC()
	}
	if command.QuoteStatus != "SIMULATED" && command.QuoteStatus != "STALE" && command.QuoteStatus != "UNAVAILABLE" {
		return CollateralDriver{}, ErrInvalidCommand
	}
	if command.MarketStatus != "OPEN" && command.MarketStatus != "CLOSED" && command.MarketStatus != "HALTED" {
		return CollateralDriver{}, ErrInvalidCommand
	}
	updated, err := store.New(service.database).UpdateCollateralQuote(ctx, store.UpdateCollateralQuoteParams{
		Symbol: command.Symbol, ReferencePriceUsd: command.ReferencePrice, QuoteStatus: command.QuoteStatus,
		MarketStatus: command.MarketStatus, ObservedAt: timestamptz(command.ObservedAt),
	})
	if err != nil {
		return CollateralDriver{}, err
	}
	return CollateralDriver{
		Symbol: updated.Symbol, DisplayName: updated.DisplayName, AssetClass: updated.AssetClass,
		ReferencePrice: updated.ReferencePriceUsd, HaircutRate: updated.HaircutRate,
		QuoteStatus: updated.QuoteStatus, MarketStatus: updated.MarketStatus, ObservedAt: updated.ObservedAt.Time.UTC(),
	}, nil
}
