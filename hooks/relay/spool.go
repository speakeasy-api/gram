package relay

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

// The offline payload spool (DNO-498). When an ingest exchange fails against
// an unreachable or 5xx-ing control plane, the built envelope is persisted
// here so a later drain can replay it and the event still gets scanned,
// flagged, and monitored retroactively. Blocking verdicts are NOT spooled
// concepts — the gating outcome is decided immediately by the ratchet and
// fail open/closed; the spool only preserves the event *record*.
//
// Layout: one JSON file per entry under $XDG_STATE_HOME/gram/hooks/spool/,
// named so lexicographic order is chronological order. One-file-per-entry is
// load-bearing: concurrent hook processes write without contention, and the
// file lock available on Windows is a no-op (see codextools_lock_windows.go),
// so nothing may ever rewrite a shared file. Caps are enforced at write time
// by the writer, which is the only party that knows the entry's size.

const (
	// spoolEntryVersion versions the on-disk entry schema; drain skips
	// versions it doesn't understand rather than guessing.
	spoolEntryVersion = 1

	// spoolMaxAge expires entries that were never drained (a laptop shelved
	// mid-outage). Mirrors the org-settings cache's 14d ceiling: events
	// older than this have lost their retroactive-scanning value and would
	// only distort session analytics on replay.
	spoolMaxAge = 14 * 24 * time.Hour
)

// spoolEntryCap and spoolBytesCap bound the spool on disk; the oldest
// entries are dropped first when either would be exceeded. Vars rather than
// consts so trim tests can exercise the caps without writing 64 MiB.
var (
	spoolEntryCap = 2000
	spoolBytesCap = 64 << 20
)

// spoolEntry is one unsent payload persisted for replay. It carries
// everything a later drain invocation needs to deliver the envelope exactly
// as the original send would have: the same Idempotency-Key (so a retry
// after a partial original delivery dedupes server-side), the deployment
// identity to route and re-resolve auth against, and the config path the
// identity came from. The envelope itself already contains the event's
// occurred_at, so replay preserves original timestamps.
type spoolEntry struct {
	V              int                          `json:"v"`
	IdempotencyKey string                       `json:"idempotency_key"`
	ServerURL      string                       `json:"server_url"`
	OrgID          string                       `json:"org_id,omitempty"`
	ProjectSlug    string                       `json:"project_slug,omitempty"`
	ConfigPath     string                       `json:"config_path,omitempty"`
	SpooledAt      time.Time                    `json:"spooled_at"`
	Envelope       components.IngestRequestBody `json:"envelope"`
}

// unsent reports whether the control plane failed to store the event: the
// server was unreachable (statusCode 0), failing (5xx), or shedding load
// (429/408 — the request wasn't processed, and replaying later is exactly
// what a rate-limiting server wants). Other 4xx don't spool: the server
// answered and would reject the replay identically. Matches the device
// agent's downtime classification (its ADR-0010).
func unsent(res ingestResult) bool {
	return res.statusCode == 0 || res.statusCode >= 500 ||
		res.statusCode == http.StatusTooManyRequests || res.statusCode == http.StatusRequestTimeout
}

// spoolUnsent persists a payload whose delivery failed against a down
// control plane. Best-effort by design: a spool failure only logs — the
// event's gating outcome was already decided, and buffering must never
// affect the user's session.
func (r *Relay) spoolUnsent(idemKey string, payload components.IngestRequestBody) {
	dir := spoolDir()
	if dir == "" {
		r.debugf("spool: no writable state dir; entry dropped")
		return
	}
	entry := spoolEntry{
		V:              spoolEntryVersion,
		IdempotencyKey: idemKey,
		ServerURL:      r.cfg.ServerURL,
		OrgID:          r.cfg.OrgID,
		ProjectSlug:    r.cfg.ProjectSlug,
		ConfigPath:     r.cfg.ConfigPath,
		SpooledAt:      time.Now().UTC(),
		Envelope:       payload,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		r.debugf("spool: marshal failed: %v", err)
		return
	}
	if dropped := trimSpool(dir, len(data), time.Now(), spoolEntryCap, spoolBytesCap); dropped > 0 {
		r.debugf("spool: cap reached; dropped %d oldest entries", dropped)
	}
	name := spoolFileName(time.Now())
	tmp := filepath.Join(dir, name+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		r.debugf("spool: write failed: %v", err)
		return
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmp)
		r.debugf("spool: commit failed: %v", err)
		return
	}
	r.debugf("spool: stored event=%s bytes=%d", payload.Event.Type, len(data))
}

// spoolDirPath resolves the spool directory without creating it, or "" when
// no state home exists. Mirrors codexToolStatePath's XDG_STATE_HOME
// resolution. Readers (drain, the spawn check) use this so an install that
// never spooled doesn't grow an empty directory.
func spoolDirPath() string {
	stateHome := strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateHome, "gram", "hooks", "spool")
}

// spoolDir resolves and creates the spool directory, or "" when state cannot
// be kept — the entry is then dropped, matching the pre-spool behavior.
func spoolDir() string {
	dir := spoolDirPath()
	if dir == "" {
		return ""
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	return dir
}

// spoolSeq breaks filename ties within one process; the pid breaks ties
// across concurrent processes.
var spoolSeq atomic.Uint64

// spoolFileName builds an entry filename whose lexicographic order is
// chronological order: zero-padded nanoseconds first, pid and a per-process
// sequence only as tie-breakers.
func spoolFileName(now time.Time) string {
	return fmt.Sprintf("%020d-%06d-%06d.json",
		now.UnixNano(), os.Getpid()%1_000_000, spoolSeq.Add(1)%1_000_000)
}

// spoolNanos extracts the creation time from an entry filename; ok is false
// for files that don't follow the naming scheme (they are left alone).
func spoolNanos(name string) (int64, bool) {
	if !strings.HasSuffix(name, ".json") || len(name) < 20 {
		return 0, false
	}
	n, err := strconv.ParseInt(name[:20], 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// trimSpool makes room for one incoming entry of incomingBytes: expired
// entries go first, then the oldest entries until both the count and byte
// caps hold with the new entry included. Returns how many entries were
// removed. Runs lock-free — names are unique per writer, and a concurrent
// trim racing to delete the same oldest file is harmless (ENOENT ignored).
func trimSpool(dir string, incomingBytes int, now time.Time, entryCap, bytesCap int) int {
	des, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	type entryFile struct {
		name  string
		nanos int64
		size  int64
	}
	var files []entryFile
	var total int64
	for _, de := range des {
		if de.IsDir() {
			continue
		}
		nanos, ok := spoolNanos(de.Name())
		if !ok {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		files = append(files, entryFile{name: de.Name(), nanos: nanos, size: info.Size()})
		total += info.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })

	dropped := 0
	cutoff := now.Add(-spoolMaxAge).UnixNano()
	for _, f := range files {
		expired := f.nanos < cutoff
		overCap := len(files)-dropped+1 > entryCap || total+int64(incomingBytes) > int64(bytesCap)
		if !expired && !overCap {
			break
		}
		if err := os.Remove(filepath.Join(dir, f.name)); err != nil && !os.IsNotExist(err) {
			continue
		}
		total -= f.size
		dropped++
	}
	return dropped
}
