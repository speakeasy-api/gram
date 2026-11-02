//go:build codemodeintegration

package codemode

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type runnerHost struct {
	called  atomic.Int64
	entered chan struct{}
	block   bool
	refuse  bool
	data    json.RawMessage
}

func (*runnerHost) Servers(context.Context, PageArgs) (*ServerPage, error) {
	return &ServerPage{Items: []Server{{Slug: "remote", Name: "Remote tools"}}}, nil
}
func (*runnerHost) Search(context.Context, SearchArgs) (*SearchPage, error) {
	return &SearchPage{Items: []Candidate{{Path: "remote--ping", Description: "Ping"}}}, nil
}
func (h *runnerHost) Describe(context.Context, string) (*Description, error) {
	if h.refuse {
		return nil, ErrToolUnavailable
	}
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
	require.Equal(t, `{"value":9007199254740993}`, string(result.Value))
	require.Equal(t, "done\n", result.Output)
	require.Equal(t, []CallOutcome{{Path: "remote--ping", Outcome: "completed"}}, result.ToolCalls)
	require.EqualValues(t, 1, host.called.Load())
	first := client.session
	fresh := client.Run(t.Context(), uuid.New(), "path", func(context.Context) (Host, error) { return host, nil })
	require.NotNil(t, fresh.Error, "interpreter state must not survive")
	require.Same(t, first, client.session, "independent executions reuse the yamux connection")
}

// A helper refusal reaches Python as a catchable RuntimeError; the runner
// rejects a callback result that carries a value as well as an error.
func TestCodeRunnerHelperRefusalRaisesInPython(t *testing.T) {
	t.Parallel()
	client := realRunnerClient(t)
	host := &runnerHost{refuse: true}
	result := client.Run(t.Context(), uuid.New(), `try:
    await tools.describe("remote--missing")
    message = "described"
except RuntimeError as error:
    message = str(error)
message`, func(context.Context) (Host, error) { return host, nil })
	require.Nil(t, result.Error, "%+v", result.Error)
	require.JSONEq(t, `"tool operation unavailable or arguments invalid"`, string(result.Value))
}

func TestCodeRunnerFailedRunKeepsPrintedOutput(t *testing.T) {
	t.Parallel()
	client := realRunnerClient(t)
	for _, tc := range []struct {
		name      string
		code      string
		message   string
		output    string
		truncated bool
	}{
		{"traceback", "print(\"before traceback\")\n1 / 0", "ZeroDivisionError", "before traceback\n", false},
		{"result too large", "print(\"building\")\n\"x\" * 300000", "JSON result exceeds limit", "building\n", false},
		{"truncated output", "print(\"x\" * 20000)\n1 / 0", "ZeroDivisionError", strings.Repeat("x", maxPrintBytes), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := client.Run(t.Context(), uuid.New(), tc.code, func(context.Context) (Host, error) { return &runnerHost{}, nil })
			require.NotNil(t, result.Error)
			require.Equal(t, "execution_failed", result.Error.Code)
			require.Contains(t, result.Error.Message, tc.message)
			require.Equal(t, tc.output, result.Output)
			require.Equal(t, tc.truncated, result.OutputTruncated)
			require.JSONEq(t, `null`, string(result.Value))
		})
	}
}

// The runner's wall deadline falls before the server's, so its deadline
// report, with what the program printed, reaches the server first.
func TestCodeRunnerDeadlineKeepsPrintedOutput(t *testing.T) {
	t.Parallel()
	client := realRunnerClient(t)
	host := &runnerHost{entered: make(chan struct{}, 1), block: true}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	result := client.Run(ctx, uuid.New(), `print("waiting")
await tools.call("remote--write", {})`, func(context.Context) (Host, error) { return host, nil })
	require.NotNil(t, result.Error)
	require.Equal(t, "deadline_exceeded", result.Error.Code)
	require.Equal(t, "waiting\n", result.Output)
	require.Equal(t, []CallOutcome{{Path: "remote--write", Outcome: "unknown"}}, result.ToolCalls)
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

// codeRunnerDeadlineRuns is how many runs end at their deadline in
// TestCodeRunnerDeadlineKeepsUnknownWrite. The runner's wall deadline falls
// runnerDeadlineLead before the client's, so the runner reports each one.
const codeRunnerDeadlineRuns = 10

func TestCodeRunnerDeadlineKeepsUnknownWrite(t *testing.T) {
	t.Parallel()
	client := realRunnerClient(t)
	host := &runnerHost{block: true}
	factory := func(context.Context) (Host, error) { return host, nil }
	require.Nil(t, client.Run(t.Context(), uuid.New(), "None", factory).Error)
	session := client.session
	for range codeRunnerDeadlineRuns {
		ctx, cancel := context.WithTimeout(t.Context(), runnerDeadlineLead+250*time.Millisecond)
		result := client.Run(ctx, uuid.New(), `await tools.call("remote--write", {})`, factory)
		cancel()
		require.NotNil(t, result.Error)
		require.Equal(t, "deadline_exceeded", result.Error.Code, result.Error.Message)
		require.Equal(t, []CallOutcome{{Path: "remote--write", Outcome: "unknown"}}, result.ToolCalls)
	}
	result := client.Run(t.Context(), uuid.New(), "42", factory)
	require.Nil(t, result.Error, "%+v", result.Error)
	require.Same(t, session, client.session)
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
	host := &runnerHost{entered: make(chan struct{}, RunnerStreams), block: true}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan *ExecutionResult, RunnerStreams)
	for range RunnerStreams {
		go func() {
			done <- client.Run(ctx, uuid.New(), `await tools.call("remote--write", {})`, func(context.Context) (Host, error) { return host, nil })
		}()
	}
	for range RunnerStreams {
		select {
		case <-host.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("callbacks did not start")
		}
	}
	client.mu.Lock()
	session := client.session
	client.mu.Unlock()
	var refusals sync.WaitGroup
	for range 32 {
		refusals.Go(func() {
			result := client.Run(t.Context(), uuid.New(), "42", func(context.Context) (Host, error) {
				t.Error("saturated runtime must refuse before resolving tool scope")
				return host, nil
			})
			if assert.NotNil(t, result.Error) {
				assert.Equal(t, "capacity_exceeded", result.Error.Code, "a full runner refuses as retryable capacity after its wait")
			}
		})
	}
	refusals.Wait()
	require.False(t, session.IsClosed())
	cancel()
	for range RunnerStreams {
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
	require.Equal(t, "deadline_exceeded", result.Error.Code)
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

// replyData returns tool result data whose callback reply frame is about
// reply bytes long.
func replyData(t *testing.T, reply int) json.RawMessage {
	t.Helper()
	value, err := json.Marshal(&ToolResult{Outcome: "completed", OK: true, Data: json.RawMessage(`""`), Content: json.RawMessage(`[]`)})
	require.NoError(t, err)
	envelope, err := json.Marshal(callbackReply(1, value, nil))
	require.NoError(t, err)
	data, err := json.Marshal(strings.Repeat("x", reply-len(envelope)))
	require.NoError(t, err)
	return data
}

// Eight replies just under 1 MiB fit the 8 MiB reply budget. Their 64 KiB
// arguments do not count toward it on either side of the protocol.
func TestCodeRunnerReplyBudgetCountsOnlyReplies(t *testing.T) {
	t.Parallel()
	client := realRunnerClient(t)
	host := &runnerHost{data: replyData(t, 1_040_000)}
	result := client.Run(t.Context(), uuid.New(), `import asyncio
pad = "x" * 65536
replies = await asyncio.gather(*[tools.call("remote--read", {"pad": pad}) for _ in range(8)])
len(replies)`, func(context.Context) (Host, error) { return host, nil })
	require.Nil(t, result.Error, "%+v", result.Error)
	require.JSONEq(t, `8`, string(result.Value))
	require.Len(t, result.ToolCalls, 8)
}

// The ninth reply of about 1 MiB passes the 8 MiB budget. The run reports
// callback_limit, every call the host returned stays completed, and the
// connection serves the next run.
func TestCodeRunnerReplyBudgetReportsCallbackLimit(t *testing.T) {
	t.Parallel()
	client := realRunnerClient(t)
	host := &runnerHost{data: replyData(t, 1_040_000)}
	factory := func(context.Context) (Host, error) { return host, nil }
	result := client.Run(t.Context(), uuid.New(), `import asyncio
for _ in range(2):
    await asyncio.gather(*[tools.call("remote--read", {}) for _ in range(8)])`, factory)
	require.NotNil(t, result.Error)
	require.Equal(t, "callback_limit", result.Error.Code)
	require.Equal(t, callbackBudgetMessage, result.Error.Message)
	require.GreaterOrEqual(t, len(result.ToolCalls), 9)
	for _, call := range result.ToolCalls {
		require.Equal(t, "completed", call.Outcome, "the host returned every dispatched call")
	}
	session := client.session
	next := client.Run(t.Context(), uuid.New(), "42", factory)
	require.Nil(t, next.Error, "%+v", next.Error)
	require.JSONEq(t, `42`, string(next.Value))
	require.Same(t, session, client.session, "a budget failure must not close the runner connection")
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
	require.JSONEq(t, strconv.Itoa(MaxResultBytes-len(base)), string(result.Value), "Python receives the whole payload")
	require.Equal(t, []CallOutcome{{Path: "remote--ping", Outcome: "completed"}}, result.ToolCalls)
}
