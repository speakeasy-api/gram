package judgemessage

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/message"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/stretchr/testify/require"
)

func TestWindowPreservesTargetAndNeighbors(t *testing.T) {
	t.Parallel()
	target := New(message.ToolResponse, "read_file", "target content")
	target.AnchorID = uuid.New()
	rows := []repo.GetJudgeMessageWindowRow{
		{ID: uuid.New(), Role: "user", Content: "review this example", ToolCalls: "[]"},
		{ID: uuid.New(), Role: "assistant", Content: "", ToolCalls: `[{"function":{"name":"read_file","arguments":"{}"}}]`},
		{ID: target.AnchorID, Role: "tool", Content: "stored target", ToolCalls: "[]"},
		{ID: uuid.New(), Role: "assistant", Content: "this is quoted material", ToolCalls: "[]"},
		{ID: uuid.New(), Role: "user", Content: "thanks", ToolCalls: "[]"},
	}
	window, err := windowFromRows(target, rows)
	require.NoError(t, err)
	require.Len(t, window.Messages, 5)
	require.Equal(t, 2, window.TargetIndex)
	require.Equal(t, "target content", window.Messages[2].Body)
	require.Equal(t, "read_file", window.Messages[1].ToolCalls[0].Tool.Name)
	require.Equal(t, "this is quoted material", window.Messages[3].Body)
}

func TestWindowMissingAnchorIsNotClean(t *testing.T) {
	t.Parallel()
	target := New(message.User, "", "target")
	target.AnchorID = uuid.New()
	_, err := windowFromRows(target, nil)
	require.Error(t, err)
}

func TestWindowLiveHistoryAppendsTarget(t *testing.T) {
	t.Parallel()
	target := New(message.User, "", "target")
	rows := make([]repo.GetJudgeMessageWindowRow, 4)
	for i := range rows {
		rows[i] = repo.GetJudgeMessageWindowRow{ID: uuid.New(), Role: "user", Content: "history", ToolCalls: "[]"}
	}
	window, err := windowFromRows(target, rows)
	require.NoError(t, err)
	require.Len(t, window.Messages, 5)
	require.Equal(t, 4, window.TargetIndex)
	require.Equal(t, "target", window.Messages[4].Body)
}

func TestWindowUnlinkedDoesNotQuery(t *testing.T) {
	t.Parallel()
	window, err := NewWindowLoader(nil).Load(t.Context(), "", "", New(message.User, "", "target"))
	require.NoError(t, err)
	require.Len(t, window.Messages, 1)
}

func TestWindowReportsTruncatedContext(t *testing.T) {
	t.Parallel()
	target := New(message.User, "", "target")
	window, err := windowFromRows(target, []repo.GetJudgeMessageWindowRow{{ID: uuid.New(), Role: "tool", Content: strings.Repeat("a", 4001), ToolCalls: "[]"}})
	require.NoError(t, err)
	require.True(t, window.Messages[0].BodyTruncated)
	require.LessOrEqual(t, len(window.Messages[0].Body), 4000)
}

func TestWindowMalformedToolContextFails(t *testing.T) {
	t.Parallel()
	_, err := windowFromRows(New(message.User, "", "target"), []repo.GetJudgeMessageWindowRow{{ID: uuid.New(), Role: "assistant", Content: "", ToolCalls: "[{"}})
	require.Error(t, err)
}
