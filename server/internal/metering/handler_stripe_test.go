package metering_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	stripeclient "github.com/speakeasy-api/gram/server/internal/thirdparty/stripe"
)

type captureV2MeterEventClient struct {
	inputs []stripeclient.V2MeterEventInput
	err    error
}

func (c *captureV2MeterEventClient) CreateMeterEvent(_ context.Context, input stripeclient.V2MeterEventInput) error {
	c.inputs = append(c.inputs, input)
	return c.err
}

var tumStripeCatalog = metering.StripeCatalogFunc(func(definition metering.Definition) (string, error) {
	if definition == metering.AgentSessionStorage() {
		return "tum", nil
	}
	return "", nil
})

const stripeExportReadingsMetric = "gram.metering.stripe_export.readings"

func newStripeExporterMetricReader(t *testing.T) (*sdkmetric.MeterProvider, *sdkmetric.ManualReader) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		require.NoError(t, meterProvider.Shutdown(context.Background()))
	})
	return meterProvider, reader
}

func stripeExportReadingOutcomes(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()

	var resourceMetrics metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &resourceMetrics))

	outcomes := make(map[string]int64)
	for _, scope := range resourceMetrics.ScopeMetrics {
		for _, candidate := range scope.Metrics {
			if candidate.Name != stripeExportReadingsMetric {
				continue
			}

			require.Equal(t, "{reading}", candidate.Unit)
			sum, ok := candidate.Data.(metricdata.Sum[int64])
			require.True(t, ok, "Stripe export readings must be an int64 counter")
			for _, point := range sum.DataPoints {
				require.Equal(t, 1, point.Attributes.Len())
				value, ok := point.Attributes.Value(attr.MeteringStripeExportDispositionKey)
				require.True(t, ok)
				outcome := value.AsString()
				require.Contains(t, []string{"ineligible", "sent"}, outcome)
				outcomes[outcome] += point.Value
			}
		}
	}
	return outcomes
}

func TestMeterReadingStripeExporterDisabledAcknowledgesWithoutProcessing(t *testing.T) {
	t.Parallel()

	client := &captureV2MeterEventClient{inputs: nil, err: errors.New("must not be called")}
	meterProvider, reader := newStripeExporterMetricReader(t)
	exporter := metering.NewMeterReadingStripeExporter(testenv.NewLogger(t), meterProvider, nil, client, nil, false)

	require.NoError(t, exporter.Handle(t.Context(), new(meteringv1.MeterReading), gcp.MessageMetadata{}))
	require.Empty(t, client.inputs)
	require.Empty(t, stripeExportReadingOutcomes(t, reader))
}

func TestMeterReadingStripeExporterNacksUnsupportedKindWithoutOutcome(t *testing.T) {
	t.Parallel()

	client := &captureV2MeterEventClient{inputs: nil, err: errors.New("must not be called")}
	meterProvider, reader := newStripeExporterMetricReader(t)
	exporter := metering.NewMeterReadingStripeExporter(testenv.NewLogger(t), meterProvider, nil, client, nil, true)
	reading := new(meteringv1.MeterReading)
	reading.SetKind(meteringv1.MeterReading_KIND_UNSPECIFIED)

	err := exporter.Handle(t.Context(), reading, gcp.MessageMetadata{})
	require.ErrorContains(t, err, "unsupported meter reading kind")
	require.Empty(t, client.inputs)
	require.Empty(t, stripeExportReadingOutcomes(t, reader))
}

func TestMeterReadingStripeExporterTransparentlyAcknowledgesAdjustments(t *testing.T) {
	t.Parallel()

	conn, _ := newMeteringPostgres(t)
	client := &captureV2MeterEventClient{inputs: nil, err: errors.New("must not be called")}
	meterProvider, reader := newStripeExporterMetricReader(t)
	exporter := metering.NewMeterReadingStripeExporter(testenv.NewLogger(t), meterProvider, conn, client, tumStripeCatalog, true)
	reading := new(meteringv1.MeterReading)
	reading.SetKind(meteringv1.MeterReading_KIND_ADJUSTMENT)
	reading.SetMeterId("unrecognized.meter")
	reading.SetMeterVersion(99)

	require.NoError(t, exporter.Handle(t.Context(), reading, gcp.MessageMetadata{}))
	require.Empty(t, client.inputs)
	require.Equal(t, map[string]int64{"ineligible": 1}, stripeExportReadingOutcomes(t, reader))
}

func TestMeterReadingStripeExporterNacksUnrecognizedMeter(t *testing.T) {
	t.Parallel()

	conn, _ := newMeteringPostgres(t)
	client := &captureV2MeterEventClient{inputs: nil, err: nil}
	meterProvider, reader := newStripeExporterMetricReader(t)
	exporter := metering.NewMeterReadingStripeExporter(testenv.NewLogger(t), meterProvider, conn, client, tumStripeCatalog, true)
	reading := new(meteringv1.MeterReading)
	reading.SetKind(meteringv1.MeterReading_KIND_USAGE)
	reading.SetMeterId("unrecognized.meter")
	reading.SetMeterVersion(1)

	err := exporter.Handle(t.Context(), reading, gcp.MessageMetadata{})
	require.ErrorContains(t, err, "is not registered")
	require.Empty(t, client.inputs)
	require.Empty(t, stripeExportReadingOutcomes(t, reader))
}

func TestMeterReadingStripeExporterAcknowledgesCatalogMiss(t *testing.T) {
	t.Parallel()

	client := &captureV2MeterEventClient{inputs: nil, err: errors.New("must not be called")}
	meterProvider, reader := newStripeExporterMetricReader(t)
	exporter := metering.NewMeterReadingStripeExporter(
		testenv.NewLogger(t),
		meterProvider,
		nil,
		client,
		metering.StripeCatalogFunc(func(metering.Definition) (string, error) {
			return "", nil
		}),
		true,
	)
	reading, _ := stripeUsageMessage(t, "organization", time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC), 1)

	require.NoError(t, exporter.Handle(t.Context(), reading, gcp.MessageMetadata{}))
	require.Empty(t, client.inputs)
	require.Equal(t, map[string]int64{"ineligible": 1}, stripeExportReadingOutcomes(t, reader))
}

func TestMeterReadingStripeExporterNacksCatalogError(t *testing.T) {
	t.Parallel()

	mapErr := errors.New("catalog unavailable")
	client := &captureV2MeterEventClient{inputs: nil, err: errors.New("must not be called")}
	meterProvider, reader := newStripeExporterMetricReader(t)
	exporter := metering.NewMeterReadingStripeExporter(
		testenv.NewLogger(t),
		meterProvider,
		nil,
		client,
		metering.StripeCatalogFunc(func(metering.Definition) (string, error) {
			return "", mapErr
		}),
		true,
	)
	reading, _ := stripeUsageMessage(t, "organization", time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC), 1)

	err := exporter.Handle(t.Context(), reading, gcp.MessageMetadata{})
	require.ErrorIs(t, err, mapErr)
	require.Empty(t, client.inputs)
	require.Empty(t, stripeExportReadingOutcomes(t, reader))
}

func TestMeterReadingStripeExporterAcknowledgesOrganizationWithoutStripeCustomer(t *testing.T) {
	t.Parallel()

	conn, organizationID := newMeteringPostgres(t)
	client := &captureV2MeterEventClient{inputs: nil, err: nil}
	meterProvider, reader := newStripeExporterMetricReader(t)
	exporter := metering.NewMeterReadingStripeExporter(testenv.NewLogger(t), meterProvider, conn, client, tumStripeCatalog, true)
	reading, _ := stripeUsageMessage(t, organizationID, time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC), 17)

	require.NoError(t, exporter.Handle(t.Context(), reading, gcp.MessageMetadata{}))
	require.Empty(t, client.inputs)
	require.Equal(t, map[string]int64{"ineligible": 1}, stripeExportReadingOutcomes(t, reader))

	require.NoError(t, testrepo.New(conn).CreateStripeBillingMetadataFixture(t.Context(), testrepo.CreateStripeBillingMetadataFixtureParams{
		OrganizationID:   organizationID,
		StripeCustomerID: pgtype.Text{String: "cus_added", Valid: true},
	}))
	require.NoError(t, exporter.Handle(t.Context(), reading, gcp.MessageMetadata{}))
	require.Len(t, client.inputs, 1)
	require.Equal(t, map[string]int64{"ineligible": 1, "sent": 1}, stripeExportReadingOutcomes(t, reader))
}

func TestMeterReadingStripeExporterNacksCustomerLookupErrorWithoutOutcome(t *testing.T) {
	t.Parallel()

	conn, organizationID := newMeteringPostgres(t)
	client := &captureV2MeterEventClient{inputs: nil, err: errors.New("must not be called")}
	meterProvider, reader := newStripeExporterMetricReader(t)
	exporter := metering.NewMeterReadingStripeExporter(testenv.NewLogger(t), meterProvider, conn, client, tumStripeCatalog, true)
	conn.Close()
	reading, _ := stripeUsageMessage(t, organizationID, time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC), 17)

	err := exporter.Handle(t.Context(), reading, gcp.MessageMetadata{})
	require.ErrorContains(t, err, "look up Stripe customer")
	require.Empty(t, client.inputs)
	require.Empty(t, stripeExportReadingOutcomes(t, reader))
}

func TestMeterReadingStripeExporterSendsMappedUsage(t *testing.T) {
	t.Parallel()

	conn, organizationID := newMeteringPostgres(t)
	require.NoError(t, testrepo.New(conn).CreateStripeBillingMetadataFixture(t.Context(), testrepo.CreateStripeBillingMetadataFixtureParams{
		OrganizationID:   organizationID,
		StripeCustomerID: pgtype.Text{String: "cus_test", Valid: true},
	}))
	client := &captureV2MeterEventClient{inputs: nil, err: nil}
	meterProvider, reader := newStripeExporterMetricReader(t)
	exporter := metering.NewMeterReadingStripeExporter(testenv.NewLogger(t), meterProvider, conn, client, tumStripeCatalog, true)
	occurredAt := time.Date(2026, time.August, 28, 10, 1, 2, 345, time.UTC)
	reading, _ := stripeUsageMessage(t, organizationID, occurredAt, 23)

	require.NoError(t, exporter.Handle(t.Context(), reading, gcp.MessageMetadata{}))
	require.Equal(t, []stripeclient.V2MeterEventInput{{
		Identifier: reading.GetId(),
		EventName:  "tum",
		CustomerID: "cus_test",
		Value:      23,
		Timestamp:  occurredAt.UTC(),
	}}, client.inputs)
	require.Equal(t, map[string]int64{"sent": 1}, stripeExportReadingOutcomes(t, reader))
}

func TestMeterReadingStripeExporterCachesStripeCustomer(t *testing.T) {
	t.Parallel()

	conn, organizationID := newMeteringPostgres(t)
	require.NoError(t, testrepo.New(conn).CreateStripeBillingMetadataFixture(t.Context(), testrepo.CreateStripeBillingMetadataFixtureParams{
		OrganizationID:   organizationID,
		StripeCustomerID: pgtype.Text{String: "cus_cached", Valid: true},
	}))
	client := &captureV2MeterEventClient{inputs: nil, err: nil}
	exporter := metering.NewMeterReadingStripeExporter(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, client, tumStripeCatalog, true)
	first, _ := stripeUsageMessage(t, organizationID, time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC), 1)
	require.NoError(t, exporter.Handle(t.Context(), first, gcp.MessageMetadata{}))

	conn.Close()
	second, _ := stripeUsageMessage(t, organizationID, time.Date(2026, time.August, 28, 10, 1, 0, 0, time.UTC), 2)
	require.NoError(t, exporter.Handle(t.Context(), second, gcp.MessageMetadata{}))
	require.Len(t, client.inputs, 2)
}

func TestMeterReadingStripeExporterNacksClassifiedStripeFailure(t *testing.T) {
	t.Parallel()

	conn, organizationID := newMeteringPostgres(t)
	require.NoError(t, testrepo.New(conn).CreateStripeBillingMetadataFixture(t.Context(), testrepo.CreateStripeBillingMetadataFixtureParams{
		OrganizationID:   organizationID,
		StripeCustomerID: pgtype.Text{String: "cus_test", Valid: true},
	}))
	stripeErr := &stripeclient.V2MeterEventError{
		Class:          stripeclient.V2MeterEventErrorRateLimit,
		Code:           "rate_limit",
		HTTPStatusCode: http.StatusTooManyRequests,
		Err:            errors.New("rate limited"),
	}
	client := &captureV2MeterEventClient{inputs: nil, err: stripeErr}
	meterProvider, reader := newStripeExporterMetricReader(t)
	exporter := metering.NewMeterReadingStripeExporter(testenv.NewLogger(t), meterProvider, conn, client, tumStripeCatalog, true)
	reading, _ := stripeUsageMessage(t, organizationID, time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC), 1)

	err := exporter.Handle(t.Context(), reading, gcp.MessageMetadata{})
	require.ErrorIs(t, err, stripeErr)
	require.Len(t, client.inputs, 1)
	require.Empty(t, stripeExportReadingOutcomes(t, reader))
}

func stripeUsageMessage(t *testing.T, organizationID string, occurredAt time.Time, value int64) (*meteringv1.MeterReading, metering.Reading) {
	t.Helper()
	return usageMessage(t, metering.UsageInput{
		Meter:       metering.AgentSessionStorage(),
		Scope:       metering.ProjectScope(organizationID, uuid.New()),
		OperationID: uuid.NewString(),
		Value:       value,
		OccurredAt:  occurredAt,
		ProducedAt:  occurredAt.Add(time.Second),
		Source:      "stripe-exporter-test",
		Attributes:  nil,
	})
}
