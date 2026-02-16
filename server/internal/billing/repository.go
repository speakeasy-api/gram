package billing

import (
	"context"
	"net/http"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
)

type Tier string

const (
	TierBase       Tier = "free"
	TierPro        Tier = "pro"
	TierEnterprise Tier = "enterprise"
)

type Customer struct {
	OrganizationID string
	PeriodUsage    *gen.PeriodUsage
}

type PolarWebhookPayload struct {
	Type string `json:"type"`
	Data struct {
		Customer *WebhookCustomer `json:"customer,omitempty"`
		Product  *WebhookProduct  `json:"product,omitempty"`
	} `json:"data"`
}

type WebhookCustomer struct {
	ID         string `json:"id"`
	ExternalID string `json:"external_id"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

type WebhookProduct struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type Repository interface {
	GetCustomer(ctx context.Context, orgID string) (*Customer, error)
	GetCustomerTier(ctx context.Context, orgID string) (*Tier, bool, error)
	GetPeriodUsage(ctx context.Context, orgID string) (*gen.PeriodUsage, error)
	// this enforces that we can only get usage results from a stored value, specifically for hotpath usage with no outbound API call
	GetStoredPeriodUsage(ctx context.Context, orgID string) (*gen.PeriodUsage, error)
	CreateCheckout(ctx context.Context, orgID string, serverURL string, successURL string) (string, error)
	CreateCustomerSession(ctx context.Context, orgID string) (string, error)
	GetUsageTiers(ctx context.Context) (*gen.UsageTiers, error)
	ValidateAndParseWebhookEvent(ctx context.Context, payload []byte, webhookHeader http.Header) (*PolarWebhookPayload, error)
	InvalidateBillingCustomerCaches(ctx context.Context, orgID string) error
}
