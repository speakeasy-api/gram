package relay

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/speakeasy-api/agenthooks/agenthookstest"
	"github.com/stretchr/testify/require"
)

// transcriptServer records transcript uploads and counts every request, so a
// test can prove that nothing besides the upload reached the server.
type transcriptServer struct {
	*httptest.Server
	mu       sync.Mutex
	uploads  []transcriptUpload
	headers  []http.Header
	requests atomic.Int32
	status   func(n int) int
}

func newTranscriptServer(t *testing.T) *transcriptServer {
	t.Helper()
	ts := &transcriptServer{status: func(int) int { return http.StatusOK }}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(ts.requests.Add(1))
		if r.URL.Path != transcriptUploadPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gz, err := gzip.NewReader(r.Body)
		require.NoError(t, err)
		body, err := io.ReadAll(gz)
		require.NoError(t, err)
		var upload transcriptUpload
		require.NoError(t, json.Unmarshal(body, &upload))
		ts.mu.Lock()
		ts.uploads = append(ts.uploads, upload)
		ts.headers = append(ts.headers, r.Header.Clone())
		ts.mu.Unlock()
		w.WriteHeader(ts.status(n))
	}))
	t.Cleanup(ts.Close)
	return ts
}

func (ts *transcriptServer) recorded() ([]transcriptUpload, []http.Header) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return append([]transcriptUpload(nil), ts.uploads...), append([]http.Header(nil), ts.headers...)
}

// claudeTranscriptFile creates a transcript under a temporary Claude config
// root, where validateClaudeTranscriptPath accepts it.
func claudeTranscriptFile(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", root)
	dir := filepath.Join(root, "projects", "work-repo")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	return filepath.Join(dir, "sess-cowork-1.jsonl")
}

func appendTranscript(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString(text)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func transcriptOnlyConfig(t *testing.T, serverURL string) Config {
	t.Helper()
	cfg := authedConfig(t, serverURL)
	cfg.TranscriptOnly = true
	return cfg
}

func invokeClaude(t *testing.T, cfg Config, payload map[string]any) agenthookstest.Result {
	t.Helper()
	t.Setenv("SPEAKEASY_AI_DEVICE_AGENT_COMMANDS", "speakeasy-hooks-test-missing-device-agent")
	b, err := json.Marshal(payload)
	require.NoError(t, err)
	return agenthookstest.Invoke(t, NewRunner(cfg), agenthooks.ProviderClaudeCode, b, "--variant=cli")
}

func claudeStop(transcript string) map[string]any {
	return map[string]any{
		"session_id":       "sess-cowork-1",
		"transcript_path":  transcript,
		"cwd":              "/work/repo",
		"hook_event_name":  "Stop",
		"stop_hook_active": false,
	}
}

func entryUUIDs(t *testing.T, upload transcriptUpload) []string {
	t.Helper()
	ids := make([]string, len(upload.Entries))
	for i, raw := range upload.Entries {
		var entry struct {
			UUID string `json:"uuid"`
		}
		require.NoError(t, json.Unmarshal(raw, &entry))
		ids[i] = entry.UUID
	}
	return ids
}

const (
	transcriptUser      = `{"type":"user","uuid":"u-1","message":{"role":"user","content":"list the files"}}` + "\n"
	transcriptBookkeep  = `{"type":"api-request-blob","hash":"h","message":{"role":"system","content":[]}}` + "\n"
	transcriptAssistant = `{"type":"assistant","uuid":"a-1","message":{"id":"msg_1","role":"assistant","content":[{"type":"text","text":"Two files."}]}}` + "\n"
	transcriptLater     = `{"type":"user","uuid":"u-2","message":{"role":"user","content":"thanks"}}` + "\n"
)

func TestTranscriptOnlyShipsNewLinesOnEachStop(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	cfg := transcriptOnlyConfig(t, ts.URL)
	transcript := claudeTranscriptFile(t)

	// The last line is still being written: no trailing newline.
	partial := `{"type":"assistant","uuid":"a-2","message":{"id":"msg_2","role":"assistant","content":[{"type":"text","text":"Done"}]}}`
	appendTranscript(t, transcript, transcriptUser+transcriptBookkeep+transcriptAssistant+partial)

	res := invokeClaude(t, cfg, claudeStop(transcript))
	require.Equal(t, 0, res.ExitCode, res.Stderr)

	uploads, headers := ts.recorded()
	require.Len(t, uploads, 1)
	first := uploads[0]
	require.Equal(t, transcriptSchemaV1, first.SchemaVersion)
	require.Equal(t, "sess-cowork-1", first.SessionID)
	require.Equal(t, "claude", first.Adapter)
	require.Equal(t, "/work/repo", first.Cwd)
	require.Equal(t, int64(0), first.OffsetStart)
	require.Equal(t, int64(len(transcriptUser+transcriptBookkeep+transcriptAssistant)), first.OffsetEnd)
	require.Equal(t, []string{"u-1", "a-1"}, entryUUIDs(t, first), "bookkeeping entries and the partial line stay behind")
	require.Equal(t, "test-hooks-key", headers[0].Get("Gram-Key"))
	require.Equal(t, "default", headers[0].Get("Gram-Project"))
	require.Equal(t, "gzip", headers[0].Get("Content-Encoding"))

	// The partial line completes and another turn lands: only those ship.
	appendTranscript(t, transcript, "\n"+transcriptLater)
	invokeClaude(t, cfg, claudeStop(transcript))
	uploads, _ = ts.recorded()
	require.Len(t, uploads, 2)
	require.Equal(t, first.OffsetEnd, uploads[1].OffsetStart)
	require.Equal(t, []string{"a-2", "u-2"}, entryUUIDs(t, uploads[1]))

	// Nothing new: no request at all.
	invokeClaude(t, cfg, claudeStop(transcript))
	require.Equal(t, int32(2), ts.requests.Load())
}

func TestTranscriptOnlyRetriesUnsettledUpload(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	ts.status = func(n int) int {
		if n == 1 {
			return http.StatusServiceUnavailable
		}
		return http.StatusOK
	}
	cfg := transcriptOnlyConfig(t, ts.URL)
	transcript := claudeTranscriptFile(t)
	appendTranscript(t, transcript, transcriptUser+transcriptAssistant)

	invokeClaude(t, cfg, claudeStop(transcript))
	invokeClaude(t, cfg, claudeStop(transcript))
	invokeClaude(t, cfg, claudeStop(transcript))

	uploads, _ := ts.recorded()
	require.Len(t, uploads, 2, "the failed chunk is resent once, then settled")
	require.Equal(t, entryUUIDs(t, uploads[0]), entryUUIDs(t, uploads[1]))
}

func TestTranscriptOnlyRelaysNoOtherEvent(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	cfg := transcriptOnlyConfig(t, ts.URL)
	transcript := claudeTranscriptFile(t)
	appendTranscript(t, transcript, transcriptUser)

	for _, payload := range []map[string]any{
		{"session_id": "sess-cowork-1", "transcript_path": transcript, "cwd": "/work/repo", "hook_event_name": "UserPromptSubmit", "prompt": "list the files"},
		{"session_id": "sess-cowork-1", "transcript_path": transcript, "cwd": "/work/repo", "hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}, "tool_use_id": "toolu_1"},
		{"session_id": "sess-cowork-1", "transcript_path": transcript, "cwd": "/work/repo", "hook_event_name": "SessionStart", "source": "startup"},
		{"session_id": "sess-cowork-1", "transcript_path": transcript, "cwd": "/work/repo", "hook_event_name": "SubagentStop", "stop_hook_active": false},
	} {
		res := invokeClaude(t, cfg, payload)
		require.Equal(t, 0, res.ExitCode, res.Stderr)
	}
	require.Equal(t, int32(0), ts.requests.Load())
}

func TestTranscriptShippingOffWithoutFlag(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	cfg := authedConfig(t, ts.URL)
	transcript := claudeTranscriptFile(t)
	appendTranscript(t, transcript, transcriptUser)

	invokeClaude(t, cfg, claudeStop(transcript))
	uploads, _ := ts.recorded()
	require.Empty(t, uploads, "the default distribution never uploads transcripts")
}

func TestTranscriptUploadSettled(t *testing.T) {
	t.Parallel()
	for status, settled := range map[int]bool{
		0:                                        false,
		http.StatusOK:                            true,
		http.StatusBadRequest:                    true,
		http.StatusConflict:                      true,
		http.StatusRequestEntityTooLarge:         true,
		http.StatusUnauthorized:                  false,
		http.StatusForbidden:                     false,
		http.StatusRequestTimeout:                false,
		http.StatusTooManyRequests:               false,
		http.StatusInternalServerError:           false,
		http.StatusServiceUnavailable:            false,
		http.StatusTemporaryRedirect:             false,
		http.StatusNetworkAuthenticationRequired: false,
	} {
		require.Equal(t, settled, transcriptUploadSettled(status), status)
	}
}

func TestReadTranscriptChunkRestartsWhenFileShrinks(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "t.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(transcriptUser), 0o600))

	chunk, err := readTranscriptChunk(path, 10_000)
	require.NoError(t, err)
	require.Equal(t, int64(0), chunk.start)
	require.Len(t, chunk.entries, 1)
}

func TestSplitInlineFlagsReadsTranscriptOnly(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "speakeasy.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"server_url":"https://app.example.test","project":"default","transcript_only":true}`), 0o600))
	cfg, _ := SplitInlineFlags(Config{}, []string{"--config=" + path})
	require.True(t, cfg.TranscriptOnly)

	b, err := json.Marshal(FileConfig{ServerURL: "https://app.example.test", Project: "default"})
	require.NoError(t, err)
	require.NotContains(t, string(b), "transcript_only", "the flag is omitted unless set, so other distributions render unchanged")
}
