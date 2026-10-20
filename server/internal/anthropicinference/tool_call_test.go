package anthropicinference

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	goahttp "goa.design/goa/v3/http"

	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func toolCallFrame() Frame {
	frame := exampleFrame()
	frame.Type, frame.RequestID = frameTypeToolCall, "tool-call-request-example"
	frame.Messages = []Message{{Role: "assistant", Content: json.RawMessage(`[
		{"type":"text","text":"Let me check."},
		{"type":"tool_use","id":"toolu_read","tool_name":"read_file","input":{"path":"reports/q3.txt"},"tool_info":{"type":"client"}},
		{"type":"tool_use","id":"toolu_bash","tool_name":"bash","input":{"command":"ls -la reports/"},"tool_info":{"type":"client","tool_type":"bash_20250124"}}
	]`)}}
	return frame
}

func TestServiceDeniesToolCallFrameBeforeToolsRun(t *testing.T) {
	t.Parallel()
	result := new(risk.ScanResult)
	result.Action = "block"
	result.UserMessage = new("Remove the restricted path.")
	store := &memoryStore{userID: "user-example"}
	scanner := &recordingScanner{result: result}
	service := &Service{logger: testenv.NewLogger(t), store: store, scanner: scanner}
	verdict, err := service.Process(t.Context(), Config{}, toolCallFrame())
	require.NoError(t, err)
	require.Equal(t, Verdict{Action: "deny", DenyReason: "Remove the restricted path.", ReferenceID: ""}, verdict)
	require.NotEmpty(t, scanner.inputs)
	require.Equal(t, "user-example", scanner.userIDs[0])
	require.Empty(t, store.saved, "the next prompt frame archives the tool calls")
	require.Nil(t, store.accepted, "a tool call frame never moves the checkpoint")
}

func TestServiceScansEachToolCallAsAToolRequest(t *testing.T) {
	t.Parallel()
	store := &memoryStore{}
	scanner := &recordingScanner{}
	service := &Service{logger: testenv.NewLogger(t), store: store, scanner: scanner}
	frame := toolCallFrame()
	// Earlier messages the protocol may add were judged by their prompt frame.
	frame.Messages = append([]Message{textMessage("user", "earlier prompt")}, frame.Messages...)
	verdict, err := service.Process(t.Context(), Config{}, frame)
	require.NoError(t, err)
	require.Equal(t, "allow", verdict.Action)
	require.Equal(t, []policyInput{
		{kind: message.ToolRequest, tool: "read_file", text: `{"path":"reports/q3.txt"}`, toolCallID: "toolu_read"},
		{kind: message.ToolRequest, tool: "bash", text: `{"command":"ls -la reports/"}`, toolCallID: "toolu_bash"},
	}, scanner.inputs)
	require.Equal(t, []string{"anthropic-inference:tool-call-request-example:0", "anthropic-inference:tool-call-request-example:1"}, scanner.operationIDs)
	require.Empty(t, store.saved)
	require.Nil(t, store.accepted)
}

func TestServiceDeniesToolCallFrameThatCannotBeScannedInTime(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		service := &Service{logger: testenv.NewLogger(t), store: &memoryStore{}, scanner: &fakeScanner{delay: time.Second, stall: `{"command":"ls -la reports/"}`}}
		start := time.Now()
		verdict, err := service.Process(t.Context(), Config{}, toolCallFrame())
		require.NoError(t, err)
		require.Equal(t, verdictBudget, time.Since(start))
		require.Equal(t, Verdict{Action: "deny", DenyReason: unavailableDenyReason, ReferenceID: ""}, verdict)
	})
}

func TestServicePropagatesToolCallScannerErrors(t *testing.T) {
	t.Parallel()
	service := &Service{logger: testenv.NewLogger(t), store: &memoryStore{}, scanner: &recordingScanner{err: errors.New("scanner unavailable")}}
	_, err := service.Process(t.Context(), Config{}, toolCallFrame())
	require.ErrorContains(t, err, "evaluate inference policy")
}

func TestServiceAllowsToolCallFrameWithoutToolCalls(t *testing.T) {
	t.Parallel()
	scanner := &recordingScanner{}
	service := &Service{logger: testenv.NewLogger(t), store: &memoryStore{}, scanner: scanner}
	frame := toolCallFrame()
	frame.Messages = []Message{textMessage("assistant", "no calls")}
	verdict, err := service.Process(t.Context(), Config{}, frame)
	require.NoError(t, err)
	require.Equal(t, "allow", verdict.Action)
	require.Empty(t, scanner.inputs)
}

func TestHandlerProcessesToolCallFrames(t *testing.T) {
	t.Parallel()
	processor := &testProcessor{verdict: Verdict{Action: "deny", DenyReason: "Remove restricted content.", ReferenceID: ""}}
	response := serveSigned(newTestHandler(t, processor), `{"type":"tool_call","request_id":"request-example","tenant_id":"tenant-example"}`)
	require.Equal(t, http.StatusOK, response.Code)
	require.JSONEq(t, `{"action":"deny","deny_reason":"Remove restricted content."}`, response.Body.String())
	require.Equal(t, 1, processor.calls)
}

func TestSignedToolCallFrameEnforcesWithoutArchiving(t *testing.T) {
	t.Parallel()
	store, db, config := newTestStore(t)
	prompt := exampleFrame()
	saveFrame(t, store, config, prompt, "user-example")

	result := new(risk.ScanResult)
	result.Action = "block"
	scanner := &recordingScanner{result: result}
	service := &Service{logger: testenv.NewLogger(t), store: store, scanner: scanner}
	key := []byte("EXAMPLE-signing-secret")
	config.SigningSecrets = []string{"whsec_" + base64.StdEncoding.EncodeToString(key)}
	mux := goahttp.NewMuxer()
	Attach(mux, testenv.NewLogger(t), service, &testResolver{config: config, err: nil})

	sessioned := toolCallFrame()
	sessioned.RequestID = prompt.RequestID
	sessionless := sessioned
	sessionless.SessionID = ""
	for _, frame := range []Frame{sessioned, sessionless} {
		scanner.reset()
		body, err := json.Marshal(frame)
		require.NoError(t, err)
		request := httptest.NewRequest(http.MethodPost, "/hooks/anthropic-inference/example", bytes.NewReader(body))
		request.Header = signedHeaders(body, key, time.Now())
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code)
		require.Contains(t, response.Body.String(), `"action":"deny"`)
		require.NotEmpty(t, scanner.inputs)
		require.Equal(t, message.ToolRequest, scanner.inputs[0].kind)
	}

	// The session's frame is judged as the conversation's last known user.
	scanner.reset()
	_, err := service.Process(t.Context(), config, sessioned)
	require.NoError(t, err)
	require.Equal(t, "user-example", scanner.userIDs[0])

	queries := chatrepo.New(db)
	messages, err := queries.ListChatMessages(t.Context(), chatrepo.ListChatMessagesParams{ChatID: conversationID(config, prompt), ProjectID: config.ProjectID})
	require.NoError(t, err)
	require.Len(t, messages, 1, "only the prompt frame is archived")
	_, err = queries.GetChat(t.Context(), chatrepo.GetChatParams{ID: conversationID(config, sessionless), ProjectID: config.ProjectID})
	require.ErrorIs(t, err, pgx.ErrNoRows, "a tool call frame never starts a conversation")
}
