package router

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/KDTikkly/Cytisus/internal/crypto/provider"
	"github.com/KDTikkly/Cytisus/internal/money"
)

var (
	calculationPolicy = money.RoundingPolicy{Version: "crypto-routing-calculation-v1", DecimalPlaces: 18, Mode: money.RoundHalfEven}
	quantityPolicy    = money.RoundingPolicy{Version: "crypto-routing-quantity-v1", DecimalPlaces: 18, Mode: money.RoundTowardZero}
	completionDust    = money.MustParse("0.000001")
)

type Router struct {
	venues          []provider.VenueAdapter
	platformFeeRate money.Decimal
	quoteMaxAge     time.Duration
	providerTimeout time.Duration
	now             func() time.Time
}

type Dependencies struct {
	Venues          []provider.VenueAdapter
	PlatformFeeRate money.Decimal
	QuoteMaxAge     time.Duration
	ProviderTimeout time.Duration
	Now             func() time.Time
}

func New(dependencies Dependencies) (*Router, error) {
	if len(dependencies.Venues) != 3 || dependencies.PlatformFeeRate.Sign() < 0 || dependencies.PlatformFeeRate.Compare(money.MustParse("1")) >= 0 {
		return nil, provider.ErrInvalidRequest
	}
	if dependencies.QuoteMaxAge <= 0 {
		dependencies.QuoteMaxAge = 5 * time.Second
	}
	if dependencies.ProviderTimeout <= 0 {
		dependencies.ProviderTimeout = 2 * time.Second
	}
	if dependencies.Now == nil {
		dependencies.Now = time.Now
	}
	return &Router{
		venues: dependencies.Venues, platformFeeRate: dependencies.PlatformFeeRate,
		quoteMaxAge: dependencies.QuoteMaxAge, providerTimeout: dependencies.ProviderTimeout, now: dependencies.Now,
	}, nil
}

func (router *Router) Sell(
	ctx context.Context,
	asset string,
	quantity money.Decimal,
	scenario provider.Scenario,
	clientOrderPrefix string,
) (Result, error) {
	if router == nil || asset == "" || asset == "USD" || !quantity.IsPositive() || clientOrderPrefix == "" {
		return Result{}, provider.ErrInvalidRequest
	}
	candidates, attempts := router.quotes(ctx, asset, provider.SideSell, scenario)
	result := Result{Side: provider.SideSell, Asset: asset, InputAmount: quantity, Attempts: attempts}
	if len(candidates) == 0 {
		result.Status = "FAILED"
		result.FailureCode = "NO_EXECUTABLE_LIQUIDITY"
		return result, nil
	}
	result.ReferencePrice = referencePrice(candidates, provider.SideSell)
	remaining := quantity
	for _, candidate := range candidates {
		if !remaining.IsPositive() {
			break
		}
		allocation := minimum(remaining, candidate.quote.AvailableQuantity)
		child := router.execute(ctx, candidate, provider.SideSell, allocation, scenario, clientOrderPrefix, int16(len(result.Children)+1))
		result.Children = append(result.Children, child)
		if !child.FilledQuantity.IsPositive() {
			continue
		}
		var err error
		remaining, err = remaining.Sub(child.FilledQuantity)
		if err != nil {
			return Result{}, err
		}
		if err := addChildTotals(&result, child); err != nil {
			return Result{}, err
		}
	}
	return router.finalize(result, remaining)
}

func (router *Router) Buy(
	ctx context.Context,
	asset string,
	usdBudget money.Decimal,
	scenario provider.Scenario,
	clientOrderPrefix string,
) (Result, error) {
	if router == nil || asset == "" || asset == "USD" || !usdBudget.IsPositive() || clientOrderPrefix == "" {
		return Result{}, provider.ErrInvalidRequest
	}
	candidates, attempts := router.quotes(ctx, asset, provider.SideBuy, scenario)
	result := Result{Side: provider.SideBuy, Asset: asset, InputAmount: usdBudget, UnspentUSD: usdBudget, Attempts: attempts}
	if len(candidates) == 0 {
		result.Status = "FAILED"
		result.FailureCode = "NO_EXECUTABLE_LIQUIDITY"
		return result, nil
	}
	result.ReferencePrice = referencePrice(candidates, provider.SideBuy)
	remainingBudget := usdBudget
	for _, candidate := range candidates {
		if remainingBudget.Compare(completionDust) <= 0 {
			break
		}
		platformUnitFee, err := candidate.quote.Ask.Multiply(router.platformFeeRate, calculationPolicy)
		if err != nil {
			return Result{}, err
		}
		totalUnitCost, err := candidate.effectivePrice.Add(platformUnitFee)
		if err != nil {
			return Result{}, err
		}
		affordable, err := remainingBudget.Divide(totalUnitCost, quantityPolicy)
		if err != nil {
			return Result{}, err
		}
		allocation := minimum(affordable, candidate.quote.AvailableQuantity)
		if !allocation.IsPositive() {
			continue
		}
		child := router.execute(ctx, candidate, provider.SideBuy, allocation, scenario, clientOrderPrefix, int16(len(result.Children)+1))
		result.Children = append(result.Children, child)
		if !child.FilledQuantity.IsPositive() {
			continue
		}
		platformFee, err := child.GrossUSD.Multiply(router.platformFeeRate, calculationPolicy)
		if err != nil {
			return Result{}, err
		}
		childCost, err := child.GrossUSD.Add(child.VenueFeeUSD)
		if err != nil {
			return Result{}, err
		}
		childCost, err = childCost.Add(platformFee)
		if err != nil {
			return Result{}, err
		}
		remainingBudget, err = remainingBudget.Sub(childCost)
		if err != nil {
			return Result{}, err
		}
		if remainingBudget.Sign() < 0 {
			return Result{}, fmt.Errorf("buy route exceeded USD budget: %w", provider.ErrInvalidRequest)
		}
		if err := addChildTotals(&result, child); err != nil {
			return Result{}, err
		}
	}
	result.UnspentUSD = remainingBudget
	return router.finalize(result, remainingBudget)
}

type candidate struct {
	adapter        provider.VenueAdapter
	quote          provider.Quote
	effectivePrice money.Decimal
}

func (router *Router) quotes(
	ctx context.Context,
	asset string,
	side provider.Side,
	scenario provider.Scenario,
) ([]candidate, []Attempt) {
	candidates := make([]candidate, 0, len(router.venues))
	attempts := make([]Attempt, 0, len(router.venues))
	for _, venue := range router.venues {
		providerContext, cancel := context.WithTimeout(ctx, router.providerTimeout)
		quote, err := venue.Quote(providerContext, provider.QuoteRequest{Asset: asset, Scenario: scenario})
		cancel()
		if err != nil {
			attempts = append(attempts, Attempt{Venue: venue.Name(), FailureCode: providerFailureCode(err)})
			continue
		}
		attempt := Attempt{Venue: quote.Venue, Quote: &quote}
		if quote.Status != provider.QuoteExecutable || router.now().UTC().Sub(quote.ObservedAt.UTC()) > router.quoteMaxAge {
			attempt.FailureCode = "STALE_QUOTE"
			attempts = append(attempts, attempt)
			continue
		}
		feePerUnit, calcErr := quote.Ask.Multiply(quote.FeeRate, calculationPolicy)
		effective := money.Zero()
		if calcErr == nil && side == provider.SideBuy {
			effective, calcErr = quote.Ask.Add(feePerUnit)
		} else if calcErr == nil {
			feePerUnit, calcErr = quote.Bid.Multiply(quote.FeeRate, calculationPolicy)
			if calcErr == nil {
				effective, calcErr = quote.Bid.Sub(feePerUnit)
			}
		}
		if calcErr != nil || !effective.IsPositive() {
			attempt.FailureCode = "INVALID_QUOTE"
			attempts = append(attempts, attempt)
			continue
		}
		attempts = append(attempts, attempt)
		candidates = append(candidates, candidate{adapter: venue, quote: quote, effectivePrice: effective})
	}
	sort.SliceStable(candidates, func(left, right int) bool {
		comparison := candidates[left].effectivePrice.Compare(candidates[right].effectivePrice)
		if side == provider.SideSell {
			comparison = -comparison
		}
		if comparison == 0 {
			return candidates[left].quote.Venue < candidates[right].quote.Venue
		}
		return comparison < 0
	})
	return candidates, attempts
}

func (router *Router) execute(
	ctx context.Context,
	candidate candidate,
	side provider.Side,
	quantity money.Decimal,
	scenario provider.Scenario,
	prefix string,
	sequence int16,
) Child {
	clientOrderID := fmt.Sprintf("%s.%d.%s", prefix, sequence, candidate.quote.Venue)
	child := Child{
		Sequence: sequence, Venue: candidate.quote.Venue, ClientOrderID: clientOrderID,
		RequestedQuantity: quantity, QuoteBid: candidate.quote.Bid, QuoteAsk: candidate.quote.Ask,
		VenueFeeRate: candidate.quote.FeeRate, EffectiveUnitPrice: candidate.effectivePrice,
		QuoteObservedAt: candidate.quote.ObservedAt,
	}
	providerContext, cancel := context.WithTimeout(ctx, router.providerTimeout)
	execution, err := candidate.adapter.Execute(providerContext, provider.ExecutionRequest{
		ClientOrderID: clientOrderID, Asset: candidate.quote.Asset, Side: side,
		Quantity: quantity, Quote: candidate.quote, Scenario: scenario,
	})
	cancel()
	if err != nil {
		child.FailureCode = providerFailureCode(err)
		child.Status = "FAILED"
		if child.FailureCode == "TIMEOUT" {
			child.Status = "TIMEOUT"
		}
		return child
	}
	child.ProviderOrderID = execution.ProviderOrderID
	child.ExternalFillID = execution.ExternalFillID
	child.FilledQuantity = execution.FilledQuantity
	child.ExecutionPrice = execution.Price
	child.Status = execution.Status
	child.FailureCode = execution.FailureCode
	child.OccurredAt = execution.OccurredAt
	child.ProviderPayload = execution.Payload
	if execution.FilledQuantity.IsPositive() {
		child.GrossUSD, err = execution.FilledQuantity.Multiply(execution.Price, calculationPolicy)
		if err == nil {
			child.VenueFeeUSD, err = child.GrossUSD.Multiply(candidate.quote.FeeRate, calculationPolicy)
		}
		if err != nil {
			child.Status = "FAILED"
			child.FailureCode = "CALCULATION_FAILED"
			child.FilledQuantity = money.Zero()
			child.GrossUSD = money.Zero()
			child.VenueFeeUSD = money.Zero()
		}
	}
	return child
}

func (router *Router) finalize(result Result, remaining money.Decimal) (Result, error) {
	if !result.FilledQuantity.IsPositive() {
		result.Status = "FAILED"
		result.FailureCode = "NO_EXECUTABLE_LIQUIDITY"
		return result, nil
	}
	average, err := result.GrossUSD.Divide(result.FilledQuantity, calculationPolicy)
	if err != nil {
		return Result{}, err
	}
	result.AverageExecutionPrice = average
	result.PlatformFeeUSD, err = result.GrossUSD.Multiply(router.platformFeeRate, calculationPolicy)
	if err != nil {
		return Result{}, err
	}
	if result.Side == provider.SideBuy {
		result.FinalCustomerUSD, err = result.GrossUSD.Add(result.VenueFeeUSD)
		if err == nil {
			result.FinalCustomerUSD, err = result.FinalCustomerUSD.Add(result.PlatformFeeUSD)
		}
	} else {
		result.FinalCustomerUSD, err = result.GrossUSD.Sub(result.VenueFeeUSD)
		if err == nil {
			result.FinalCustomerUSD, err = result.FinalCustomerUSD.Sub(result.PlatformFeeUSD)
		}
	}
	if err != nil || !result.FinalCustomerUSD.IsPositive() {
		return Result{}, fmt.Errorf("calculate final customer consideration: %w", err)
	}
	referenceGross, err := result.FilledQuantity.Multiply(result.ReferencePrice, calculationPolicy)
	if err != nil {
		return Result{}, err
	}
	if result.Side == provider.SideBuy {
		result.PriceImprovementUSD, err = referenceGross.Sub(result.GrossUSD)
	} else {
		result.PriceImprovementUSD, err = result.GrossUSD.Sub(referenceGross)
	}
	if err != nil {
		return Result{}, err
	}
	if result.PriceImprovementUSD.Sign() < 0 {
		result.PriceImprovementUSD = money.Zero()
	}
	complete := false
	if result.Side == provider.SideSell {
		complete = !remaining.IsPositive()
	} else {
		complete = remaining.Compare(completionDust) <= 0
	}
	if complete {
		result.Status = "FILLED"
		result.FailureCode = ""
	} else {
		result.Status = "PARTIALLY_FILLED"
		result.FailureCode = "PARTIAL_LIQUIDITY"
	}
	return result, nil
}

func addChildTotals(result *Result, child Child) error {
	var err error
	result.FilledQuantity, err = result.FilledQuantity.Add(child.FilledQuantity)
	if err == nil {
		result.GrossUSD, err = result.GrossUSD.Add(child.GrossUSD)
	}
	if err == nil {
		result.VenueFeeUSD, err = result.VenueFeeUSD.Add(child.VenueFeeUSD)
	}
	return err
}

func referencePrice(candidates []candidate, side provider.Side) money.Decimal {
	ordered := append([]candidate(nil), candidates...)
	sort.SliceStable(ordered, func(left, right int) bool { return ordered[left].quote.Venue < ordered[right].quote.Venue })
	if side == provider.SideBuy {
		return ordered[0].quote.Ask
	}
	return ordered[0].quote.Bid
}

func providerFailureCode(err error) string {
	switch {
	case errors.Is(err, provider.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return "TIMEOUT"
	case errors.Is(err, provider.ErrUnavailable):
		return "VENUE_FAILURE"
	default:
		return "PROVIDER_FAILURE"
	}
}

func minimum(left, right money.Decimal) money.Decimal {
	if left.Compare(right) <= 0 {
		return left
	}
	return right
}
