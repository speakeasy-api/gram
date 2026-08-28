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

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
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

func TestMeterReadingStripeExporterTransparentlyAcknowledgesAdjustments(t *testing.T) {
	t.Parallel()

	conn, _ := newMeteringPostgres(t)
	client := &captureV2MeterEventClient{inputs: nil, err: errors.New("must not be called")}
	exporter := metering.NewMeterReadingStripeExporter(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, client)
	reading := new(meteringv1.MeterReading)
	reading.SetKind(meteringv1.MeterReading_KIND_ADJUSTMENT)
	reading.SetMeterId("unrecognized.meter")
	reading.SetMeterVersion(99)

	require.NoError(t, exporter.Handle(t.Context(), reading, gcp.MessageMetadata{}))
	require.Empty(t, client.inputs)
}

func TestMeterReadingStripeExporterNacksUnrecognizedMeter(t *testing.T) {
	t.Parallel()

	conn, _ := newMeteringPostgres(t)
	client := &captureV2MeterEventClient{inputs: nil, err: nil}
	exporter := metering.NewMeterReadingStripeExporter(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, client)
	reading := new(meteringv1.MeterReading)
	reading.SetKind(meteringv1.MeterReading_KIND_USAGE)
	reading.SetMeterId("unrecognized.meter")
	reading.SetMeterVersion(1)

	err := exporter.Handle(t.Context(), reading, gcp.MessageMetadata{})
	require.ErrorContains(t, err, "is not mapped to Stripe")
	require.Empty(t, client.inputs)
}

func TestMeterReadingStripeExporterAcknowledgesOrganizationWithoutStripeCustomer(t *testing.T) {
	t.Parallel()

	conn, organizationID := newMeteringPostgres(t)
	client := &captureV2MeterEventClient{inputs: nil, err: nil}
	exporter := metering.NewMeterReadingStripeExporter(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, client)
	reading, _ := stripeUsageMessage(t, organizationID, time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC), 17)

	require.NoError(t, exporter.Handle(t.Context(), reading, gcp.MessageMetadata{}))
	require.Empty(t, client.inputs)

	require.NoError(t, testrepo.New(conn).CreateStripeBillingMetadataFixture(t.Context(), testrepo.CreateStripeBillingMetadataFixtureParams{
		OrganizationID:   organizationID,
		StripeCustomerID: pgtype.Text{String: "cus_added", Valid: true},
	}))
	require.NoError(t, exporter.Handle(t.Context(), reading, gcp.MessageMetadata{}))
	require.Len(t, client.inputs, 1)
}

func TestMeterReadingStripeExporterSendsMappedUsage(t *testing.T) {
	t.Parallel()

	conn, organizationID := newMeteringPostgres(t)
	require.NoError(t, testrepo.New(conn).CreateStripeBillingMetadataFixture(t.Context(), testrepo.CreateStripeBillingMetadataFixtureParams{
		OrganizationID:   organizationID,
		StripeCustomerID: pgtype.Text{String: "cus_test", Valid: true},
	}))
	client := &captureV2MeterEventClient{inputs: nil, err: nil}
	exporter := metering.NewMeterReadingStripeExporter(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, client)
	occurredAt := time.Date(2026, time.August, 28, 10, 1, 2, 345, time.UTC)
	reading, _ := stripeUsageMessage(t, organizationID, occurredAt, 23)

	require.NoError(t, exporter.Handle(t.Context(), reading, gcp.MessageMetadata{}))
	require.Equal(t, []stripeclient.V2MeterEventInput{{
		Identifier: reading.GetId(),
		EventName:  "aicp_agent_session_storage_v1",
		CustomerID: "cus_test",
		Value:      23,
		Timestamp:  occurredAt.UTC(),
	}}, client.inputs)
}

func TestMeterReadingStripeExporterCachesStripeCustomer(t *testing.T) {
	t.Parallel()

	conn, organizationID := newMeteringPostgres(t)
	require.NoError(t, testrepo.New(conn).CreateStripeBillingMetadataFixture(t.Context(), testrepo.CreateStripeBillingMetadataFixtureParams{
		OrganizationID:   organizationID,
		StripeCustomerID: pgtype.Text{String: "cus_cached", Valid: true},
	}))
	client := &captureV2MeterEventClient{inputs: nil, err: nil}
	exporter := metering.NewMeterReadingStripeExporter(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, client)
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
	exporter := metering.NewMeterReadingStripeExporter(testenv.NewLogger(t), testenv.NewMeterProvider(t), conn, client)
	reading, _ := stripeUsageMessage(t, organizationID, time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC), 1)

	err := exporter.Handle(t.Context(), reading, gcp.MessageMetadata{})
	require.ErrorIs(t, err, stripeErr)
	require.Len(t, client.inputs, 1)
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
