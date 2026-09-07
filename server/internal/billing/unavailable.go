package billing

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"

	gen "github.com/speakeasy-api/gram/server/gen/usage"
	"github.com/speakeasy-api/gram/server/internal/attr"
)

var errLegacyBillingUnavailable = errors.New("legacy billing operations are unavailable with the Stripe provider")

// UnavailableClient fails closed for Polar-shaped operations when Stripe is
// the configured billing provider.
type UnavailableClient struct {
	logger  *slog.Logger
	logOnce sync.Once
}

// NewUnavailableClient creates a fail-closed legacy billing client.
func NewUnavailableClient(logger *slog.Logger) *UnavailableClient {
	return &UnavailableClient{
		logger:  logger.With(attr.SlogComponent("billing_unavailable")),
		logOnce: sync.Once{},
	}
}

var _ Repository = (*UnavailableClient)(nil)
var _ Tracker = (*UnavailableClient)(nil)

func (*UnavailableClient) GetCustomer(context.Context, string) (*Customer, error) {
	return nil, errLegacyBillingUnavailable
}

func (*UnavailableClient) GetCustomerTier(context.Context, string) (*Tier, bool, error) {
	return nil, false, errLegacyBillingUnavailable
}

func (*UnavailableClient) GetPeriodUsage(context.Context, string) (*gen.PeriodUsage, error) {
	return nil, errLegacyBillingUnavailable
}

func (*UnavailableClient) GetStoredPeriodUsage(context.Context, string) (*gen.PeriodUsage, error) {
	return nil, errLegacyBillingUnavailable
}

func (*UnavailableClient) CreateCheckout(context.Context, string, string, string) (string, error) {
	return "", errLegacyBillingUnavailable
}

func (*UnavailableClient) CreateTopUpCheckout(context.Context, string, string, string) (string, error) {
	return "", errLegacyBillingUnavailable
}

func (*UnavailableClient) IsTopUpProductID(string) bool {
	return false
}

func (*UnavailableClient) CreateCustomerSession(context.Context, string) (string, error) {
	return "", errLegacyBillingUnavailable
}

func (*UnavailableClient) AttachAssistantsBenefit(context.Context, string, string) (string, error) {
	return "", errLegacyBillingUnavailable
}

func (*UnavailableClient) CancelSubscriptionAtPeriodEnd(context.Context, string) error {
	return errLegacyBillingUnavailable
}

func (*UnavailableClient) GetUsageTiers(context.Context) (*gen.UsageTiers, error) {
	return nil, errLegacyBillingUnavailable
}

func (*UnavailableClient) ValidateAndParseWebhookEvent(context.Context, []byte, http.Header) (*PolarWebhookPayload, error) {
	return nil, errLegacyBillingUnavailable
}

func (*UnavailableClient) InvalidateBillingCustomerCaches(context.Context, string) error {
	return errLegacyBillingUnavailable
}

func (c *UnavailableClient) TrackToolCallUsage(ctx context.Context, _ ToolCallUsageEvent) {
	c.logUnavailable(ctx)
}

func (c *UnavailableClient) TrackPromptCallUsage(ctx context.Context, _ PromptCallUsageEvent) {
	c.logUnavailable(ctx)
}

func (c *UnavailableClient) TrackModelUsage(ctx context.Context, _ ModelUsageEvent) {
	c.logUnavailable(ctx)
}

func (c *UnavailableClient) TrackPlatformUsage(ctx context.Context, _ []PlatformUsageEvent) {
	c.logUnavailable(ctx)
}

func (c *UnavailableClient) logUnavailable(ctx context.Context) {
	c.logOnce.Do(func() {
		c.logger.WarnContext(ctx, "legacy billing usage tracking is unavailable with the Stripe provider")
	})
}
