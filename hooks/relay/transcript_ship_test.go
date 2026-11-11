package relay

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/speakeasy-api/agenthooks"
	"github.com/speakeasy-api/agenthooks/agenthookstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
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

// respond answers every request with code.
func (ts *transcriptServer) respond(code int) {
	ts.setStatus(func(int) int { return code })
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

// coworkConfig configures the relay as the standard Claude plugin runs inside
// Claude's Cowork VM, which reports Claude Code's Cowork entrypoint.
func coworkConfig(t *testing.T, serverURL string) Config {
	t.Helper()
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "remote_cowork")
	return authedConfig(t, serverURL)
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

func TestCoworkShipsNewLinesOnEachStop(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	cfg := coworkConfig(t, ts.URL)
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
	require.Equal(t, "cowork", first.Adapter)
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

	// Nothing new: no upload. (Per-event hooks run in Cowork too, so a
	// prompt agenthooks backfills from the transcript may reach the ingest
	// endpoint; only transcript uploads are counted here.)
	invokeClaude(t, cfg, claudeStop(transcript))
	uploads, _ = ts.recorded()
	require.Len(t, uploads, 2)
}

func TestCoworkRetriesUnsettledUpload(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	ts.setStatus(func(n int) int {
		if n == 1 {
			return http.StatusServiceUnavailable
		}
		return http.StatusOK
	})
	cfg := coworkConfig(t, ts.URL)
	transcript := claudeTranscriptFile(t)
	appendTranscript(t, transcript, transcriptUser+transcriptAssistant)

	invokeClaude(t, cfg, claudeStop(transcript))
	invokeClaude(t, cfg, claudeStop(transcript))
	invokeClaude(t, cfg, claudeStop(transcript))

	uploads, _ := ts.recorded()
	require.Len(t, uploads, 2, "the failed chunk is resent once, then settled")
	require.Equal(t, entryUUIDs(t, uploads[0]), entryUUIDs(t, uploads[1]))
}

// transcriptMarker is the offset marker shipTranscript keeps for transcript.
func transcriptMarker(t *testing.T, transcript string) string {
	t.Helper()
	// The marker is keyed by the validated path, as shipTranscript sees it.
	validated, err := validateClaudeTranscriptPath(transcript)
	require.NoError(t, err)
	return transcriptHighWaterPath(validated)
}

// TestCoworkDropsBatchAfterRepeatedServerErrors: a batch answered 5xx
// on transcriptMaxServerErrors Stops in a row is dropped, so a batch the
// server can never store does not hold back everything after it. Only what
// the batch held at its first failure is dropped; lines appended since are
// sent again.
func TestCoworkDropsBatchAfterRepeatedServerErrors(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	ts.respond(http.StatusServiceUnavailable)
	cfg := coworkConfig(t, ts.URL)
	transcript := claudeTranscriptFile(t)
	appendTranscript(t, transcript, transcriptUser+transcriptAssistant)
	marker := transcriptMarker(t, transcript)

	invokeClaude(t, cfg, claudeStop(transcript))
	appendTranscript(t, transcript, transcriptLater)
	for range transcriptMaxServerErrors - 2 {
		invokeClaude(t, cfg, claudeStop(transcript))
	}
	require.Equal(t, int64(0), readTranscriptHighWater(marker))
	// The last server error drops the batch, and the line appended after its
	// first failure goes out again as the next batch, which fails once.
	invokeClaude(t, cfg, claudeStop(transcript))
	require.Equal(t, int64(len(transcriptUser+transcriptAssistant)), readTranscriptHighWater(marker))

	ts.respond(http.StatusOK)
	invokeClaude(t, cfg, claudeStop(transcript))
	require.Equal(t, int64(len(transcriptUser+transcriptAssistant+transcriptLater)), readTranscriptHighWater(marker))
	uploads, _ := ts.recorded()
	require.Len(t, uploads, transcriptMaxServerErrors+2)
	require.Equal(t, []string{"u-1", "a-1"}, entryUUIDs(t, uploads[0]))
	for _, upload := range uploads[1:transcriptMaxServerErrors] {
		require.Equal(t, []string{"u-1", "a-1", "u-2"}, entryUUIDs(t, upload))
	}
	for _, upload := range uploads[transcriptMaxServerErrors:] {
		require.Equal(t, []string{"u-2"}, entryUUIDs(t, upload), "only the line after the dropped batch is sent")
	}
}

// TestCoworkCountsServerErrorsPerBatch: a stored batch moves the
// offset, so server errors the batch before it drew do not count against the
// next one.
func TestCoworkCountsServerErrorsPerBatch(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	cfg := coworkConfig(t, ts.URL)
	transcript := claudeTranscriptFile(t)
	appendTranscript(t, transcript, transcriptUser)
	marker := transcriptMarker(t, transcript)

	ts.respond(http.StatusServiceUnavailable)
	for range transcriptMaxServerErrors - 1 {
		invokeClaude(t, cfg, claudeStop(transcript))
	}
	ts.respond(http.StatusOK)
	invokeClaude(t, cfg, claudeStop(transcript))
	require.Equal(t, int64(len(transcriptUser)), readTranscriptHighWater(marker))
	require.NoFileExists(t, transcriptServerErrorsPath(marker), "moving the offset clears the count")

	appendTranscript(t, transcript, transcriptLater)
	ts.respond(http.StatusServiceUnavailable)
	for range transcriptMaxServerErrors - 1 {
		invokeClaude(t, cfg, claudeStop(transcript))
	}
	require.Equal(t, int64(len(transcriptUser)), readTranscriptHighWater(marker), "the next batch starts its own count")
}

// TestCoworkNeverDropsBatchOnRecoverableFailure: credentials, routing,
// and transport failures recover without the batch changing, so they retry
// without limit and break a run of server errors.
func TestCoworkNeverDropsBatchOnRecoverableFailure(t *testing.T) {
	for name, fail := range map[string]func(ts *transcriptServer){
		"unauthorized": func(ts *transcriptServer) {
			ts.respond(http.StatusUnauthorized)
		},
		// A 2xx without the acknowledgement is reported as status 0, like a
		// transport failure.
		"unacknowledged": func(ts *transcriptServer) {
			ts.respond(http.StatusOK)
			ts.setBody(func(int) []byte { return []byte("<!doctype html><html></html>") })
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			ts := newTranscriptServer(t)
			cfg := coworkConfig(t, ts.URL)
			transcript := claudeTranscriptFile(t)
			appendTranscript(t, transcript, transcriptUser)
			marker := transcriptMarker(t, transcript)

			serverError := func() {
				ts.setBody(nil)
				ts.respond(http.StatusInternalServerError)
			}

			fail(ts)
			for range 2 * transcriptMaxServerErrors {
				invokeClaude(t, cfg, claudeStop(transcript))
			}
			require.Equal(t, int64(0), readTranscriptHighWater(marker))

			// Server errors either side of a recoverable failure are not in a row.
			for range transcriptMaxServerErrors - 1 {
				serverError()
				invokeClaude(t, cfg, claudeStop(transcript))
				fail(ts)
				invokeClaude(t, cfg, claudeStop(transcript))
			}
			serverError()
			invokeClaude(t, cfg, claudeStop(transcript))
			require.Equal(t, int64(0), readTranscriptHighWater(marker))
			uploads, _ := ts.recorded()
			require.Len(t, uploads, 4*transcriptMaxServerErrors-1)
		})
	}
}

// TestCoworkRecordsRestartOfReplacedFile: once a file shorter than the
// stored offset restarts shipping from zero, a failed upload must not leave
// the old offset behind, or the retry would resume mid-file after the
// replacement grows past it.
func TestCoworkRecordsRestartOfReplacedFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	ts.setStatus(func(n int) int {
		if n == 1 {
			return http.StatusServiceUnavailable
		}
		return http.StatusOK
	})
	cfg := coworkConfig(t, ts.URL)
	transcript := claudeTranscriptFile(t)
	appendTranscript(t, transcript, transcriptUser)
	marker := transcriptMarker(t, transcript)
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

// TestCoworkRecordsRestartOfUnfinishedReplacement: a replacement
// holding only a line still being written ships nothing, but its restart must
// still be recorded, or once that line grows past the old offset the next Stop
// would resume inside it and skip it.
func TestCoworkRecordsRestartOfUnfinishedReplacement(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	cfg := coworkConfig(t, ts.URL)
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

// TestCoworkKeepsChunkWithoutAcknowledgement: a 200 that is not the
// endpoint's acknowledgement, such as a single-page app answering an
// unrouted path, must not move the offset.
func TestCoworkKeepsChunkWithoutAcknowledgement(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	ts.setBody(func(int) []byte { return []byte("<!doctype html><html></html>") })
	cfg := coworkConfig(t, ts.URL)
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

// TestCoworkRelaysGatedEvents: inside the Cowork VM, prompt, tool and
// permission events reach the server as in Claude Code, so policy is enforced
// in real time. Their envelopes carry transcript_capture so the server keys
// their rows by the ids the shipped transcript shares; the observe-only events
// stay local.
func TestCoworkRelaysGatedEvents(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	fs := newFakeServer(t, func(p components.IngestRequestBody) (int, decision) {
		if p.Event.Type == components.TypeToolRequested {
			return http.StatusOK, decision{Decision: "deny", Reason: "", Message: "Blocked by policy"}
		}
		return http.StatusOK, decision{Decision: "allow", Reason: "", Message: ""}
	})
	cfg := coworkConfig(t, fs.URL)
	transcript := claudeTranscriptFile(t)
	appendTranscript(t, transcript, transcriptUser)

	res := invokeClaude(t, cfg, map[string]any{"session_id": "sess-cowork-1", "transcript_path": transcript, "cwd": "/work/repo", "hook_event_name": "UserPromptSubmit", "prompt": "list the files"})
	require.Equal(t, 0, res.ExitCode, res.Stderr)
	require.NotContains(t, string(res.Stdout), `"decision":"block"`)

	res = invokeClaude(t, cfg, map[string]any{"session_id": "sess-cowork-1", "transcript_path": transcript, "cwd": "/work/repo", "hook_event_name": "PreToolUse", "tool_name": "Bash", "tool_input": map[string]any{"command": "ls"}, "tool_use_id": "toolu_1"})
	require.Equal(t, 0, res.ExitCode, res.Stderr)
	require.Contains(t, string(res.Stdout), `"permissionDecision":"deny"`, "the server's deny is enforced in Cowork")

	for _, payload := range []map[string]any{
		{"session_id": "sess-cowork-1", "transcript_path": transcript, "cwd": "/work/repo", "hook_event_name": "SessionStart", "source": "startup"},
		{"session_id": "sess-cowork-1", "transcript_path": transcript, "cwd": "/work/repo", "hook_event_name": "SubagentStop", "stop_hook_active": false},
	} {
		res := invokeClaude(t, cfg, payload)
		require.Equal(t, 0, res.ExitCode, res.Stderr)
	}

	require.Equal(t, 2, fs.count(), "only the prompt and the tool request reach the server")
	fs.mu.Lock()
	defer fs.mu.Unlock()
	for _, req := range fs.requests {
		require.NotNil(t, req.Session)
		require.Equal(t, true, *req.Session.TranscriptCapture, "Cowork events share their row keys with the transcript")
	}
}

func TestClaudeCodeEventsKeepCapture(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	fs := newFakeServer(t, nil)
	cfg := authedConfig(t, fs.URL)

	res := invokeClaude(t, cfg, map[string]any{"session_id": "sess-cli-1", "cwd": "/work/repo", "hook_event_name": "UserPromptSubmit", "prompt": "list the files"})
	require.Equal(t, 0, res.ExitCode, res.Stderr)
	require.Equal(t, 1, fs.count())
	require.Nil(t, fs.last().Session.TranscriptCapture, "outside Cowork the event is the capture path")
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

// TestReadTranscriptChunkStopsAtBatchEntryCap: a chunk of small entries ends at
// transcriptBatchEntries, long before the byte budget.
func TestReadTranscriptChunkStopsAtBatchEntryCap(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "t.jsonl")
	line := `{"type":"user","uuid":"u"}` + "\n"
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat(line, transcriptBatchEntries+1)), 0o600))

	chunks := readTranscriptChunks(t, path, 0)
	require.Len(t, chunks, 2)
	require.Len(t, chunks[0].entries, transcriptBatchEntries)
	require.Len(t, chunks[1].entries, 1)
	require.Equal(t, int64(transcriptBatchEntries*len(line)), chunks[0].end)
}

// TestCoworkShipsBacklogInBatches: a long transcript, such as a fresh
// VM replaying from offset zero, goes up in contiguous uploads of at most
// transcriptBatchEntries entries within one Stop, each persisting the offset.
func TestCoworkShipsBacklogInBatches(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	cfg := coworkConfig(t, ts.URL)
	transcript := claudeTranscriptFile(t)
	var content strings.Builder
	var want []string
	for i := range 2*transcriptBatchEntries + 7 {
		id := "u-" + strconv.Itoa(i)
		content.WriteString(paddedTranscriptLine("user", id, 128, "x"))
		want = append(want, id)
	}
	appendTranscript(t, transcript, content.String())

	invokeClaude(t, cfg, claudeStop(transcript))
	uploads, _ := ts.recorded()
	require.Len(t, uploads, 3)
	var shipped []string
	for i, upload := range uploads {
		require.Len(t, upload.Entries, []int{transcriptBatchEntries, transcriptBatchEntries, 7}[i])
		if i > 0 {
			require.Equal(t, uploads[i-1].OffsetEnd, upload.OffsetStart)
		}
		shipped = append(shipped, entryUUIDs(t, upload)...)
	}
	require.Equal(t, want, shipped)
	require.Equal(t, int64(content.Len()), uploads[2].OffsetEnd)
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

// TestCoworkCrossesBookkeepingWithoutUploading: chunks holding only
// bookkeeping lines advance the offset without a request.
func TestCoworkCrossesBookkeepingWithoutUploading(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	cfg := coworkConfig(t, ts.URL)
	transcript := claudeTranscriptFile(t)
	bookkeeping := bookkeepingRun(2)
	appendTranscript(t, transcript, bookkeeping+transcriptUser)

	invokeClaude(t, cfg, claudeStop(transcript))
	uploads, _ := ts.recorded()
	require.Len(t, uploads, 1)
	require.Equal(t, int64(len(bookkeeping)), uploads[0].OffsetStart)
	require.Equal(t, []string{"u-1"}, entryUUIDs(t, uploads[0]))
}

// TestCoworkMaxLineFitsServerBodyCap: the longest line the binary
// ships, inside its envelope, stays under the server's body cap even
// uncompressed. The line is all <, which HTML escaping would grow sixfold.
func TestCoworkMaxLineFitsServerBodyCap(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	cfg := coworkConfig(t, ts.URL)
	transcript := claudeTranscriptFile(t)
	appendTranscript(t, transcript, paddedTranscriptLine("user", "big", transcriptMaxLineBytes, "<"))

	invokeClaude(t, cfg, claudeStop(transcript))
	uploads, _ := ts.recorded()
	require.Len(t, uploads, 1, "the test server rejects a body over the cap")
	require.Equal(t, []string{"big"}, entryUUIDs(t, uploads[0]))
}

// TestStopOutsideCoworkRelaysToIngest: outside Claude's Cowork VM, Stop is
// relayed to ingest as before and no transcript is uploaded, even when the
// transcript itself names Cowork.
func TestStopOutsideCoworkRelaysToIngest(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	cfg := authedConfig(t, ts.URL)
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	transcript := claudeTranscriptFile(t)
	appendTranscript(t, transcript, `{"type":"user","uuid":"u-1","entrypoint":"remote_cowork","message":{"role":"user","content":"hi"}}`+"\n")

	res := invokeClaude(t, cfg, claudeStop(transcript))
	require.Equal(t, 0, res.ExitCode, res.Stderr)
	uploads, _ := ts.recorded()
	require.Empty(t, uploads, "the hook environment names Claude Code, which wins over the transcript")
	require.Positive(t, ts.requests.Load(), "Stop still reaches ingest")
}

// TestCoworkModeFollowsHookEnvironment: only the entrypoint Claude Code puts
// in the hook environment decides. A transcript naming Cowork is ignored, so
// events that fire before its first line is written decide the same way as
// the Stop after them.
func TestCoworkModeFollowsHookEnvironment(t *testing.T) {
	for entrypoint, ships := range map[string]bool{"remote_cowork": true, "REMOTE_COWORK": true, "cli": false, "": false, "not_cowork": false, "remote": false} {
		t.Setenv("XDG_STATE_HOME", t.TempDir())
		ts := newTranscriptServer(t)
		cfg := coworkConfig(t, ts.URL)
		t.Setenv("CLAUDE_CODE_ENTRYPOINT", entrypoint)
		transcript := claudeTranscriptFile(t)
		appendTranscript(t, transcript, `{"type":"user","uuid":"u-1","entrypoint":"remote_cowork","message":{"role":"user","content":"hi"}}`+"\n")

		invokeClaude(t, cfg, claudeStop(transcript))
		uploads, _ := ts.recorded()
		require.Equal(t, ships, len(uploads) == 1, "CLAUDE_CODE_ENTRYPOINT=%q", entrypoint)
	}
}

// The first upload of a session names the signed-in Claude account, so the
// server can give the chat an owner without waiting on OTEL.
func TestCoworkUploadNamesTheClaudeAccount(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	cfg := coworkConfig(t, ts.URL)
	transcript := claudeTranscriptFile(t)
	t.Setenv("CLAUDE_CODE_USER_EMAIL", "cowork@example.com")
	appendTranscript(t, transcript, transcriptUser+transcriptAssistant)

	res := invokeClaude(t, cfg, claudeStop(transcript))
	require.Equal(t, 0, res.ExitCode, res.Stderr)

	uploads, _ := ts.recorded()
	require.Len(t, uploads, 1)
	require.Equal(t, "cowork@example.com", uploads[0].UserEmail)
}

// TestCoworkSurfacesServerWarnings: Cowork relays no per-event hook, so a warn
// policy can only act on the prompt once the transcript reaches the server.
// The warnings the server returns make Claude continue once and tell the user;
// a Stop that ships nothing new finishes as before.
func TestCoworkSurfacesServerWarnings(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := newTranscriptServer(t)
	ts.setBody(func(n int) []byte {
		if n > 1 {
			return []byte(`{"stored":1}`)
		}
		return []byte(`{"stored":2,"warnings":[{"policy_id":"p-1","policy_name":"PII policy","message":"EXAMPLE-MATCH looks like sensitive information."}]}`)
	})
	cfg := coworkConfig(t, ts.URL)
	transcript := claudeTranscriptFile(t)
	appendTranscript(t, transcript, transcriptUser+transcriptAssistant)

	res := invokeClaude(t, cfg, claudeStop(transcript))
	require.Equal(t, 0, res.ExitCode, res.Stderr)
	var out struct {
		Decision      string `json:"decision"`
		Reason        string `json:"reason"`
		SystemMessage string `json:"systemMessage"`
	}
	require.NoError(t, json.Unmarshal([]byte(res.Stdout), &out), res.Stdout)
	require.Equal(t, "block", out.Decision, "Claude continues so the user is told")
	require.Contains(t, out.Reason, `Speakeasy policy "PII policy": EXAMPLE-MATCH looks like sensitive information.`)
	require.Equal(t, `Speakeasy policy "PII policy": EXAMPLE-MATCH looks like sensitive information.`, out.SystemMessage)

	// Claude's reply lands; the next Stop ships it and, with no warning, finishes.
	appendTranscript(t, transcript, transcriptLater)
	res = invokeClaude(t, cfg, claudeStop(transcript))
	require.Equal(t, 0, res.ExitCode, res.Stderr)
	require.NotContains(t, string(res.Stdout), `"decision":"block"`)
	require.Equal(t, int32(2), ts.requests.Load())
}
