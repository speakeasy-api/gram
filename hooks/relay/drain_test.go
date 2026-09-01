package relay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/speakeasy-api/agenthooks"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/hooks/delegation"
	"github.com/speakeasy-api/gram/hooks/sdk/models/components"
)

// seedSpoolEntry writes a well-formed entry aged `age`, returning its
// idempotency key.
func seedSpoolEntry(t *testing.T, serverURL string, age time.Duration, sessionID string) string {
	t.Helper()
	return seedSpoolEntryWithConfig(t, serverURL, age, sessionID, "")
}

// seedSpoolEntryWithConfig is seedSpoolEntry with the entry's recorded
// config path, for the auth-fallback paths.
func seedSpoolEntryWithConfig(t *testing.T, serverURL string, age time.Duration, sessionID, configPath string) string {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "gram", "hooks", "spool")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	key := newIdempotencyToken()
	entry := spoolEntry{
		V:              spoolEntryVersion,
		IdempotencyKey: key,
		ServerURL:      serverURL,
		OrgID:          "",
		ProjectSlug:    "default",
		ConfigPath:     configPath,
		SpooledAt:      time.Now().Add(-age).UTC(),
		Envelope: components.IngestRequestBody{
			SchemaVersion: schemaVersion,
			Source:        components.HookIngestSource{Adapter: "claude", AdapterVersion: nil, RawEventName: nil, Hostname: nil, UserEmail: nil},
			Session:       &components.HookIngestSession{ID: &sessionID, TurnID: nil, Cwd: nil, Model: nil},
			Event:         components.HookIngestEvent{Type: components.TypeToolRequested, OccurredAt: nil},
			Data:          nil,
			Raw:           nil,
		},
	}
	b, err := json.Marshal(entry)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, spoolFileName(time.Now().Add(-age))), b, 0o600))
	return key
}

func makeSpoolEntryGoverned(t *testing.T, name string) {
	t.Helper()
	entry := readSpoolEntry(t, name)
	event := delegation.EventPreToolUse
	entry.Envelope.Source.RawEventName = &event
	entry.Envelope.Event.Type = components.TypeToolRequested
	b, err := json.Marshal(entry)
	require.NoError(t, err)
	dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "gram", "hooks", "spool")
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), b, 0o600))
}

func seedGovernedSpoolEntry(t *testing.T, serverURL string, age time.Duration, configPath string) string {
	t.Helper()
	authFile := filepath.Join(t.TempDir(), "hooks-auth.env")
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authFile)
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	require.NoError(t, writeAuth(creds{ServerURL: serverURL, APIKey: "cached-key", Project: "default", Org: "", RefreshToken: "refresh", ProofPrivateKey: delegation.EncodePrivateKey(privateKey), ContractVersion: delegation.ContractVersion, Source: credCache}))
	seedSpoolEntryWithConfig(t, serverURL, age, "governed", configPath)
	files := spoolFiles(t)
	require.NotEmpty(t, files)
	makeSpoolEntryGoverned(t, files[len(files)-1])
	return authFile
}

func drainEnv(t *testing.T) {
	t.Helper()
	setSpoolStateHome(t)
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "drain-key")
}

// TestDrainReplaysOldestFirstWithStoredKeys pins the replay contract: entries
// go out in chronological order, each under its originally minted
// Idempotency-Key, every request carrying the X-Gram-Replayed marker, and
// accepted entries leave the spool.
func TestDrainReplaysOldestFirstWithStoredKeys(t *testing.T) {
	drainEnv(t)
	fs := newFakeServer(t, nil)
	keyOld := seedSpoolEntry(t, fs.URL, 2*time.Hour, "sess-old")
	keyNew := seedSpoolEntry(t, fs.URL, time.Hour, "sess-new")

	s := Drain(t.Context())
	require.Equal(t, DrainSummary{Replayed: 2, Dropped: 0, Expired: 0, Skipped: 0, Remaining: 0, Aborted: false}, s)
	require.Empty(t, spoolFiles(t), "accepted entries must leave the spool")

	fs.mu.Lock()
	defer fs.mu.Unlock()
	require.Len(t, fs.requests, 2)
	require.Equal(t, "sess-old", *fs.requests[0].Session.ID, "oldest entry must replay first")
	require.Equal(t, "sess-new", *fs.requests[1].Session.ID)
	require.Equal(t, keyOld, fs.headers[0].Get("Idempotency-Key"), "replay must reuse the original send's key")
	require.Equal(t, keyNew, fs.headers[1].Get("Idempotency-Key"))
	for _, h := range fs.headers {
		marker, err := strconv.ParseBool(h.Get("X-Gram-Replayed"))
		require.NoError(t, err)
		require.True(t, marker, "replayed traffic must be distinguishable from live traffic")
	}
}

func TestDrainReplaysSkillContent(t *testing.T) {
	drainEnv(t)
	content := []byte("# Offline skill\n")
	rawSHA256 := sha256Hex(content)
	originalUpload := executeSkillUpload
	t.Cleanup(func() { executeSkillUpload = originalUpload })
	var capturedTasks []skillUploadTask
	failUpload := true
	executeSkillUpload = func(_ context.Context, task skillUploadTask) error {
		capturedTasks = append(capturedTasks, task)
		if failUpload {
			return errors.New("upload unavailable")
		}
		return nil
	}
	fs := newFakeServer(t, nil)
	fs.effects = requestedSkillCaptureEffects(true)
	seedSpoolEntry(t, fs.URL, time.Hour, "sess-offline")

	spoolPath := filepath.Join(os.Getenv("XDG_STATE_HOME"), "gram", "hooks", "spool", spoolFiles(t)[0])
	data, err := os.ReadFile(spoolPath)
	require.NoError(t, err)
	entry, err := decodeSpoolEntry(data)
	require.NoError(t, err)
	entry.Envelope.Data = activatedSkillPayload("offline").Data
	entry.Envelope.Data.Skill.RawSha256 = new(rawSHA256)
	entry.Envelope.Data.ToolCall = &components.HookToolCallData{Output: json.RawMessage(strconv.Quote(string(content)))}
	data, err = json.Marshal(entry)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(spoolPath, data, 0o600))

	summary := Drain(t.Context())
	require.Equal(t, DrainSummary{Replayed: 0, Dropped: 0, Expired: 0, Skipped: 1, Remaining: 1, Aborted: false}, summary)
	require.Len(t, capturedTasks, 1, "the first drain must attempt the upload")
	require.Len(t, spoolFiles(t), 1, "a failed upload must remain retryable")

	failUpload = false
	summary = Drain(t.Context())

	require.Equal(t, 1, summary.Replayed)
	require.Empty(t, spoolFiles(t), "a successful upload must remove the replayed entry")
	require.Equal(t, []skillUploadTask{{
		ServerURL: fs.URL, Project: "default", APIKey: "drain-key", RawSHA256: rawSHA256,
		Content: string(content),
	}, {
		ServerURL: fs.URL, Project: "default", APIKey: "drain-key", RawSHA256: rawSHA256,
		Content: string(content),
	}}, capturedTasks)
	replayed := fs.last().Data.Skill
	require.Equal(t, "offline", replayed.Name)
	require.Nil(t, replayed.SourceLevel)
	require.Nil(t, replayed.SourcePath)
	require.Equal(t, rawSHA256, *replayed.RawSha256)
}

// TestDrainAbortsWhenServerStillDown pins backpressure: the first unsent
// exchange stops the run and the backlog survives untouched.
func TestDrainAbortsWhenServerStillDown(t *testing.T) {
	drainEnv(t)
	url := closedPortURL(t)
	seedSpoolEntry(t, url, 2*time.Hour, "sess-1")
	seedSpoolEntry(t, url, time.Hour, "sess-2")

	s := Drain(t.Context())
	require.True(t, s.Aborted)
	require.Zero(t, s.Replayed)
	require.Equal(t, 2, s.Remaining, "an aborted drain must keep the backlog")
	require.Len(t, spoolFiles(t), 2)
}

// TestDrainDropsDefinitiveRejections: a 4xx answer means a replay would fail
// identically — delete rather than wedge the spool.
func TestDrainDropsDefinitiveRejections(t *testing.T) {
	drainEnv(t)
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusUnprocessableEntity, decision{Decision: "", Reason: "", Message: ""}
	})
	seedSpoolEntry(t, fs.URL, time.Hour, "sess-1")

	s := Drain(t.Context())
	require.Equal(t, 1, s.Dropped)
	require.False(t, s.Aborted)
	require.Empty(t, spoolFiles(t))
}

// TestDrainExpiresStaleEntriesWithoutSending: entries past spoolMaxAge are
// deleted unsent — replaying two-week-old telemetry would only distort
// session analytics.
func TestDrainExpiresStaleEntriesWithoutSending(t *testing.T) {
	drainEnv(t)
	fs := newFakeServer(t, nil)
	seedSpoolEntry(t, fs.URL, spoolMaxAge+time.Hour, "sess-stale")

	s := Drain(t.Context())
	require.Equal(t, 1, s.Expired)
	require.Zero(t, fs.count(), "expired entries must never reach the server")
	require.Empty(t, spoolFiles(t))
}

func TestGovernedReplayBindingIsObservational(t *testing.T) {
	event := delegation.EventPreToolUse
	sessionID := "session-replay"
	entry := spoolEntry{
		V: spoolEntryVersion,
		Envelope: components.IngestRequestBody{
			Source:  components.HookIngestSource{Adapter: delegation.ProviderClaude, RawEventName: &event},
			Session: &components.HookIngestSession{ID: &sessionID},
			Event:   components.HookIngestEvent{Type: components.TypeToolRequested},
		},
	}
	binding, ok := governedReplayBinding(entry)
	require.True(t, ok)
	require.True(t, binding.observational)
	require.Equal(t, sessionID, binding.sessionID)
}

func TestDrainReadsVersionOneEntries(t *testing.T) {
	drainEnv(t)
	fs := newFakeServer(t, nil)
	seedSpoolEntry(t, fs.URL, time.Hour, "sess-v1")
	names := spoolFiles(t)
	require.Len(t, names, 1)
	dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "gram", "hooks", "spool")
	path := filepath.Join(dir, names[0])
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(b, &raw))
	raw["v"] = spoolEntryV1
	b, err = json.Marshal(raw)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, b, 0o600))

	s := Drain(t.Context())
	require.Equal(t, 1, s.Replayed)
	require.Zero(t, s.Remaining)
	require.Equal(t, 1, fs.count())
}

// TestDrainSkipsNewerSchemaEntries: an entry written by a newer binary is
// left for a newer binary — never interpreted, never deleted.
func TestDrainSkipsNewerSchemaEntries(t *testing.T) {
	drainEnv(t)
	fs := newFakeServer(t, nil)
	key := seedSpoolEntry(t, fs.URL, time.Hour, "sess-1")
	_ = key
	names := spoolFiles(t)
	require.Len(t, names, 1)
	dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "gram", "hooks", "spool")
	path := filepath.Join(dir, names[0])
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(b, &raw))
	raw["v"] = spoolEntryVersion + 1
	b, err = json.Marshal(raw)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, b, 0o600))

	s := Drain(t.Context())
	require.Equal(t, 1, s.Skipped)
	require.Equal(t, 1, s.Remaining)
	require.Zero(t, fs.count())
}

// TestDrainSkipsUnparseableEntries: an entry this binary can't decode is not
// necessarily corrupt — a newer binary may have written an event type this
// SDK enum rejects — so it is left in place for a newer binary or the age
// cap, never deleted.
func TestDrainSkipsUnparseableEntries(t *testing.T) {
	drainEnv(t)
	dir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "gram", "hooks", "spool")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, spoolFileName(time.Now())), []byte("{corrupt"), 0o600))

	// The same protection covers a valid entry whose event type this
	// binary's strict SDK enum doesn't know yet.
	future := spoolFileName(time.Now())
	require.NoError(t, os.WriteFile(filepath.Join(dir, future), []byte(`{"v":1,"idempotency_key":"k","server_url":"https://gram.test","envelope":{"schema_version":"`+schemaVersion+`","source":{"adapter":"claude"},"event":{"type":"future.event"}}}`), 0o600))

	s := Drain(t.Context())
	require.Equal(t, 2, s.Skipped)
	require.Zero(t, s.Dropped, "unparseable entries must never be deleted")
	require.Len(t, spoolFiles(t), 2)
}

// TestDrainSkipsWithoutCredentials: no env key, no cache, no config org key
// → the entry stays for a run that can authenticate, and the exit code says
// the run was incomplete.
func TestDrainSkipsWithoutCredentials(t *testing.T) {
	setSpoolStateHome(t)
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	fs := newFakeServer(t, nil)
	seedSpoolEntry(t, fs.URL, time.Hour, "sess-1")

	var out bytes.Buffer
	code := RunDrain(t.Context(), &out)
	require.Equal(t, 0, code, "skipped-only leftovers exit 0 — a retry can't deliver them until machine state changes, and permanent exit-1 noise would train operators to ignore the signal")
	require.Contains(t, out.String(), "skipped=1")
	require.Len(t, spoolFiles(t), 1)
	require.Zero(t, fs.count())
}

// TestDrainUsesConfigOrgKeyFallback: with no env key and no cache, the
// entry's recorded config path supplies the shared org key — the same
// fallback a live send has.
func TestDrainUsesConfigOrgKeyFallback(t *testing.T) {
	setSpoolStateHome(t)
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	fs := newFakeServer(t, nil)

	cfgPath := filepath.Join(t.TempDir(), "speakeasy.json")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`{"server_url":"`+fs.URL+`","project":"default","hooks_api_key":"org-key-1"}`), 0o600))

	seedSpoolEntryWithConfig(t, fs.URL, time.Hour, "sess-1", cfgPath)

	s := Drain(t.Context())
	require.Equal(t, 1, s.Replayed)
	fs.mu.Lock()
	defer fs.mu.Unlock()
	require.Equal(t, "org-key-1", fs.headers[0].Get("Gram-Key"))
}

// A transport fallback must not discard the proof enrollment needed to mint
// assertions for later governed entries in the same deployment backlog.
func TestDrainOrgFallbackPreservesProofForLaterGovernedEntry(t *testing.T) {
	setSpoolStateHome(t)
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	requests := 0
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		requests++
		if requests == 1 {
			return http.StatusUnauthorized, decision{}
		}
		return http.StatusOK, decision{Decision: "allow"}
	})
	cfgPath := filepath.Join(t.TempDir(), "speakeasy.json")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`{"server_url":"`+fs.URL+`","project":"default","hooks_api_key":"org-key"}`), 0o600))
	seedSpoolEntryWithConfig(t, fs.URL, 2*time.Hour, "ungoverned", cfgPath)
	seedGovernedSpoolEntry(t, fs.URL, time.Hour, cfgPath)
	require.Len(t, spoolFiles(t), 2)

	summary := Drain(t.Context())
	require.Equal(t, DrainSummary{Replayed: 2, Remaining: 0}, summary)
	require.Equal(t, 3, fs.count(), "cached rejection, org fallback, then governed replay")
	fs.mu.Lock()
	defer fs.mu.Unlock()
	require.Equal(t, "cached-key", fs.headers[2].Get("Gram-Key"), "a governed assertion must use its bound enrollment key")
	require.NotEmpty(t, fs.headers[2].Get("X-Gram-Acting-User"))
}

func TestDrainAbortsOnMintFailure(t *testing.T) {
	setSpoolStateHome(t)
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	ingestRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rpc/cliAuth.delegateHooksActingUser" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		ingestRequests++
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	seedGovernedSpoolEntry(t, server.URL, time.Hour, "")
	require.Len(t, spoolFiles(t), 1)

	summary := Drain(t.Context())
	require.True(t, summary.Aborted)
	require.Equal(t, 1, summary.Remaining)
	require.Zero(t, summary.Skipped)
	require.Zero(t, ingestRequests)
}

func TestDrainSkipsGovernedEntryWithoutProofAndContinues(t *testing.T) {
	drainEnv(t)
	fs := newFakeServer(t, nil)
	seedSpoolEntry(t, fs.URL, 2*time.Hour, "governed")
	seedSpoolEntry(t, fs.URL, time.Hour, "ordinary")
	files := spoolFiles(t)
	require.Len(t, files, 2)
	makeSpoolEntryGoverned(t, files[0])

	summary := Drain(t.Context())

	require.Equal(t, 1, summary.Replayed)
	require.Equal(t, 1, summary.Skipped)
	require.Equal(t, 1, summary.Remaining)
	require.False(t, summary.Aborted)
	require.Equal(t, 1, fs.count())
}

func TestDrainMintAuthRejectionRequiresReauth(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
	}{
		{name: "unauthorized", status: http.StatusUnauthorized},
		{name: "forbidden", status: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			setSpoolStateHome(t)
			t.Setenv("GRAM_HOOKS_API_KEY", "")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/rpc/cliAuth.delegateHooksActingUser" {
					w.WriteHeader(test.status)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(server.Close)
			authFile := seedGovernedSpoolEntry(t, server.URL, time.Hour, "")
			require.Len(t, spoolFiles(t), 1)

			summary := Drain(t.Context())

			require.True(t, summary.Aborted)
			require.Equal(t, 1, summary.Remaining)
			_, err := os.Stat(authFile)
			require.ErrorIs(t, err, os.ErrNotExist)
			require.True(t, reauthNeeded())
		})
	}
}

// TestMaybeSpawnDrainAfterRecovery drives the real provider path: sends fail
// against a dead server (payloads spool), the server comes back, and the
// next successful send spawns exactly one detached drain — debounced across
// a burst.
func TestMaybeSpawnDrainAfterRecovery(t *testing.T) {
	setSpoolStateHome(t)
	spawns := 0
	orig := startDrainProcess
	startDrainProcess = func() error { spawns++; return nil }
	t.Cleanup(func() { startDrainProcess = orig })

	// Outage: the payload spools, and no drain spawns off a failed send.
	cfg := authedConfig(t, closedPortURL(t))
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Len(t, spoolFiles(t), 1)
	require.Zero(t, spawns, "a failed send must not spawn a drain")

	// Recovery: a successful send with a non-empty spool spawns the drain.
	fs := newFakeServer(t, nil)
	cfg.ServerURL = fs.URL
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Equal(t, 1, spawns, "the first healthy send after an outage must flush the backlog")

	// Burst: the debounce marker suppresses a second spawn.
	invoke(t, cfg, agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Equal(t, 1, spawns, "a burst of healthy sends must not stack drains")
}

// TestMaybeSpawnDrainSkipsEmptySpool: healthy sends on a machine with no
// backlog never pay the spawn cost.
func TestMaybeSpawnDrainSkipsEmptySpool(t *testing.T) {
	setSpoolStateHome(t)
	spawns := 0
	orig := startDrainProcess
	startDrainProcess = func() error { spawns++; return nil }
	t.Cleanup(func() { startDrainProcess = orig })

	fs := newFakeServer(t, nil)
	invoke(t, authedConfig(t, fs.URL), agenthooks.ProviderClaudeCode, "claude/pre_tool_use.json")
	require.Zero(t, spawns)
}

// TestRunDrainExitCodes: 0 when the spool ends empty, 1 when work remains.
func TestRunDrainExitCodes(t *testing.T) {
	drainEnv(t)
	var out bytes.Buffer
	require.Equal(t, 0, RunDrain(t.Context(), &out), "an empty spool is a clean run")

	fs := newFakeServer(t, nil)
	seedSpoolEntry(t, fs.URL, time.Hour, "sess-1")
	out.Reset()
	require.Equal(t, 0, RunDrain(t.Context(), &out))
	require.Contains(t, out.String(), "replayed=1")
}

// TestDrainAuthRejectionPreservesBacklog pins the machine-state rule: a
// rejected credential must never delete entries — the backlog would deliver
// fine after a re-login — and the deployment's remaining entries are skipped
// without further sends.
func TestDrainAuthRejectionPreservesBacklog(t *testing.T) {
	drainEnv(t)
	fs := newFakeServer(t, func(components.IngestRequestBody) (int, decision) {
		return http.StatusUnauthorized, decision{Decision: "", Reason: "", Message: ""}
	})
	seedSpoolEntry(t, fs.URL, 2*time.Hour, "sess-1")
	seedSpoolEntry(t, fs.URL, time.Hour, "sess-2")

	s := Drain(t.Context())
	require.Zero(t, s.Dropped, "auth rejection must never delete backlog")
	require.Equal(t, 2, s.Skipped)
	require.Len(t, spoolFiles(t), 2)
	require.Equal(t, 1, fs.count(), "after one rejection the deployment's remaining entries skip without sends")
}

// TestDrainAuthenticatesFromDiskCache proves a drain with no env key at all
// (the device agent's exec context) resolves the deployment's credential
// from the disk auth cache, exactly as a live send would.
func TestDrainAuthenticatesFromDiskCache(t *testing.T) {
	setSpoolStateHome(t)
	authFile := filepath.Join(t.TempDir(), "hooks-auth.env")
	t.Setenv("GRAM_HOOKS_AUTH_FILE", authFile)
	t.Setenv("GRAM_HOOKS_API_KEY", "")
	fs := newFakeServer(t, nil)

	// Cached credential for this deployment (format matches writeAuth).
	require.NoError(t, os.WriteFile(authFile, []byte(
		"server_url="+fs.URL+"\napi_key=cached-personal-key\nproject=default\nemail=\norg=\n"), 0o600))
	seedSpoolEntry(t, fs.URL, time.Hour, "sess-1")

	s := Drain(t.Context())
	require.Equal(t, 1, s.Replayed)
	fs.mu.Lock()
	defer fs.mu.Unlock()
	require.Equal(t, "cached-personal-key", fs.headers[0].Get("Gram-Key"))
}

// TestDrainEnvKeyPinnedToItsDeployment: the drain batches entries recorded
// by other sessions, so an env key must not be posted to a different
// deployment's server — the entry resolves from its config org key instead.
func TestDrainEnvKeyPinnedToItsDeployment(t *testing.T) {
	setSpoolStateHome(t)
	t.Setenv("GRAM_HOOKS_AUTH_FILE", filepath.Join(t.TempDir(), "hooks-auth.env"))
	t.Setenv("GRAM_HOOKS_API_KEY", "env-key-for-dev")
	t.Setenv("GRAM_HOOKS_SERVER_URL", "http://127.0.0.1:1")
	fs := newFakeServer(t, nil)

	cfgPath := filepath.Join(t.TempDir(), "speakeasy.json")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`{"server_url":"`+fs.URL+`","project":"default","hooks_api_key":"org-key-3"}`), 0o600))
	seedSpoolEntryWithConfig(t, fs.URL, time.Hour, "sess-1", cfgPath)

	s := Drain(t.Context())
	require.Equal(t, 1, s.Replayed)
	fs.mu.Lock()
	defer fs.mu.Unlock()
	require.Equal(t, "org-key-3", fs.headers[0].Get("Gram-Key"),
		"an env key named for another deployment must not be sent to this entry's server")
}

// TestRunDrainAbortExitsNonZero: unreachable mid-run is the one retryable
// outcome, and the exit code says so.
func TestRunDrainAbortExitsNonZero(t *testing.T) {
	drainEnv(t)
	seedSpoolEntry(t, closedPortURL(t), time.Hour, "sess-1")
	var out bytes.Buffer
	require.Equal(t, 1, RunDrain(t.Context(), &out))
	require.Contains(t, out.String(), "aborted=true")
}

// TestDrainPinsEntryProject: a GRAM_HOOKS_PROJECT_SLUG inherited from the
// spawning session must not reroute another project's entries — the stored
// deployment identity is the routing truth.
func TestDrainPinsEntryProject(t *testing.T) {
	drainEnv(t)
	t.Setenv("GRAM_HOOKS_PROJECT_SLUG", "someone-elses-project")
	fs := newFakeServer(t, nil)
	seedSpoolEntry(t, fs.URL, time.Hour, "sess-1")

	s := Drain(t.Context())
	require.Equal(t, 1, s.Replayed)
	fs.mu.Lock()
	defer fs.mu.Unlock()
	require.Equal(t, "default", fs.headers[0].Get("Gram-Project"),
		"the replay must route to the entry's recorded project, not the drain environment's")
}

// TestDecodeSpoolEntryPreservesLargeIntegers pins byte-level fidelity for
// every any-typed envelope field: raw and the tool call's input/output/error
// must survive the spool round-trip without float64 rounding of >2^53
// integers.
func TestDecodeSpoolEntryPreservesLargeIntegers(t *testing.T) {
	const bigID = "9007199254740993" // 2^53 + 1: rounds to ...992 through float64
	entryJSON := `{
		"v": 1,
		"idempotency_key": "k",
		"server_url": "https://gram.test",
		"envelope": {
			"schema_version": "` + schemaVersion + `",
			"source": {"adapter": "claude"},
			"event": {"type": "tool.completed"},
			"data": {"tool_call": {"name": "t", "input": {"id": ` + bigID + `}, "output": {"ts": ` + bigID + `}, "error": {"code": ` + bigID + `}}},
			"raw": {"snowflake": ` + bigID + `}
		}
	}`

	entry, err := decodeSpoolEntry([]byte(entryJSON))
	require.NoError(t, err)

	remarshaled, err := json.Marshal(entry.Envelope)
	require.NoError(t, err)
	require.Equal(t, 4, bytes.Count(remarshaled, []byte(bigID)),
		"raw, input, output, and error must all round-trip the integer exactly; a float64 detour rounds it")
}

// TestDrainReplaysLegacySkillSourcePath pins the upgrade path for v1 entries
// spooled by a binary that recorded the manifest's source path instead of its
// content: the drain re-reads the path and uploads only content that still
// hashes to the value recorded at capture time.
func TestDrainReplaysLegacySkillSourcePath(t *testing.T) {
	drainEnv(t)
	content := []byte("# Offline skill\n")
	rawSHA256 := sha256Hex(content)
	manifest := filepath.Join(t.TempDir(), "SKILL.md")
	require.NoError(t, os.WriteFile(manifest, content, 0o600))
	originalUpload := executeSkillUpload
	t.Cleanup(func() { executeSkillUpload = originalUpload })
	var capturedTasks []skillUploadTask
	executeSkillUpload = func(_ context.Context, task skillUploadTask) error {
		capturedTasks = append(capturedTasks, task)
		return nil
	}
	fs := newFakeServer(t, nil)
	fs.effects = requestedSkillCaptureEffects(true)
	seedSpoolEntry(t, fs.URL, time.Hour, "sess-legacy")

	spoolPath := filepath.Join(os.Getenv("XDG_STATE_HOME"), "gram", "hooks", "spool", spoolFiles(t)[0])
	data, err := os.ReadFile(spoolPath)
	require.NoError(t, err)
	entry, err := decodeSpoolEntry(data)
	require.NoError(t, err)
	entry.Envelope.Data = activatedSkillPayload("legacy").Data
	entry.Envelope.Data.Skill.RawSha256 = new(rawSHA256)
	entry.Envelope.Data.Skill.SourcePath = new(manifest)
	entry.Envelope.Data.ToolCall = nil
	data, err = json.Marshal(entry)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(spoolPath, data, 0o600))

	summary := Drain(t.Context())

	require.Equal(t, 1, summary.Replayed)
	require.Empty(t, spoolFiles(t))
	require.Equal(t, []skillUploadTask{{
		ServerURL: fs.URL, Project: "default", APIKey: "drain-key", RawSHA256: rawSHA256,
		Content: string(content),
	}}, capturedTasks)
}

func TestReplayedSkillRejectsMismatchedLegacySource(t *testing.T) {
	manifest := filepath.Join(t.TempDir(), "SKILL.md")
	require.NoError(t, os.WriteFile(manifest, []byte("edited since capture\n"), 0o600))
	rawSHA256 := sha256Hex([]byte("# original\n"))
	entry := spoolEntry{}
	entry.Envelope.Data = &components.HookIngestData{Skill: &components.HookSkillData{
		Name: "legacy", RawSha256: new(rawSHA256), SourcePath: new(manifest),
	}}

	require.Nil(t, replayedSkill(entry), "content that no longer matches the recorded hash must not upload")
}

func TestReplayedSkillRejectsNonRegularLegacySource(t *testing.T) {
	content := []byte("# original\n")
	target := filepath.Join(t.TempDir(), "SKILL.md")
	require.NoError(t, os.WriteFile(target, content, 0o600))
	link := filepath.Join(t.TempDir(), "SKILL.md")
	require.NoError(t, os.Symlink(target, link))
	rawSHA256 := sha256Hex(content)
	entry := spoolEntry{}
	entry.Envelope.Data = &components.HookIngestData{Skill: &components.HookSkillData{
		Name: "legacy", RawSha256: new(rawSHA256), SourcePath: new(link),
	}}

	require.Nil(t, replayedSkill(entry), "a source path that is not a regular file must drain content-less")
}
