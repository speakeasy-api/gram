package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

// Draining the offline payload spool (DNO-498, part 2 of the capture in
// spool.go). Entries replay oldest-first — global chronological order, which
// implies per-session order — each under its originally minted
// Idempotency-Key so the server stores at most one copy no matter how many
// delivery paths raced. Every replayed request carries the X-Gram-Replayed
// marker so downtime backlog is distinguishable from live traffic
// (DNO-499).
//
// Two things trigger a drain: any hook invocation whose send succeeds while
// entries exist (maybeSpawnDrain — the control plane just proved reachable),
// and the device agent exec'ing `speakeasy-hooks drain` the moment its
// downtime detector sees recovery (device-agent ADR-0011). Both funnel into
// RunDrain and serialize on the drain lock.

// DrainSummary reports one drain run's outcome.
type DrainSummary struct {
	// Replayed entries were accepted by the server and deleted.
	Replayed int
	// Dropped entries were deleted without acceptance: the server rejected
	// them definitively (4xx — a replay would fail identically), or the
	// file was corrupt and could never replay.
	Dropped int
	// Expired entries aged past spoolMaxAge and were deleted unsent.
	Expired int
	// Skipped entries were left in place for a later run: a newer schema
	// version this binary doesn't understand, or a deployment we currently
	// hold no credential for.
	Skipped int
	// Remaining is the entry count left on disk when the run ended.
	Remaining int
	// Aborted is true when the control plane proved unreachable mid-drain;
	// the rest of the backlog is kept rather than hammered.
	Aborted bool
}

// RunDrain is the `speakeasy-hooks drain` entrypoint. Exit 0 when the spool
// ended empty; 1 when entries remain or the run aborted, so a supervising
// caller (the device agent's recovery trigger) logs the incomplete run.
func RunDrain(ctx context.Context, out io.Writer) int {
	s := Drain(ctx)
	fmt.Fprintf(out, "spool drain: replayed=%d dropped=%d expired=%d skipped=%d remaining=%d aborted=%t\n",
		s.Replayed, s.Dropped, s.Expired, s.Skipped, s.Remaining, s.Aborted)
	if s.Remaining > 0 || s.Aborted {
		return 1
	}
	return 0
}

// Drain replays the spool under the drain lock. Concurrent drains serialize;
// on Windows, where the lock is a no-op, a racing double-send is harmless
// because both carry the same Idempotency-Key.
func Drain(ctx context.Context) DrainSummary {
	var s DrainSummary
	dir := spoolDirPath()
	if dir == "" {
		return s
	}
	withFileLock(filepath.Join(dir, "drain"), func() {
		s = drainSpool(ctx, dir)
	})
	return s
}

func drainSpool(ctx context.Context, dir string) DrainSummary {
	var s DrainSummary
	cutoff := time.Now().Add(-spoolMaxAge).UnixNano()
	clients := make(map[string]*client)
	auths := make(map[string]drainAuth)

	for _, name := range listSpoolEntries(dir) {
		if ctx.Err() != nil {
			s.Aborted = true
			break
		}
		path := filepath.Join(dir, name)
		if nanos, _ := spoolNanos(name); nanos < cutoff {
			_ = os.Remove(path)
			s.Expired++
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			// Gone means a concurrent trim took it; anything else leaves the
			// file for a later run.
			if !os.IsNotExist(err) {
				s.Skipped++
			}
			continue
		}
		var entry spoolEntry
		if err := json.Unmarshal(b, &entry); err != nil {
			// A corrupt entry can never replay; keeping it would wedge the
			// head of the spool forever.
			_ = os.Remove(path)
			s.Dropped++
			continue
		}
		if entry.V != spoolEntryVersion {
			// A newer binary wrote it — not this one's to interpret or delete.
			s.Skipped++
			continue
		}
		c, ok := resolveDrainAuth(entry, auths)
		if !ok {
			s.Skipped++
			continue
		}
		cl := clients[entry.ServerURL]
		if cl == nil {
			cl = newReplayClient(entry.ServerURL)
			clients[entry.ServerURL] = cl
		}
		res := cl.send(ctx, c, entry.Envelope, entry.IdempotencyKey)
		switch {
		case res.statusCode >= 200 && res.statusCode < 300:
			_ = os.Remove(path)
			s.Replayed++
		case unsent(res):
			s.Aborted = true
		default:
			_ = os.Remove(path)
			s.Dropped++
		}
		if s.Aborted {
			break
		}
	}
	s.Remaining = len(listSpoolEntries(dir))
	return s
}

type drainAuth struct {
	c  creds
	ok bool
}

// resolveDrainAuth resolves the credential for one entry's deployment,
// memoized per identity. The entry's recorded identity is the routing truth;
// the config file it points at (when still present and still describing the
// same deployment) contributes only the shared org-key fallback — env keys
// and the disk auth cache resolve exactly as a live send would.
func resolveDrainAuth(entry spoolEntry, memo map[string]drainAuth) (creds, bool) {
	key := entry.ConfigPath + "\x00" + entry.ServerURL + "\x00" + entry.OrgID + "\x00" + entry.ProjectSlug
	if a, ok := memo[key]; ok {
		return a.c, a.ok
	}
	a := drainAuth{c: creds{ServerURL: "", APIKey: "", Project: "", Email: "", Org: "", Source: credEnv}, ok: false}
	if !insecureServerURL(entry.ServerURL) {
		cfg := Config{ServerURL: entry.ServerURL, ProjectSlug: entry.ProjectSlug, OrgID: entry.OrgID, HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: entry.ConfigPath, ConfigError: ""}
		if entry.ConfigPath != "" {
			if fc, err := readFileConfig(entry.ConfigPath); err == nil &&
				fc.ServerURL == entry.ServerURL &&
				(entry.OrgID == "" || fc.Org == "" || fc.Org == entry.OrgID) {
				cfg.HooksAPIKey = fc.HooksAPIKey
			}
		}
		a.c, a.ok = resolveAuth(cfg)
	}
	memo[key] = a
	return a.c, a.ok
}

// listSpoolEntries returns the conforming entry filenames, oldest first.
// Files that don't follow the naming scheme (the drain lock, the spawn
// marker, strays) are not entries.
func listSpoolEntries(dir string) []string {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, de := range des {
		if de.IsDir() {
			continue
		}
		if _, ok := spoolNanos(de.Name()); ok {
			names = append(names, de.Name())
		}
	}
	sort.Strings(names)
	return names
}

// spawnDrainDebounce suppresses repeated spawns while a burst of successful
// sends lands on a non-empty spool: the first drain empties it; the rest
// would only contend on the drain lock.
const spawnDrainDebounce = 30 * time.Second

// startDrainProcess launches the detached drain. A package var so tests can
// intercept it — the real implementation re-execs this binary, and in a test
// process that would re-exec the test binary.
var startDrainProcess = func() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	cmd := exec.Command(exe, "drain")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start drain: %w", err)
	}
	// Detached on purpose: the hook process exits immediately and the drain
	// outlives it, same posture as agenthooks' --async worker.
	return cmd.Process.Release()
}

// maybeSpawnDrain launches a detached drain when the control plane just
// proved reachable and spooled entries exist — the first successful send
// after an outage flushes the backlog without waiting for the device agent's
// recovery trigger (which covers idle machines). Best-effort and debounced;
// never blocks the hook.
func (r *Relay) maybeSpawnDrain() {
	dir := spoolDirPath()
	if dir == "" || len(listSpoolEntries(dir)) == 0 {
		return
	}
	marker := filepath.Join(dir, "last-spawn")
	if info, err := os.Stat(marker); err == nil && time.Since(info.ModTime()) < spawnDrainDebounce {
		return
	}
	// Best-effort debounce: two hooks racing this write both spawn, and the
	// drain lock serializes them into one useful run plus a no-op.
	_ = os.WriteFile(marker, nil, 0o600)
	if err := startDrainProcess(); err != nil {
		r.debugf("spool: drain spawn failed: %v", err)
		return
	}
	r.debugf("spool: spawned detached drain")
}

// finishExchange runs the spool bookkeeping for a final exchange result: an
// unsent payload is kept for replay; a healthy exchange flushes any backlog
// via a detached drain.
func (r *Relay) finishExchange(idemKey string, payload components.IngestRequestBody, res ingestResult) {
	switch {
	case unsent(res):
		r.spoolUnsent(idemKey, payload)
	case res.statusCode >= 200 && res.statusCode < 300:
		r.maybeSpawnDrain()
	}
}

// newReplayClient is newClient plus the replay marker: every request carries
// X-Gram-Replayed: 1 so the server can distinguish downtime backlog from
// live traffic and tag it for observability (DNO-499). The marker rides a
// transport wrapper because the generated SDK has no per-request header
// hook.
func newReplayClient(serverURL string) *client {
	return clientWith(serverURL, &http.Client{
		Timeout:   perAttemptTime,
		Transport: replayMarkerTransport{base: http.DefaultTransport},
	})
}

type replayMarkerTransport struct{ base http.RoundTripper }

func (t replayMarkerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("X-Gram-Replayed", "1")
	return t.base.RoundTrip(req)
}
