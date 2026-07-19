package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
)

const localProviderName = "local-bank-simulator"

type Local struct {
	environment config.Environment
}

func NewLocal(environment config.Environment) (*Local, error) {
	if environment != config.EnvironmentLocal && environment != config.EnvironmentTest {
		return nil, fmt.Errorf("create local bank provider: %w", ErrUnavailable)
	}
	return &Local{environment: environment}, nil
}

func (provider *Local) Name() string { return localProviderName }

func (provider *Local) Capabilities(context.Context) ([]Capability, error) {
	return []Capability{
		{Rail: RailACH, Funding: true, Withdrawal: true, OwnershipVerification: true, Simulated: true},
		{Rail: RailWire, Funding: true, Withdrawal: true, OwnershipVerification: true, Simulated: true},
	}, nil
}

func (provider *Local) Health(context.Context) (Health, error) {
	return Health{Available: true, Mode: "SIMULATED"}, nil
}

func (provider *Local) InitiateFunding(ctx context.Context, request TransferRequest) (TransferResponse, error) {
	return provider.prepare(ctx, "funding", request)
}

func (provider *Local) PrepareWithdrawal(ctx context.Context, request TransferRequest) (TransferResponse, error) {
	return provider.prepare(ctx, "withdrawal", request)
}

func (provider *Local) prepare(ctx context.Context, operation string, request TransferRequest) (TransferResponse, error) {
	if err := ctx.Err(); err != nil {
		return TransferResponse{}, err
	}
	if strings.Contains(strings.ToLower(request.ExternalAccountReference), "timeout") {
		return TransferResponse{}, ErrTimeout
	}
	if request.Rail != RailACH && request.Rail != RailWire || !request.Amount.IsPositive() || request.ClientReference == "" {
		return TransferResponse{}, ErrUnsupportedCapability
	}
	digest := sha256.Sum256([]byte(operation + ":" + request.ClientReference + ":" + request.ExternalAccountReference + ":" + string(request.Rail) + ":" + request.Amount.String()))
	status := "INITIATED"
	if request.Rail == RailWire {
		status = "INSTRUCTIONS_ISSUED"
	}
	if operation == "withdrawal" {
		status = "DRAFT"
	}
	return TransferResponse{
		Provider:           localProviderName,
		ProviderTransferID: "sim_" + hex.EncodeToString(digest[:16]),
		InitialStatus:      status,
		Simulated:          true,
	}, nil
}
