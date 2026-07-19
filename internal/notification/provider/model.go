package provider

import (
	"context"
	"errors"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
)

type Capability struct {
	Mode         string
	PushEnabled  bool
	EmailEnabled bool
}

type Delivery struct {
	NotificationEventID string
	CustomerReference   string
	Channel             string
	TitleKey            string
	BodyKey             string
}

type Result struct {
	Status      string
	FailureCode string
}

type Adapter interface {
	Name() string
	Capabilities(context.Context) Capability
	Health(context.Context) error
	Deliver(context.Context, Delivery) (Result, error)
}

var (
	ErrProductionMode  = errors.New("local notification simulator is disabled in production")
	ErrInvalidDelivery = errors.New("invalid notification delivery")
)

func ValidateEnvironment(environment config.Environment) error {
	if environment != config.EnvironmentLocal && environment != config.EnvironmentTest {
		return ErrProductionMode
	}
	return nil
}
