package relay

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParsePromptAttachments_ChainedParentsFileAndDirectory(t *testing.T) {
	t.Parallel()

	transcript := strings.Join([]string{
		`{"type":"user","uuid":"user-1","promptId":"prompt-1"}`,
		`{"type":"attachment","uuid":"file-1","parentUuid":"user-1","timestamp":"2026-07-22T09:38:49.652Z","attachment":{"type":"file","filename":"/repo/marker.txt","displayPath":"marker.txt","content":{"type":"text","file":{"filePath":"/repo/marker.txt","content":"MARKER_abc123\n","numLines":2000,"startLine":1,"totalLines":3001}}}}`,
		`{"type":"attachment","uuid":"dir-1","parentUuid":"file-1","timestamp":"2026-07-22T09:38:50.000Z","attachment":{"type":"directory","path":"/repo/subdir","displayPath":"subdir/","content":"inner.txt"}}`,
		`not-json`,
		`{"type":"attachment","uuid":"noise-1","parentUuid":"user-1","attachment":{"type":"skill_listing","content":"ignored"}}`,
	}, "\n") + "\n"

	got, nextOffset, err := parsePromptAttachments(strings.NewReader(transcript), 0)
	require.NoError(t, err)
	require.Equal(t, int64(len(transcript)), nextOffset)
	require.Len(t, got, 2)

	require.Equal(t, "file-1", got[0].entry.EntryUUID)
	require.Equal(t, "prompt-1", *got[0].entry.PromptID)
	require.Equal(t, "/repo/marker.txt", *got[0].entry.FilePath)
	require.Equal(t, "marker.txt", *got[0].entry.DisplayPath)
	require.Equal(t, "file", got[0].entry.AttachmentKind)
	require.Equal(t, "MARKER_abc123\n", got[0].entry.Content)
	require.Equal(t, int64(2000), *got[0].entry.NumLines)
	require.Equal(t, int64(1), *got[0].entry.StartLine)
	require.Equal(t, int64(3001), *got[0].entry.TotalLines)

	require.Equal(t, "dir-1", got[1].entry.EntryUUID)
	require.Equal(t, "prompt-1", *got[1].entry.PromptID)
	require.Equal(t, "/repo/subdir", *got[1].entry.FilePath)
	require.Equal(t, "subdir/", *got[1].entry.DisplayPath)
	require.Equal(t, "directory", got[1].entry.AttachmentKind)
	require.Equal(t, "inner.txt", got[1].entry.Content)
}

func TestParsePromptAttachments_HighWaterSkipsOldAttachments(t *testing.T) {
	t.Parallel()

	first := `{"type":"user","uuid":"user-1","promptId":"prompt-1"}` + "\n"
	second := `{"type":"attachment","uuid":"old","parentUuid":"user-1","attachment":{"type":"file","content":{"type":"text","file":{"content":"old"}}}}` + "\n"
	third := `{"type":"attachment","uuid":"new","parentUuid":"user-1","attachment":{"type":"file","content":{"type":"text","file":{"content":"new"}}}}` + "\n"

	got, _, err := parsePromptAttachments(strings.NewReader(first+second+third), int64(len(first)+len(second)))
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "new", got[0].entry.EntryUUID)
	require.Equal(t, "prompt-1", *got[0].entry.PromptID)
}

func TestParsePromptAttachments_DoesNotAdvancePastUnterminatedAttachment(t *testing.T) {
	t.Parallel()

	user := `{"type":"user","uuid":"user-1","promptId":"prompt-1"}` + "\n"
	attachment := `{"type":"attachment","uuid":"file-1","parentUuid":"user-1","attachment":{"type":"file","content":{"type":"text","file":{"content":"MARKER_abc123"}}}}`

	got, nextOffset, err := parsePromptAttachments(strings.NewReader(user+attachment), 0)
	require.NoError(t, err)
	require.Empty(t, got)
	require.Equal(t, int64(len(user)), nextOffset)

	got, nextOffset, err = parsePromptAttachments(strings.NewReader(user+attachment+"\n"), nextOffset)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, int64(len(user)+len(attachment)+1), nextOffset)
	require.Equal(t, "file-1", got[0].entry.EntryUUID)
	require.Equal(t, "prompt-1", *got[0].entry.PromptID)
	require.Equal(t, "MARKER_abc123", got[0].entry.Content)
}

func TestParsePromptAttachments_UnresolvedParentKeepsAttachment(t *testing.T) {
	t.Parallel()

	transcript := `{"type":"attachment","uuid":"file-1","parentUuid":"missing","attachment":{"type":"file","content":{"type":"text","file":{"content":"MARKER_abc123"}}}}` + "\n"

	got, _, err := parsePromptAttachments(strings.NewReader(transcript), 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "file-1", got[0].entry.EntryUUID)
	require.Nil(t, got[0].entry.PromptID)
}
