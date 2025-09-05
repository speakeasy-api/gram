package polar

import (
	"fmt"
	"time"

	polarComponents "github.com/polarsource/polar-go/models/components"
	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/cache"
)

type PolarCustomerState struct {
	OrganizationID                 string // the Speakeasy organization ID (not the Polar organization ID)
	*polarComponents.CustomerState        // nil means no state yet exists for this customer
}

var _ cache.CacheableObject[PolarCustomerState] = (*PolarCustomerState)(nil)

func (p PolarCustomerState) CacheKey() string {
	return CustomerStateCacheKey(p.OrganizationID)
}

func CustomerStateCacheKey(orgID string) string {
	return fmt.Sprintf("polar_customer_state:%s", orgID)
}

func (p PolarCustomerState) TTL() time.Duration {
	return 15 * time.Minute
}

func (p PolarCustomerState) AdditionalCacheKeys() []string {
	return []string{}
}

type Product struct {
	polarComponents.Product
}

var _ cache.CacheableObject[Product] = (*Product)(nil)

func (p Product) CacheKey() string {
	return ProductCacheKey(p.ID)
}

func ProductCacheKey(productID string) string {
	return fmt.Sprintf("polar_product:%s", productID)
}

func (p Product) TTL() time.Duration {
	return 1 * time.Hour // 1 hour because the products in Polar are almost never changed
}

func (p Product) AdditionalCacheKeys() []string {
	return []string{}
}

type PolarPeriodUsageState struct {
	OrganizationID string
	gen.PeriodUsage
}

var _ cache.CacheableObject[PolarPeriodUsageState] = (*PolarPeriodUsageState)(nil)

func (p PolarPeriodUsageState) CacheKey() string {
	return PeriodUsageStateCacheKey(p.OrganizationID)
}

func PeriodUsageStateCacheKey(orgID string) string {
	return fmt.Sprintf("polar_period_usage_state:%s", orgID)
}

func (p PolarPeriodUsageState) TTL() time.Duration {
	return 1 * time.Hour
}

func (p PolarPeriodUsageState) AdditionalCacheKeys() []string {
	return []string{}
}
