package anthropicinference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

type memoryStore struct {
	accepted [][]byte
	saved    []Frame
	userID   string
	err      error
	loadErr  error
}

func (s *memoryStore) ResolveActor(_ context.Context, _ Config, _ Frame) (string, error) {
	return s.userID, nil
}

func (s *memoryStore) Save(_ context.Context, _ Config, frame Frame, _ string) (conversation, int, error) {
	s.saved = append(s.saved, frame)
	return conversation{chatID: uuid.Nil, adopted: false}, 0, s.err
}

func (s *memoryStore) Begin(context.Context, Config, conversation, string) (checkpointSession, error) {
	return &memoryCheckpoint{store: s}, nil
}

type memoryCheckpoint struct{ store *memoryStore }

func (s *memoryCheckpoint) Load(context.Context) ([][]byte, error) {
	return s.store.accepted, s.store.loadErr
}

func (s *memoryCheckpoint) Accept(ctx context.Context, hashes [][]byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("checkpoint context: %w", err)
	}
	s.store.accepted = hashes
	return nil
}

type recordingScanner struct {
	mu           sync.Mutex
	indexes      []int
	operationIDs []string
	inputs       []policyInput
	userIDs      []string
	result       *risk.ScanResult
	incomplete   bool
	err          error
}

// Inputs evaluate concurrently; the operation index restores transcript order.
func (s *recordingScanner) ScanForInferenceEnforcement(_ context.Context, request risk.RealtimeScanRequest) (*risk.InferenceScanOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	operationID := request.Provenance.OperationID
	index, err := strconv.Atoi(operationID[strings.LastIndex(operationID, ":")+1:])
	if err != nil {
		index = len(s.indexes)
	}
	s.indexes = append(s.indexes, index)
	s.inputs = append(s.inputs, policyInput{kind: request.MessageType, tool: request.ToolName, text: request.Text, toolCallID: request.Provenance.ToolCallID})
	s.userIDs = append(s.userIDs, request.Provenance.UserID)
	s.operationIDs = append(s.operationIDs, operationID)
	for i := len(s.indexes) - 1; i > 0 && s.indexes[i-1] > s.indexes[i]; i-- {
		s.indexes[i-1], s.indexes[i] = s.indexes[i], s.indexes[i-1]
		s.inputs[i-1], s.inputs[i] = s.inputs[i], s.inputs[i-1]
		s.userIDs[i-1], s.userIDs[i] = s.userIDs[i], s.userIDs[i-1]
		s.operationIDs[i-1], s.operationIDs[i] = s.operationIDs[i], s.operationIDs[i-1]
	}
	return &risk.InferenceScanOutcome{Result: s.result, Complete: !s.incomplete && s.err == nil}, s.err
}

func (s *recordingScanner) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.indexes, s.inputs, s.userIDs, s.operationIDs = nil, nil, nil, nil
}

func exampleFrame() Frame {
	return Frame{
		Type: "prompt", RequestID: "request-example", TenantID: "tenant-example",
		Actor:  Actor{Type: "user", ID: "actor-example", EmailAddress: "user@example.test"},
		Source: Source{Application: "claude-ai"}, SessionID: "session-example", Model: "claude-sonnet-4-6",
		Messages: []Message{{Role: "user", Content: json.RawMessage(`[{"type":"text","text":"EXAMPLE prompt"}]`)}},
	}
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

type deltaStore struct {
	memoryStore
	newStart int
}

func (s *deltaStore) Save(ctx context.Context, config Config, frame Frame, userID string) (conversation, int, error) {
	binding, _, err := s.memoryStore.Save(ctx, config, frame, userID)
	return binding, s.newStart, err
}

func TestServiceScansOnlyNewMessagesAndTheCurrentTurn(t *testing.T) {
	t.Parallel()
	frame := exampleFrame()
	frame.Messages = []Message{
		textMessage("user", "old prompt"), textMessage("assistant", "old reply"),
		textMessage("user", "second prompt"), textMessage("assistant", "second reply"),
		textMessage("user", "new prompt"),
	}
	scanner := &recordingScanner{inputs: nil, userIDs: nil, result: nil, err: nil}
	service := &Service{logger: testenv.NewLogger(t), store: &deltaStore{memoryStore: memoryStore{accepted: transcriptHashes(frame.Messages[:4]), saved: nil, userID: "", err: nil}, newStart: 4}, scanner: scanner}
	_, err := service.Process(t.Context(), Config{}, frame)
	require.NoError(t, err)
	require.Len(t, scanner.inputs, 1)
	require.Equal(t, "new prompt", scanner.inputs[0].text)

	// A frame that adds an assistant reply and a tool result is judged from the
	// first new message, which precedes the current turn.
	scanner.reset()
	service.store = &deltaStore{memoryStore: memoryStore{accepted: transcriptHashes(frame.Messages[:3]), saved: nil, userID: "", err: nil}, newStart: 3}
	_, err = service.Process(t.Context(), Config{}, frame)
	require.NoError(t, err)
	require.Len(t, scanner.inputs, 2)
	require.Equal(t, "second reply", scanner.inputs[0].text)
	require.Equal(t, "new prompt", scanner.inputs[1].text)
}

func TestServiceStillDeniesRedeliveredCurrentTurn(t *testing.T) {
	t.Parallel()
	frame := exampleFrame()
	frame.Messages = []Message{textMessage("user", "old prompt"), textMessage("assistant", "old reply"), textMessage("user", "blocked prompt")}
	result := new(risk.ScanResult)
	result.Action = "block"
	scanner := &recordingScanner{inputs: nil, userIDs: nil, result: result, err: nil}
	// Everything in the frame is already stored and accepted, as after a redelivery.
	service := &Service{logger: testenv.NewLogger(t), store: &deltaStore{memoryStore: memoryStore{accepted: transcriptHashes(frame.Messages), saved: nil, userID: "", err: nil}, newStart: 3}, scanner: scanner}
	verdict, err := service.Process(t.Context(), Config{}, frame)
	require.NoError(t, err)
	require.Equal(t, "deny", verdict.Action)
	require.Len(t, scanner.inputs, 1)
	require.Equal(t, "blocked prompt", scanner.inputs[0].text)
}

type stallingScanner struct {
	stallOn string
	calls   atomic.Int32
}

// Each input takes a second; the input whose text matches stallOn never finishes.
func (s *stallingScanner) ScanForInferenceEnforcement(ctx context.Context, request risk.RealtimeScanRequest) (*risk.InferenceScanOutcome, error) {
	s.calls.Add(1)
	wait := time.After(time.Second)
	if request.Text == s.stallOn {
		wait = nil
	}
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("scan deadline: %w", ctx.Err())
	case <-wait:
		return &risk.InferenceScanOutcome{Result: nil, Complete: true}, nil
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
		store := &memoryStore{}
		service := &Service{logger: testenv.NewLogger(t), store: store, scanner: &stallingScanner{stallOn: ""}}
		start := time.Now()
		verdict, err := service.Process(t.Context(), Config{}, frame)
		require.NoError(t, err)
		require.Equal(t, "allow", verdict.Action)
		require.Equal(t, 2*time.Second, time.Since(start))
		require.Equal(t, transcriptHashes(frame.Messages), store.accepted)
	})
}

func TestServiceAcceptsCleanPrefixWhenBudgetExhausted(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		frame := exampleFrame()
		frame.Messages = []Message{textMessage("user", "first"), textMessage("assistant", "reply"), textMessage("user", "stall"), textMessage("user", "last")}
		store := &memoryStore{}
		scanner := &stallingScanner{stallOn: "stall"}
		service := &Service{logger: testenv.NewLogger(t), store: store, scanner: scanner}
		start := time.Now()
		verdict, err := service.Process(t.Context(), Config{}, frame)
		require.NoError(t, err)
		require.Equal(t, verdictBudget, time.Since(start))
		// Every input was attempted; only the stalled one was cut off.
		require.EqualValues(t, len(frame.Messages), scanner.calls.Load())
		require.Equal(t, Verdict{Action: "deny", DenyReason: unavailableDenyReason, ReferenceID: ""}, verdict)
		require.Equal(t, transcriptHashes(frame.Messages[:2]), store.accepted)

		// The redelivery picks up where the budget ran out.
		retry := &recordingScanner{}
		service.scanner = retry
		verdict, err = service.Process(t.Context(), Config{}, frame)
		require.NoError(t, err)
		require.Equal(t, "allow", verdict.Action)
		require.Len(t, retry.inputs, 2)
		require.Equal(t, "stall", retry.inputs[0].text)
		require.Equal(t, transcriptHashes(frame.Messages), store.accepted)
	})
}
