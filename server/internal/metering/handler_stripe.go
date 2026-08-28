package metering

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"golang.org/x/sync/singleflight"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	meteringrepo "github.com/speakeasy-api/gram/server/internal/metering/repo"
	"github.com/speakeasy-api/gram/server/internal/streams"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
)

const (
	meterStripeExportErrors    = "gram.metering.stripe_export.errors"
	stripeCustomerCacheMaxSize = 4096
	stripeCustomerCacheTTL     = 10 * time.Minute
)

type stripeMeterKey struct {
	id      MeterID
	version uint32
}

var stripeMeterEventNames = map[stripeMeterKey]string{
	{id: MeterAgentSessionStorage, version: 1}: "aicp_agent_session_storage_v1",
}

type stripeCustomerReader interface {
	GetStripeCustomerID(context.Context, string) (pgtype.Text, error)
}

// MeterReadingStripeExporter sends recognized usage readings to Stripe billing meters.
type MeterReadingStripeExporter struct {
	stripeCustomers stripeCustomerReader
	stripe          stripeclient.V2MeterEventClient
	customers       *expirable.LRU[string, string]
	customerLookups singleflight.Group
	exportErrors    metric.Int64Counter
}

// NewMeterReadingStripeExporter creates a Stripe meter-reading subscriber.
func NewMeterReadingStripeExporter(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	readReplica *pgxpool.Pool,
	stripe stripeclient.V2MeterEventClient,
) *MeterReadingStripeExporter {
	exportErrors, err := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/metering").Int64Counter(
		meterStripeExportErrors,
		metric.WithDescription("Stripe meter-reading export failures by classified cause."),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterStripeExportErrors), attr.SlogError(err))
	}

	return &MeterReadingStripeExporter{
		stripeCustomers: meteringrepo.New(readReplica),
		stripe:          stripe,
		customers:       expirable.NewLRU[string, string](stripeCustomerCacheMaxSize, nil, stripeCustomerCacheTTL),
		customerLookups: singleflight.Group{},
		exportErrors:    exportErrors,
	}
}

var _ streams.Handler[*meteringv1.MeterReading] = (*MeterReadingStripeExporter)(nil)

// Handle transparently acknowledges adjustments and exports recognized usage readings.
func (e *MeterReadingStripeExporter) Handle(ctx context.Context, reading *meteringv1.MeterReading, _ gcp.MessageMetadata) error {
	if reading.GetKind() == meteringv1.MeterReading_KIND_ADJUSTMENT {
		return nil
	}
	if reading.GetKind() != meteringv1.MeterReading_KIND_USAGE {
		return fmt.Errorf("unsupported meter reading kind %s", reading.GetKind())
	}

	eventName, ok := stripeMeterEventNames[stripeMeterKey{
		id:      MeterID(reading.GetMeterId()),
		version: reading.GetMeterVersion(),
	}]
	if !ok {
		return fmt.Errorf("meter %q version %d is not mapped to Stripe", reading.GetMeterId(), reading.GetMeterVersion())
	}

	occurredAt, err := time.Parse(time.RFC3339Nano, reading.GetOccurredAt())
	if err != nil {
		return fmt.Errorf("parse meter reading occurred_at: %w", err)
	}

	customerID, err := e.stripeCustomerID(ctx, reading.GetOrganizationId())
	if err != nil {
		return fmt.Errorf("look up Stripe customer: %w", err)
	}
	if customerID == "" {
		return nil
	}

	err = e.stripe.CreateMeterEvent(ctx, stripeclient.V2MeterEventInput{
		Identifier: reading.GetId(),
		EventName:  eventName,
		CustomerID: customerID,
		Value:      reading.GetValue(),
		Timestamp:  occurredAt.UTC(),
	})
	if err != nil {
		e.recordExportError(ctx, err)
		return fmt.Errorf("export meter reading to Stripe: %w", err)
	}
	return nil
}

func (e *MeterReadingStripeExporter) stripeCustomerID(ctx context.Context, organizationID string) (string, error) {
	if customerID, cached := e.customers.Get(organizationID); cached {
		return customerID, nil
	}

	value, err, _ := e.customerLookups.Do(organizationID, func() (any, error) {
		if customerID, cached := e.customers.Get(organizationID); cached {
			return customerID, nil
		}

		resolved, err := e.stripeCustomers.GetStripeCustomerID(ctx, organizationID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return "", nil
		case err != nil:
			return "", fmt.Errorf("query Stripe customer: %w", err)
		case !resolved.Valid || resolved.String == "":
			return "", nil
		}

		e.customers.Add(organizationID, resolved.String)
		return resolved.String, nil
	})
	if err != nil {
		return "", fmt.Errorf("coalesce Stripe customer lookup: %w", err)
	}

	customerID, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("unexpected Stripe customer lookup result %T", value)
	}
	return customerID, nil
}

func (e *MeterReadingStripeExporter) recordExportError(ctx context.Context, err error) {
	if e.exportErrors == nil {
		return
	}

	class := stripeclient.V2MeterEventErrorUnknown
	code := ""
	statusCode := 0
	if stripeErr, ok := errors.AsType[*stripeclient.V2MeterEventError](err); ok {
		class = stripeErr.Class
		code = stripeErr.Code
		statusCode = stripeErr.HTTPStatusCode
	}

	attributes := []attribute.KeyValue{attr.ErrorType(class)}
	if code != "" {
		attributes = append(attributes, attr.StripeErrorCode(code))
	}
	if statusCode != 0 {
		attributes = append(attributes, attr.HTTPResponseStatusCode(statusCode))
	}
	e.exportErrors.Add(ctx, 1, metric.WithAttributes(attributes...))
}
