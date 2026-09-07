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

type stripeExportOutcome string

const (
	meterStripeExportErrors    = "gram.metering.stripe_export.errors"
	meterStripeExportReadings  = "gram.metering.stripe_export.readings"
	stripeCustomerCacheMaxSize = 4096
	stripeCustomerCacheTTL     = 10 * time.Minute

	stripeExportOutcomeIneligible stripeExportOutcome = "ineligible"
	stripeExportOutcomeSent       stripeExportOutcome = "sent"
)

type stripeCustomerReader interface {
	GetStripeCustomerID(context.Context, string) (pgtype.Text, error)
}

// StripeCatalog maps registered meter definitions to Stripe meter event names.
// Returning an empty name with no error explicitly drops and acknowledges the reading.
type StripeCatalog interface {
	MeterEventName(Definition) (string, error)
}

// StripeCatalogFunc adapts a function into a [StripeCatalog].
type StripeCatalogFunc func(Definition) (string, error)

// MeterEventName implements [StripeCatalog].
func (f StripeCatalogFunc) MeterEventName(definition Definition) (string, error) {
	return f(definition)
}

// MeterReadingStripeExporter sends recognized usage readings to Stripe billing meters.
type MeterReadingStripeExporter struct {
	enabled         bool
	stripeCustomers stripeCustomerReader
	stripe          stripeclient.V2MeterEventClient
	stripeCatalog   StripeCatalog
	customers       *expirable.LRU[string, string]
	customerLookups singleflight.Group
	exportErrors    metric.Int64Counter
	readings        metric.Int64Counter
}

// NewMeterReadingStripeExporter creates a Stripe meter-reading subscriber.
func NewMeterReadingStripeExporter(
	logger *slog.Logger,
	meterProvider metric.MeterProvider,
	readReplica *pgxpool.Pool,
	stripe stripeclient.V2MeterEventClient,
	stripeCatalog StripeCatalog,
	enabled bool,
) *MeterReadingStripeExporter {
	meter := meterProvider.Meter("github.com/speakeasy-api/gram/server/internal/metering")
	exportErrors, err := meter.Int64Counter(
		meterStripeExportErrors,
		metric.WithDescription("Stripe meter-reading export failures by classified cause."),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterStripeExportErrors), attr.SlogError(err))
	}

	readings, err := meter.Int64Counter(
		meterStripeExportReadings,
		metric.WithDescription("Stripe meter readings by export disposition: ineligible or sent."),
		metric.WithUnit("{reading}"),
	)
	if err != nil {
		logger.ErrorContext(context.Background(), "create metric", attr.SlogMetricName(meterStripeExportReadings), attr.SlogError(err))
	}

	return &MeterReadingStripeExporter{
		enabled:         enabled,
		stripeCustomers: meteringrepo.New(readReplica),
		stripe:          stripe,
		stripeCatalog:   stripeCatalog,
		customers:       expirable.NewLRU[string, string](stripeCustomerCacheMaxSize, nil, stripeCustomerCacheTTL),
		customerLookups: singleflight.Group{},
		exportErrors:    exportErrors,
		readings:        readings,
	}
}

var _ streams.Handler[*meteringv1.MeterReading] = (*MeterReadingStripeExporter)(nil)

// Handle acknowledges every reading while disabled, transparently acknowledges
// adjustments, and exports recognized usage readings.
func (e *MeterReadingStripeExporter) Handle(ctx context.Context, reading *meteringv1.MeterReading, _ gcp.MessageMetadata) error {
	if !e.enabled {
		return nil
	}
	if reading.GetKind() == meteringv1.MeterReading_KIND_ADJUSTMENT {
		e.recordReadingOutcome(ctx, stripeExportOutcomeIneligible)
		return nil
	}
	if reading.GetKind() != meteringv1.MeterReading_KIND_USAGE {
		return fmt.Errorf("unsupported meter reading kind %s", reading.GetKind())
	}

	definition, ok := LookupDefinition(MeterID(reading.GetMeterId()), reading.GetMeterVersion())
	if !ok {
		return fmt.Errorf("meter %q version %d is not registered", reading.GetMeterId(), reading.GetMeterVersion())
	}
	if e.stripeCatalog == nil {
		return errors.New("stripe catalog is not configured")
	}
	eventName, err := e.stripeCatalog.MeterEventName(definition)
	if err != nil {
		return fmt.Errorf("map meter %q version %d to Stripe: %w", reading.GetMeterId(), reading.GetMeterVersion(), err)
	}
	if eventName == "" {
		e.recordReadingOutcome(ctx, stripeExportOutcomeIneligible)
		return nil
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
		e.recordReadingOutcome(ctx, stripeExportOutcomeIneligible)
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
	e.recordReadingOutcome(ctx, stripeExportOutcomeSent)
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

func (e *MeterReadingStripeExporter) recordReadingOutcome(ctx context.Context, outcome stripeExportOutcome) {
	if e.readings == nil {
		return
	}

	e.readings.Add(ctx, 1, metric.WithAttributes(attr.MeteringStripeExportDisposition(outcome)))
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
