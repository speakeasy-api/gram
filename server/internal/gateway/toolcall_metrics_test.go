package gateway

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	externalmcptypes "github.com/speakeasy-api/gram/server/internal/externalmcp/repo/types"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testmcp"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// toolCallSeries collects the tool.call data points recorded so far, keyed by
// their attribute set. Every executor records exactly one point per call, so a
// test asserting on a single call reads the only entry.
func toolCallSeries(t *testing.T, reader sdkmetric.Reader) map[attribute.Set]int64 {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	points := map[attribute.Set]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "tool.call" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "tool.call must be an int64 counter")
			for _, dp := range sum.DataPoints {
				points[dp.Attributes] = dp.Value
			}
		}
	}

	return points
}

// onlyToolCallAttributes returns the attribute set of the single tool.call
// series recorded, failing when the call recorded no point or more than one.
func onlyToolCallAttributes(t *testing.T, reader sdkmetric.Reader) attribute.Set {
	t.Helper()

	points := toolCallSeries(t, reader)
	require.Len(t, points, 1, "a tool call must record exactly one tool.call series")

	for set, value := range points {
		require.Equal(t, int64(1), value)
		return set
	}

	return *attribute.EmptySet()
}

func attributeValue(t *testing.T, set attribute.Set, key attribute.Key) string {
	t.Helper()

	value, ok := set.Value(key)
	require.True(t, ok, "attribute %s must be present", key)

	return value.Emit()
}

func newMetricToolProxy(t *testing.T, reader sdkmetric.Reader) *ToolProxy {
	t.Helper()

	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	return NewToolProxy(
		testenv.NewLogger(t),
		tracerProvider,
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
		ToolCallSourceMCP,
		testenv.NewEncryptionClient(t),
		nil,
		policy,
		funcs,
		nil,
	)
}

func newExternalMCPToolDescriptor() *ToolDescriptor {
	return &ToolDescriptor{
		ID:               uuid.New().String(),
		Name:             "notion",
		Description:      nil,
		DeploymentID:     uuid.New().String(),
		ProjectID:        uuid.New().String(),
		ProjectSlug:      "test-project",
		OrganizationID:   uuid.New().String(),
		OrganizationSlug: "test-org",
		// Proxy tools name the MCP server, not the tool that runs.
		URN: urn.NewTool(urn.ToolKindExternalMCP, "notion", "proxy"),
	}
}

func newExternalMCPTestServer(t *testing.T, isError bool) *httptest.Server {
	t.Helper()

	server := testmcp.NewStreamableHTTPServer(t, &testmcp.Server{
		Tools: []testmcp.Tool{{
			Annotations:  nil,
			Description:  "Search a workspace",
			Icons:        nil,
			InputSchema:  map[string]any{"type": "object"},
			Meta:         nil,
			Name:         "search",
			OutputSchema: nil,
			Response: testmcp.ToolResponse{
				Content: []map[string]any{{"type": "text", "text": "result"}},
				IsError: isError,
			},
			Title: "",
		}},
	})
	t.Cleanup(server.Close)

	return server
}

func externalMCPPlan(remoteURL string) *ExternalMCPToolCallPlan {
	return &ExternalMCPToolCallPlan{
		RemoteURL:         remoteURL,
		ToolName:          "search",
		Slug:              "notion",
		RequiresOAuth:     false,
		TransportType:     externalmcptypes.TransportTypeStreamableHTTP,
		HeaderDefinitions: nil,
	}
}

func callToolProxy(t *testing.T, ctx context.Context, proxy *ToolProxy, plan *ToolCallPlan, body string) (*httptest.ResponseRecorder, error) {
	t.Helper()

	recorder := httptest.NewRecorder()
	err := proxy.Do(ctx, recorder, bytes.NewReader([]byte(body)), toolconfig.ToolCallEnv{
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
		GramChatID: "",
		MCPClient:  toolconfig.MCPClientIdentity{Name: "", Version: "", OAuthClientID: ""},
	}, plan, tm.HTTPLogAttributes{})

	return recorder, err
}

// A status of zero means no response was produced at all, which is a different
// operational signal from a tool that ran and returned a failing status.
func TestToolCallOutcomeForStatus(t *testing.T) {
	t.Parallel()

	require.Equal(t, toolCallOutcomeNoResponse, toolCallOutcomeForStatus(0))
	require.Equal(t, toolCallOutcomeSuccess, toolCallOutcomeForStatus(http.StatusOK))
	require.Equal(t, toolCallOutcomeSuccess, toolCallOutcomeForStatus(http.StatusFound))
	require.Equal(t, toolCallOutcomeToolError, toolCallOutcomeForStatus(http.StatusBadRequest))
	require.Equal(t, toolCallOutcomeToolError, toolCallOutcomeForStatus(http.StatusInternalServerError))
}

// External MCP executions were absent from tool.call entirely, so the counter
// undercounted every billable tool call served through an external MCP server.
func TestToolProxy_Do_ExternalMCP_RecordsToolCall(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	proxy := newMetricToolProxy(t, reader)
	server := newExternalMCPTestServer(t, false)
	descriptor := newExternalMCPToolDescriptor()

	recorder, err := callToolProxy(t, t.Context(), proxy, NewExternalMCPToolCallPlan(descriptor, externalMCPPlan(server.URL)), `{}`)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)

	set := onlyToolCallAttributes(t, reader)
	require.Equal(t, string(urn.ToolKindExternalMCP), attributeValue(t, set, attr.ToolCallKindKey))
	require.Equal(t, string(toolCallOutcomeSuccess), attributeValue(t, set, attr.OutcomeKey))
	require.Equal(t, "200", attributeValue(t, set, attribute.Key("http.response.status_code")))
	require.Equal(t, descriptor.OrganizationID, attributeValue(t, set, attr.OrganizationIDKey))
}

// The upstream tool name is resolved only when the call is dispatched, so
// reading it off the proxy tool's URN would label every external MCP series
// "proxy" and leave dashboards unable to name the tool that ran.
func TestToolProxy_Do_ExternalMCP_RecordsUpstreamToolName(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	proxy := newMetricToolProxy(t, reader)
	server := newExternalMCPTestServer(t, false)
	descriptor := newExternalMCPToolDescriptor()
	require.Equal(t, "proxy", descriptor.URN.Name, "the descriptor must carry the placeholder name this test is about")

	_, err := callToolProxy(t, t.Context(), proxy, NewExternalMCPToolCallPlan(descriptor, externalMCPPlan(server.URL)), `{}`)
	require.NoError(t, err)

	set := onlyToolCallAttributes(t, reader)
	require.Equal(t, "search", attributeValue(t, set, attr.ToolNameKey))
}

// Some upstreams answer an unknown tool name with an errored result rather
// than a protocol error, which would put the caller's unvalidated string on the
// metric dimension. Only a name the upstream answered successfully is recorded,
// so an errored result is attributed to the URN name whatever it was called.
func TestToolProxy_Do_ExternalMCP_DoesNotRecordToolNameOnErroredResult(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	proxy := newMetricToolProxy(t, reader)
	server := newExternalMCPTestServer(t, true)
	descriptor := newExternalMCPToolDescriptor()

	_, err := callToolProxy(t, t.Context(), proxy, NewExternalMCPToolCallPlan(descriptor, externalMCPPlan(server.URL)), `{}`)
	require.NoError(t, err)

	set := onlyToolCallAttributes(t, reader)
	require.Equal(t, descriptor.URN.Name, attributeValue(t, set, attr.ToolNameKey))
	require.Equal(t, string(toolCallOutcomeToolError), attributeValue(t, set, attr.OutcomeKey))
}

// A proxy tool's name is taken from the caller's request without being checked
// against the upstream's tool list, so a call that never reached a tool must
// not put that string on a metric dimension.
func TestToolProxy_Do_ExternalMCP_DoesNotRecordUnreachedToolName(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	proxy := newMetricToolProxy(t, reader)
	server := newExternalMCPTestServer(t, false)
	descriptor := newExternalMCPToolDescriptor()

	plan := externalMCPPlan(server.URL)
	plan.ToolName = "no-such-tool-" + uuid.NewString()

	_, err := callToolProxy(t, t.Context(), proxy, NewExternalMCPToolCallPlan(descriptor, plan), `{}`)
	require.Error(t, err, "the upstream must reject a tool it does not expose")

	set := onlyToolCallAttributes(t, reader)
	require.Equal(t, descriptor.URN.Name, attributeValue(t, set, attr.ToolNameKey))
}

// An upstream reporting failure in-band still answers with HTTP 200, so the
// status code records it as a success. The outcome dimension is the only place
// this failure class is visible.
func TestToolProxy_Do_ExternalMCP_RecordsUpstreamErrorResultAsToolError(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	proxy := newMetricToolProxy(t, reader)
	server := newExternalMCPTestServer(t, true)

	recorder, err := callToolProxy(t, t.Context(), proxy, NewExternalMCPToolCallPlan(newExternalMCPToolDescriptor(), externalMCPPlan(server.URL)), `{}`)
	require.NoError(t, err, "an errored tool result is forwarded to the caller, not raised as a gateway error")
	require.Equal(t, http.StatusOK, recorder.Code, "the response written to the caller must be unchanged")

	set := onlyToolCallAttributes(t, reader)
	require.Equal(t, string(toolCallOutcomeToolError), attributeValue(t, set, attr.OutcomeKey))
	require.Equal(t, "200", attributeValue(t, set, attribute.Key("http.response.status_code")))
}

// A call that never reaches a working upstream writes no response at all, and
// is counted apart from a tool that ran and failed. The upstream here answers
// the MCP handshake with a 404, the shape a misconfigured remote URL takes.
func TestToolProxy_Do_ExternalMCP_RecordsNoResponseWhenUpstreamRejectsHandshake(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	proxy := newMetricToolProxy(t, reader)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(upstream.Close)

	_, err := callToolProxy(t, t.Context(), proxy, NewExternalMCPToolCallPlan(newExternalMCPToolDescriptor(), externalMCPPlan(upstream.URL)), `{}`)
	require.Error(t, err)

	set := onlyToolCallAttributes(t, reader)
	require.Equal(t, string(toolCallOutcomeNoResponse), attributeValue(t, set, attr.OutcomeKey))
	require.Equal(t, "0", attributeValue(t, set, attribute.Key("http.response.status_code")))
}

// The three executors that already recorded tool.call were rewritten onto the
// record struct, so their emitted labels are pinned here: the tool name comes
// from the URN, not from the descriptor's own name.
func TestToolProxy_Do_HTTP_RecordsUnchangedToolCallLabels(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	proxy := newMetricToolProxy(t, reader)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(upstream.Close)

	descriptor := newTestToolDescriptor()
	plan := NewHTTPToolCallPlan(descriptor, &HTTPToolCallPlan{
		ServerEnvVar:       "TEST_SERVER_URL",
		DefaultServerUrl:   NullString{Value: upstream.URL, Valid: true},
		Security:           []*HTTPToolSecurity{},
		SecurityScopes:     map[string][]string{},
		Method:             http.MethodGet,
		Path:               "/things",
		Schema:             []byte{},
		HeaderParams:       map[string]*HTTPParameter{},
		QueryParams:        map[string]*HTTPParameter{},
		PathParams:         map[string]*HTTPParameter{},
		RequestContentType: NullString{Value: "application/json", Valid: true},
		ResponseFilter:     nil,
	})

	_, err := callToolProxy(t, t.Context(), proxy, plan, `{}`)
	require.NoError(t, err)

	set := onlyToolCallAttributes(t, reader)
	require.Equal(t, string(urn.ToolKindHTTP), attributeValue(t, set, attr.ToolCallKindKey))
	require.Equal(t, descriptor.URN.Name, attributeValue(t, set, attr.ToolNameKey))
	require.Equal(t, string(toolCallOutcomeSuccess), attributeValue(t, set, attr.OutcomeKey))
	require.Equal(t, "200", attributeValue(t, set, attribute.Key("http.response.status_code")))
}

func newPromptToolCallPlanForTest(engine string) *ToolCallPlan {
	descriptor := &ToolDescriptor{
		ID:               uuid.New().String(),
		Name:             "summarize",
		Description:      nil,
		DeploymentID:     "",
		ProjectID:        uuid.New().String(),
		ProjectSlug:      "test-project",
		OrganizationID:   uuid.New().String(),
		OrganizationSlug: "test-org",
		URN:              urn.NewTool(urn.ToolKindPrompt, "doc", "summarize"),
	}

	return NewPromptToolCallPlan(descriptor, &PromptToolCallPlan{
		TemplateID: uuid.New().String(),
		Prompt:     "Summarize {{topic}}",
		Engine:     engine,
		Kind:       "prompt",
	})
}

// Prompt executions were the second tool kind missing from tool.call, which is
// why their volume could not be measured at all.
func TestToolProxy_Do_Prompt_RecordsToolCall(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	proxy := newMetricToolProxy(t, reader)

	recorder, err := callToolProxy(t, t.Context(), proxy, newPromptToolCallPlanForTest("mustache"), `{"arguments":{"topic":"gram"}}`)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "Summarize gram", recorder.Body.String())

	set := onlyToolCallAttributes(t, reader)
	require.Equal(t, string(urn.ToolKindPrompt), attributeValue(t, set, attr.ToolCallKindKey))
	require.Equal(t, "summarize", attributeValue(t, set, attr.ToolNameKey))
	require.Equal(t, string(toolCallOutcomeSuccess), attributeValue(t, set, attr.OutcomeKey))
	require.Equal(t, "200", attributeValue(t, set, attribute.Key("http.response.status_code")))
}

// The prompt executor rejects an unparseable request before rendering, which is
// the other path the caller sees as a bad request.
func TestToolProxy_Do_Prompt_RecordsUnparseableRequestAsBadRequest(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	proxy := newMetricToolProxy(t, reader)

	_, err := callToolProxy(t, t.Context(), proxy, newPromptToolCallPlanForTest("mustache"), `not json`)
	require.Error(t, err)

	set := onlyToolCallAttributes(t, reader)
	require.Equal(t, string(toolCallOutcomeToolError), attributeValue(t, set, attr.OutcomeKey))
	require.Equal(t, "400", attributeValue(t, set, attribute.Key("http.response.status_code")))
}

// A prompt that fails to render answers the caller with a bad request, so the
// metric records that status rather than the no-response sentinel.
func TestToolProxy_Do_Prompt_RecordsRenderFailureAsBadRequest(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	proxy := newMetricToolProxy(t, reader)

	_, err := callToolProxy(t, t.Context(), proxy, newPromptToolCallPlanForTest("unsupported-engine"), `{"arguments":{}}`)
	require.Error(t, err)

	set := onlyToolCallAttributes(t, reader)
	require.Equal(t, string(toolCallOutcomeToolError), attributeValue(t, set, attr.OutcomeKey))
	require.Equal(t, "400", attributeValue(t, set, attribute.Key("http.response.status_code")))
}
