package gram

import (
	"testing"
	"time"

	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"

	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/otel/dialect"
	"github.com/speakeasy-api/gram/server/internal/otelpub"
)

func newTestConnectionLogExport(t *testing.T, organizationID string, enabled bool) (*connectionLogExport, *[]*otelv1.InboundLogRecord) {
	t.Helper()

	published := &[]*otelv1.InboundLogRecord{}
	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			record, ok := args.Get(1).(*otelv1.InboundLogRecord)
			require.True(t, ok)
			*published = append(*published, record)
		}).Return(gcp.NewSuccessPublishResult()).Maybe()

	features := &feature.InMemory{}
	features.SetFlag(feature.FlagMCPConnectionLogExport, organizationID, enabled)
	return &connectionLogExport{
		logs: otelpub.NewLogger(publisher,
			resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceNameKey.String(gatewayRecordsServiceName)),
			dialect.ConnectionLogScope),
		features: features,
	}, published
}

func connectionLogTestRecord() log.Record {
	var record log.Record
	record.SetEventName("speakeasy.mcp_connection")
	record.SetTimestamp(time.Now())
	record.SetSeverity(log.SeverityError)
	record.AddAttributes(log.String("error.type", "idp_unavailable"), log.Float64("speakeasy.mcp_connection.duration", 1.5), log.Int64("speakeasy.mcp_connection.repeated_failures", 2), log.Bool("speakeasy.mcp_connection.final", true))
	return record
}

func TestConnectionLogExport_PublishesUnderTheRowTenant(t *testing.T) {
	t.Parallel()

	export, published := newTestConnectionLogExport(t, "org-row", true)
	require.NoError(t, export.Publish(t.Context(), "org-row", "project-row", "attempt:step:reason", connectionLogTestRecord()))

	require.Len(t, *published, 1)
	record := (*published)[0]
	require.Equal(t, "org-row", record.GetProvenance().GetOrganizationId())
	require.Equal(t, "project-row", record.GetProvenance().GetProjectId())
	require.Equal(t, "attempt:step:reason", record.GetRecordId())
	require.Equal(t, dialect.ConnectionLogScope, record.GetScope().GetName())
}

func TestConnectionLogExport_FlagIsPerOrganization(t *testing.T) {
	t.Parallel()

	export, _ := newTestConnectionLogExport(t, "org-on", true)
	enabled, err := export.Enabled(t.Context(), "org-on")
	require.NoError(t, err)
	require.True(t, enabled)

	export, _ = newTestConnectionLogExport(t, "org-off", false)
	enabled, err = export.Enabled(t.Context(), "org-off")
	require.NoError(t, err)
	require.False(t, enabled)
}

func TestConnectionLogExport_UnknownFlagIsAnError(t *testing.T) {
	t.Parallel()

	export, _ := newTestConnectionLogExport(t, "org-on", true)
	enabled, err := export.Enabled(t.Context(), "org-without-an-entry")
	require.ErrorIs(t, err, errConnectionLogExportFlagUnknown)
	require.False(t, enabled)
}
