package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	return NewToolProxy(
		testenv.NewLogger(t),
		tracerProvider,
		meterProvider,
		ToolCallSourceMCP,
		testenv.NewEncryptionClient(t),
		nil,
		policy,
		funcs,
		nil,
		nil,
	)
}

func callToolProxy(t *testing.T, ctx context.Context, proxy *ToolProxy, plan *ToolCallPlan, body string) (*httptest.ResponseRecorder, error) {
	t.Helper()

	recorder := httptest.NewRecorder()
	err := proxy.Do(ctx, recorder, strings.NewReader(body), toolconfig.ToolCallEnv{
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
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
