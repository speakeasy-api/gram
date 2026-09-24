package otel

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	otelv1 "github.com/speakeasy-api/gram/infra/gen/gram/otel/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	otelserver "github.com/speakeasy-api/gram/server/gen/http/otel/server"
	gen "github.com/speakeasy-api/gram/server/gen/otel"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	collectortracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestTracesResponseUsesOTLPSuccessStatus(t *testing.T) {
	t.Parallel()

	response := httptest.NewRecorder()
	err := otelserver.EncodeTracesResponse(nil)(t.Context(), response, nil)

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.Code)
}

func TestTracesRejectsInvalidExportBeforePublishing(t *testing.T) {
	t.Parallel()

	validSpan := &tracev1.Span{
		TraceId:           make([]byte, 16),
		SpanId:            make([]byte, 8),
		Name:              "valid",
		StartTimeUnixNano: 1,
		EndTimeUnixNano:   2,
	}
	validSpan.TraceId[0] = 1
	validSpan.SpanId[0] = 2
	invalidSpan, ok := proto.Clone(validSpan).(*tracev1.Span)
	require.True(t, ok)
	invalidSpan.TraceId = make([]byte, 15)
	invalidSpan.Name = "invalid"
	request := &collectortracev1.ExportTraceServiceRequest{
		ResourceSpans: []*tracev1.ResourceSpans{{
			ScopeSpans: []*tracev1.ScopeSpans{{
				Spans: []*tracev1.Span{validSpan, invalidSpan},
			}},
		}},
	}
	body, err := proto.Marshal(request)
	require.NoError(t, err)

	publisher := gcp.NewMockPublisher[*otelv1.InboundSpan]()
	publisher.On("Publish", mock.Anything, mock.Anything).Return(gcp.NewSuccessPublishResult()).Maybe()
	service := &Service{
		logger:          testenv.NewLogger(t),
		tracer:          testenv.NewTracerProvider(t).Tracer("test"),
		auth:            nil,
		logPublisher:    nil,
		metricPublisher: nil,
		spanPublisher:   publisher,
	}
	projectID := uuid.New()
	ctx := contextvalues.SetAuthContext(t.Context(), &contextvalues.AuthContext{
		ActiveOrganizationID:  "organization-id",
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
	})

	err = service.Traces(ctx, &gen.TracesPayload{
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
		ContentEncoding:  nil,
	}, io.NopCloser(bytes.NewReader(body)))

	require.ErrorContains(t, err, "invalid OTLP trace export")
	publisher.AssertNotCalled(t, "Publish", mock.Anything, mock.Anything)
}
