package metering_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	meteringv1 "github.com/speakeasy-api/gram/infra/gen/gram/metering/v1"
	"github.com/speakeasy-api/gram/infra/pkg/gcp"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/metering"
	"github.com/speakeasy-api/gram/server/internal/middleware"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type bandwidthCapturePublisher struct {
	mu                  sync.Mutex
	messages            []*meteringv1.MeterReading
	publishContextError []error
	requireBatchSize    int
}

func (p *bandwidthCapturePublisher) Publish(ctx context.Context, message *meteringv1.MeterReading, _ ...gcp.PublishOption) gcp.PublishResult {
	p.mu.Lock()
	p.messages = append(p.messages, message)
	p.publishContextError = append(p.publishContextError, ctx.Err())
	p.mu.Unlock()
	return bandwidthPublishResult{publisher: p}
}

func (p *bandwidthCapturePublisher) Stop(context.Context) error { return nil }

func (p *bandwidthCapturePublisher) published() ([]*meteringv1.MeterReading, []error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*meteringv1.MeterReading(nil), p.messages...), append([]error(nil), p.publishContextError...)
}

type bandwidthPublishResult struct {
	publisher *bandwidthCapturePublisher
}

func (r bandwidthPublishResult) Ready() <-chan struct{} {
	ready := make(chan struct{})
	close(ready)
	return ready
}

func (r bandwidthPublishResult) Get(context.Context) (string, error) {
	r.publisher.mu.Lock()
	defer r.publisher.mu.Unlock()
	if r.publisher.requireBatchSize > 0 && len(r.publisher.messages) != r.publisher.requireBatchSize {
		return "", errors.New("publisher result awaited before the whole exchange batch was published")
	}
	return "published", nil
}

func TestMCPBandwidthMiddlewareMetersHostedRemoteTunneledMetaAndPlatformBodies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		requestPath string
		serverType  metering.MCPServerType
	}{
		{requestPath: "/mcp/hosted", serverType: metering.MCPServerTypeHosted},
		{requestPath: "/mcp/remote", serverType: metering.MCPServerTypeRemote},
		{requestPath: "/mcp/tunneled", serverType: metering.MCPServerTypeTunneled},
		{requestPath: "/mcp/meta", serverType: metering.MCPServerTypeMeta},
		{requestPath: "/mcp/direct", serverType: metering.MCPServerTypeDirectToolset},
		{requestPath: "/platform/mcp/platform-toolset", serverType: metering.MCPServerTypePlatformToolset},
	}
	for _, tt := range tests {
		requestPath := tt.requestPath
		publisher := &bandwidthCapturePublisher{requireBatchSize: 2}
		projectID := uuid.New()
		var gotBody string
		var readErr, writeErr error
		handler := metering.NewMCPBandwidthMiddleware(testenv.NewLogger(t), publisher)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			metering.AttributeMCPBandwidth(r.Context(), "org-test", projectID)
			metering.AttributeMCPBandwidthServer(r.Context(), tt.serverType, "server-id", "server-slug")
			body, err := io.ReadAll(r.Body)
			readErr = err
			gotBody = string(body)
			_, writeErr = io.WriteString(w, "response-body")
		}))

		request := httptest.NewRequest(http.MethodPost, requestPath+"?query=omitted", strings.NewReader("request-body"))
		request = request.WithContext(customdomains.WithContext(request.Context(), &customdomains.Context{
			OrganizationID: "org-test",
			Domain:         "mcp.example.test",
			DomainID:       uuid.New(),
		}))
		request.URL.Fragment = "omitted"
		handler.ServeHTTP(httptest.NewRecorder(), request)
		require.NoError(t, readErr, requestPath)
		require.Equal(t, "request-body", gotBody, requestPath)
		require.NoError(t, writeErr, requestPath)
		messages, contextErrors := publisher.published()
		require.Len(t, messages, 2, requestPath)
		require.Equal(t, string(metering.MeterMCPBandwidthIngress), messages[0].GetMeterId(), requestPath)
		require.Equal(t, int64(len("request-body")), messages[0].GetValue(), requestPath)
		require.Equal(t, string(metering.MeterMCPBandwidthEgress), messages[1].GetMeterId(), requestPath)
		require.Equal(t, int64(len("response-body")), messages[1].GetValue(), requestPath)
		require.Equal(t, messages[0].GetOperationId(), messages[1].GetOperationId(), requestPath)
		require.NotEmpty(t, messages[0].GetOperationId(), requestPath)
		_, err := uuid.Parse(messages[0].GetOperationId())
		require.NoError(t, err, requestPath)
		require.Equal(t, messages[0].GetOccurredAt(), messages[1].GetOccurredAt(), requestPath)
		require.Equal(t, messages[0].GetProducedAt(), messages[1].GetProducedAt(), requestPath)
		require.Equal(t, messages[0].GetOccurredAt(), messages[0].GetProducedAt(), requestPath)
		require.Equal(t, "mcp_bandwidth_middleware", messages[0].GetSource(), requestPath)
		require.Equal(t, "org-test", messages[0].GetOrganizationId(), requestPath)
		require.Equal(t, projectID.String(), messages[0].GetProjectId(), requestPath)
		require.Equal(t, string(metering.UnitBytes), messages[0].GetUnit(), requestPath)
		require.Equal(t, string(metering.MeasurementHTTPBodyBytes), messages[0].GetMeasurementMethod(), requestPath)
		require.Equal(t, requestPath, messages[0].GetAttributes()[metering.AttributeRequestPath], requestPath)
		require.Equal(t, requestPath, messages[1].GetAttributes()[metering.AttributeRequestPath], requestPath)
		require.Equal(t, string(tt.serverType), messages[0].GetAttributes()[metering.AttributeMCPServerType], requestPath)
		require.Equal(t, "server-id", messages[0].GetAttributes()[metering.AttributeMCPServerID], requestPath)
		require.Equal(t, string(tt.serverType), messages[1].GetAttributes()[metering.AttributeMCPServerType], requestPath)
		require.Equal(t, "server-id", messages[1].GetAttributes()[metering.AttributeMCPServerID], requestPath)
		require.Equal(t, "server-slug", messages[0].GetAttributes()[metering.AttributeMCPServerSlug], requestPath)
		require.Equal(t, "server-slug", messages[1].GetAttributes()[metering.AttributeMCPServerSlug], requestPath)
		require.Equal(t, "mcp.example.test", messages[0].GetAttributes()[metering.AttributeCustomDomain], requestPath)
		require.Equal(t, "mcp.example.test", messages[1].GetAttributes()[metering.AttributeCustomDomain], requestPath)
		require.Equal(t, []error{nil, nil}, contextErrors, requestPath)
	}
}

func TestMCPBandwidthMiddlewareCountsGeneratedErrorsAndPartialWrites(t *testing.T) {
	t.Parallel()

	publisher := &bandwidthCapturePublisher{}
	partial := &partialResponseWriter{header: make(http.Header), limit: 7}
	handler := metering.NewMCPBandwidthMiddleware(testenv.NewLogger(t), publisher)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metering.AttributeMCPBandwidth(r.Context(), "org-test", uuid.New())
		_, _ = io.ReadAll(r.Body)
		http.Error(w, "generated error", http.StatusBadGateway)
	}))

	handler.ServeHTTP(partial, httptest.NewRequest(http.MethodPost, "/mcp/generated-error", strings.NewReader("read-me")))
	messages, _ := publisher.published()
	require.Len(t, messages, 2)
	require.Equal(t, int64(len("read-me")), messages[0].GetValue())
	require.Equal(t, int64(7), messages[1].GetValue())
}

func TestMCPBandwidthMiddlewareSkipsPanicsRecoveredOutsideMeter(t *testing.T) {
	t.Parallel()

	publisher := &bandwidthCapturePublisher{}
	handler := middleware.NewRecovery(testenv.NewLogger(t))(
		metering.NewMCPBandwidthMiddleware(testenv.NewLogger(t), publisher)(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				metering.AttributeMCPBandwidth(r.Context(), "org-test", uuid.New())
				_, _ = io.ReadAll(r.Body)
				_, _ = io.WriteString(w, "partial response")
				panic("boom")
			}),
		),
	)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp/panic", strings.NewReader("request")))
	messages, _ := publisher.published()
	require.Empty(t, messages)
}

func TestMCPBandwidthMiddlewarePublishesStreamBytesOnlyAfterClose(t *testing.T) {
	t.Parallel()

	publisher := &bandwidthCapturePublisher{}
	inner := &flushResponseRecorder{ResponseRecorder: httptest.NewRecorder()}
	var firstWriteErr, secondWriteErr error
	var flusherOK, unwrapperOK bool
	var unwrapped http.ResponseWriter
	var messagesDuringHandler int
	handler := metering.NewMCPBandwidthMiddleware(testenv.NewLogger(t), publisher)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metering.AttributeMCPBandwidth(r.Context(), "org-test", uuid.New())
		w.Header().Set("Content-Type", "text/event-stream")
		_, firstWriteErr = io.WriteString(w, "data: one\n\n")
		flusher, ok := w.(http.Flusher)
		flusherOK = ok
		if flusherOK {
			flusher.Flush()
		}
		messages, _ := publisher.published()
		messagesDuringHandler = len(messages)
		_, secondWriteErr = io.WriteString(w, "data: two\n\n")
		unwrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		unwrapperOK = ok
		if unwrapperOK {
			unwrapped = unwrapper.Unwrap()
		}
	}))

	handler.ServeHTTP(inner, httptest.NewRequest(http.MethodGet, "/mcp/stream", nil))
	require.NoError(t, firstWriteErr)
	require.NoError(t, secondWriteErr)
	require.True(t, flusherOK)
	require.True(t, unwrapperOK)
	require.Equal(t, http.ResponseWriter(inner), unwrapped)
	require.Zero(t, messagesDuringHandler)
	messages, _ := publisher.published()
	require.Len(t, messages, 1)
	require.Equal(t, string(metering.MeterMCPBandwidthEgress), messages[0].GetMeterId())
	require.Equal(t, int64(len("data: one\n\ndata: two\n\n")), messages[0].GetValue())
	require.True(t, inner.flushed)
}

func TestMCPBandwidthMiddlewareDetachesPublicationFromRequestCancellation(t *testing.T) {
	t.Parallel()

	publisher := &bandwidthCapturePublisher{requireBatchSize: 2}
	ctx, cancel := context.WithCancel(t.Context())
	handler := metering.NewMCPBandwidthMiddleware(testenv.NewLogger(t), publisher)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metering.AttributeMCPBandwidth(r.Context(), "org-test", uuid.New())
		_, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, "response")
		cancel()
	}))

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp/cancelled", strings.NewReader("request")).WithContext(ctx))
	messages, contextErrors := publisher.published()
	require.Len(t, messages, 2)
	require.Equal(t, []error{nil, nil}, contextErrors)
}

func TestMCPBandwidthMiddlewareSkipsHTMLInstallAndNonMCPExchanges(t *testing.T) {
	t.Parallel()

	paths := []string{
		"/rpc/projects.list",
		"/mcp/server/install",
		"/platform-mcp",
		"/platform/mcp",
	}
	for _, requestPath := range paths {
		publisher := &bandwidthCapturePublisher{}
		handler := metering.NewMCPBandwidthMiddleware(testenv.NewLogger(t), publisher)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			metering.AttributeMCPBandwidth(r.Context(), "org-test", uuid.New())
			_, _ = io.ReadAll(r.Body)
			_, _ = io.WriteString(w, "response")
		}))
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, requestPath, strings.NewReader("request")))
		messages, _ := publisher.published()
		require.Empty(t, messages, requestPath)
	}

	publisher := &bandwidthCapturePublisher{}
	htmlInstall := metering.NewMCPBandwidthMiddleware(testenv.NewLogger(t), publisher)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metering.AttributeMCPBandwidth(r.Context(), "org-test", uuid.New())
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<html>install</html>")
	}))
	htmlInstall.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/mcp/hosted", nil))
	messages, _ := publisher.published()
	require.Empty(t, messages)
}

func TestMCPBandwidthMiddlewareSkipsUnknownTenantsAndZeroByteDirections(t *testing.T) {
	t.Parallel()

	unknownPublisher := &bandwidthCapturePublisher{}
	unknownTenant := metering.NewMCPBandwidthMiddleware(testenv.NewLogger(t), unknownPublisher)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		_, _ = io.WriteString(w, "not found")
	}))
	unknownTenant.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp/unknown", strings.NewReader("request")))
	unknownMessages, _ := unknownPublisher.published()
	require.Empty(t, unknownMessages)

	zeroPublisher := &bandwidthCapturePublisher{}
	zero := metering.NewMCPBandwidthMiddleware(testenv.NewLogger(t), zeroPublisher)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		metering.AttributeMCPBandwidth(r.Context(), "org-test", uuid.New())
	}))
	zero.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/mcp/zero", nil))
	zeroMessages, _ := zeroPublisher.published()
	require.Empty(t, zeroMessages)

	egressOnlyPublisher := &bandwidthCapturePublisher{}
	egressOnly := metering.NewMCPBandwidthMiddleware(testenv.NewLogger(t), egressOnlyPublisher)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metering.AttributeMCPBandwidth(r.Context(), "org-test", uuid.New())
		_, _ = io.WriteString(w, "response")
	}))
	egressOnly.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp/egress-only", nil))
	egressOnlyMessages, _ := egressOnlyPublisher.published()
	require.Len(t, egressOnlyMessages, 1)
	require.Equal(t, string(metering.MeterMCPBandwidthEgress), egressOnlyMessages[0].GetMeterId())

	ingressOnlyPublisher := &bandwidthCapturePublisher{}
	ingressOnly := metering.NewMCPBandwidthMiddleware(testenv.NewLogger(t), ingressOnlyPublisher)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		metering.AttributeMCPBandwidth(r.Context(), "org-test", uuid.New())
		_, _ = io.ReadAll(r.Body)
	}))
	ingressOnly.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp/ingress-only", strings.NewReader("request")))
	ingressOnlyMessages, _ := ingressOnlyPublisher.published()
	require.Len(t, ingressOnlyMessages, 1)
	require.Equal(t, string(metering.MeterMCPBandwidthIngress), ingressOnlyMessages[0].GetMeterId())
}

type partialResponseWriter struct {
	header http.Header
	limit  int
}

func (w *partialResponseWriter) Header() http.Header { return w.header }
func (w *partialResponseWriter) WriteHeader(int)     {}
func (w *partialResponseWriter) Write(p []byte) (int, error) {
	return min(w.limit, len(p)), io.ErrShortWrite
}

type flushResponseRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (w *flushResponseRecorder) Flush() {
	w.flushed = true
	w.ResponseRecorder.Flush()
}
