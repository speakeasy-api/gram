package polar

import (
	"fmt"
	"time"

	polarComponents "github.com/polarsource/polar-go/models/components"
	"github.com/speakeasy-api/gram/server/internal/cache"
)

type PolarCustomerState struct {
	OrganizationID                 string // the Speakeasy organization ID (not the Polar organization ID)
	*polarComponents.CustomerState        // nil means no state yet exists for this customer
}

var _ cache.CacheableObject[PolarCustomerState] = (*PolarCustomerState)(nil)

func (p PolarCustomerState) CacheKey() string {
	return OrgCacheKey(p.OrganizationID)
}

func OrgCacheKey(orgID string) string {
	return fmt.Sprintf("polar_customer_state:%s", orgID)
}

func (p PolarCustomerState) TTL() time.Duration {
	return 20 * time.Minute
}

func (p PolarCustomerState) AdditionalCacheKeys() []string {
	return []string{}
}
