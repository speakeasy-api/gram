package anthropicinference

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk"
)

type memoryStore struct {
	saved  []Frame
	userID string
	err    error
}

func (s *memoryStore) ResolveActor(_ context.Context, _ Config, _ Frame) (string, error) {
	return s.userID, nil
}
func (s *memoryStore) Save(_ context.Context, _ Config, frame Frame, _ string) error {
	s.saved = append(s.saved, frame)
	return s.err
}

type recordingScanner struct {
	inputs  []policyInput
	userIDs []string
	result  *risk.ScanResult
	err     error
}

func (s *recordingScanner) ScanForEnforcement(_ context.Context, request risk.RealtimeScanRequest) (*risk.ScanResult, error) {
	s.inputs = append(s.inputs, policyInput{kind: request.MessageType, tool: request.ToolName, text: request.Text})
	s.userIDs = append(s.userIDs, request.Provenance.UserID)
	return s.result, s.err
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
	service := &Service{store: store, scanner: scanner}
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
	service := &Service{store: store, scanner: scanner}
	_, err := service.Process(t.Context(), Config{ID: "example", OrganizationID: "org_example", ProjectID: uuid.New(), TenantID: "tenant-example", SigningSecrets: nil}, exampleFrame())
	require.ErrorContains(t, err, "evaluate inference policy")
	require.Len(t, store.saved, 1)
}

func TestServicePropagatesStorageErrors(t *testing.T) {
	t.Parallel()
	store := &memoryStore{saved: nil, userID: "", err: errors.New("storage unavailable")}
	scanner := &recordingScanner{inputs: nil, userIDs: nil, result: nil, err: nil}
	service := &Service{store: store, scanner: scanner}
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
		{kind: message.User, tool: "", text: "prompt"},
		{kind: message.PromptAttachment, tool: "", text: "file contents"},
		{kind: message.ToolResponse, tool: "read_file", text: "output"},
		{kind: message.Assistant, tool: "", text: "reply"},
		{kind: message.ToolRequest, tool: "read_file", text: `{"path":"example.txt"}`},
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
		{kind: message.ToolRequest, tool: "bash", text: `{"cmd":"ls"}`},
	}, inputs)
}

func TestUnknownContentBlocksDoNotBreakParsing(t *testing.T) {
	t.Parallel()
	inputs, err := policyInputs([]Message{{Role: "user", Content: json.RawMessage(`[{"type":"future","content":[{"unknown":true}]},{"type":"text","text":"known"}]`)}})
	require.NoError(t, err)
	require.Equal(t, []policyInput{{kind: message.User, tool: "", text: "known"}}, inputs)
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
			service := &Service{store: store, scanner: scanner}
			verdict, err := service.Process(t.Context(), Config{ID: "example", OrganizationID: "org_example", ProjectID: uuid.New(), TenantID: "tenant-example", SigningSecrets: nil}, exampleFrame())
			require.NoError(t, err)
			require.Equal(t, "deny", verdict.Action)
		})
	}
}
