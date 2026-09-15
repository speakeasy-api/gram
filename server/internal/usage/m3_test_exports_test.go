package usage

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
)

type M3StripeWebhookMetrics struct {
	subscriptionLosses atomic.Int64
}

func (*M3StripeWebhookMetrics) RecordInvoicePaymentFailed(context.Context) {}

func (m *M3StripeWebhookMetrics) RecordSubscriptionLost(context.Context) {
	m.subscriptionLosses.Add(1)
}

func (m *M3StripeWebhookMetrics) SubscriptionLosses() int64 {
	return m.subscriptionLosses.Load()
}

func CloneM3BillingValidationDatabase(t *testing.T, name string) (*pgxpool.Pool, error) {
	t.Helper()
	db, err := infra.CloneTestDatabase(t, name)
	if err != nil {
		return nil, fmt.Errorf("clone M3 billing validation database: %w", err)
	}
	return db, nil
}

func NewM3StripeWebhookService(t *testing.T, db *pgxpool.Pool, stripeClient stripeclient.Client) (*Service, *M3StripeWebhookMetrics) {
	t.Helper()
	metrics := &M3StripeWebhookMetrics{}
	service := &Service{
		logger:          testenv.NewLogger(t),
		db:              db,
		auditLogger:     audit.NewLogger(),
		stripeClient:    stripeClient,
		stripeHandler:   nil,
		stripeMetrics:   metrics,
		productFeatures: nil,
	}
	service.stripeHandler = service.serviceStripeWebhookHandler
	return service, metrics
}
