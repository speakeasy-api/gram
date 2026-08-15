package usage

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
)

const meterStripeInvoicePaymentFailed = "billing.stripe.invoice_payment_failed"

type stripeWebhookMetricsRecorder interface {
	RecordInvoicePaymentFailed(context.Context)
}

type stripeWebhookMetrics struct {
	invoicePaymentFailed metric.Int64Counter
}

func newStripeWebhookMetrics(meterProvider metric.MeterProvider, logger *slog.Logger) *stripeWebhookMetrics {
	counter, err := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/usage").Int64Counter(
		meterStripeInvoicePaymentFailed,
		metric.WithDescription("Number of distinct Stripe invoice payment-failure webhooks durably received"),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterStripeInvoicePaymentFailed), attr.SlogError(err))
	}

	return &stripeWebhookMetrics{invoicePaymentFailed: counter}
}

func (m *stripeWebhookMetrics) RecordInvoicePaymentFailed(ctx context.Context) {
	if m.invoicePaymentFailed != nil {
		m.invoicePaymentFailed.Add(ctx, 1)
	}
}
