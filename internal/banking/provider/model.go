package provider

import (
	"context"
	"errors"
	"time"

	"github.com/KDTikkly/Cytisus/internal/money"
)

type Rail string

const (
	RailACH  Rail = "ACH"
	RailWire Rail = "WIRE"
)

type Capability struct {
	Rail                  Rail
	Funding               bool
	Withdrawal            bool
	OwnershipVerification bool
	Simulated             bool
}

type Health struct {
	Available bool
	Mode      string
}

type TransferRequest struct {
	ClientReference          string
	ExternalAccountReference string
	Rail                     Rail
	Amount                   money.Decimal
}

type TransferResponse struct {
	Provider           string
	ProviderTransferID string
	InitialStatus      string
	Simulated          bool
}

type Adapter interface {
	Name() string
	Capabilities(context.Context) ([]Capability, error)
	Health(context.Context) (Health, error)
	InitiateFunding(context.Context, TransferRequest) (TransferResponse, error)
	PrepareWithdrawal(context.Context, TransferRequest) (TransferResponse, error)
}

type Event struct {
	Provider        string
	ExternalEventID string
	ResourceType    string
	ResourceID      string
	EventType       string
	ReasonCode      string
	Payload         []byte
	OccurredAt      time.Time
}

var (
	ErrTimeout               = errors.New("bank provider timed out")
	ErrUnavailable           = errors.New("bank provider is unavailable")
	ErrUnsupportedCapability = errors.New("bank provider capability is unsupported")
)
