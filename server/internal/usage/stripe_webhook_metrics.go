package usage

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const (
	meterStripeInvoicePaymentFailed = "billing.stripe.invoice_payment_failed"
	meterStripeSubscriptionLost     = "billing.stripe.subscription_lost"
)

type stripeWebhookMetricsRecorder interface {
	RecordInvoicePaymentFailed(context.Context)
	RecordSubscriptionLost(context.Context)
}

type stripeWebhookMetrics struct {
	invoicePaymentFailed metric.Int64Counter
	subscriptionLost     metric.Int64Counter
}

func newStripeWebhookMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *stripeWebhookMetrics {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/usage")
	counter, err := meter.Int64Counter(
		meterStripeInvoicePaymentFailed,
		metric.WithDescription("Number of distinct Stripe invoice payment-failure webhooks durably received"),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterStripeInvoicePaymentFailed), attr.SlogError(err))
	}
	subscriptionLost, err := meter.Int64Counter(
		meterStripeSubscriptionLost,
		metric.WithDescription("Number of PAYG subscriptions durably deactivated after Stripe subscription loss"),
		metric.WithUnit("{subscription}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterStripeSubscriptionLost), attr.SlogError(err))
	}

	return &stripeWebhookMetrics{invoicePaymentFailed: counter, subscriptionLost: subscriptionLost}
}

func (m *stripeWebhookMetrics) RecordSubscriptionLost(ctx context.Context) {
	if m.subscriptionLost != nil {
		m.subscriptionLost.Add(ctx, 1)
	}
}

func (m *stripeWebhookMetrics) RecordInvoicePaymentFailed(ctx context.Context) {
	if m.invoicePaymentFailed != nil {
		m.invoicePaymentFailed.Add(ctx, 1)
	}
}
