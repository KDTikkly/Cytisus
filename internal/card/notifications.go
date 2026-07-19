package card

import (
	"context"

	cardprovider "github.com/KDTikkly/Cytisus/internal/card/provider"
	"github.com/KDTikkly/Cytisus/internal/notification"
)

func (service *Service) ProviderCapabilities(ctx context.Context) cardprovider.Capabilities {
	return service.provider.Capabilities(ctx)
}

func (service *Service) ListNotifications(ctx context.Context, accessToken string, pageSize int32) ([]notification.InboxItem, error) {
	_, profile, err := service.resolveCustomer(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	return notification.ListInbox(ctx, service.database, profile.CustomerReference, pageSize)
}
