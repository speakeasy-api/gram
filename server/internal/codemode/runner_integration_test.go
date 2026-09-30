//go:build codemodeintegration

package codemode

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

type runnerHost struct {
	called  atomic.Int64
	entered chan struct{}
	block   bool
	data    json.RawMessage
}

func (*runnerHost) Servers(context.Context, PageArgs) (*ServerPage, error) {
	return &ServerPage{Items: []Server{{Slug: "remote", Name: "Remote tools"}}}, nil
}
func (*runnerHost) Search(context.Context, SearchArgs) (*SearchPage, error) {
	return &SearchPage{Items: []Candidate{{Path: "remote--ping", Description: "Ping"}}}, nil
}
func (*runnerHost) Describe(context.Context, string) (*Description, error) {
	return &Description{Path: "remote--ping", Definition: json.RawMessage(`{"name":"remote--ping","inputSchema":{"type":"object"}}`)}, nil
}
func (h *runnerHost) Call(ctx context.Context, path string, args json.RawMessage) (*ToolResult, error) {
	h.called.Add(1)
	if h.entered != nil {
		h.entered <- struct{}{}
	}
	if h.block {
		<-ctx.Done()
		return &ToolResult{Outcome: "unknown", Error: "tool_call_outcome_unknown"}, nil
	}
	data := h.data
	if data == nil {
		data = json.RawMessage(`{"value":9007199254740993}`)
	}
	return &ToolResult{Outcome: "completed", OK: true, Data: data, Content: json.RawMessage(`[]`)}, nil
}

func realRunnerClient(t *testing.T) *RunnerClient {
	t.Helper()
	endpoint, token := testenv.LaunchCodeRunner(t)
	policy, err := guardian.NewUnsafePolicy(testenv.NewTracerProvider(t), nil)
	require.NoError(t, err)
	client, err := NewRunnerClient(endpoint, token, policy.Dialer())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	return client
}

func TestCodeRunnerRealPythonWorkflow(t *testing.T) {
	t.Parallel()
	client := realRunnerClient(t)
	host := &runnerHost{}
	result := client.Run(t.Context(), uuid.New(), `page = await tools.search("ping")
path = page["items"][0]["path"]
details = await tools.describe(path)
response = await tools.call(path, {})
print("done")
response["data"]`, func(context.Context) (Host, error) { return host, nil })
	require.Nil(t, result.Error, "%+v", result.Error)
	require.JSONEq(t, `{"value":9007199254740993}`, string(result.Value))
	require.Equal(t, "done\n", result.Output)
	require.Equal(t, []CallOutcome{{Path: "remote--ping", Outcome: "completed"}}, result.ToolCalls)
	require.EqualValues(t, 1, host.called.Load())
	first := client.session
	fresh := client.Run(t.Context(), uuid.New(), "path", func(context.Context) (Host, error) { return host, nil })
	require.NotNil(t, fresh.Error, "interpreter state must not survive")
	require.Same(t, first, client.session, "independent executions reuse the yamux connection")
}

func TestCodeRunnerCancellationPreservesUnknownWrite(t *testing.T) {
	t.Parallel()
	client := realRunnerClient(t)
	host := &runnerHost{entered: make(chan struct{}, 1), block: true}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan *ExecutionResult, 1)
	go func() {
		done <- client.Run(ctx, uuid.New(), `await tools.call("remote--write", {})`, func(context.Context) (Host, error) { return host, nil })
	}()
	select {
	case <-host.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("callback did not start")
	}
	cancel()
	select {
	case result := <-done:
		require.NotNil(t, result.Error)
		require.Equal(t, []CallOutcome{{Path: "remote--write", Outcome: "unknown"}}, result.ToolCalls)
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled execution did not stop")
	}
	require.EqualValues(t, 1, host.called.Load())
	result := client.Run(t.Context(), uuid.New(), "42", func(context.Context) (Host, error) { return host, nil })
	require.Nil(t, result.Error, "%+v", result.Error)
	require.JSONEq(t, `42`, string(result.Value))
}

func TestCodeRunnerConcurrentHelpers(t *testing.T) {
	t.Parallel()
	client := realRunnerClient(t)
	host := &runnerHost{}
	result := client.Run(t.Context(), uuid.New(), `import asyncio
await asyncio.gather(tools.call("remote--ping", {}), tools.call("remote--ping", {}))`, func(context.Context) (Host, error) { return host, nil })
	require.Nil(t, result.Error, "%+v", result.Error)
	require.Len(t, result.ToolCalls, 2)
	require.EqualValues(t, 2, host.called.Load())
}

func TestCodeRunnerBurstDoesNotInterruptOtherExecutions(t *testing.T) {
	t.Parallel()
	client := realRunnerClient(t)
	host := &runnerHost{entered: make(chan struct{}, maxRunnerStreams), block: true}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan *ExecutionResult, maxRunnerStreams)
	for range maxRunnerStreams {
		go func() {
			done <- client.Run(ctx, uuid.New(), `await tools.call("remote--write", {})`, func(context.Context) (Host, error) { return host, nil })
		}()
	}
	for range maxRunnerStreams {
		select {
		case <-host.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("callbacks did not start")
		}
	}
	client.mu.Lock()
	session := client.session
	client.mu.Unlock()
	for range 32 {
		result := client.Run(t.Context(), uuid.New(), "42", func(context.Context) (Host, error) {
			t.Error("saturated runtime must refuse before resolving tool scope")
			return host, nil
		})
		require.NotNil(t, result.Error)
		require.Equal(t, "admission_refused", result.Error.Code)
	}
	require.False(t, session.IsClosed())
	cancel()
	for range maxRunnerStreams {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("cancelled callback did not finish")
		}
	}
	result := client.Run(t.Context(), uuid.New(), "42", func(context.Context) (Host, error) { return host, nil })
	require.Nil(t, result.Error, "%+v", result.Error)
	require.Same(t, session, client.session)
}

func TestCodeRunnerScopeResolutionUsesExecutionDeadline(t *testing.T) {
	t.Parallel()
	client := realRunnerClient(t)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	result := client.Run(ctx, uuid.New(), "42", func(ctx context.Context) (Host, error) { <-ctx.Done(); return nil, ctx.Err() })
	require.NotNil(t, result.Error)
	require.Equal(t, "admission_refused", result.Error.Code)
	require.Nil(t, client.session, "scope resolution must finish before submitting code")
}

func TestCodeRunnerAdmissionRefusalClosesStream(t *testing.T) {
	t.Parallel()
	client := realRunnerClient(t)
	host := &runnerHost{}
	factory := func(context.Context) (Host, error) { return host, nil }
	id := uuid.New()
	require.Nil(t, client.Run(t.Context(), id, "None", factory).Error)
	connection := client.session
	for range 24 {
		result := client.Run(t.Context(), id, "42", factory)
		require.NotNil(t, result.Error)
		require.Equal(t, "admission_refused", result.Error.Code)
	}
	result := client.Run(t.Context(), uuid.New(), "42", factory)
	require.Nil(t, result.Error, "%+v", result.Error)
	require.Same(t, connection, client.session, "refused streams must release both sides before another opens")
}

func TestCodeRunnerFullSizeCallbackFitsEnvelope(t *testing.T) {
	t.Parallel()
	client := realRunnerClient(t)
	base, err := json.Marshal(&ToolResult{Outcome: "completed", OK: true, Data: json.RawMessage(`""`), Content: json.RawMessage(`[]`)})
	require.NoError(t, err)
	data, err := json.Marshal(strings.Repeat("x", MaxResultBytes-len(base)))
	require.NoError(t, err)
	host := &runnerHost{data: data}
	result := client.Run(t.Context(), uuid.New(), `response = await tools.call("remote--ping", {})
len(response["data"])`, func(context.Context) (Host, error) { return host, nil })
	require.Nil(t, result.Error, "%+v", result.Error)
	require.Equal(t, []CallOutcome{{Path: "remote--ping", Outcome: "completed"}}, result.ToolCalls)
}
