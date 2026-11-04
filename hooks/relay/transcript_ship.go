package relay

// Transcript shipping for the unified chat distribution. Claude's unified
// Chat/Cowork mode runs plugin hooks only inside the Cowork VM, so that
// distribution registers just the Stop hook (Config.TranscriptOnly) and, each
// time it fires, uploads the transcript lines written since the last
// successful upload to the dedicated transcript endpoint.
//
// The transcript file is its own durable queue: a per-transcript byte offset
// advances only once the server has settled a chunk, so a failed upload is
// retried from the same offset on the next Stop. The server keys rows by the
// transcript entry's uuid, which makes every re-send — a retry, or a fresh VM
// that has lost its state and starts again from offset zero — idempotent.

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/speakeasy-api/agenthooks"
)

const (
	transcriptUploadPath = "/transcript"
	transcriptSchemaV1   = "claude.transcript.v1"

	// transcriptChunkBytes (4 MiB) bounds the transcript bytes one chunk read
	// consumes, shipped or skipped, so a long run of bookkeeping lines is
	// crossed a chunk at a time with the Stop budget checked in between.
	transcriptChunkBytes = 4 << 20

	// transcriptMaxLineBytes (8 MiB) bounds a single transcript line. A longer
	// line (an enormous tool result) is skipped rather than blocking the
	// offset forever. A line at the cap plus its envelope stays under the
	// server's 10 MiB body cap (claudeTranscriptMaxBodyBytes in
	// server/internal/hooks) even uncompressed: the server would answer a
	// larger body 413, which settles the chunk and drops the line.
	transcriptMaxLineBytes = 8 << 20

	// transcriptMaxEntries is the server's per-upload entry cap
	// (claudeTranscriptMaxEntries); a larger chunk would be answered 413 and
	// dropped the same way.
	transcriptMaxEntries = 20000

	// transcriptReadBufferBytes (64 KiB) holds typical transcript lines whole;
	// longer lines are assembled from buffer-sized fragments.
	transcriptReadBufferBytes = 64 << 10

	// transcriptResponseLimit (64 KiB) bounds how much of a response is read;
	// the acknowledgement is a few bytes of JSON.
	transcriptResponseLimit = 64 << 10
)

// transcriptShipBudget bounds the whole upload on one Stop. Claude runs Stop
// synchronously, so the budget is time the user waits; anything left over is
// shipped on the next Stop.
const transcriptShipBudget = 15 * time.Second

// transcriptUpload is the request body of POST /transcript.
type transcriptUpload struct {
	SchemaVersion string            `json:"schema_version"`
	SessionID     string            `json:"session_id"`
	Adapter       string            `json:"adapter"`
	Cwd           string            `json:"cwd,omitempty"`
	Hostname      string            `json:"hostname,omitempty"`
	UserEmail     string            `json:"user_email,omitempty"`
	OffsetStart   int64             `json:"offset_start"`
	OffsetEnd     int64             `json:"offset_end"`
	Entries       []json.RawMessage `json:"entries"`
}

// transcriptChunk is a run of complete transcript lines read from one offset.
type transcriptChunk struct {
	start   int64
	end     int64
	entries []json.RawMessage
}

// onTranscriptStop is the only handler registered in transcript-only mode.
func (r *Relay) onTranscriptStop(ctx context.Context, e *agenthooks.StopEvent) (agenthooks.StopDecision, error) {
	r.shipTranscript(ctx, e)
	return agenthooks.Finish(), nil
}

// shipTranscript uploads the transcript lines added since the last settled
// upload. Every failure is logged and swallowed: a missed upload is retried on
// the next Stop, and the hook must never disturb the session.
func (r *Relay) shipTranscript(ctx context.Context, e *agenthooks.StopEvent) {
	base := &e.Event
	if base.Provider != agenthooks.ProviderClaudeCode || base.Kind != agenthooks.KindStop {
		return
	}
	if r.cfg.ConfigError != "" {
		r.debugf("transcript skipped: config error path=%s err=%s", r.cfg.ConfigPath, r.cfg.ConfigError)
		return
	}
	if insecureServerURL(r.cfg.ServerURL) {
		r.debugf("transcript skipped: insecure server url %s", r.cfg.ServerURL)
		return
	}
	sessionID := strings.TrimSpace(base.Session.ID)
	if sessionID == "" {
		r.debugf("transcript skipped: no session id")
		return
	}
	path, err := validateClaudeTranscriptPath(base.Session.TranscriptPath)
	if err != nil {
		r.debugf("transcript skipped: %v", err)
		return
	}
	c, ok := resolveAuth(r.cfg)
	if !ok {
		r.debugf("transcript skipped: no credentials")
		return
	}
	// The transcript endpoint authenticates hooks keys only, like the skill
	// content upload; a device-agent token box has nothing it can send.
	if c.Source == credToken {
		r.debugf("transcript skipped: token credentials are not accepted")
		return
	}

	ctx, cancel := context.WithTimeout(ctx, transcriptShipBudget)
	defer cancel()
	ctx = withHarnessInfo(ctx, base)

	upload := transcriptUpload{
		SchemaVersion: transcriptSchemaV1,
		SessionID:     sessionID,
		Adapter:       transcriptAdapter(base),
		Cwd:           strings.TrimSpace(base.Session.CWD),
		Hostname:      hostname(),
		UserEmail:     resolveUserEmail(ctx, e),
		OffsetStart:   0,
		OffsetEnd:     0,
		Entries:       nil,
	}

	marker := transcriptHighWaterPath(path)
	ship := func() {
		offset := readTranscriptHighWater(marker)
		for ctx.Err() == nil {
			chunk, err := readTranscriptChunk(path, offset)
			if err != nil {
				r.debugf("transcript read failed path=%s offset=%d: %v", path, offset, err)
				return
			}
			if chunk.start != offset {
				// The file was replaced. Record the restart now, even when nothing
				// is complete yet or this upload fails: if the new file then grows
				// past the old offset, resuming from there would land mid-line and
				// skip what precedes it.
				writeTranscriptHighWater(marker, chunk.start)
			}
			if chunk.end == chunk.start {
				return
			}
			if len(chunk.entries) > 0 {
				upload.OffsetStart, upload.OffsetEnd, upload.Entries = chunk.start, chunk.end, chunk.entries
				status, err := postTranscript(ctx, c, upload)
				r.debugf("transcript upload session=%s offset=%d-%d entries=%d status=%d err=%v", sessionID, chunk.start, chunk.end, len(chunk.entries), status, err)
				if !transcriptUploadSettled(status) {
					return
				}
			}
			offset = chunk.end
			writeTranscriptHighWater(marker, offset)
		}
	}
	if marker == "" {
		ship()
		return
	}
	if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
		ship()
		return
	}
	// Stop and a concurrent Stop for the same transcript must not both ship
	// the same range and race the offset.
	withFileLock(marker, ship)
}

// transcriptAdapter reports the hook source slug the server stamps on the
// session: cowork when agenthooks detected the Cowork runtime.
func transcriptAdapter(base *agenthooks.Event) string {
	if base.Variant == agenthooks.VariantCowork {
		return "cowork"
	}
	return adapterSlug(base.Provider)
}

// transcriptUploadSettled reports whether the server has answered a chunk for
// good, so the offset may move past it. A definitive 4xx would be rejected
// identically on every retry; credentials and throttling can recover, so
// those chunks stay queued with transport failures and 5xx. The endpoint
// never answers 404 or 405 itself: those come from a request routed
// elsewhere (an ingress without the route, or a server that predates it), so
// those chunks stay queued too.
func transcriptUploadSettled(status int) bool {
	switch {
	case status >= 200 && status < 300:
		return true
	case status == http.StatusUnauthorized, status == http.StatusForbidden,
		status == http.StatusNotFound, status == http.StatusMethodNotAllowed,
		status == http.StatusRequestTimeout, status == http.StatusTooManyRequests:
		return false
	case status >= 400 && status < 500:
		return true
	default:
		return false
	}
}

// readTranscriptChunk reads complete lines from offset, up to
// transcriptChunkBytes of them whether shipped or skipped, and keeps the
// conversation entries among them, at most transcriptMaxEntries. An entry
// that would push a chunk already carrying entries past the budget starts the
// next one, so an upload holds at most the budget or a single line. A
// trailing line without its newline is still being written and is left for
// the next Stop. A file shorter than offset was replaced, so it is read again
// from the start; the server deduplicates what it already holds.
func readTranscriptChunk(path string, offset int64) (transcriptChunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return transcriptChunk{}, fmt.Errorf("open transcript: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return transcriptChunk{}, fmt.Errorf("stat transcript: %w", err)
	}
	if offset < 0 || offset > info.Size() {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return transcriptChunk{}, fmt.Errorf("seek transcript: %w", err)
	}

	chunk := transcriptChunk{start: offset, end: offset, entries: nil}
	reader := bufio.NewReaderSize(f, transcriptReadBufferBytes)
	limit := chunk.start + transcriptChunkBytes
	for chunk.end < limit && len(chunk.entries) < transcriptMaxEntries {
		line, size, err := readTranscriptLine(reader)
		if errors.Is(err, io.EOF) {
			// A partial line is still being written; leave it for later.
			return chunk, nil
		}
		if err != nil {
			return chunk, fmt.Errorf("read transcript: %w", err)
		}
		// Lines that are not shipped are consumed where they stand: deferring
		// them to the next chunk would only read them again.
		entry, ok := transcriptConversationEntry(line)
		if ok && len(chunk.entries) > 0 && chunk.end+size > limit {
			return chunk, nil
		}
		chunk.end += size
		if ok {
			chunk.entries = append(chunk.entries, entry)
		}
	}
	return chunk, nil
}

// readTranscriptLine reads one line through its newline and returns its
// length in the file. The bytes are valid only until the next read. A line
// longer than transcriptMaxLineBytes is held only up to that cap, then read
// through its newline and comes back nil. io.EOF means the file ends inside a
// line that is still being written.
func readTranscriptLine(r *bufio.Reader) ([]byte, int64, error) {
	fragment, err := r.ReadSlice('\n')
	size := int64(len(fragment))
	if !errors.Is(err, bufio.ErrBufferFull) {
		// The whole line fits the reader's buffer.
		return fragment, size, err
	}
	line := bytes.Clone(fragment)
	for errors.Is(err, bufio.ErrBufferFull) {
		fragment, err = r.ReadSlice('\n')
		size += int64(len(fragment))
		if size <= transcriptMaxLineBytes {
			line = append(line, fragment...)
		} else {
			line = nil
		}
	}
	return line, size, err
}

// transcriptConversationEntry keeps the user and assistant entries the
// server stores. Bookkeeping entries (raw API requests, snapshots, titles)
// are dropped here so they never leave the machine, and anything that is not
// a JSON object is skipped.
func transcriptConversationEntry(line []byte) (json.RawMessage, bool) {
	line = bytes.TrimSpace(line)
	var head struct {
		Type string `json:"type"`
	}
	if len(line) == 0 || line[0] != '{' || json.Unmarshal(line, &head) != nil {
		return nil, false
	}
	switch head.Type {
	case "user", "assistant":
		return json.RawMessage(bytes.Clone(line)), true
	default:
		return nil, false
	}
}

// postTranscript sends one gzip-compressed upload and returns the HTTP status,
// or 0 when no response arrived. A 2xx counts only when it carries the
// endpoint's JSON acknowledgement: a request routed anywhere else (a proxy or
// a single-page app answering every path) could answer 200 without storing
// anything, and moving the offset past it would lose those lines for good.
func postTranscript(ctx context.Context, c creds, upload transcriptUpload) (int, error) {
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	enc := json.NewEncoder(gz)
	// Escaping <, > and & grows them sixfold, which could push a line at
	// transcriptMaxLineBytes past the server's body cap.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(upload); err != nil {
		return 0, fmt.Errorf("encode transcript upload: %w", err)
	}
	if err := gz.Close(); err != nil {
		return 0, fmt.Errorf("compress transcript upload: %w", err)
	}

	endpoint := strings.TrimRight(c.ServerURL, "/") + transcriptUploadPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &compressed)
	if err != nil {
		return 0, fmt.Errorf("build transcript request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Gram-Key", c.APIKey)
	if c.Project != "" {
		req.Header.Set("Gram-Project", c.Project)
	}

	client := &http.Client{
		Transport: ingestTransport(),
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 0,
		Jar:     nil,
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("send transcript upload: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, transcriptResponseLimit))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, nil
	}
	var ack struct {
		Stored *int64 `json:"stored"`
	}
	if json.Unmarshal(respBody, &ack) != nil || ack.Stored == nil {
		return 0, fmt.Errorf("transcript upload answered %d without an acknowledgement from %s", resp.StatusCode, endpoint)
	}
	return resp.StatusCode, nil
}

// transcriptHighWaterPath is the per-transcript offset marker, kept apart
// from the prompt-attachment markers so the two consumers never couple. ""
// when no state directory exists; shipping then restarts from offset zero
// every time and relies on server-side deduplication.
func transcriptHighWaterPath(transcriptPath string) string {
	stateDir := hooksStateDir()
	if stateDir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(transcriptPath))
	return filepath.Join(stateDir, "transcripts", hex.EncodeToString(sum[:])+".offset")
}

func readTranscriptHighWater(marker string) int64 {
	if marker == "" {
		return 0
	}
	b, err := os.ReadFile(marker)
	if err != nil {
		return 0
	}
	offset, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil || offset < 0 {
		return 0
	}
	return offset
}

func writeTranscriptHighWater(marker string, offset int64) {
	if marker == "" {
		return
	}
	_ = atomicWriteCacheFile(marker, []byte(strconv.FormatInt(offset, 10)))
}
