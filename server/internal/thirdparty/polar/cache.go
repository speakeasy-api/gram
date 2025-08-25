package polar

import (
	"context"
	"fmt"
	"time"

	polarComponents "github.com/polarsource/polar-go/models/components"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/attr"
	)

type PolarCustomerState struct {
	polarComponents.CustomerState
}

var _ cache.CacheableObject[PolarCustomerState] = (*PolarCustomerState)(nil)

func (p PolarCustomerState) CacheKey() string {
	return fmt.Sprintf("polar_customer_state:%s", p.CustomerState.OrganizationID)
}

func (p PolarCustomerState) TTL() time.Duration {
	return 20 * time.Minute
}

func (p PolarCustomerState) AdditionalCacheKeys() []string {
	return []string{}
}

func (p *Client) GetCustomerState(ctx context.Context, orgID string) (*polarComponents.CustomerState, error) {
	if customerState, err := p.customerStateCache.Get(ctx, PolarCustomerState{CustomerState: polarComponents.CustomerState{OrganizationID: orgID}}.CacheKey()); err == nil {
		return &customerState.CustomerState, nil
	}

	customerState, err := p.getCustomerState(ctx, orgID)
	if err != nil {
		return nil, err
	}

	if err = p.customerStateCache.Store(ctx, PolarCustomerState{CustomerState: *customerState}); err != nil {
		p.logger.ErrorContext(ctx, "failed to cache customer state", attr.SlogError(err))
	}

	return customerState, nil
}
