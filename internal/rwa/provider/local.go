package provider

import (
	"context"
	"strings"
	"time"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
)

type LocalAddressVerifier struct {
	now func() time.Time
}

func NewLocalAddressVerifier(environment config.Environment) (*LocalAddressVerifier, error) {
	if environment == config.EnvironmentProduction {
		return nil, ErrProductionMode
	}
	return &LocalAddressVerifier{now: time.Now}, nil
}

func (verifier *LocalAddressVerifier) VerifyControl(_ context.Context, request AddressProofRequest) (AddressProofResult, error) {
	if !validAddress(request.Address) || !strings.HasPrefix(request.ProofReference, "sim-proof:") {
		return AddressProofResult{}, ErrInvalidRequest
	}
	return AddressProofResult{Verified: true, Provider: "local-address-proof-simulator", Checked: verifier.now().UTC()}, nil
}

func (verifier *LocalAddressVerifier) ReviewRisk(_ context.Context, address string) (AddressRiskResult, error) {
	if !validAddress(address) {
		return AddressRiskResult{}, ErrInvalidRequest
	}
	approved := !strings.HasSuffix(strings.ToLower(address), "dead")
	reason := "SIMULATED_RISK_CLEAR"
	if !approved {
		reason = "SIMULATED_RISK_REJECTED"
	}
	return AddressRiskResult{Approved: approved, ReasonCode: reason, Provider: "local-chain-risk-simulator", Checked: verifier.now().UTC()}, nil
}

func (verifier *LocalAddressVerifier) String() string {
	return "local-address-proof-simulator"
}
