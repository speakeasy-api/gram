package relay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParsePromptAttachments_ChainedParentsFileAndDirectory(t *testing.T) {
	t.Parallel()

	transcript := strings.Join([]string{
		`{"type":"user","uuid":"user-1","promptId":"prompt-1","message":{"role":"user","content":"  read @marker.txt  \n"}}`,
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
	require.Equal(t, promptTextSHA256("read @marker.txt"), *got[0].entry.PromptSha256)
	require.Equal(t, "/repo/marker.txt", *got[0].entry.FilePath)
	require.Equal(t, "marker.txt", *got[0].entry.DisplayPath)
	require.Equal(t, "file", got[0].entry.AttachmentKind)
	require.Equal(t, "MARKER_abc123\n", got[0].entry.Content)
	require.Equal(t, int64(2000), *got[0].entry.NumLines)
	require.Equal(t, int64(1), *got[0].entry.StartLine)
	require.Equal(t, int64(3001), *got[0].entry.TotalLines)

	require.Equal(t, "dir-1", got[1].entry.EntryUUID)
	require.Equal(t, "prompt-1", *got[1].entry.PromptID)
	require.Equal(t, promptTextSHA256("read @marker.txt"), *got[1].entry.PromptSha256)
	require.Equal(t, "/repo/subdir", *got[1].entry.FilePath)
	require.Equal(t, "subdir/", *got[1].entry.DisplayPath)
	require.Equal(t, "directory", got[1].entry.AttachmentKind)
	require.Equal(t, "inner.txt", got[1].entry.Content)
}

func TestParsePromptAttachments_HighWaterSkipsOldAttachments(t *testing.T) {
	t.Parallel()

	first := `{"type":"user","uuid":"user-1","promptId":"prompt-1","message":{"role":"user","content":[{"type":"text","text":"first line"},{"type":"text","text":"second line"}]}}` + "\n"
	second := `{"type":"attachment","uuid":"old","parentUuid":"user-1","attachment":{"type":"file","content":{"type":"text","file":{"content":"old"}}}}` + "\n"
	third := `{"type":"attachment","uuid":"new","parentUuid":"user-1","attachment":{"type":"file","content":{"type":"text","file":{"content":"new"}}}}` + "\n"

	got, _, err := parsePromptAttachments(strings.NewReader(first+second+third), int64(len(first)+len(second)))
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "new", got[0].entry.EntryUUID)
	require.Equal(t, "prompt-1", *got[0].entry.PromptID)
	require.Equal(t, promptTextSHA256("first line\nsecond line"), *got[0].entry.PromptSha256)
}

func TestParsePromptAttachments_DoesNotAdvancePastUnterminatedAttachment(t *testing.T) {
	t.Parallel()

	user := `{"type":"user","uuid":"user-1","promptId":"prompt-1","message":{"role":"user","content":"read @marker.txt"}}` + "\n"
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
	require.Equal(t, promptTextSHA256("read @marker.txt"), *got[0].entry.PromptSha256)
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

func TestParsePromptAttachmentsFile_RejectsOutOfRootTranscriptPath(t *testing.T) {
	configRoot := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(configRoot, "projects"), 0o700))
	t.Setenv("CLAUDE_CONFIG_DIR", configRoot)

	outside := filepath.Join(t.TempDir(), "transcript.jsonl")
	require.NoError(t, os.WriteFile(outside, []byte("{}\n"), 0o600))

	_, _, err := parsePromptAttachmentsFile(outside, 0)
	require.ErrorContains(t, err, "outside claude transcript root")
}

func TestParsePromptAttachmentsFile_RejectsTraversalTranscriptPath(t *testing.T) {
	configRoot := t.TempDir()
	sessionDir := filepath.Join(configRoot, "projects", "session")
	require.NoError(t, os.MkdirAll(sessionDir, 0o700))
	t.Setenv("CLAUDE_CONFIG_DIR", configRoot)

	transcript := filepath.Join(sessionDir, "transcript.jsonl")
	require.NoError(t, os.WriteFile(transcript, []byte("{}\n"), 0o600))
	traversal := filepath.Join(configRoot, "projects", "session") + string(os.PathSeparator) + ".." + string(os.PathSeparator) + "session" + string(os.PathSeparator) + "transcript.jsonl"

	_, _, err := parsePromptAttachmentsFile(traversal, 0)
	require.ErrorContains(t, err, "traversal")
}

func TestParsePromptAttachmentsFile_RejectsSymlinkTranscriptPath(t *testing.T) {
	configRoot := t.TempDir()
	sessionDir := filepath.Join(configRoot, "projects", "session")
	require.NoError(t, os.MkdirAll(sessionDir, 0o700))
	t.Setenv("CLAUDE_CONFIG_DIR", configRoot)

	target := filepath.Join(sessionDir, "transcript.jsonl")
	require.NoError(t, os.WriteFile(target, []byte("{}\n"), 0o600))
	link := filepath.Join(sessionDir, "linked.jsonl")
	require.NoError(t, os.Symlink(target, link))

	_, _, err := parsePromptAttachmentsFile(link, 0)
	require.ErrorContains(t, err, "symlink")
}
