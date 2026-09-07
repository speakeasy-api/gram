package otel

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	otelserver "github.com/speakeasy-api/gram/server/gen/http/otel/server"
	gen "github.com/speakeasy-api/gram/server/gen/otel"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	collectorlogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

func TestLogsResponseUsesOTLPSuccessStatus(t *testing.T) {
	t.Parallel()

	response := httptest.NewRecorder()
	err := otelserver.EncodeLogsResponse(nil)(t.Context(), response, nil)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.Code)
}

func TestLogsPublishesFlattenedRecordsWithAuthenticatedProvenance(t *testing.T) {
	t.Parallel()

	request := &collectorlogsv1.ExportLogsServiceRequest{
		ResourceLogs: []*logsv1.ResourceLogs{{
			Resource: &resourcev1.Resource{Attributes: []*commonv1.KeyValue{{
				Key:   "service.name",
				Value: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "producer"}},
			}}},
			SchemaUrl: "resource-schema",
			ScopeLogs: []*logsv1.ScopeLogs{{
				Scope:     &commonv1.InstrumentationScope{Name: "producer.scope", Version: "1.2.3"},
				SchemaUrl: "scope-schema",
				LogRecords: []*logsv1.LogRecord{{
					TimeUnixNano:         123,
					ObservedTimeUnixNano: 456,
					SeverityNumber:       logsv1.SeverityNumber_SEVERITY_NUMBER_INFO,
					SeverityText:         "INFO",
					Body:                 &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "hello"}},
					TraceId:              []byte("0123456789abcdef"),
					SpanId:               []byte("01234567"),
				}},
			}},
		}},
	}
	body, err := proto.Marshal(request)
	require.NoError(t, err)

	var published *otelv1.InboundLogRecord
	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		record, ok := args.Get(1).(*otelv1.InboundLogRecord)
		require.True(t, ok)
		published = record
	}).Return(gcp.NewSuccessPublishResult()).Once()
	service := &Service{
		logger:          testenv.NewLogger(t),
		tracer:          testenv.NewTracerProvider(t).Tracer("test"),
		auth:            nil,
		logPublisher:    publisher,
		metricPublisher: nil,
		spanPublisher:   nil,
	}
	projectID := uuid.MustParse(testLogProjectID)
	ctx := contextvalues.SetAuthContext(t.Context(), testOTELAuthContext(projectID))

	err = service.Logs(ctx, &gen.LogsPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ContentEncoding:  nil,
	}, io.NopCloser(bytes.NewReader(body)))

	require.NoError(t, err)
	publisher.AssertExpectations(t)
	require.NotNil(t, published)
	require.Equal(t, uint64(123), published.GetTimeUnixNano())
	require.Equal(t, uint64(456), published.GetObservedTimeUnixNano())
	require.Equal(t, "hello", published.GetBody().GetStringValue())
	require.Equal(t, "producer", published.GetResource().GetAttributes()[0].GetValue().GetStringValue())
	require.Equal(t, "resource-schema", published.GetResourceSchemaUrl())
	require.Equal(t, "producer.scope", published.GetScope().GetName())
	require.Equal(t, "scope-schema", published.GetScopeSchemaUrl())
	require.Equal(t, ProvenanceSource, published.GetProvenance().GetSource())
	require.Equal(t, testLogOrganizationID, published.GetProvenance().GetOrganizationId())
	require.Equal(t, projectID.String(), published.GetProvenance().GetProjectId())
	_, err = uuid.Parse(published.GetRecordId())
	require.NoError(t, err)
}

func TestLogsStampsObservedTimeWhenMissing(t *testing.T) {
	t.Parallel()

	request := &collectorlogsv1.ExportLogsServiceRequest{
		ResourceLogs: []*logsv1.ResourceLogs{{
			ScopeLogs: []*logsv1.ScopeLogs{{
				LogRecords: []*logsv1.LogRecord{{
					Body: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "no timestamps"}},
				}},
			}},
		}},
	}
	body, err := proto.Marshal(request)
	require.NoError(t, err)

	var published *otelv1.InboundLogRecord
	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		record, ok := args.Get(1).(*otelv1.InboundLogRecord)
		require.True(t, ok)
		published = record
	}).Return(gcp.NewSuccessPublishResult()).Once()
	service := &Service{
		logger:          testenv.NewLogger(t),
		tracer:          testenv.NewTracerProvider(t).Tracer("test"),
		auth:            nil,
		logPublisher:    publisher,
		metricPublisher: nil,
		spanPublisher:   nil,
	}
	projectID := uuid.MustParse(testLogProjectID)
	ctx := contextvalues.SetAuthContext(t.Context(), testOTELAuthContext(projectID))

	before := uint64(time.Now().UnixNano())
	err = service.Logs(ctx, &gen.LogsPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ContentEncoding:  nil,
	}, io.NopCloser(bytes.NewReader(body)))

	require.NoError(t, err)
	publisher.AssertExpectations(t)
	require.NotNil(t, published)
	require.Zero(t, published.GetTimeUnixNano())
	require.GreaterOrEqual(t, published.GetObservedTimeUnixNano(), before)
}

func TestLogsRejectsInvalidExportBeforePublishing(t *testing.T) {
	t.Parallel()

	request := &collectorlogsv1.ExportLogsServiceRequest{
		ResourceLogs: []*logsv1.ResourceLogs{{
			ScopeLogs: []*logsv1.ScopeLogs{{
				LogRecords: []*logsv1.LogRecord{
					{Body: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "valid"}}},
					{Body: &commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "invalid"}}, TraceId: make([]byte, 15)},
				},
			}},
		}},
	}
	body, err := proto.Marshal(request)
	require.NoError(t, err)

	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult()).Maybe()
	service := &Service{
		logger:          testenv.NewLogger(t),
		tracer:          testenv.NewTracerProvider(t).Tracer("test"),
		auth:            nil,
		logPublisher:    publisher,
		metricPublisher: nil,
		spanPublisher:   nil,
	}
	projectID := uuid.MustParse(testLogProjectID)
	ctx := contextvalues.SetAuthContext(t.Context(), testOTELAuthContext(projectID))

	err = service.Logs(ctx, &gen.LogsPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ContentEncoding:  nil,
	}, io.NopCloser(bytes.NewReader(body)))

	require.ErrorContains(t, err, "invalid OTLP log export")
	publisher.AssertNotCalled(t, "Publish", mock.Anything, mock.Anything)
}

func TestLogsRejectsRecordOverMaximumSizeBeforePublishing(t *testing.T) {
	t.Parallel()

	request := &collectorlogsv1.ExportLogsServiceRequest{
		ResourceLogs: []*logsv1.ResourceLogs{{
			ScopeLogs: []*logsv1.ScopeLogs{{
				LogRecords: []*logsv1.LogRecord{{
					Body: &commonv1.AnyValue{
						Value: &commonv1.AnyValue_BytesValue{BytesValue: make([]byte, maxOTLPLogRecordBytes)},
					},
				}},
			}},
		}},
	}
	body, err := proto.Marshal(request)
	require.NoError(t, err)

	publisher := gcp.NewMockPublisher[*otelv1.InboundLogRecord]()
	publisher.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult()).Maybe()
	service := &Service{
		logger:          testenv.NewLogger(t),
		tracer:          testenv.NewTracerProvider(t).Tracer("test"),
		auth:            nil,
		logPublisher:    publisher,
		metricPublisher: nil,
		spanPublisher:   nil,
	}
	projectID := uuid.MustParse(testLogProjectID)
	ctx := contextvalues.SetAuthContext(t.Context(), testOTELAuthContext(projectID))

	err = service.Logs(ctx, &gen.LogsPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ContentEncoding:  nil,
	}, io.NopCloser(bytes.NewReader(body)))

	require.ErrorContains(t, err, "invalid OTLP log export")
	publisher.AssertNotCalled(t, "Publish", mock.Anything, mock.Anything)
}

func TestValidateLogRecordAcceptsRecordBelowMaximumSize(t *testing.T) {
	t.Parallel()

	recordID := "record-id"
	record := (&otelv1.InboundLogRecord_builder{
		RecordId: &recordID,
		Body: (&otelv1.InboundLogRecord_AnyValue_builder{
			BytesValue: make([]byte, maxOTLPLogRecordBytes-1024),
		}).Build(),
	}).Build()

	require.LessOrEqual(t, proto.Size(record), maxOTLPLogRecordBytes)
	require.NoError(t, ValidateInboundLogRecord(record))
}

func testOTELAuthContext(projectID uuid.UUID) *contextvalues.AuthContext {
	return &contextvalues.AuthContext{
		ActiveOrganizationID:  testLogOrganizationID,
		UserID:                "",
		ExternalUserID:        "",
		APIKeyID:              "",
		APIKeyName:            "",
		OrgWidePluginHooksKey: false,
		SessionID:             nil,
		ProjectID:             &projectID,
		OrganizationSlug:      "",
		Email:                 nil,
		AccountType:           "",
		HasActiveSubscription: false,
		Whitelisted:           false,
		ProjectSlug:           nil,
		APIKeyScopes:          nil,
		IsAdmin:               false,
	}
}
