package intentpolicy_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/intentpolicy"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return b
}

func TestParseTranscriptMessagesDocument(t *testing.T) {
	t.Parallel()

	tr, err := intentpolicy.ParseTranscript(readFixture(t, "messages.json"))
	require.NoError(t, err)
	require.Equal(t, intentpolicy.FormatMessages, tr.Format)

	roles := make([]string, 0, len(tr.Messages))
	for _, m := range tr.Messages {
		roles = append(roles, m.Role)
	}
	require.Equal(t, []string{"user", "assistant", "tool", "tool", "assistant"}, roles)

	assistant := tr.Messages[1]
	require.Equal(t, "Listing files first.", assistant.Content)
	require.Len(t, assistant.ToolCalls, 2)
	require.Equal(t, "toolu_1", assistant.ToolCalls[0].ID)
	require.Equal(t, "Bash", assistant.ToolCalls[0].Name)
	require.JSONEq(t, `{"command":"ls -la"}`, string(assistant.ToolCalls[0].Arguments))
	require.Equal(t, "toolu_2", tr.Messages[3].ToolCallID)
	require.Equal(t, "created #1", tr.Messages[3].Content)
	require.Equal(t, "Done.", tr.Messages[4].Content)
}

func TestParseTranscriptBareMessagesArray(t *testing.T) {
	t.Parallel()

	tr, err := intentpolicy.ParseTranscript([]byte(`[{"role":"user","content":"hi"},{"role":"assistant","content":[{"type":"tool_use","id":"t","name":"Read","input":{"file_path":"a"}}]}]`))
	require.NoError(t, err)
	require.Equal(t, intentpolicy.FormatMessages, tr.Format)
	require.Len(t, tr.Messages, 2)
	require.Equal(t, "Read", tr.Messages[1].ToolCalls[0].Name)
}

func TestParseTranscriptClaudeCode(t *testing.T) {
	t.Parallel()

	tr, err := intentpolicy.ParseTranscript(readFixture(t, "claude-code.jsonl"))
	require.NoError(t, err)
	require.Equal(t, intentpolicy.FormatClaudeCode, tr.Format)
	require.Equal(t, "/work/example", tr.Cwd)
	require.Equal(t, 1, tr.InvalidLines)
	require.Len(t, tr.Messages, 3)
	require.JSONEq(t, `{"command":"go test ./..."}`, string(tr.Messages[1].ToolCalls[0].Arguments))
	require.Equal(t, "toolu_a", tr.Messages[2].ToolCallID)
	require.NotNil(t, tr.Messages[0].Timestamp)
}

func TestParseTranscriptPi(t *testing.T) {
	t.Parallel()

	tr, err := intentpolicy.ParseTranscript(readFixture(t, "pi.jsonl"))
	require.NoError(t, err)
	require.Equal(t, intentpolicy.FormatPi, tr.Format)
	require.Equal(t, "/work/pi", tr.Cwd)
	require.Len(t, tr.Messages, 3)
	require.Equal(t, "bash", tr.Messages[1].ToolCalls[0].Name)
	require.JSONEq(t, `{"command":"ls docs/"}`, string(tr.Messages[1].ToolCalls[0].Arguments))
	require.Equal(t, "tool", tr.Messages[2].Role)
	require.Equal(t, "call_1", tr.Messages[2].ToolCallID)
	require.Equal(t, "a.md", tr.Messages[2].Content)
}

func TestParseTranscriptEmpty(t *testing.T) {
	t.Parallel()

	_, err := intentpolicy.ParseTranscript([]byte("not a transcript\n"))
	require.ErrorIs(t, err, intentpolicy.ErrEmptyTranscript)
}
