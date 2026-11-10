package anthropicinference

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type memoryStore struct {
	saved  []Frame
	userID string
	err    error
}

func (s *memoryStore) ResolveConversation(_ context.Context, config Config, frame Frame) (uuid.UUID, string, error) {
	return conversationID(config, frame), conversationOutcomeSession, nil
}

func (s *memoryStore) ResolveActor(_ context.Context, _ Config, _ Frame) (string, error) {
	return s.userID, nil
}

func (s *memoryStore) Save(_ context.Context, _ Config, frame Frame, _ string) error {
	s.saved = append(s.saved, frame)
	return s.err
}

type recordingScanner struct {
	mu           sync.Mutex
	records      []scanRecord
	indexes      []int
	operationIDs []string
	inputs       []policyInput
	userIDs      []string
	result       *risk.ScanResult
	incomplete   bool
	err          error
}

type scanRecord struct {
	index       int
	operationID string
	input       policyInput
	userID      string
}

// Inputs evaluate concurrently; the operation index restores transcript order.
func (s *recordingScanner) ScanForInferenceEnforcement(_ context.Context, request risk.RealtimeScanRequest) (*risk.InferenceScanOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	operationID := request.Provenance.OperationID
	index, err := strconv.Atoi(operationID[strings.LastIndex(operationID, ":")+1:])
	if err != nil {
		index = len(s.records)
	}
	s.records = append(s.records, scanRecord{
		index: index, operationID: operationID, userID: request.Provenance.UserID,
		input: policyInput{kind: request.MessageType, tool: request.ToolName, text: request.Text, toolCallID: request.Provenance.ToolCallID},
	})
	slices.SortStableFunc(s.records, func(a, b scanRecord) int { return cmp.Compare(a.index, b.index) })
	s.indexes, s.operationIDs, s.inputs, s.userIDs = nil, nil, nil, nil
	for _, record := range s.records {
		s.indexes = append(s.indexes, record.index)
		s.operationIDs = append(s.operationIDs, record.operationID)
		s.inputs = append(s.inputs, record.input)
		s.userIDs = append(s.userIDs, record.userID)
	}
	return &risk.InferenceScanOutcome{Result: s.result, Complete: !s.incomplete && s.err == nil}, s.err
}

func (s *recordingScanner) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records, s.indexes, s.inputs, s.userIDs, s.operationIDs = nil, nil, nil, nil, nil
}

// fakeScanner finishes each input after delay. The stall input only ends with
// its context, the fail input returns an error, and the block input matches a
// blocking policy.
type fakeScanner struct {
	delay  time.Duration
	stall  string
	fail   string
	block  string
	result *risk.ScanResult
	calls  atomic.Int32
}

func (s *fakeScanner) ScanForInferenceEnforcement(ctx context.Context, request risk.RealtimeScanRequest) (*risk.InferenceScanOutcome, error) {
	s.calls.Add(1)
	wait := time.After(s.delay)
	switch {
	case s.stall != "" && request.Text == s.stall:
		wait = nil
	case s.fail != "" && request.Text == s.fail:
		return nil, errors.New("scanner unavailable")
	}
	result := s.result
	if s.block != "" && request.Text == s.block {
		result = new(risk.ScanResult)
		result.Action = "block"
	}
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("scan deadline: %w", ctx.Err())
	case <-wait:
		return &risk.InferenceScanOutcome{Result: result, Complete: true}, nil
	}
}

func exampleFrame() Frame {
	return Frame{
		Type: "prompt", RequestID: "request-example", TenantID: "tenant-example",
		Actor:  Actor{Type: "user", ID: "actor-example", EmailAddress: "user@example.test"},
		Source: Source{Application: "claude-ai"}, SessionID: "session-example", Model: "claude-sonnet-4-6",
		Messages: []Message{{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"EXAMPLE prompt"}]`)}},
	}
}

func TestNewServiceTagsLoggerWithComponent(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	logger := slog.New(&o11y.ContextHandler{Handler: slog.NewJSONHandler(&logs, nil)}).With(attr.SlogComponent("server"), attr.SlogOrganizationID("org_example"))
	service := NewService(logger, testenv.NewMeterProvider(t), nil, nil, nil, nil)
	store, ok := service.store.(*postgresStore)
	require.True(t, ok)

	service.logger.WarnContext(t.Context(), "service log")
	store.logger.WarnContext(t.Context(), "store log")
	logger.WarnContext(t.Context(), "parent log")

	decoder := json.NewDecoder(&logs)
	for _, component := range []string{"anthropic-inference", "anthropic-inference", "server"} {
		var raw json.RawMessage
		require.NoError(t, decoder.Decode(&raw))
		require.Equal(t, 1, bytes.Count(raw, []byte(`"`+string(attr.ComponentKey)+`":`)))

		var record map[string]any
		require.NoError(t, json.Unmarshal(raw, &record))
		require.Equal(t, component, record[string(attr.ComponentKey)])
		require.Equal(t, "org_example", record[string(attr.OrganizationIDKey)])
	}
	require.False(t, decoder.More())
}

func TestServicePersistsDeniedConversation(t *testing.T) {
	t.Parallel()
	store := &memoryStore{saved: nil, userID: "user-example", err: nil}
	result := new(risk.ScanResult)
	result.Action = "block"
	scanner := &recordingScanner{inputs: nil, userIDs: nil, result: result, err: nil}
	service := &Service{logger: testenv.NewLogger(t), store: store, scanner: scanner}
	verdict, err := service.Process(t.Context(), Config{ID: "example", OrganizationID: "org_example", ProjectID: uuid.New(), TenantID: "tenant-example", SigningSecrets: nil}, exampleFrame())
	require.NoError(t, err)
	require.Equal(t, "deny", verdict.Action)
	require.Len(t, store.saved, 1)
	require.Equal(t, []string{"user-example"}, scanner.userIDs)
}

func TestServicePropagatesScannerErrors(t *testing.T) {
	t.Parallel()
	store := &memoryStore{saved: nil, userID: "", err: nil}
	scanner := &recordingScanner{inputs: nil, userIDs: nil, result: nil, err: errors.New("scanner unavailable")}
	service := &Service{logger: testenv.NewLogger(t), store: store, scanner: scanner}
	_, err := service.Process(t.Context(), Config{ID: "example", OrganizationID: "org_example", ProjectID: uuid.New(), TenantID: "tenant-example", SigningSecrets: nil}, exampleFrame())
	require.ErrorContains(t, err, "evaluate inference policy")
	require.Len(t, store.saved, 1)
}

func TestServicePropagatesStorageErrors(t *testing.T) {
	t.Parallel()
	store := &memoryStore{saved: nil, userID: "", err: errors.New("storage unavailable")}
	scanner := &recordingScanner{inputs: nil, userIDs: nil, result: nil, err: nil}
	service := &Service{logger: testenv.NewLogger(t), store: store, scanner: scanner}
	_, err := service.Process(t.Context(), Config{ID: "example", OrganizationID: "org_example", ProjectID: uuid.New(), TenantID: "tenant-example", SigningSecrets: nil}, exampleFrame())
	require.ErrorContains(t, err, "store inference transcript")
	require.Empty(t, scanner.inputs)
}

func TestPolicyInputsPreserveContentScopes(t *testing.T) {
	t.Parallel()
	inputs, err := policyInputs([]Message{
		{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"prompt"},{"type":"attachment","text":"file contents"},{"type":"tool_result","tool_name":"read_file","content":"output"}]`)},
		{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"reply"},{"type":"tool_use","name":"read_file","input":{"path":"example.txt"}}]`)},
	})
	require.NoError(t, err)
	require.Equal(t, []policyInput{
		{kind: message.User, tool: "", text: "prompt", toolCallID: ""},
		{kind: message.PromptAttachment, tool: "", text: "file contents", toolCallID: ""},
		{kind: message.ToolResponse, tool: "read_file", text: "output", toolCallID: ""},
		{kind: message.Assistant, tool: "", text: "reply", toolCallID: "", message: 1},
		{kind: message.ToolRequest, tool: "read_file", text: `{"path":"example.txt"}`, toolCallID: "", message: 1},
	}, inputs)
}

func TestPolicyInputsDecodesToolNameFromInferenceHooksShape(t *testing.T) {
	t.Parallel()
	// Inference hooks use tool_name (not name) on tool_use blocks.
	inputs, err := policyInputs([]Message{
		{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","tool_name":"bash","input":{"cmd":"ls"}}]`)},
	})
	require.NoError(t, err)
	require.Equal(t, []policyInput{
		{kind: message.ToolRequest, tool: "bash", text: `{"cmd":"ls"}`, toolCallID: ""},
	}, inputs)
}

func TestUnknownContentBlocksDoNotBreakParsing(t *testing.T) {
	t.Parallel()
	inputs, err := policyInputs([]Message{{Role: "user", Content: json.RawMessage(`[{"type":"future","content":[{"unknown":true}]},{"type":"text","text":"known"}]`)}})
	require.NoError(t, err)
	require.Equal(t, []policyInput{{kind: message.User, tool: "", text: "known", toolCallID: ""}}, inputs)
}

func TestServiceDeniesWarnAndQuarantineMatches(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"warn", "quarantine"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			store := &memoryStore{saved: nil, userID: "", err: nil}
			result := new(risk.ScanResult)
			result.Action = action
			scanner := &recordingScanner{inputs: nil, userIDs: nil, result: result, err: nil}
			service := &Service{logger: testenv.NewLogger(t), store: store, scanner: scanner}
			verdict, err := service.Process(t.Context(), Config{ID: "example", OrganizationID: "org_example", ProjectID: uuid.New(), TenantID: "tenant-example", SigningSecrets: nil}, exampleFrame())
			require.NoError(t, err)
			require.Equal(t, "deny", verdict.Action)
		})
	}
}

func TestServicePreservesRawToolInvocationIDs(t *testing.T) {
	t.Parallel()
	store := &memoryStore{saved: nil, userID: "user-example", err: nil}
	scanner := &recordingScanner{inputs: nil, userIDs: nil, result: nil, err: nil}
	service := &Service{logger: testenv.NewLogger(t), store: store, scanner: scanner}
	frame := exampleFrame()
	frame.Messages = []Message{
		{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":" call-1 ","tool_name":"read_file","input":{"path":"example.txt"}}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":" call-1 ","tool_name":"read_file","content":"output"},{"type":"text","text":"continue"}]`)},
	}
	_, err := service.Process(t.Context(), Config{ID: "example", OrganizationID: "org_example", ProjectID: uuid.New(), TenantID: "tenant-example", SigningSecrets: nil}, frame)
	require.NoError(t, err)
	require.Equal(t, []policyInput{
		{kind: message.ToolRequest, tool: "read_file", text: `{"path":"example.txt"}`, toolCallID: " call-1 "},
		{kind: message.ToolResponse, tool: "read_file", text: "output", toolCallID: " call-1 "},
		{kind: message.User, tool: "", text: "continue", toolCallID: ""},
	}, scanner.inputs)
}

func TestServiceScansOnlyTheLatestExchange(t *testing.T) {
	t.Parallel()
	frame := exampleFrame()
	frame.Messages = []Message{
		textMessage("user", "old prompt"), textMessage("assistant", "old reply"),
		textMessage("user", "second prompt"), textMessage("assistant", "second reply"),
		textMessage("user", "new prompt"),
	}
	scanner := &recordingScanner{}
	service := &Service{logger: testenv.NewLogger(t), store: &memoryStore{}, scanner: scanner}
	verdict, err := service.Process(t.Context(), Config{}, frame)
	require.NoError(t, err)
	require.Equal(t, "allow", verdict.Action)
	require.Equal(t, []string{"second reply", "new prompt"}, []string{scanner.inputs[0].text, scanner.inputs[1].text})
	require.Len(t, scanner.inputs, 2)
}

func TestServiceStillDeniesRedeliveredCurrentTurn(t *testing.T) {
	t.Parallel()
	frame := exampleFrame()
	frame.Messages = []Message{textMessage("user", "old prompt"), textMessage("assistant", "old reply"), textMessage("user", "blocked prompt")}
	service := &Service{logger: testenv.NewLogger(t), store: &memoryStore{}, scanner: &fakeScanner{block: "blocked prompt"}}
	for range 2 {
		verdict, err := service.Process(t.Context(), Config{}, frame)
		require.NoError(t, err)
		require.Equal(t, "deny", verdict.Action)
	}
}

func TestServiceStillDeniesRetriedToolCall(t *testing.T) {
	t.Parallel()
	frame := exampleFrame()
	frame.Messages = []Message{
		textMessage("user", "prompt"),
		{Role: "assistant", Content: json.RawMessage(`[{"type":"tool_use","id":"call-1","tool_name":"bash","input":{"cmd":"blocked"}}]`)},
		{Role: "user", Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"call-1","tool_name":"bash","content":"output"}]`)},
	}
	service := &Service{logger: testenv.NewLogger(t), store: &memoryStore{}, scanner: &fakeScanner{block: `{"cmd":"blocked"}`}}
	for range 2 {
		verdict, err := service.Process(t.Context(), Config{}, frame)
		require.NoError(t, err)
		require.Equal(t, "deny", verdict.Action)
	}
}

func TestServiceDenialDoesNotOutliveItsTurn(t *testing.T) {
	t.Parallel()
	frame := exampleFrame()
	frame.Messages = []Message{textMessage("user", "blocked prompt")}
	service := &Service{logger: testenv.NewLogger(t), store: &memoryStore{}, scanner: &fakeScanner{block: "blocked prompt"}}
	verdict, err := service.Process(t.Context(), Config{}, frame)
	require.NoError(t, err)
	require.Equal(t, "deny", verdict.Action)

	// The conversation moves on past the denied prompt.
	for turn := range 3 {
		frame.Messages = append(frame.Messages, textMessage("assistant", fmt.Sprintf("reply %d", turn)), textMessage("user", fmt.Sprintf("benign prompt %d", turn)))
		verdict, err := service.Process(t.Context(), Config{}, frame)
		require.NoError(t, err)
		require.Equal(t, "allow", verdict.Action, "turn %d", turn)
	}
}

func TestServiceEvaluatesInputsConcurrently(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		frame := exampleFrame()
		frame.Messages = nil
		for i := range 2 * scanConcurrency {
			frame.Messages = append(frame.Messages, textMessage("user", fmt.Sprintf("prompt %d", i)))
		}
		service := &Service{logger: testenv.NewLogger(t), store: &memoryStore{}, scanner: &fakeScanner{delay: time.Second}}
		start := time.Now()
		verdict, err := service.Process(t.Context(), Config{}, frame)
		require.NoError(t, err)
		require.Equal(t, "allow", verdict.Action)
		require.Equal(t, 2*time.Second, time.Since(start))
	})
}

func TestServiceDeniesWhenTheLatestExchangeOutrunsTheBudget(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		frame := exampleFrame()
		frame.Messages = []Message{textMessage("user", "first"), textMessage("assistant", "reply"), textMessage("user", "stall")}
		service := &Service{logger: testenv.NewLogger(t), store: &memoryStore{}, scanner: &fakeScanner{delay: time.Second, stall: "stall"}}
		start := time.Now()
		verdict, err := service.Process(t.Context(), Config{}, frame)
		require.NoError(t, err)
		require.Equal(t, verdictBudget, time.Since(start))
		require.Equal(t, Verdict{Action: "deny", DenyReason: unavailableDenyReason, ReferenceID: ""}, verdict)
	})
}

func TestServiceReservesResponseHeadroom(t *testing.T) {
	t.Parallel()
	// Pin the requested evaluation cap separately from behavioral assertions.
	require.Equal(t, 4250*time.Millisecond, verdictBudget)
	require.Less(t, requestBudget, 5*time.Second)
	for _, elapsed := range []time.Duration{0, time.Second} {
		t.Run(elapsed.String(), func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), requestBudget)
				defer cancel()
				// Account for configuration/body work before Process starts.
				<-time.After(elapsed)
				frame := exampleFrame()
				frame.Messages = []Message{textMessage("user", "stall")}
				service := &Service{logger: testenv.NewLogger(t), store: &memoryStore{}, scanner: &fakeScanner{stall: "stall"}}
				verdict, err := service.Process(ctx, Config{}, frame)
				require.NoError(t, err)
				require.Equal(t, "deny", verdict.Action)
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				require.GreaterOrEqual(t, time.Until(deadline), responseBudget)
			})
		})
	}
}

func TestMetricApplicationIsBounded(t *testing.T) {
	t.Parallel()
	require.Equal(t, "claude-chat-web", metricApplication("claude-ai"))
	require.Equal(t, "claude-code-web", metricApplication("claude-code"))
	require.Equal(t, "claude-design", metricApplication("claude-design"))
	require.Equal(t, "anthropic-inference", metricApplication(""))
	require.Equal(t, "other", metricApplication("some-future-product"))
	require.Equal(t, "anthropic-inference", metricApplication("  "), "whitespace trims to the absent application")
}
