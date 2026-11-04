package relay

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/speakeasy-api/agenthooks/agenthookstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// transcriptServerMaxBodyBytes is the server's request body cap
// (claudeTranscriptMaxBodyBytes in server/internal/hooks). The test server
// applies it to the decompressed body, since the binary's line cap promises
// an upload fits even uncompressed.
const transcriptServerMaxBodyBytes = 10 << 20

// transcriptServer records transcript uploads and counts every request, so a
// test can prove that nothing besides the upload reached the server.
type transcriptServer struct {
	*httptest.Server
	requests atomic.Int32
	// mu guards everything below: the handler goroutine reads the responders
	// while a test swaps them between requests.
	mu      sync.Mutex
	uploads []transcriptUpload
	headers []http.Header
	status  func(n int) int
	// body, when set, replaces the endpoint's JSON acknowledgement.
	body func(n int) []byte
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
		// assert, not require: FailNow must not run off the test goroutine.
		gz, err := gzip.NewReader(r.Body)
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		body, err := io.ReadAll(gz)
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if len(body) > transcriptServerMaxBodyBytes {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		var upload transcriptUpload
		if !assert.NoError(t, json.Unmarshal(body, &upload)) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		ts.mu.Lock()
		ts.uploads = append(ts.uploads, upload)
		ts.headers = append(ts.headers, r.Header.Clone())
		statusFn, bodyFn := ts.status, ts.body
		ts.mu.Unlock()
		status := statusFn(n)
		if bodyFn != nil {
			w.WriteHeader(status)
			_, _ = w.Write(bodyFn(n))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]int{"stored": len(upload.Entries)})
	}))
	t.Cleanup(ts.Close)
	return ts
}

func (ts *transcriptServer) recorded() ([]transcriptUpload, []http.Header) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return append([]transcriptUpload(nil), ts.uploads...), append([]http.Header(nil), ts.headers...)
}

func (ts *transcriptServer) setStatus(status func(n int) int) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.status = status
}

func (ts *transcriptServer) setBody(body func(n int) []byte) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.body = body
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
	ts.setStatus(func(n int) int {
		if n == 1 {
			return http.StatusServiceUnavailable
		}
		return http.StatusOK
	})
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

// TestTranscriptOnlyRecordsRestartOfReplacedFile: once a file shorter than the
// stored offset restarts shipping from zero, a failed upload must not leave
// the old offset behind, or the retry would resume mid-file after the
// replacement grows past it.
func TestTranscriptOnlyRecordsRestartOfReplacedFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	ts.setStatus(func(n int) int {
		if n == 1 {
			return http.StatusServiceUnavailable
		}
		return http.StatusOK
	})
	cfg := transcriptOnlyConfig(t, ts.URL)
	transcript := claudeTranscriptFile(t)
	appendTranscript(t, transcript, transcriptUser)
	// The marker is keyed by the validated path, as shipTranscript sees it.
	validated, err := validateClaudeTranscriptPath(transcript)
	require.NoError(t, err)
	marker := transcriptHighWaterPath(validated)
	require.NoError(t, os.MkdirAll(filepath.Dir(marker), 0o700))
	writeTranscriptHighWater(marker, 10_000)

	invokeClaude(t, cfg, claudeStop(transcript))
	require.Equal(t, int64(0), readTranscriptHighWater(marker), "the restart is recorded although the upload failed")
	appendTranscript(t, transcript, paddedTranscriptLine("user", "big", 20_000, "x")+transcriptLater)
	invokeClaude(t, cfg, claudeStop(transcript))

	uploads, _ := ts.recorded()
	require.Len(t, uploads, 2)
	require.Equal(t, []string{"u-1", "big", "u-2"}, entryUUIDs(t, uploads[1]))
}

// TestTranscriptOnlyRecordsRestartOfUnfinishedReplacement: a replacement
// holding only a line still being written ships nothing, but its restart must
// still be recorded, or once that line grows past the old offset the next Stop
// would resume inside it and skip it.
func TestTranscriptOnlyRecordsRestartOfUnfinishedReplacement(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	cfg := transcriptOnlyConfig(t, ts.URL)
	transcript := claudeTranscriptFile(t)
	shipped := transcriptUser + transcriptAssistant
	appendTranscript(t, transcript, shipped)
	invokeClaude(t, cfg, claudeStop(transcript))

	// The replacement's first line ends past the old offset.
	first := paddedTranscriptLine("user", "r-1", 2*len(shipped), "x")
	cut := len(shipped) / 2
	require.NoError(t, os.WriteFile(transcript, []byte(first[:cut]), 0o600))
	invokeClaude(t, cfg, claudeStop(transcript))
	appendTranscript(t, transcript, first[cut:]+transcriptLater)
	invokeClaude(t, cfg, claudeStop(transcript))

	uploads, _ := ts.recorded()
	require.Len(t, uploads, 2)
	require.Equal(t, []string{"r-1", "u-2"}, entryUUIDs(t, uploads[1]))
}

// TestTranscriptOnlyKeepsChunkWithoutAcknowledgement: a 200 that is not the
// endpoint's acknowledgement, such as a single-page app answering an
// unrouted path, must not move the offset.
func TestTranscriptOnlyKeepsChunkWithoutAcknowledgement(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	ts.setBody(func(int) []byte { return []byte("<!doctype html><html></html>") })
	cfg := transcriptOnlyConfig(t, ts.URL)
	transcript := claudeTranscriptFile(t)
	appendTranscript(t, transcript, transcriptUser)

	invokeClaude(t, cfg, claudeStop(transcript))
	ts.setBody(nil)
	invokeClaude(t, cfg, claudeStop(transcript))
	invokeClaude(t, cfg, claudeStop(transcript))

	uploads, _ := ts.recorded()
	require.Len(t, uploads, 2, "the unacknowledged chunk is resent, then settled")
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
		http.StatusNotFound:                      false,
		http.StatusMethodNotAllowed:              false,
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

// paddedTranscriptLine returns a transcript line of exactly size bytes,
// newline included, whose content is fill repeated.
func paddedTranscriptLine(entryType, uuid string, size int, fill string) string {
	head := `{"type":"` + entryType + `","uuid":"` + uuid + `","message":{"role":"user","content":"`
	tail := `"}}` + "\n"
	return head + strings.Repeat(fill, size-len(head)-len(tail)) + tail
}

// readTranscriptChunks reads path chunk by chunk from offset until a read
// makes no progress.
func readTranscriptChunks(t *testing.T, path string, offset int64) []transcriptChunk {
	t.Helper()
	var chunks []transcriptChunk
	for {
		chunk, err := readTranscriptChunk(path, offset)
		require.NoError(t, err)
		if chunk.end == chunk.start {
			return chunks
		}
		chunks = append(chunks, chunk)
		offset = chunk.end
	}
}

// bookkeepingRun returns n chunk budgets' worth of 64 KiB bookkeeping lines.
func bookkeepingRun(n int) string {
	line := paddedTranscriptLine("api-request-blob", "b", 64<<10, "x")
	return strings.Repeat(line, n*transcriptChunkBytes/len(line))
}

func chunkEntryUUIDs(t *testing.T, chunks []transcriptChunk) []string {
	t.Helper()
	var entries []json.RawMessage
	for _, chunk := range chunks {
		entries = append(entries, chunk.entries...)
	}
	return entryUUIDs(t, transcriptUpload{Entries: entries})
}

// TestReadTranscriptChunkCountsSkippedLines: skipped bookkeeping lines count
// toward the chunk budget, so a long run of them is crossed in bounded chunks
// that carry nothing.
func TestReadTranscriptChunkCountsSkippedLines(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "t.jsonl")
	bookkeeping := bookkeepingRun(3)
	require.NoError(t, os.WriteFile(path, []byte(bookkeeping+transcriptUser), 0o600))

	chunks := readTranscriptChunks(t, path, 0)
	require.Len(t, chunks, 4)
	for _, chunk := range chunks {
		require.LessOrEqual(t, chunk.end-chunk.start, int64(transcriptChunkBytes))
	}
	require.Equal(t, []string{"u-1"}, chunkEntryUUIDs(t, chunks))
	require.Len(t, chunks[3].entries, 1, "only the last chunk carries an entry")
	require.Equal(t, int64(len(bookkeeping)+len(transcriptUser)), chunks[3].end)
}

// TestReadTranscriptChunkStopsAtServerEntryCap: a chunk never carries more
// entries than the server accepts in one upload.
func TestReadTranscriptChunkStopsAtServerEntryCap(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "t.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat(`{"type":"user","uuid":"u"}`+"\n", transcriptMaxEntries+1)), 0o600))

	chunks := readTranscriptChunks(t, path, 0)
	require.Len(t, chunks, 2)
	require.Len(t, chunks[0].entries, transcriptMaxEntries)
	require.Len(t, chunks[1].entries, 1)
}

// TestReadTranscriptChunkDefersEntryPastBudget: an entry that would push a
// chunk already carrying entries past the budget starts the next chunk.
func TestReadTranscriptChunkDefersEntryPastBudget(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "t.jsonl")
	content := paddedTranscriptLine("api-request-blob", "b", 3<<20, "x") +
		paddedTranscriptLine("user", "a", 512<<10, "x") +
		paddedTranscriptLine("user", "c", 1<<20, "x")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	chunks := readTranscriptChunks(t, path, 0)
	require.Len(t, chunks, 2)
	require.Equal(t, []string{"a"}, chunkEntryUUIDs(t, chunks[:1]))
	require.Equal(t, []string{"c"}, chunkEntryUUIDs(t, chunks[1:]))
}

func TestReadTranscriptChunkSkipsOversizedLine(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "t.jsonl")
	content := transcriptUser + paddedTranscriptLine("user", "big", transcriptMaxLineBytes+1, "x") + transcriptLater
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	chunks := readTranscriptChunks(t, path, 0)
	require.Equal(t, []string{"u-1", "u-2"}, chunkEntryUUIDs(t, chunks))
	require.Equal(t, int64(len(content)), chunks[len(chunks)-1].end)
}

// TestReadTranscriptChunkHoldsBackUnterminatedLongLine: an oversized line
// that is still being written keeps the offset in front of it until its
// newline lands, and is then skipped.
func TestReadTranscriptChunkHoldsBackUnterminatedLongLine(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "t.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(transcriptUser+strings.Repeat("x", transcriptMaxLineBytes+1)), 0o600))

	chunks := readTranscriptChunks(t, path, 0)
	require.Equal(t, []string{"u-1"}, chunkEntryUUIDs(t, chunks))
	require.Equal(t, int64(len(transcriptUser)), chunks[len(chunks)-1].end)

	appendTranscript(t, path, "\n"+transcriptLater)
	chunks = readTranscriptChunks(t, path, int64(len(transcriptUser)))
	require.Equal(t, []string{"u-2"}, chunkEntryUUIDs(t, chunks))
}

// TestTranscriptOnlyCrossesBookkeepingWithoutUploading: chunks holding only
// bookkeeping lines advance the offset without a request.
func TestTranscriptOnlyCrossesBookkeepingWithoutUploading(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	cfg := transcriptOnlyConfig(t, ts.URL)
	transcript := claudeTranscriptFile(t)
	bookkeeping := bookkeepingRun(2)
	appendTranscript(t, transcript, bookkeeping+transcriptUser)

	invokeClaude(t, cfg, claudeStop(transcript))
	uploads, _ := ts.recorded()
	require.Len(t, uploads, 1)
	require.Equal(t, int64(len(bookkeeping)), uploads[0].OffsetStart)
	require.Equal(t, []string{"u-1"}, entryUUIDs(t, uploads[0]))
}

// TestTranscriptOnlyMaxLineFitsServerBodyCap: the longest line the binary
// ships, inside its envelope, stays under the server's body cap even
// uncompressed. The line is all <, which HTML escaping would grow sixfold.
func TestTranscriptOnlyMaxLineFitsServerBodyCap(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	cfg := transcriptOnlyConfig(t, ts.URL)
	transcript := claudeTranscriptFile(t)
	appendTranscript(t, transcript, paddedTranscriptLine("user", "big", transcriptMaxLineBytes, "<"))

	invokeClaude(t, cfg, claudeStop(transcript))
	uploads, _ := ts.recorded()
	require.Len(t, uploads, 1, "the test server rejects a body over the cap")
	require.Equal(t, []string{"big"}, entryUUIDs(t, uploads[0]))
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

// TestLoadConfigIgnoresTranscriptOnlyEnv: transcript-only mode turns policy
// enforcement off, so only the packaged speakeasy.json may enable it.
func TestLoadConfigIgnoresTranscriptOnlyEnv(t *testing.T) {
	for _, name := range []string{"SPEAKEASY_AI_HOOKS_TRANSCRIPT_ONLY", "GRAM_HOOKS_TRANSCRIPT_ONLY"} {
		t.Setenv(name, "true")
	}
	require.False(t, LoadConfig(Config{}).TranscriptOnly)
}

// TestWritePluginBakesTranscriptOnly: `speakeasy-hooks install
// --transcript-only` is how local testing reaches the Cowork mode, since no
// environment variable can switch it on.
func TestWritePluginBakesTranscriptOnly(t *testing.T) {
	t.Parallel()
	for _, transcriptOnly := range []bool{true, false} {
		dir := t.TempDir()
		require.NoError(t, WritePlugin(t.Context(), "claude", dir, PluginConfig{
			ServerURL:      "https://gram.test",
			ProjectSlug:    "default",
			BinaryPath:     "/tmp/speakeasy-hooks",
			TranscriptOnly: transcriptOnly,
		}))
		fc, err := readFileConfig(filepath.Join(dir, configFileName))
		require.NoError(t, err)
		require.Equal(t, transcriptOnly, fc.TranscriptOnly)
	}
}

// TestWritePluginRejectsTranscriptOnlyForOtherProviders: transcript shipping
// reads only Claude Code's transcript, so any other provider is refused before
// a credential-bearing speakeasy.json is written.
func TestWritePluginRejectsTranscriptOnlyForOtherProviders(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"cursor", "codex", "opencode", "copilot", "openclaw", "pi"} {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			err := WritePlugin(t.Context(), provider, dir, PluginConfig{
				ServerURL:      "https://gram.test",
				ProjectSlug:    "default",
				HooksAPIKey:    "shared-key",
				BinaryPath:     "/tmp/speakeasy-hooks",
				TranscriptOnly: true,
			})
			require.ErrorIs(t, err, ErrTranscriptOnlyProvider)
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}
