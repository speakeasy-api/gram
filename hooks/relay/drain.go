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
	"strings"
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

// RunDrain is the `speakeasy-hooks drain` entrypoint. Exit 1 only when the
// control plane proved unreachable mid-run — the one outcome a retry fixes —
// so a supervising caller (the device agent's recovery trigger) logs it as
// incomplete. Skipped-only leftovers (no credential, newer entry schema)
// exit 0: retrying can't deliver them until the machine's state changes, and
// a permanent exit-1 would train operators to ignore the signal. The counts
// on stdout carry the detail either way.
func RunDrain(ctx context.Context, out io.Writer) int {
	s := Drain(ctx)
	fmt.Fprintf(out, "spool drain: replayed=%d dropped=%d expired=%d skipped=%d remaining=%d aborted=%t\n",
		s.Replayed, s.Dropped, s.Expired, s.Skipped, s.Remaining, s.Aborted)
	if s.Aborted {
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
			if removeSpoolEntry(path) {
				s.Expired++
			} else {
				s.Skipped++
			}
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
		entry, err := decodeSpoolEntry(b)
		if err != nil {
			// Unparseable to THIS binary is not corrupt: a newer binary may
			// have written an event type this one's SDK enum rejects. Never
			// delete what we couldn't read — leave it for a newer binary, or
			// the age cap.
			s.Skipped++
			continue
		}
		if entry.V != spoolEntryVersion {
			// A newer binary wrote it — not this one's to interpret or delete.
			s.Skipped++
			continue
		}
		key := drainAuthKey(entry)
		a := resolveDrainAuth(entry, key, auths)
		if !a.ok {
			s.Skipped++
			continue
		}
		cl := clients[entry.ServerURL]
		if cl == nil {
			cl = newReplayClient(entry.ServerURL)
			clients[entry.ServerURL] = cl
		}
		res := cl.send(ctx, a.c, entry.Envelope, entry.IdempotencyKey)
		if res.authRejected {
			// A rejected credential is machine state, not event state — the
			// backlog would deliver fine after a re-login or key rotation.
			// Mirror the live path's one fallback (a rejected cached key
			// retries through the config's shared org key), then skip the
			// deployment's remaining entries. Never delete on auth rejection.
			if a.c.Source == credCache && a.orgKey != "" {
				org := creds{ServerURL: entry.ServerURL, APIKey: a.orgKey, Project: entry.ProjectSlug, Email: "", Org: entry.OrgID, Source: credOrg}
				res = cl.send(ctx, org, entry.Envelope, entry.IdempotencyKey)
				if !res.authRejected {
					auths[key] = drainAuth{c: org, ok: true, orgKey: a.orgKey}
				}
			}
			if res.authRejected {
				auths[key] = drainAuth{c: creds{ServerURL: "", APIKey: "", Project: "", Email: "", Org: "", Source: credEnv}, ok: false, orgKey: ""}
				s.Skipped++
				continue
			}
		}
		switch {
		case res.accepted():
			if removeSpoolEntry(path) {
				s.Replayed++
			} else {
				// Delivered but not deleted: the next drain re-sends under
				// the same key and the server dedupes; count it skipped so
				// Remaining stays truthful.
				s.Skipped++
			}
		case res.unsent():
			s.Aborted = true
		default:
			if removeSpoolEntry(path) {
				s.Dropped++
			} else {
				s.Skipped++
			}
		}
		if s.Aborted {
			break
		}
	}
	s.Remaining = len(listSpoolEntries(dir))
	return s
}

// decodeSpoolEntry unmarshals an entry, restoring Envelope.Raw to the stored
// bytes verbatim: the generated envelope decoder produces float64 maps for
// Raw, which silently mutates integers above 2^53 (nanosecond timestamps,
// snowflake ids) on re-marshal, while json.RawMessage — the same type
// buildEnvelope put there — round-trips exactly.
func decodeSpoolEntry(b []byte) (spoolEntry, error) {
	var entry spoolEntry
	if err := json.Unmarshal(b, &entry); err != nil {
		return spoolEntry{}, err
	}
	var probe struct {
		Envelope struct {
			Raw json.RawMessage `json:"raw"`
		} `json:"envelope"`
	}
	if err := json.Unmarshal(b, &probe); err == nil && len(probe.Envelope.Raw) > 0 && string(probe.Envelope.Raw) != "null" {
		entry.Envelope.Raw = probe.Envelope.Raw
	}
	return entry, nil
}

type drainAuth struct {
	c  creds
	ok bool
	// orgKey is the config file's shared key, kept aside for the
	// auth-rejection fallback (mirroring deliver's org retry).
	orgKey string
}

// drainAuthKey identifies one deployment for credential memoization. Every
// field that influences resolveDrainAuth's outcome must appear here.
func drainAuthKey(entry spoolEntry) string {
	return entry.ConfigPath + "\x00" + entry.ServerURL + "\x00" + entry.OrgID + "\x00" + entry.ProjectSlug
}

// resolveDrainAuth resolves the credential for one entry's deployment,
// memoized per identity. The entry's recorded identity is the routing truth;
// the config file it points at (when still present and still describing the
// same deployment) contributes only the shared org-key fallback. The disk
// auth cache resolves exactly as a live send would; an env key is pinned to
// the env-configured deployment when one is named, because the drain —
// unlike a live send — batches entries recorded by other sessions and must
// not post one deployment's key to another deployment's server.
func resolveDrainAuth(entry spoolEntry, key string, memo map[string]drainAuth) drainAuth {
	if a, ok := memo[key]; ok {
		return a
	}
	a := drainAuth{c: creds{ServerURL: "", APIKey: "", Project: "", Email: "", Org: "", Source: credEnv}, ok: false, orgKey: ""}
	if !insecureServerURL(entry.ServerURL) {
		cfg := Config{ServerURL: entry.ServerURL, ProjectSlug: entry.ProjectSlug, OrgID: entry.OrgID, HooksAPIKey: "", BrowserLogin: false, Nonblocking: false, DebugLog: "", ConfigPath: entry.ConfigPath, ConfigError: ""}
		if entry.ConfigPath != "" {
			if fc, err := readFileConfig(entry.ConfigPath); err == nil &&
				sameDeployment(fc.ServerURL, fc.Org, entry.ServerURL, entry.OrgID) {
				cfg.HooksAPIKey = fc.HooksAPIKey
			}
		}
		a.orgKey = cfg.HooksAPIKey
		a.c, a.ok = resolveAuth(cfg)
		if a.ok && a.c.Source == credEnv {
			if envURL := strings.TrimRight(strings.TrimSpace(os.Getenv("GRAM_HOOKS_SERVER_URL")), "/"); envURL != "" && envURL != entry.ServerURL {
				// The env key belongs to the env-named deployment; resolve
				// this entry from the cache or org key instead.
				if cached, ok := readCachedAuth(cfg); ok {
					a.c, a.ok = cached, true
				} else if cfg.HooksAPIKey != "" {
					a.c, a.ok = creds{ServerURL: cfg.ServerURL, APIKey: cfg.HooksAPIKey, Project: cfg.ProjectSlug, Email: "", Org: cfg.OrgID, Source: credOrg}, true
				} else {
					a.c, a.ok = creds{ServerURL: "", APIKey: "", Project: "", Email: "", Org: "", Source: credEnv}, false
				}
			}
		}
		if a.ok && entry.ProjectSlug != "" {
			// The stored deployment identity is the replay routing truth: a
			// GRAM_HOOKS_PROJECT_SLUG (or cached project) inherited from the
			// spawning session must not reroute another project's entries.
			a.c.Project = entry.ProjectSlug
		}
	}
	memo[key] = a
	return a
}

// removeSpoolEntry deletes an entry file, treating already-gone as success —
// a concurrent trim raced us to it.
func removeSpoolEntry(path string) bool {
	err := os.Remove(path)
	return err == nil || os.IsNotExist(err)
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
	// Detached on purpose, matching agenthooks' --async worker: a new
	// session/process group so a provider that signals the hook's process
	// group on timeout can't kill the drain mid-run.
	cmd.SysProcAttr = drainSysProcAttr()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start drain: %w", err)
	}
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
	case res.unsent():
		r.spoolUnsent(idemKey, payload)
	case res.accepted():
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
