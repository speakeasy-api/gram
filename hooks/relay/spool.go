package relay

import (
	"encoding/json"
	"fmt"
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
// by the writer, which is the only party that knows the entry's size — and
// they are deliberately soft under concurrency: simultaneous failed hooks
// can each pass the trim before committing, overshooting by a handful of
// bounded entries, which costs less than a cross-process lock on the
// session-gating path would.

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
// across drain triggers dedupes server-side within the server's replay
// claim window — a partially delivered *original* is deduped only within
// the server's short live-claim window, the accepted gap until a durable
// server-side backstop lands, tracked on DNO-498), the deployment identity
// to route and re-resolve auth against, and the config path the identity
// came from. The envelope itself already contains the event's occurred_at,
// so replay preserves original timestamps.
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

// maxSpoolEntryBytes caps one entry on disk. A single entry must never
// approach spoolBytesCap: the trim would evict the whole backlog to make
// room arithmetic can never make. An oversize entry first sheds the Raw
// debug echo (the backend doesn't read it; the structured Data fields carry
// the scannable content) and is dropped entirely only if still oversize.
const maxSpoolEntryBytes = 8 << 20

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
	if len(data) > maxSpoolEntryBytes {
		entry.Envelope.Raw = nil
		if data, err = json.Marshal(entry); err != nil {
			r.debugf("spool: marshal failed: %v", err)
			return
		}
		if len(data) > maxSpoolEntryBytes {
			r.debugf("spool: entry exceeds %d bytes even without the raw echo; dropped", maxSpoolEntryBytes)
			return
		}
		r.debugf("spool: raw echo stripped from oversize entry")
	}
	if dropped := trimSpool(dir, len(data), time.Now(), spoolEntryCap, spoolBytesCap); dropped > 0 {
		r.debugf("spool: cap reached; dropped %d oldest entries", dropped)
	}
	// Two attempts: the one self-inflicted failure mode is the stale-.tmp
	// sweep racing a writer that a suspend paused for over an hour between
	// write and rename. The data is still in hand, so a failed commit just
	// retries under a fresh (current-time) name.
	var commitErr error
	for range 2 {
		if commitErr = commitSpoolEntry(dir, data); commitErr == nil {
			break
		}
	}
	if commitErr != nil {
		r.debugf("spool: commit failed: %v", commitErr)
		return
	}
	r.debugf("spool: stored event=%s bytes=%d", payload.Event.Type, len(data))
}

// spoolBehindBacklog is spoolUnsent's non-evicting variant for the ordered
// send-queue path: it appends the event behind the existing backlog, but
// refuses (returning false) when doing so would evict older entries at the
// caps — dropping the oldest conversation rows to admit a newer event would
// invert the very ordering the queue exists to preserve. The caller falls
// back to a live send: at the cap, delivering beats both dropping backlog
// and dropping the event.
func (r *Relay) spoolBehindBacklog(idemKey string, payload components.IngestRequestBody) bool {
	dir := spoolDir()
	if dir == "" {
		r.debugf("spool: no writable state dir; queue-behind unavailable")
		return false
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
		return false
	}
	if len(data) > maxSpoolEntryBytes {
		entry.Envelope.Raw = nil
		if data, err = json.Marshal(entry); err != nil {
			r.debugf("spool: marshal failed: %v", err)
			return false
		}
		if len(data) > maxSpoolEntryBytes {
			return false
		}
		r.debugf("spool: raw echo stripped from oversize entry")
	}
	if spoolOverCap(dir, len(data)) {
		return false
	}
	var commitErr error
	for range 2 {
		if commitErr = commitSpoolEntry(dir, data); commitErr == nil {
			break
		}
	}
	if commitErr != nil {
		r.debugf("spool: commit failed: %v", commitErr)
		return false
	}
	r.debugf("spool: queued event=%s bytes=%d behind backlog", payload.Event.Type, len(data))
	return true
}

// spoolOverCap reports whether admitting one more entry of incomingBytes
// would exceed either spool cap.
func spoolOverCap(dir string, incomingBytes int) bool {
	des, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	var count int
	var total int64
	for _, de := range des {
		if de.IsDir() {
			continue
		}
		if _, ok := spoolNanos(de.Name()); !ok {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		count++
		total += info.Size()
	}
	return count+1 > spoolEntryCap || total+int64(incomingBytes) > int64(spoolBytesCap)
}

// commitSpoolEntry writes one entry under a fresh chronological name via the
// same-directory temp+rename idiom.
func commitSpoolEntry(dir string, data []byte) error {
	name := spoolFileName(time.Now())
	tmp := filepath.Join(dir, name+".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// undrainableMarkerName is the spool-dir sidecar listing entries a completed
// drain run proved THIS binary can never deliver (newer schema version,
// undecodable) — a determination that is per-binary deterministic, so the
// marker cannot go stale while the same binary runs. Credential-related
// skips are deliberately excluded: those become deliverable after a
// re-login, so ordering behind them still matters. The name doesn't match
// the entry shape, so listSpoolEntries and the caps never count it.
const undrainableMarkerName = "undrainable"

// readUndrainable loads the marker as a set. Missing or torn reads yield an
// empty set — the conservative direction (entries count as backlog).
func readUndrainable(dir string) map[string]bool {
	b, err := os.ReadFile(filepath.Join(dir, undrainableMarkerName))
	if err != nil {
		return nil
	}
	out := make(map[string]bool)
	for name := range strings.SplitSeq(strings.TrimSpace(string(b)), "\n") {
		if name = strings.TrimSpace(name); name != "" {
			out[name] = true
		}
	}
	return out
}

// writeUndrainable persists the set, or removes the marker when empty.
// Callers hold the drain lock; failures are swallowed — a missing marker
// only means observe events keep queueing behind a poison entry.
func writeUndrainable(dir string, names []string) {
	path := filepath.Join(dir, undrainableMarkerName)
	if len(names) == 0 {
		_ = os.Remove(path)
		return
	}
	_ = os.WriteFile(path, []byte(strings.Join(names, "\n")+"\n"), 0o600)
}

// spoolHasBacklog reports whether undelivered entries are waiting on disk.
// deliver consults it so observe-only events queue behind an outage backlog
// instead of overtaking it, and the drain paths use it as their emptiness
// guard. Entries the last drain run marked undrainable don't count: a
// poison entry (newer schema) lingers until the 14-day expiry, and counting
// it would reroute every observe event through the spool for two weeks.
// It answers a boolean, so it probes the directory in batches and returns on
// the first live entry instead of materializing and sorting the full listing
// the way listSpoolEntries does — this runs on every observe event.
func spoolHasBacklog() bool {
	dir := spoolDirPath()
	if dir == "" {
		return false
	}
	f, err := os.Open(dir)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	var und map[string]bool
	undLoaded := false
	for {
		names, err := f.Readdirnames(64)
		for _, name := range names {
			if _, ok := spoolNanos(name); !ok {
				continue
			}
			// Lazy: the marker is only worth reading once an entry exists.
			if !undLoaded {
				und = readUndrainable(dir)
				undLoaded = true
			}
			if !und[name] {
				return true
			}
		}
		if err != nil {
			return false
		}
	}
}

// spoolDirPath resolves the spool directory without creating it, or "" when
// no state home exists. Readers (drain, the spawn check) use this so an
// install that never spooled doesn't grow an empty directory.
func spoolDirPath() string {
	dir := hooksStateDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "spool")
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
// for anything that doesn't match the exact %020d-%06d-%06d.json shape the
// writer produces — a foreign file that merely starts with digits must never
// be trimmed, expired, or drained as an entry.
func spoolNanos(name string) (int64, bool) {
	const shape = len("00000000000000000000-000000-000000.json")
	if len(name) != shape || name[20] != '-' || name[27] != '-' || !strings.HasSuffix(name, ".json") {
		return 0, false
	}
	for i := 0; i < len(name)-len(".json"); i++ {
		if i == 20 || i == 27 {
			continue
		}
		if name[i] < '0' || name[i] > '9' {
			return 0, false
		}
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
		// A writer killed between write and rename leaves a .tmp orphan that
		// no cap or expiry would ever see; sweep ones old enough that no
		// running writer plausibly owns them. A writer that a suspend parked
		// inside that window loses only its rename — spoolUnsent retries the
		// commit under a fresh name, so the event survives the sweep.
		if before, ok := strings.CutSuffix(de.Name(), ".tmp"); ok {
			if nanos, ok := spoolNanos(before); ok && nanos < now.Add(-time.Hour).UnixNano() {
				_ = os.Remove(filepath.Join(dir, de.Name()))
			}
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
