package crypto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/KDTikkly/Cytisus/internal/crypto/provider"
	"github.com/KDTikkly/Cytisus/internal/crypto/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (service *Service) AddWithdrawalAddress(ctx context.Context, command AddWithdrawalAddressCommand) (WithdrawalAddress, error) {
	command.Asset = normalizedAsset(command.Asset)
	command.Network = normalizedAsset(command.Network)
	if !idempotencyKeyPattern.MatchString(command.IdempotencyKey) || command.Asset == "" || command.Network == "" ||
		len(command.ExternalAddress) < 8 || command.Label == "" {
		return WithdrawalAddress{}, ErrInvalidCommand
	}
	_, profile, err := service.resolveCustomer(ctx, command.AccessToken)
	if err != nil {
		return WithdrawalAddress{}, err
	}
	queries := store.New(service.database)
	if err := service.validateAssetNetwork(ctx, queries, command.Asset, command.Network, true); err != nil {
		return WithdrawalAddress{}, err
	}
	policy, err := queries.GetActivePolicy(ctx)
	if err != nil {
		return WithdrawalAddress{}, fmt.Errorf("get address cooling policy: %w", err)
	}
	risk, err := service.analyzeAddress(ctx, command.Asset, command.Network, command.ExternalAddress)
	if err != nil {
		return WithdrawalAddress{}, err
	}
	requestHash, err := hashValue(struct {
		Asset, Network, Address, Label string
		Risk                           AddressRiskContext
	}{command.Asset, command.Network, command.ExternalAddress, command.Label, command.RiskContext})
	if err != nil {
		return WithdrawalAddress{}, err
	}
	addressID, err := newUUID()
	if err != nil {
		return WithdrawalAddress{}, err
	}
	status := "COOLING_OFF"
	riskLevel := "LOW"
	reasonCode := "ADDRESS_COOLING_STARTED"
	coolingReason := dynamicCoolingReason(command.RiskContext)
	coolingUntil := timestamptz(service.now().UTC().Add(service.coolingDuration(policy, command.RiskContext)))
	if risk.Decision == "ELEVATED" {
		riskLevel = "ELEVATED"
		reasonCode = "CHAIN_RISK_ELEVATED"
		coolingReason = "Enhanced address review and cooling are active."
	}
	if risk.Decision == "BLOCK" {
		status = "SUSPENDED"
		riskLevel = "BLOCKED"
		reasonCode = "CHAIN_RISK_BLOCKED"
		coolingReason = "This address cannot be used; add a different address or contact support."
		coolingUntil = pgtype.Timestamptz{}
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return WithdrawalAddress{}, fmt.Errorf("begin withdrawal address: %w", err)
	}
	defer tx.Rollback(ctx)
	queries = store.New(tx)
	if _, err := queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference); err != nil {
		return WithdrawalAddress{}, err
	}
	inserted, err := queries.InsertWithdrawalAddressRequest(ctx, store.InsertWithdrawalAddressRequestParams{
		CustomerReference: profile.CustomerReference, IdempotencyKey: command.IdempotencyKey,
		RequestHash: requestHash, AddressID: addressID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		existingRequest, getErr := queries.GetWithdrawalAddressRequest(ctx, store.GetWithdrawalAddressRequestParams{
			CustomerReference: profile.CustomerReference, IdempotencyKey: command.IdempotencyKey,
		})
		if getErr != nil {
			return WithdrawalAddress{}, getErr
		}
		if existingRequest.RequestHash != requestHash {
			return WithdrawalAddress{}, ErrIdempotencyConflict
		}
		existing, getErr := queries.GetWithdrawalAddress(ctx, existingRequest.AddressID)
		if getErr != nil {
			return WithdrawalAddress{}, getErr
		}
		return withdrawalAddressFromStore(existing, true), nil
	}
	if err != nil || inserted != addressID {
		return WithdrawalAddress{}, fmt.Errorf("record withdrawal address request: %w", err)
	}
	created, err := queries.CreateWithdrawalAddress(ctx, store.CreateWithdrawalAddressParams{
		ID: addressID, CustomerReference: profile.CustomerReference, AssetSymbol: command.Asset,
		NetworkCode: command.Network, ExternalAddress: command.ExternalAddress, Label: command.Label,
		Status: status, RiskLevel: riskLevel, CoolingUntil: coolingUntil, CoolingReason: coolingReason,
		PolicyVersion: policy.PolicyVersion,
	})
	if err != nil {
		return WithdrawalAddress{}, fmt.Errorf("create withdrawal address: %w", err)
	}
	metadata, _ := json.Marshal(map[string]string{"risk_provider": risk.Provider, "risk_level": riskLevel, "mode": "SIMULATED"})
	if _, err := queries.InsertWithdrawalAddressEvent(ctx, store.InsertWithdrawalAddressEventParams{
		AddressID: created.ID, ToStatus: created.Status, ActorType: "USER", ActorID: profile.CustomerReference,
		ReasonCode: reasonCode, Metadata: metadata,
	}); err != nil {
		return WithdrawalAddress{}, fmt.Errorf("insert withdrawal address event: %w", err)
	}
	payload, _ := json.Marshal(map[string]string{"address_id": created.ID.String(), "status": created.Status, "asset": created.AssetSymbol})
	if err := recordMutation(ctx, tx, mutation{
		Action: "crypto.withdrawal_address.created", ResourceType: "crypto.withdrawal_address", ResourceID: created.ID.String(),
		ActorType: "USER", ActorID: profile.CustomerReference, CorrelationID: created.ID,
		Metadata: metadata, AggregateType: "crypto.withdrawal_address", AggregateID: created.ID.String(),
		EventType: "crypto.withdrawal_address.created", Payload: payload,
	}); err != nil {
		return WithdrawalAddress{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return WithdrawalAddress{}, fmt.Errorf("commit withdrawal address: %w", err)
	}
	return withdrawalAddressFromStore(created, false), nil
}

func (service *Service) ActivateWithdrawalAddress(ctx context.Context, command ActivateWithdrawalAddressCommand) (WithdrawalAddress, error) {
	_, profile, err := service.resolveCustomer(ctx, command.AccessToken)
	if err != nil {
		return WithdrawalAddress{}, err
	}
	addressID, err := parseUUID(command.AddressID)
	if err != nil {
		return WithdrawalAddress{}, ErrAddressNotFound
	}
	current, err := store.New(service.database).GetWithdrawalAddress(ctx, addressID)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && current.CustomerReference != profile.CustomerReference {
		return WithdrawalAddress{}, ErrAddressNotFound
	}
	if err != nil {
		return WithdrawalAddress{}, err
	}
	if current.Status != "COOLING_OFF" {
		return WithdrawalAddress{}, ErrInvalidState
	}
	if !current.CoolingUntil.Valid || service.now().UTC().Before(current.CoolingUntil.Time.UTC()) {
		return WithdrawalAddress{}, ErrCoolingOff
	}
	risk, err := service.analyzeAddress(ctx, current.AssetSymbol, current.NetworkCode, current.ExternalAddress)
	if err != nil {
		return WithdrawalAddress{}, err
	}
	tx, err := service.database.Begin(ctx)
	if err != nil {
		return WithdrawalAddress{}, fmt.Errorf("begin address activation: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := store.New(tx)
	if _, err := queries.GetCustomerProfileForUpdate(ctx, profile.CustomerReference); err != nil {
		return WithdrawalAddress{}, err
	}
	current, err = queries.GetWithdrawalAddressForUpdate(ctx, addressID)
	if err != nil || current.CustomerReference != profile.CustomerReference {
		return WithdrawalAddress{}, ErrAddressNotFound
	}
	if current.Status != "COOLING_OFF" || service.now().UTC().Before(current.CoolingUntil.Time.UTC()) {
		return WithdrawalAddress{}, ErrCoolingOff
	}
	status := "ACTIVE"
	riskLevel := current.RiskLevel
	reasonCode := "ADDRESS_COOLING_COMPLETED"
	nextReason := "Cooling completed; this address is active."
	if risk.Decision == "BLOCK" {
		status = "SUSPENDED"
		riskLevel = "BLOCKED"
		reasonCode = "CHAIN_RISK_BLOCKED"
		nextReason = "This address cannot be used; add a different address or contact support."
	}
	updated, err := queries.TransitionWithdrawalAddress(ctx, store.TransitionWithdrawalAddressParams{
		Status: status, RiskLevel: riskLevel, CoolingReason: nextReason, ID: addressID,
	})
	if err != nil {
		return WithdrawalAddress{}, fmt.Errorf("activate withdrawal address: %w", err)
	}
	metadata, _ := json.Marshal(map[string]string{"risk_provider": risk.Provider, "risk_decision": risk.Decision})
	if _, err := queries.InsertWithdrawalAddressEvent(ctx, store.InsertWithdrawalAddressEventParams{
		AddressID: addressID, FromStatus: textValue(current.Status), ToStatus: status,
		ActorType: "USER", ActorID: profile.CustomerReference, ReasonCode: reasonCode, Metadata: metadata,
	}); err != nil {
		return WithdrawalAddress{}, err
	}
	payload, _ := json.Marshal(map[string]string{"address_id": addressID.String(), "status": status})
	if err := recordMutation(ctx, tx, mutation{
		Action: "crypto.withdrawal_address." + addressStatusAction(status), ResourceType: "crypto.withdrawal_address", ResourceID: addressID.String(),
		ActorType: "USER", ActorID: profile.CustomerReference, CorrelationID: addressID,
		Metadata: metadata, AggregateType: "crypto.withdrawal_address", AggregateID: addressID.String(),
		EventType: "crypto.withdrawal_address." + addressStatusAction(status), EventVersion: int32(updated.Version), Payload: payload,
	}); err != nil {
		return WithdrawalAddress{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return WithdrawalAddress{}, fmt.Errorf("commit address activation: %w", err)
	}
	if status == "SUSPENDED" {
		return withdrawalAddressFromStore(updated, false), ErrAddressRiskBlocked
	}
	return withdrawalAddressFromStore(updated, false), nil
}

func (service *Service) ListWithdrawalAddresses(ctx context.Context, accessToken string) ([]WithdrawalAddress, error) {
	_, profile, err := service.resolveCustomer(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	rows, err := store.New(service.database).ListWithdrawalAddresses(ctx, profile.CustomerReference)
	if err != nil {
		return nil, fmt.Errorf("list crypto withdrawal addresses: %w", err)
	}
	result := make([]WithdrawalAddress, 0, len(rows))
	for _, row := range rows {
		result = append(result, withdrawalAddressFromStore(row, false))
	}
	return result, nil
}

func (service *Service) analyzeAddress(ctx context.Context, asset, network, address string) (provider.AddressRisk, error) {
	providerContext, cancel := context.WithTimeout(ctx, service.providerTimeout)
	result, err := service.chainAnalytics.AnalyzeAddress(providerContext, provider.AddressRiskRequest{Asset: asset, Network: network, Address: address})
	cancel()
	if err != nil {
		return provider.AddressRisk{}, fmt.Errorf("analyze crypto address: %w", err)
	}
	if result.Provider != service.chainAnalytics.Name() || result.Simulated && service.environment == "production" ||
		(result.Decision != "ALLOW" && result.Decision != "ELEVATED" && result.Decision != "BLOCK") {
		return provider.AddressRisk{}, provider.ErrUnavailable
	}
	return result, nil
}

func (service *Service) coolingDuration(policy store.CryptoPolicy, risk AddressRiskContext) time.Duration {
	hours := policy.AddressCoolingBaseHours
	if risk.UntrustedDevice {
		hours += policy.AddressCoolingDeviceHours
	}
	if risk.RecentSecurityChange {
		hours += policy.AddressCoolingSecurityHours
	}
	if risk.RecentRecovery {
		hours += policy.AddressCoolingRecoveryHours
	}
	if hours > policy.AddressCoolingMaxHours {
		hours = policy.AddressCoolingMaxHours
	}
	return time.Duration(hours) * time.Hour
}

func dynamicCoolingReason(risk AddressRiskContext) string {
	if risk.RecentRecovery {
		return "Cooling is active after recent account recovery."
	}
	if risk.RecentSecurityChange {
		return "Cooling is active after a recent security change."
	}
	if risk.UntrustedDevice {
		return "Cooling is active because this device is not yet trusted."
	}
	return "Standard withdrawal address cooling is active."
}

func withdrawalAddressFromStore(stored store.CryptoWithdrawalAddress, replayed bool) WithdrawalAddress {
	return WithdrawalAddress{
		ID: stored.ID.String(), Asset: stored.AssetSymbol, Network: stored.NetworkCode,
		ExternalAddress: stored.ExternalAddress, Label: stored.Label, Status: stored.Status,
		RiskLevel: stored.RiskLevel, CoolingUntil: timePointer(stored.CoolingUntil), CoolingReason: stored.CoolingReason,
		PolicyVersion: stored.PolicyVersion, CreatedAt: stored.CreatedAt.Time.UTC(), UpdatedAt: stored.UpdatedAt.Time.UTC(), Replayed: replayed,
	}
}

func addressStatusAction(status string) string {
	if status == "ACTIVE" {
		return "activated"
	}
	return "suspended"
}
