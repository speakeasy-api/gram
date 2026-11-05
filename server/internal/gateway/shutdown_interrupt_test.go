package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/shutdowndrain"
	tm "github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// shutdownTestTimeout bounds every wait in these tests. An interrupted call
// returns in milliseconds, so reaching it means the interrupt never landed.
const shutdownTestTimeout = 5 * time.Second

// heldToolServer is an HTTP tool's server that answers with prefix (when
// non-empty) and then holds the response open until the gateway goes away.
// reached closes once the server has written everything it will write, and
// gone closes once it sees the gateway's request cancelled.
type heldToolServer struct {
	server  *httptest.Server
	reached chan struct{}
	gone    chan struct{}
}

func newHeldToolServer(t *testing.T, contentType string, prefix string) *heldToolServer {
	t.Helper()

	s := &heldToolServer{server: nil, reached: make(chan struct{}), gone: make(chan struct{})}
	release := make(chan struct{})
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if prefix != "" {
			w.Header().Set("Content-Type", contentType)
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, prefix)
			_ = http.NewResponseController(w).Flush()
		}
		close(s.reached)
		select {
		case <-r.Context().Done():
			close(s.gone)
		case <-release:
		}
	}))
	t.Cleanup(s.server.Close)
	// Runs before server.Close (LIFO) so a handler still holding its response
	// does not block shutdown.
	t.Cleanup(func() { close(release) })

	return s
}

func newShutdownTestToolProxy(t *testing.T, drain *shutdowndrain.Drain, platformTools PlatformExecutor) *ToolProxy {
	t.Helper()

	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)

	return NewToolProxy(
		testenv.NewLogger(t),
		tracerProvider,
		testenv.NewMeterProvider(t),
		ToolCallSourceMCP,
		testenv.NewEncryptionClient(t),
		nil,
		policy,
		funcs,
		platformTools,
		drain,
	)
}

func newShutdownTestHTTPPlan(serverURL string) *ToolCallPlan {
	return NewHTTPToolCallPlan(newTestToolDescriptor(), &HTTPToolCallPlan{
		ServerEnvVar:       "TEST_SERVER_URL",
		DefaultServerUrl:   NullString{Value: serverURL, Valid: true},
		Security:           []*HTTPToolSecurity{},
		SecurityScopes:     map[string][]string{},
		Method:             http.MethodGet,
		Path:               "/slow",
		Schema:             []byte{},
		HeaderParams:       map[string]*HTTPParameter{},
		QueryParams:        map[string]*HTTPParameter{},
		PathParams:         map[string]*HTTPParameter{},
		RequestContentType: NullString{Value: "application/json", Valid: true},
		ResponseFilter:     nil,
	})
}

func emptyToolCallBody(t *testing.T) []byte {
	t.Helper()

	body, err := json.Marshal(ToolCallBody{
		PathParameters:       nil,
		QueryParameters:      nil,
		HeaderParameters:     nil,
		Body:                 nil,
		ResponseFilter:       nil,
		EnvironmentVariables: nil,
		GramRequestSummary:   "",
	})
	require.NoError(t, err)

	return body
}

func emptyToolCallEnv() toolconfig.ToolCallEnv {
	return toolconfig.ToolCallEnv{
		SystemEnv:  toolconfig.NewCaseInsensitiveEnv(),
		UserConfig: toolconfig.NewCaseInsensitiveEnv(),
		OAuthToken: "",
		GramEmail:  "",
		GramChatID: "",
		MCPClient:  toolconfig.MCPClientIdentity{Name: "", Version: "", OAuthClientID: ""},
	}
}

// runToolCall runs plan through proxy in a goroutine and returns a channel
// that delivers Do's error once it returns.
func runToolCall(t *testing.T, proxy *ToolProxy, plan *ToolCallPlan) <-chan error {
	t.Helper()

	body := emptyToolCallBody(t)
	done := make(chan error, 1)
	go func() {
		done <- proxy.Do(t.Context(), httptest.NewRecorder(), bytes.NewReader(body), emptyToolCallEnv(), plan, tm.HTTPLogAttributes{})
	}()

	return done
}

func waitForSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(shutdownTestTimeout):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func waitForToolCall(t *testing.T, done <-chan error) error {
	t.Helper()

	select {
	case err := <-done:
		return err
	case <-time.After(shutdownTestTimeout):
		t.Fatal("timed out waiting for the tool call to return")
		return nil
	}
}

func TestToolProxy_Do_ShutdownInterruptsHTTPToolAwaitingResponse(t *testing.T) {
	t.Parallel()

	upstream := newHeldToolServer(t, "", "")
	drain := shutdowndrain.New()
	proxy := newShutdownTestToolProxy(t, drain, nil)

	done := runToolCall(t, proxy, newShutdownTestHTTPPlan(upstream.server.URL))
	waitForSignal(t, upstream.reached, "the tool's server to receive the call")

	drain.Start(0)

	err := waitForToolCall(t, done)
	require.ErrorIs(t, err, shutdowndrain.ErrInterrupted)
	waitForSignal(t, upstream.gone, "the tool's server to see the call abandoned")
}

func TestToolProxy_Do_ShutdownInterruptsHTTPToolMidBody(t *testing.T) {
	t.Parallel()

	// The body copy swallows read errors, so without the interrupt check a
	// truncated body would come back as a successful result.
	upstream := newHeldToolServer(t, "application/json", `{"rows":[`)
	drain := shutdowndrain.New()
	proxy := newShutdownTestToolProxy(t, drain, nil)

	done := runToolCall(t, proxy, newShutdownTestHTTPPlan(upstream.server.URL))
	waitForSignal(t, upstream.reached, "the tool's server to start its response")

	drain.Start(0)

	err := waitForToolCall(t, done)
	require.ErrorIs(t, err, shutdowndrain.ErrInterrupted)
}

func TestToolProxy_Do_ShutdownInterruptsHTTPToolMidStream(t *testing.T) {
	t.Parallel()

	upstream := newHeldToolServer(t, "text/event-stream", "data: {\"partial\":true}\n\n")
	drain := shutdowndrain.New()
	proxy := newShutdownTestToolProxy(t, drain, nil)

	done := runToolCall(t, proxy, newShutdownTestHTTPPlan(upstream.server.URL))
	waitForSignal(t, upstream.reached, "the tool's server to start its stream")

	drain.Start(0)

	err := waitForToolCall(t, done)
	require.ErrorIs(t, err, shutdowndrain.ErrInterrupted)
}

func TestToolProxy_Do_ShutdownInterruptsFilteredHTTPToolMidBody(t *testing.T) {
	t.Parallel()

	// The response filter reads the whole body before anything is copied, so
	// without its own check a truncated body would come back as the filter's
	// synthetic 500 rather than as an interrupt.
	upstream := newHeldToolServer(t, "application/json", `{"rows":[`)
	drain := shutdowndrain.New()
	proxy := newShutdownTestToolProxy(t, drain, nil)

	plan := newShutdownTestHTTPPlan(upstream.server.URL)
	plan.HTTP.ResponseFilter = &ResponseFilter{
		Type:         FilterTypeJQ,
		Schema:       nil,
		StatusCodes:  []string{"200"},
		ContentTypes: []string{"application/json"},
	}
	body, err := json.Marshal(ToolCallBody{
		PathParameters:       nil,
		QueryParameters:      nil,
		HeaderParameters:     nil,
		Body:                 nil,
		ResponseFilter:       &FilterRequest{Type: string(FilterTypeJQ), Filter: ".rows"},
		EnvironmentVariables: nil,
		GramRequestSummary:   "",
	})
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	done := make(chan error, 1)
	go func() {
		done <- proxy.Do(t.Context(), recorder, bytes.NewReader(body), emptyToolCallEnv(), plan, tm.HTTPLogAttributes{})
	}()
	waitForSignal(t, upstream.reached, "the tool's server to start its response")

	drain.Start(0)

	require.ErrorIs(t, waitForToolCall(t, done), shutdowndrain.ErrInterrupted)
	require.Empty(t, recorder.Header().Get(constants.HeaderFilteredResponse), "an interrupted call writes no filtered response")
}

func TestHandleResponseFiltering_ShutdownInterruptStopsTheFilter(t *testing.T) {
	t.Parallel()

	drain := shutdowndrain.New()
	ctx, release := drain.Interruptible(t.Context())
	defer release()
	drain.Start(0)
	waitForSignal(t, ctx.Done(), "the interrupt")

	// The body is already in memory, so the read succeeds and only the jq run
	// sees the cancelled context.
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader([]byte(`{"rows":[1,2,3]}`))),
	}
	result := handleResponseFiltering(ctx, testenv.NewLogger(t), &ResponseFilter{
		Type:         FilterTypeJQ,
		Schema:       nil,
		StatusCodes:  []string{"200"},
		ContentTypes: []string{"application/json"},
	}, &FilterRequest{Type: string(FilterTypeJQ), Filter: "[range(1000000)] | length"}, resp)

	require.NotNil(t, result)
	require.ErrorIs(t, result.interruptErr, context.Canceled, "a filter the drain stopped is an interrupt, not a filter mismatch")
	require.NotEqual(t, http.StatusBadRequest, result.statusCode)
}

func TestToolProxy_ReadResource_ShutdownInterruptsFunctionAwaitingResponse(t *testing.T) {
	t.Parallel()

	upstream := newHeldToolServer(t, "", "")
	drain := shutdowndrain.New()

	tracerProvider := testenv.NewTracerProvider(t)
	policy, err := guardian.NewUnsafePolicy(tracerProvider, []string{})
	require.NoError(t, err)
	proxy := NewToolProxy(
		testenv.NewLogger(t),
		tracerProvider,
		testenv.NewMeterProvider(t),
		ToolCallSourceMCP,
		testenv.NewEncryptionClient(t),
		nil,
		policy,
		&mockToolCaller{serverURL: upstream.server.URL, onCall: nil, onRequest: nil},
		nil,
		drain,
	)

	plan := NewResourceFunctionCallPlan(&ResourceDescriptor{
		ID:               uuid.New().String(),
		Name:             "test_resource",
		DeploymentID:     uuid.New().String(),
		ProjectID:        uuid.New().String(),
		ProjectSlug:      "test-project",
		OrganizationID:   uuid.New().String(),
		OrganizationSlug: "test-org",
		URN:              urn.NewResource(urn.ResourceKindFunction, "functions", "resource://test"),
		URI:              "resource://test",
	}, &ResourceFunctionCallPlan{
		FunctionID:        uuid.New().String(),
		FunctionsAccessID: uuid.New().String(),
		Runtime:           "nodejs",
		URI:               "resource://test",
		MimeType:          "text/plain",
		Variables:         []string{},
	})

	done := make(chan error, 1)
	go func() {
		done <- proxy.ReadResource(t.Context(), httptest.NewRecorder(), bytes.NewReader([]byte("{}")), emptyToolCallEnv(), plan, tm.HTTPLogAttributes{})
	}()
	waitForSignal(t, upstream.reached, "the function runner to receive the read")

	drain.Start(0)

	require.ErrorIs(t, waitForToolCall(t, done), shutdowndrain.ErrInterrupted)
	waitForSignal(t, upstream.gone, "the function runner to see the read abandoned")
}

func TestShutdownInterruptedError_KeepsAnErrorTheInterruptDidNotCause(t *testing.T) {
	t.Parallel()

	drain := shutdowndrain.New()
	ctx, release := drain.Interruptible(t.Context())
	defer release()

	drain.Start(0)
	select {
	case <-ctx.Done():
	case <-time.After(shutdownTestTimeout):
		t.Fatal("timed out waiting for the interrupt")
	}
	require.True(t, shutdowndrain.Interrupted(ctx))

	// A call that failed for its own reason just before the interrupt fired
	// keeps that failure.
	ordinary := errors.New("upstream rejected the arguments")
	require.NotErrorIs(t, shutdownInterruptedError(ctx, ordinary), shutdowndrain.ErrInterrupted)

	// A failure the cancellation caused is the interrupt.
	cancelled := fmt.Errorf("load credentials: %w", context.Canceled)
	require.ErrorIs(t, shutdownInterruptedError(ctx, cancelled), shutdowndrain.ErrInterrupted)
	require.ErrorIs(t, shutdownInterruptedError(ctx, cancelled), context.Canceled)
}

func TestToolProxy_Do_CompletedCallIsNotInterrupted(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(upstream.Close)

	drain := shutdowndrain.New()
	proxy := newShutdownTestToolProxy(t, drain, nil)
	recorder := httptest.NewRecorder()

	err := proxy.Do(t.Context(), recorder, bytes.NewReader(emptyToolCallBody(t)), emptyToolCallEnv(), newShutdownTestHTTPPlan(upstream.URL), tm.HTTPLogAttributes{})
	require.NoError(t, err)
	drain.Start(0)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"ok":true}`, recorder.Body.String())
}

// heldPlatformExecutor runs a platform tool that waits for the test to
// release it, recording whether its context was cancelled meanwhile.
type heldPlatformExecutor struct {
	entered  chan struct{}
	release  chan struct{}
	ctxError error
}

func (e *heldPlatformExecutor) ExecuteTool(ctx context.Context, _ *ToolCallPlan, _ toolconfig.ToolCallEnv, _ io.Reader) (*PlatformResult, error) {
	close(e.entered)
	<-e.release
	e.ctxError = ctx.Err()

	return &PlatformResult{StatusCode: http.StatusOK, ContentType: "application/json", Body: []byte(`{"ok":true}`)}, nil
}

func TestToolProxy_Do_ShutdownLeavesPlatformToolsRunning(t *testing.T) {
	t.Parallel()

	executor := &heldPlatformExecutor{entered: make(chan struct{}), release: make(chan struct{}), ctxError: nil}
	drain := shutdowndrain.New()
	proxy := newShutdownTestToolProxy(t, drain, executor)

	descriptor := newTestToolDescriptor()
	descriptor.URN = urn.NewTool(urn.ToolKindPlatform, "logs", "search_logs")
	plan := NewPlatformToolCallPlan(descriptor, &PlatformToolCallPlan{SourceSlug: "logs", InputSchema: nil})

	done := runToolCall(t, proxy, plan)
	waitForSignal(t, executor.entered, "the platform tool to start")

	// Interrupt, then give the drain's callbacks time to run before the tool
	// finishes; an interruptible call would have its context cancelled here.
	drain.Start(0)
	interruptRan := make(chan struct{})
	defer drain.AfterInterrupt(func() { close(interruptRan) })()
	waitForSignal(t, interruptRan, "the interrupt deadline to pass")
	close(executor.release)

	require.NoError(t, waitForToolCall(t, done))
	require.NoError(t, executor.ctxError, "a platform tool runs in process and must not be interrupted")
}
