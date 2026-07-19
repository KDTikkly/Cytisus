package crypto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	compliancestore "github.com/KDTikkly/Cytisus/internal/compliance/store"
	"github.com/KDTikkly/Cytisus/internal/crypto/provider"
	"github.com/KDTikkly/Cytisus/internal/crypto/router"
	"github.com/KDTikkly/Cytisus/internal/crypto/store"
	"github.com/KDTikkly/Cytisus/internal/ledger"
	"github.com/KDTikkly/Cytisus/internal/money"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type reservedConversion struct {
	profile  store.CryptoCustomerProfile
	stored   store.CryptoConversion
	replayed bool
}

func (service *Service) Convert(ctx context.Context, command ConvertCommand) (Conversion, error) {
	command.SourceAsset = normalizedAsset(command.SourceAsset)
	command.DestinationAsset = normalizedAsset(command.DestinationAsset)
	if !idempotencyKeyPattern.MatchString(command.IdempotencyKey) || !command.SourceAmount.IsPositive() ||
		command.SourceAsset == command.DestinationAsset || command.SourceAsset == "" || command.DestinationAsset == "" ||
		command.SourceAsset == "USD" && command.DestinationAsset == "USD" {
		return Conversion{}, ErrInvalidCommand
	}
	if command.SimulationScenario == "" {
		command.SimulationScenario = provider.ScenarioNormal
	}
	if command.SimulationScenario != provider.ScenarioNormal && !service.localSimulationAllowed() ||
		command.SimulationRiskFlag && !service.localSimulationAllowed() {
		return Conversion{}, provider.ErrProductionMode
	}
	_, profile, err := service.resolveCustomer(ctx, command.AccessToken)
	if err != nil {
		return Conversion{}, err
	}
	reserved, err := service.reserveConversion(ctx, profile, command)
	if err != nil {
		return Conversion{}, err
	}
	if reserved.stored.Status != "ROUTING" {
		return service.assembleConversion(ctx, reserved.stored, true)
	}
	policy, err := store.New(service.database).GetPolicy(ctx, reserved.stored.PolicyVersion)
	if err != nil {
		return Conversion{}, fmt.Errorf("get conversion policy: %w", err)
	}
	routingService, err := router.New(router.Dependencies{
		Venues: service.venues, PlatformFeeRate: policy.PlatformFeeRate,
		QuoteMaxAge:     time.Duration(policy.QuoteMaxAgeSeconds) * time.Second,
		ProviderTimeout: service.providerTimeout, Now: service.now,
	})
	if err != nil {
		return Conversion{}, err
	}
	conversionID := reserved.stored.ID.String()
	existingLegs, err := store.New(service.database).ListLegs(ctx, reserved.stored.ID)
	if err != nil {
		return Conversion{}, fmt.Errorf("list conversion routing progress: %w", err)
	}
	if command.SourceAsset != "USD" && len(existingLegs) == 0 {
		sellResult, routeErr := routingService.Sell(ctx, command.SourceAsset, command.SourceAmount, command.SimulationScenario, "conversion."+conversionID+".sell")
		if routeErr != nil {
			return Conversion{}, fmt.Errorf("route crypto sale: %w", routeErr)
		}
		finished, finalizeErr := service.finalizeSellLeg(ctx, reserved.profile, reserved.stored, policy, sellResult, command)
		if finalizeErr != nil {
			return Conversion{}, finalizeErr
		}
		if finished {
			stored, getErr := store.New(service.database).GetConversion(ctx, reserved.stored.ID)
			if getErr != nil {
				return Conversion{}, getErr
			}
			return service.assembleConversion(ctx, stored, reserved.replayed)
		}
		existingLegs, err = store.New(service.database).ListLegs(ctx, reserved.stored.ID)
		if err != nil {
			return Conversion{}, err
		}
	}
	if command.DestinationAsset != "USD" {
		budget := command.SourceAmount
		sequence := int16(1)
		if command.SourceAsset != "USD" {
			sequence = 2
			if len(existingLegs) == 0 {
				return Conversion{}, fmt.Errorf("missing crypto sale leg: %w", ErrInvalidState)
			}
			budget = existingLegs[0].FinalCustomerUsd
		}
		buyResult, routeErr := routingService.Buy(ctx, command.DestinationAsset, budget, command.SimulationScenario, fmt.Sprintf("conversion.%s.buy", conversionID))
		if routeErr != nil {
			return Conversion{}, fmt.Errorf("route crypto purchase: %w", routeErr)
		}
		if err := service.finalizeBuyLeg(ctx, reserved.profile, reserved.stored, policy, buyResult, sequence, command.SourceAsset == "USD"); err != nil {
			return Conversion{}, err
		}
	}
	stored, err := store.New(service.database).GetConversion(ctx, reserved.stored.ID)
	if err != nil {
		return Conversion{}, err
	}
	return service.assembleConversion(ctx, stored, reserved.replayed)
}

func (service *Service) reserveConversion(ctx context.Context, profile store.CryptoCustomerProfile, command ConvertCommand) (reservedConversion, error) {
	requestHash, err := hashValue(struct {
		SourceAsset, DestinationAsset, SourceAmount, Scenario string
		RiskFlag                                              bool
	}{command.SourceAsset, command.DestinationAsset, command.SourceAmount.String(), string(command.SimulationScenario), command.SimulationRiskFlag})
	if err != nil {
		return reservedConversion{}, err
	}
	conversionID, err := newUUID()
	if err != nil {
		return reservedConversion{}, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return reservedConversion{}, fmt.Errorf("begin crypto conversion reservation: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	inserted, err := queries.InsertConversionRequest(ctx, store.InsertConversionRequestParams{
		CustomerReference: profile.CustomerReference, IdempotencyKey: command.IdempotencyKey,
		RequestHash: requestHash, ConversionID: conversionID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existingRequest, getErr := queries.GetConversionRequest(ctx, store.GetConversionRequestParams{
			CustomerReference: profile.CustomerReference, IdempotencyKey: command.IdempotencyKey,
		})
		if getErr != nil {
			return reservedConversion{}, fmt.Errorf("get conversion replay: %w", getErr)
		}
		if existingRequest.RequestHash != requestHash {
			return reservedConversion{}, ErrIdempotencyConflict
		}
		existing, getErr := queries.GetConversion(ctx, existingRequest.ConversionID)
		if getErr != nil {
			return reservedConversion{}, fmt.Errorf("get replayed conversion: %w", getErr)
		}
		return reservedConversion{profile: profile, stored: existing, replayed: true}, nil
	}
	if err != nil || inserted != conversionID {
		return reservedConversion{}, fmt.Errorf("insert conversion request: %w", err)
	}
	lockedProfile, err := queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference)
	if err != nil {
		return reservedConversion{}, fmt.Errorf("lock crypto profile: %w", err)
	}
	if err := service.validateAsset(ctx, queries, command.SourceAsset); err != nil {
		return reservedConversion{}, err
	}
	if err := service.validateAsset(ctx, queries, command.DestinationAsset); err != nil {
		return reservedConversion{}, err
	}
	policy, err := queries.GetActivePolicy(ctx)
	if err != nil {
		return reservedConversion{}, fmt.Errorf("get active crypto policy: %w", err)
	}
	reservation, err := service.reserveSource(ctx, tx, lockedProfile, command, conversionID, policy.PolicyVersion)
	if err != nil {
		return reservedConversion{}, err
	}
	reservationID, err := parseUUID(reservation.TransactionID)
	if err != nil {
		return reservedConversion{}, err
	}
	created, err := queries.CreateConversion(ctx, store.CreateConversionParams{
		ID: conversionID, CustomerReference: profile.CustomerReference,
		SourceAsset: command.SourceAsset, DestinationAsset: command.DestinationAsset,
		SourceAmount: command.SourceAmount, SimulationScenario: string(command.SimulationScenario),
		PolicyVersion: policy.PolicyVersion, ReservationLedgerTransactionID: reservationID,
	})
	if err != nil {
		return reservedConversion{}, fmt.Errorf("create crypto conversion: %w", err)
	}
	payload, _ := json.Marshal(map[string]string{"conversion_id": conversionID.String(), "status": created.Status})
	if err := recordMutation(ctx, tx, mutation{
		Action: "crypto.conversion.requested", ResourceType: "crypto.conversion", ResourceID: conversionID.String(),
		ActorType: "USER", ActorID: profile.CustomerReference, CorrelationID: conversionID,
		Metadata: payload, AggregateType: "crypto.conversion", AggregateID: conversionID.String(),
		EventType: "crypto.conversion.requested", Payload: payload,
	}); err != nil {
		return reservedConversion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return reservedConversion{}, fmt.Errorf("commit crypto conversion reservation: %w", err)
	}
	return reservedConversion{profile: lockedProfile, stored: created}, nil
}

func (service *Service) reserveSource(ctx context.Context, tx pgx.Tx, profile store.CryptoCustomerProfile, command ConvertCommand, conversionID pgtype.UUID, policyVersion string) (ledger.PostingResult, error) {
	ledgerService := ledger.NewService(tx)
	usdAccounts, err := service.ensureUSDLedgerAccounts(ctx, tx)
	if err != nil {
		return ledger.PostingResult{}, err
	}
	entries := make([]ledger.Entry, 0, 6)
	if command.SourceAsset == "USD" {
		settled, err := ledgerService.Balance(ctx, profile.CashLedgerAccountID.String(), "USD", ledger.DimensionSettled)
		if err != nil {
			return ledger.PostingResult{}, err
		}
		withdrawable, err := ledgerService.Balance(ctx, profile.CashLedgerAccountID.String(), "USD", ledger.DimensionWithdrawable)
		if err != nil {
			return ledger.PostingResult{}, err
		}
		if settled.Compare(command.SourceAmount) < 0 || withdrawable.Compare(command.SourceAmount) < 0 {
			return ledger.PostingResult{}, ErrInsufficientCash
		}
		entries = append(entries,
			ledger.Entry{AccountID: profile.CashLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: command.SourceAmount},
			ledger.Entry{AccountID: usdAccounts.RoutingClearing, Currency: "USD", Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: command.SourceAmount},
			ledger.Entry{AccountID: profile.CashLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionWithdrawable, Direction: ledger.Credit, Amount: command.SourceAmount},
			ledger.Entry{AccountID: usdAccounts.RoutingClearing, Currency: "USD", Dimension: ledger.DimensionWithdrawable, Direction: ledger.Debit, Amount: command.SourceAmount},
			ledger.Entry{AccountID: profile.CashLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionHeld, Direction: ledger.Debit, Amount: command.SourceAmount},
			ledger.Entry{AccountID: usdAccounts.RoutingClearing, Currency: "USD", Dimension: ledger.DimensionHeld, Direction: ledger.Credit, Amount: command.SourceAmount},
		)
	} else {
		assetAccounts, err := service.ensureAssetLedgerAccounts(ctx, tx, profile, command.SourceAsset)
		if err != nil {
			return ledger.PostingResult{}, err
		}
		balance, err := ledgerService.Balance(ctx, assetAccounts.CustomerLedgerAccountID.String(), money.Currency(command.SourceAsset), ledger.DimensionSettled)
		if err != nil {
			return ledger.PostingResult{}, err
		}
		if balance.Compare(command.SourceAmount) < 0 {
			return ledger.PostingResult{}, ErrInsufficientAsset
		}
		entries = append(entries,
			ledger.Entry{AccountID: assetAccounts.CustomerLedgerAccountID.String(), Currency: money.Currency(command.SourceAsset), Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: command.SourceAmount},
			ledger.Entry{AccountID: assetAccounts.CustodyInventoryLedgerAccountID.String(), Currency: money.Currency(command.SourceAsset), Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: command.SourceAmount},
			ledger.Entry{AccountID: assetAccounts.CustomerLedgerAccountID.String(), Currency: money.Currency(command.SourceAsset), Dimension: ledger.DimensionHeld, Direction: ledger.Debit, Amount: command.SourceAmount},
			ledger.Entry{AccountID: assetAccounts.CustodyInventoryLedgerAccountID.String(), Currency: money.Currency(command.SourceAsset), Dimension: ledger.DimensionHeld, Direction: ledger.Credit, Amount: command.SourceAmount},
		)
	}
	posting, err := ledgerService.Post(ctx, ledger.PostingCommand{
		Scope: "crypto.conversion.reserve", IdempotencyKey: "conversion.reserve." + conversionID.String(),
		TransactionType: "CRYPTO_CONVERSION_RESERVATION", PolicyVersion: policyVersion, EffectiveAt: service.now().UTC(),
		Actor: ledger.Actor{Type: "USER", ID: profile.CustomerReference}, Entries: entries,
	})
	if err != nil {
		return ledger.PostingResult{}, fmt.Errorf("reserve crypto conversion source: %w", err)
	}
	return posting, nil
}

func (service *Service) finalizeSellLeg(ctx context.Context, profile store.CryptoCustomerProfile, conversion store.CryptoConversion, policy store.CryptoPolicy, result router.Result, command ConvertCommand) (bool, error) {
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin crypto sell settlement: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	if _, err := queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference); err != nil {
		return false, err
	}
	current, err := queries.GetConversionForUpdate(ctx, conversion.ID)
	if err != nil {
		return false, err
	}
	if current.Status != "ROUTING" {
		return true, nil
	}
	legs, err := queries.ListLegs(ctx, conversion.ID)
	if err != nil {
		return false, err
	}
	if len(legs) > 0 {
		return false, nil
	}
	review := false
	if result.FilledQuantity.IsPositive() {
		since := service.now().UTC().AddDate(0, 0, -int(policy.ConversionReviewWindowDays))
		rolling, sumErr := queries.SumRecentCryptoToUSD(ctx, store.SumRecentCryptoToUSDParams{
			CustomerReference: profile.CustomerReference, SinceTime: timestamptz(since),
		})
		if sumErr != nil {
			return false, fmt.Errorf("sum crypto-to-USD review window: %w", sumErr)
		}
		rolling, err = rolling.Add(result.FinalCustomerUSD)
		if err != nil {
			return false, err
		}
		review = command.SimulationRiskFlag || result.FinalCustomerUSD.Compare(policy.ConversionReviewSingleUsd) >= 0 || rolling.Compare(policy.ConversionReviewRollingUsd) >= 0
	}
	ledgerTransactionID := pgtype.UUID{}
	if result.FilledQuantity.IsPositive() {
		posting, postErr := service.postSell(ctx, tx, profile, conversion, result, policy.PolicyVersion, review)
		if postErr != nil {
			return false, postErr
		}
		ledgerTransactionID, err = parseUUID(posting.TransactionID)
		if err != nil {
			return false, err
		}
	}
	leg, err := service.persistRouteResult(ctx, tx, conversion.ID, 1, result, ledgerTransactionID)
	if err != nil {
		return false, err
	}
	remaining, err := conversion.SourceAmount.Sub(result.FilledQuantity)
	if err != nil {
		return false, err
	}
	releaseID, err := service.releaseReservation(ctx, tx, profile, conversion, remaining)
	if err != nil {
		return false, err
	}
	finished := conversion.DestinationAsset == "USD" || result.Status == "FAILED" || review
	caseID := pgtype.UUID{}
	status := result.Status
	reasonCode := result.FailureCode
	nextAction := "The simulated conversion is complete."
	if result.Status == "PARTIALLY_FILLED" {
		status = "PARTIALLY_FILLED"
		nextAction = "Review the filled amount; the unfilled quantity was released."
	}
	if result.Status == "FAILED" {
		status = "FAILED"
		nextAction = "No executable simulated liquidity was found; reserved assets were released."
	}
	if review {
		finished = true
		status = "REVIEW_REQUIRED"
		reasonCode = "CRYPTO_TO_USD_REVIEW"
		nextAction = "USD proceeds are frozen while the compliance review is completed."
		caseID, err = service.createConversionReviewCase(ctx, tx, profile.CustomerReference, conversion.ID, policy.PolicyVersion)
		if err != nil {
			return false, err
		}
		if conversion.DestinationAsset != "USD" {
			blockedResult := router.Result{Side: provider.SideBuy, Asset: conversion.DestinationAsset, InputAmount: result.FinalCustomerUSD, Status: "BLOCKED", FailureCode: "CRYPTO_TO_USD_REVIEW"}
			if _, err := service.persistRouteResult(ctx, tx, conversion.ID, 2, blockedResult, pgtype.UUID{}); err != nil {
				return false, err
			}
		}
	}
	metadata, _ := json.Marshal(map[string]any{"sell": result, "reservation_release_ledger_transaction_id": releaseID})
	if finished {
		if _, err := queries.CompleteConversion(ctx, store.CompleteConversionParams{
			Status: status, ReasonCode: textValue(reasonCode), NextAction: nextAction,
			RoutingMetadata: metadata, ComplianceCaseID: caseID, ID: conversion.ID,
		}); err != nil {
			return false, fmt.Errorf("complete crypto sale conversion: %w", err)
		}
	}
	payload, _ := json.Marshal(map[string]string{"conversion_id": conversion.ID.String(), "leg_id": leg.ID.String(), "status": status})
	if err := recordMutation(ctx, tx, mutation{
		Action: "crypto.conversion.sell_settled", ResourceType: "crypto.conversion", ResourceID: conversion.ID.String(),
		ActorType: "SYSTEM", ActorID: "crypto-routing", CorrelationID: conversion.ID,
		Metadata: payload, AggregateType: "crypto.conversion", AggregateID: conversion.ID.String(),
		EventType: "crypto.conversion.sell_settled", EventVersion: 2, Payload: payload,
	}); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit crypto sell settlement: %w", err)
	}
	return finished, nil
}

func (service *Service) finalizeBuyLeg(ctx context.Context, profile store.CryptoCustomerProfile, conversion store.CryptoConversion, policy store.CryptoPolicy, result router.Result, sequence int16, fromReservation bool) error {
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin crypto buy settlement: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	if _, err := queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference); err != nil {
		return err
	}
	current, err := queries.GetConversionForUpdate(ctx, conversion.ID)
	if err != nil {
		return err
	}
	if current.Status != "ROUTING" {
		return nil
	}
	legs, err := queries.ListLegs(ctx, conversion.ID)
	if err != nil {
		return err
	}
	for _, leg := range legs {
		if leg.LegSequence == sequence {
			return nil
		}
	}
	ledgerTransactionID := pgtype.UUID{}
	if result.FilledQuantity.IsPositive() {
		posting, postErr := service.postBuy(ctx, tx, profile, conversion, result, policy.PolicyVersion, fromReservation)
		if postErr != nil {
			return postErr
		}
		ledgerTransactionID, err = parseUUID(posting.TransactionID)
		if err != nil {
			return err
		}
	}
	if _, err := service.persistRouteResult(ctx, tx, conversion.ID, sequence, result, ledgerTransactionID); err != nil {
		return err
	}
	releaseID := ""
	if fromReservation {
		releaseID, err = service.releaseReservation(ctx, tx, profile, conversion, result.UnspentUSD)
		if err != nil {
			return err
		}
	}
	status := result.Status
	reasonCode := result.FailureCode
	nextAction := "The simulated conversion is complete."
	if result.Status == "PARTIALLY_FILLED" {
		status = "PARTIALLY_FILLED"
		nextAction = "Review the filled amount; unspent USD remains available."
	}
	if result.Status == "FAILED" {
		status = "FAILED"
		nextAction = "No executable simulated liquidity was found; reserved USD was released."
	}
	if sequence == 2 {
		for _, leg := range legs {
			if leg.LegSequence == 1 && leg.Status == "PARTIALLY_FILLED" && status == "FILLED" {
				status = "PARTIALLY_FILLED"
				reasonCode = "PARTIAL_LIQUIDITY"
				nextAction = "Review both explicit USD legs and their filled amounts."
			}
		}
	}
	metadata, _ := json.Marshal(map[string]any{"buy": result, "reservation_release_ledger_transaction_id": releaseID})
	if _, err := queries.CompleteConversion(ctx, store.CompleteConversionParams{
		Status: status, ReasonCode: textValue(reasonCode), NextAction: nextAction,
		RoutingMetadata: metadata, ID: conversion.ID,
	}); err != nil {
		return fmt.Errorf("complete crypto purchase conversion: %w", err)
	}
	payload, _ := json.Marshal(map[string]string{"conversion_id": conversion.ID.String(), "status": status})
	if err := recordMutation(ctx, tx, mutation{
		Action: "crypto.conversion.completed", ResourceType: "crypto.conversion", ResourceID: conversion.ID.String(),
		ActorType: "SYSTEM", ActorID: "crypto-routing", CorrelationID: conversion.ID,
		Metadata: payload, AggregateType: "crypto.conversion", AggregateID: conversion.ID.String(),
		EventType: "crypto.conversion.completed", EventVersion: 2, Payload: payload,
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit crypto buy settlement: %w", err)
	}
	return nil
}

func (service *Service) postSell(ctx context.Context, tx pgx.Tx, profile store.CryptoCustomerProfile, conversion store.CryptoConversion, result router.Result, policyVersion string, review bool) (ledger.PostingResult, error) {
	assetAccounts, err := service.ensureAssetLedgerAccounts(ctx, tx, profile, conversion.SourceAsset)
	if err != nil {
		return ledger.PostingResult{}, err
	}
	usdAccounts, err := service.ensureUSDLedgerAccounts(ctx, tx)
	if err != nil {
		return ledger.PostingResult{}, err
	}
	dimension := ledger.DimensionSettled
	if review {
		dimension = ledger.DimensionFrozen
	}
	entries := []ledger.Entry{
		{AccountID: assetAccounts.CustomerLedgerAccountID.String(), Currency: money.Currency(conversion.SourceAsset), Dimension: ledger.DimensionHeld, Direction: ledger.Credit, Amount: result.FilledQuantity},
		{AccountID: assetAccounts.CustodyInventoryLedgerAccountID.String(), Currency: money.Currency(conversion.SourceAsset), Dimension: ledger.DimensionHeld, Direction: ledger.Debit, Amount: result.FilledQuantity},
		{AccountID: usdAccounts.RoutingClearing, Currency: "USD", Dimension: dimension, Direction: ledger.Credit, Amount: result.GrossUSD},
		{AccountID: profile.CashLedgerAccountID.String(), Currency: "USD", Dimension: dimension, Direction: ledger.Debit, Amount: result.FinalCustomerUSD},
	}
	entries = appendPositiveFeeEntries(entries, usdAccounts, dimension, result)
	if !review {
		entries = append(entries,
			ledger.Entry{AccountID: profile.CashLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionWithdrawable, Direction: ledger.Debit, Amount: result.FinalCustomerUSD},
			ledger.Entry{AccountID: usdAccounts.RoutingClearing, Currency: "USD", Dimension: ledger.DimensionWithdrawable, Direction: ledger.Credit, Amount: result.FinalCustomerUSD},
		)
	}
	return ledger.NewService(tx).Post(ctx, ledger.PostingCommand{
		Scope: "crypto.conversion.leg", IdempotencyKey: "conversion.leg." + conversion.ID.String() + ".1",
		TransactionType: "CRYPTO_SELL", PolicyVersion: policyVersion, EffectiveAt: service.now().UTC(),
		Actor: ledger.Actor{Type: "SYSTEM", ID: "crypto-routing"}, Entries: entries,
	})
}

func (service *Service) postBuy(ctx context.Context, tx pgx.Tx, profile store.CryptoCustomerProfile, conversion store.CryptoConversion, result router.Result, policyVersion string, fromReservation bool) (ledger.PostingResult, error) {
	assetAccounts, err := service.ensureAssetLedgerAccounts(ctx, tx, profile, result.Asset)
	if err != nil {
		return ledger.PostingResult{}, err
	}
	usdAccounts, err := service.ensureUSDLedgerAccounts(ctx, tx)
	if err != nil {
		return ledger.PostingResult{}, err
	}
	dimension := ledger.DimensionSettled
	if fromReservation {
		dimension = ledger.DimensionHeld
	}
	entries := []ledger.Entry{
		{AccountID: profile.CashLedgerAccountID.String(), Currency: "USD", Dimension: dimension, Direction: ledger.Credit, Amount: result.FinalCustomerUSD},
		{AccountID: usdAccounts.RoutingClearing, Currency: "USD", Dimension: dimension, Direction: ledger.Debit, Amount: result.GrossUSD},
		{AccountID: assetAccounts.CustomerLedgerAccountID.String(), Currency: money.Currency(result.Asset), Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: result.FilledQuantity},
		{AccountID: assetAccounts.CustodyInventoryLedgerAccountID.String(), Currency: money.Currency(result.Asset), Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: result.FilledQuantity},
	}
	entries = appendPositiveFeeEntries(entries, usdAccounts, dimension, result)
	if !fromReservation {
		entries = append(entries,
			ledger.Entry{AccountID: profile.CashLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionWithdrawable, Direction: ledger.Credit, Amount: result.FinalCustomerUSD},
			ledger.Entry{AccountID: usdAccounts.RoutingClearing, Currency: "USD", Dimension: ledger.DimensionWithdrawable, Direction: ledger.Debit, Amount: result.FinalCustomerUSD},
		)
	}
	sequence := "1"
	if !fromReservation {
		sequence = "2"
	}
	return ledger.NewService(tx).Post(ctx, ledger.PostingCommand{
		Scope: "crypto.conversion.leg", IdempotencyKey: "conversion.leg." + conversion.ID.String() + "." + sequence,
		TransactionType: "CRYPTO_BUY", PolicyVersion: policyVersion, EffectiveAt: service.now().UTC(),
		Actor: ledger.Actor{Type: "SYSTEM", ID: "crypto-routing"}, Entries: entries,
	})
}

func appendPositiveFeeEntries(entries []ledger.Entry, accounts usdLedgerAccounts, dimension ledger.BalanceDimension, result router.Result) []ledger.Entry {
	if result.VenueFeeUSD.IsPositive() {
		entries = append(entries, ledger.Entry{AccountID: accounts.VenueFees, Currency: "USD", Dimension: dimension, Direction: ledger.Debit, Amount: result.VenueFeeUSD})
	}
	if result.PlatformFeeUSD.IsPositive() {
		entries = append(entries, ledger.Entry{AccountID: accounts.PlatformFees, Currency: "USD", Dimension: dimension, Direction: ledger.Debit, Amount: result.PlatformFeeUSD})
	}
	return entries
}

func (service *Service) releaseReservation(ctx context.Context, tx pgx.Tx, profile store.CryptoCustomerProfile, conversion store.CryptoConversion, amount money.Decimal) (string, error) {
	if !amount.IsPositive() {
		return "", nil
	}
	entries := make([]ledger.Entry, 0, 6)
	if conversion.SourceAsset == "USD" {
		usdAccounts, err := service.ensureUSDLedgerAccounts(ctx, tx)
		if err != nil {
			return "", err
		}
		entries = append(entries,
			ledger.Entry{AccountID: profile.CashLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionHeld, Direction: ledger.Credit, Amount: amount},
			ledger.Entry{AccountID: usdAccounts.RoutingClearing, Currency: "USD", Dimension: ledger.DimensionHeld, Direction: ledger.Debit, Amount: amount},
			ledger.Entry{AccountID: profile.CashLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: amount},
			ledger.Entry{AccountID: usdAccounts.RoutingClearing, Currency: "USD", Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: amount},
			ledger.Entry{AccountID: profile.CashLedgerAccountID.String(), Currency: "USD", Dimension: ledger.DimensionWithdrawable, Direction: ledger.Debit, Amount: amount},
			ledger.Entry{AccountID: usdAccounts.RoutingClearing, Currency: "USD", Dimension: ledger.DimensionWithdrawable, Direction: ledger.Credit, Amount: amount},
		)
	} else {
		accounts, err := service.ensureAssetLedgerAccounts(ctx, tx, profile, conversion.SourceAsset)
		if err != nil {
			return "", err
		}
		entries = append(entries,
			ledger.Entry{AccountID: accounts.CustomerLedgerAccountID.String(), Currency: money.Currency(conversion.SourceAsset), Dimension: ledger.DimensionHeld, Direction: ledger.Credit, Amount: amount},
			ledger.Entry{AccountID: accounts.CustodyInventoryLedgerAccountID.String(), Currency: money.Currency(conversion.SourceAsset), Dimension: ledger.DimensionHeld, Direction: ledger.Debit, Amount: amount},
			ledger.Entry{AccountID: accounts.CustomerLedgerAccountID.String(), Currency: money.Currency(conversion.SourceAsset), Dimension: ledger.DimensionSettled, Direction: ledger.Debit, Amount: amount},
			ledger.Entry{AccountID: accounts.CustodyInventoryLedgerAccountID.String(), Currency: money.Currency(conversion.SourceAsset), Dimension: ledger.DimensionSettled, Direction: ledger.Credit, Amount: amount},
		)
	}
	posting, err := ledger.NewService(tx).Post(ctx, ledger.PostingCommand{
		Scope: "crypto.conversion.release", IdempotencyKey: "conversion.release." + conversion.ID.String(),
		TransactionType: "CRYPTO_RESERVATION_RELEASE", PolicyVersion: conversion.PolicyVersion, EffectiveAt: service.now().UTC(),
		Actor: ledger.Actor{Type: "SYSTEM", ID: "crypto-routing"}, Entries: entries,
	})
	if err != nil {
		return "", fmt.Errorf("release crypto conversion reservation: %w", err)
	}
	return posting.TransactionID, nil
}

func (service *Service) persistRouteResult(ctx context.Context, tx pgx.Tx, conversionID pgtype.UUID, sequence int16, result router.Result, ledgerTransactionID pgtype.UUID) (store.CryptoLeg, error) {
	queries := store.New(tx)
	legID, err := newUUID()
	if err != nil {
		return store.CryptoLeg{}, err
	}
	status := result.Status
	if status == "PARTIALLY_FILLED" {
		status = "PARTIALLY_FILLED"
	}
	leg, err := queries.InsertLeg(ctx, store.InsertLegParams{
		ID: legID, ConversionID: conversionID, LegSequence: sequence, Side: string(result.Side), AssetSymbol: result.Asset,
		InputAmount: result.InputAmount, FilledQuantity: result.FilledQuantity, ReferencePrice: result.ReferencePrice,
		AverageExecutionPrice: result.AverageExecutionPrice, GrossUsd: result.GrossUSD, VenueFeeUsd: result.VenueFeeUSD,
		PlatformFeeUsd: result.PlatformFeeUSD, FinalCustomerUsd: result.FinalCustomerUSD,
		PriceImprovementUsd: result.PriceImprovementUSD, Status: status, FailureCode: textValue(result.FailureCode),
		LedgerTransactionID: ledgerTransactionID,
	})
	if err != nil {
		return store.CryptoLeg{}, fmt.Errorf("insert crypto leg: %w", err)
	}
	for _, child := range result.Children {
		childID, idErr := newUUID()
		if idErr != nil {
			return store.CryptoLeg{}, idErr
		}
		created, insertErr := queries.InsertChildOrder(ctx, store.InsertChildOrderParams{
			ID: childID, LegID: legID, ChildSequence: child.Sequence, VenueCode: child.Venue,
			ClientOrderID: child.ClientOrderID, ProviderOrderID: textValue(child.ProviderOrderID),
			RequestedQuantity: child.RequestedQuantity, FilledQuantity: child.FilledQuantity,
			QuoteBid: child.QuoteBid, QuoteAsk: child.QuoteAsk, VenueFeeRate: child.VenueFeeRate,
			EffectiveUnitPrice: child.EffectiveUnitPrice, ExecutionPrice: child.ExecutionPrice,
			GrossUsd: child.GrossUSD, VenueFeeUsd: child.VenueFeeUSD, Status: child.Status,
			FailureCode: textValue(child.FailureCode), QuoteObservedAt: timestamptz(child.QuoteObservedAt),
		})
		if insertErr != nil {
			return store.CryptoLeg{}, fmt.Errorf("insert crypto child order: %w", insertErr)
		}
		if child.FilledQuantity.IsPositive() {
			fillID, idErr := newUUID()
			if idErr != nil {
				return store.CryptoLeg{}, idErr
			}
			if _, insertErr := queries.InsertFill(ctx, store.InsertFillParams{
				ID: fillID, ChildOrderID: created.ID, VenueCode: child.Venue, ExternalFillID: child.ExternalFillID,
				Quantity: child.FilledQuantity, Price: child.ExecutionPrice, GrossUsd: child.GrossUSD,
				VenueFeeUsd: child.VenueFeeUSD, OccurredAt: timestamptz(child.OccurredAt),
			}); insertErr != nil {
				return store.CryptoLeg{}, fmt.Errorf("insert crypto fill: %w", insertErr)
			}
		}
	}
	return leg, nil
}

func (service *Service) createConversionReviewCase(ctx context.Context, tx pgx.Tx, customerReference string, conversionID pgtype.UUID, policyVersion string) (pgtype.UUID, error) {
	caseID, err := newUUID()
	if err != nil {
		return pgtype.UUID{}, err
	}
	nextAction := "USD proceeds remain frozen while the compliance team completes review."
	queries := compliancestore.New(tx)
	if _, err := queries.CreateCase(ctx, compliancestore.CreateCaseParams{
		ID: caseID, CustomerReference: customerReference, CaseType: "TRANSACTION_MONITORING_ALERT",
		ResourceType: "crypto.conversion", ResourceID: conversionID.String(), Status: "IN_REVIEW",
		ReasonCode: "CRYPTO_TO_USD_REVIEW", NextAction: nextAction, PolicyVersion: policyVersion,
	}); err != nil {
		return pgtype.UUID{}, fmt.Errorf("create crypto conversion review: %w", err)
	}
	if _, err := queries.InsertCaseEvent(ctx, compliancestore.InsertCaseEventParams{
		CaseID: caseID, ToStatus: "IN_REVIEW", ActorType: "SYSTEM", ActorID: "crypto-compliance",
		ReasonCode: "CRYPTO_TO_USD_REVIEW", Metadata: []byte(`{"funds_dimension":"FROZEN"}`),
	}); err != nil {
		return pgtype.UUID{}, fmt.Errorf("insert crypto conversion review event: %w", err)
	}
	return caseID, nil
}

func (service *Service) GetConversion(ctx context.Context, accessToken, conversionID string) (Conversion, error) {
	_, profile, err := service.resolveCustomer(ctx, accessToken)
	if err != nil {
		return Conversion{}, err
	}
	identifier, err := parseUUID(conversionID)
	if err != nil {
		return Conversion{}, ErrConversionNotFound
	}
	stored, err := store.New(service.database).GetConversion(ctx, identifier)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && stored.CustomerReference != profile.CustomerReference {
		return Conversion{}, ErrConversionNotFound
	}
	if err != nil {
		return Conversion{}, fmt.Errorf("get crypto conversion: %w", err)
	}
	return service.assembleConversion(ctx, stored, false)
}

func (service *Service) ListConversions(ctx context.Context, accessToken string, pageSize int32) ([]Conversion, error) {
	_, profile, err := service.resolveCustomer(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 50
	}
	rows, err := store.New(service.database).ListConversions(ctx, store.ListConversionsParams{CustomerReference: profile.CustomerReference, PageSize: pageSize})
	if err != nil {
		return nil, fmt.Errorf("list crypto conversions: %w", err)
	}
	result := make([]Conversion, 0, len(rows))
	for _, row := range rows {
		converted, convertErr := service.assembleConversion(ctx, row, false)
		if convertErr != nil {
			return nil, convertErr
		}
		result = append(result, converted)
	}
	return result, nil
}

func (service *Service) assembleConversion(ctx context.Context, stored store.CryptoConversion, replayed bool) (Conversion, error) {
	queries := store.New(service.database)
	legs, err := queries.ListLegs(ctx, stored.ID)
	if err != nil {
		return Conversion{}, fmt.Errorf("list crypto legs: %w", err)
	}
	children, err := queries.ListChildOrders(ctx, stored.ID)
	if err != nil {
		return Conversion{}, fmt.Errorf("list crypto child orders: %w", err)
	}
	fills, err := queries.ListFills(ctx, stored.ID)
	if err != nil {
		return Conversion{}, fmt.Errorf("list crypto fills: %w", err)
	}
	result := Conversion{
		ID: stored.ID.String(), SourceAsset: stored.SourceAsset, DestinationAsset: stored.DestinationAsset,
		SourceAmount: stored.SourceAmount, SimulationScenario: stored.SimulationScenario, Status: stored.Status,
		ReasonCode: stored.ReasonCode.String, NextAction: stored.NextAction, PolicyVersion: stored.PolicyVersion,
		ComplianceCaseID: stored.ComplianceCaseID.String(), CreatedAt: stored.CreatedAt.Time.UTC(),
		UpdatedAt: stored.UpdatedAt.Time.UTC(), Replayed: replayed,
	}
	for _, storedLeg := range legs {
		leg := Leg{
			ID: storedLeg.ID.String(), Sequence: storedLeg.LegSequence, Side: storedLeg.Side, Asset: storedLeg.AssetSymbol,
			QuoteCurrency: storedLeg.QuoteCurrency, InputAmount: storedLeg.InputAmount, FilledQuantity: storedLeg.FilledQuantity,
			ReferencePrice: storedLeg.ReferencePrice, AverageExecutionPrice: storedLeg.AverageExecutionPrice,
			GrossUSD: storedLeg.GrossUsd, VenueFeeUSD: storedLeg.VenueFeeUsd, PlatformFeeUSD: storedLeg.PlatformFeeUsd,
			FinalCustomerUSD: storedLeg.FinalCustomerUsd, PriceImprovementUSD: storedLeg.PriceImprovementUsd,
			Status: storedLeg.Status, FailureCode: storedLeg.FailureCode.String, LedgerTransactionID: storedLeg.LedgerTransactionID.String(),
		}
		for _, storedChild := range children {
			if storedChild.LegID != storedLeg.ID {
				continue
			}
			child := ChildOrder{
				ID: storedChild.ID.String(), Sequence: storedChild.ChildSequence, Venue: storedChild.VenueCode,
				ClientOrderID: storedChild.ClientOrderID, ProviderOrderID: storedChild.ProviderOrderID.String,
				RequestedQuantity: storedChild.RequestedQuantity, FilledQuantity: storedChild.FilledQuantity,
				QuoteBid: storedChild.QuoteBid, QuoteAsk: storedChild.QuoteAsk, VenueFeeRate: storedChild.VenueFeeRate,
				EffectiveUnitPrice: storedChild.EffectiveUnitPrice, ExecutionPrice: storedChild.ExecutionPrice,
				GrossUSD: storedChild.GrossUsd, VenueFeeUSD: storedChild.VenueFeeUsd, Status: storedChild.Status,
				FailureCode: storedChild.FailureCode.String, QuoteObservedAt: storedChild.QuoteObservedAt.Time.UTC(),
			}
			for _, storedFill := range fills {
				if storedFill.ChildOrderID == storedChild.ID {
					child.Fills = append(child.Fills, Fill{
						ID: storedFill.ID.String(), Venue: storedFill.VenueCode, ExternalFillID: storedFill.ExternalFillID,
						Quantity: storedFill.Quantity, Price: storedFill.Price, GrossUSD: storedFill.GrossUsd,
						VenueFeeUSD: storedFill.VenueFeeUsd, OccurredAt: storedFill.OccurredAt.Time.UTC(),
					})
				}
			}
			leg.Children = append(leg.Children, child)
		}
		result.Legs = append(result.Legs, leg)
	}
	return result, nil
}
