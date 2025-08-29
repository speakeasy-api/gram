package billing

import (
	"context"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
)

type Tier string

const (
	TierFree     Tier = "free"
	TierBusiness Tier = "business"
)

type Customer struct {
	OrganizationID string
	Tier           Tier
	PeriodUsage    *gen.PeriodUsage
}

type Repository interface {
	GetCustomer(ctx context.Context, orgID string) (*Customer, error)
	GetPeriodUsage(ctx context.Context, orgID string) (*gen.PeriodUsage, error)
	CreateCheckout(ctx context.Context, orgID string, serverURL string) (string, error)
	CreateCustomerSession(ctx context.Context, orgID string) (string, error)
}
