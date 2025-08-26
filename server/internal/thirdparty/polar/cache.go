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
	OrganizationID string // the Speakeasy organization ID (not the Polar organization ID)
	*polarComponents.CustomerState // nil means no state yet exists for this customer
}

var _ cache.CacheableObject[PolarCustomerState] = (*PolarCustomerState)(nil)

func (p PolarCustomerState) CacheKey() string {
	return orgCacheKey(p.OrganizationID)
}

func orgCacheKey(orgID string) string {
	return fmt.Sprintf("polar_customer_state:%s", orgID)
}

func (p PolarCustomerState) TTL() time.Duration {
	return 20 * time.Minute
}

func (p PolarCustomerState) AdditionalCacheKeys() []string {
	return []string{}
}

func (p *Client) GetCustomerState(ctx context.Context, orgID string) (*polarComponents.CustomerState, error) {
	if customerState, err := p.customerStateCache.Get(ctx, orgCacheKey(orgID)); err == nil {
		return customerState.CustomerState, nil
	}

	customerState, err := p.getCustomerState(ctx, orgID)
	if err != nil {
		return nil, err
	}

	if err = p.customerStateCache.Store(ctx, PolarCustomerState{OrganizationID: orgID, CustomerState: customerState}); err != nil {
		p.logger.ErrorContext(ctx, "failed to cache customer state", attr.SlogError(err))
	}

	return customerState, nil
}
