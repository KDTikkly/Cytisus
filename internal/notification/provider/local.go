package provider

import (
	"context"
	"strings"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
)

type Local struct{}

func NewLocal(environment config.Environment) (*Local, error) {
	if err := ValidateEnvironment(environment); err != nil {
		return nil, err
	}
	return &Local{}, nil
}

func (*Local) Name() string { return "local-notification-simulator" }

func (*Local) Capabilities(context.Context) Capability {
	return Capability{Mode: "SIMULATED", PushEnabled: true, EmailEnabled: true}
}

func (*Local) Health(context.Context) error { return nil }

func (*Local) Deliver(_ context.Context, delivery Delivery) (Result, error) {
	if strings.TrimSpace(delivery.NotificationEventID) == "" || strings.TrimSpace(delivery.CustomerReference) == "" ||
		(delivery.Channel != "IN_APP" && delivery.Channel != "PUSH" && delivery.Channel != "EMAIL") ||
		strings.TrimSpace(delivery.TitleKey) == "" || strings.TrimSpace(delivery.BodyKey) == "" {
		return Result{}, ErrInvalidDelivery
	}
	return Result{Status: "SENT"}, nil
}
