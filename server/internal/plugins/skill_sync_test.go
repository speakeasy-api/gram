package plugins

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// skillSyncHarness renders skill_sync.sh alongside the shared http/auth helpers
// into a temp dir and seeds a cached hooks credential so gram_hooks_prepare_auth
// succeeds against the given (local) server URL. It returns the plugin dir plus
// the resolved skills and state directories the script will operate on.
type skillSyncHarness struct {
	dir       string
	scriptDir string
	skillsDir string
	stateDir  string
	authFile  string
	serverURL string
}

func newSkillSyncHarness(t *testing.T, serverURL string) *skillSyncHarness {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "skill_sync.sh"), renderSkillSyncScript(GenerateConfig{ServerURL: "https://app.getgram.ai"}), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "http.sh"), renderSharedHTTPScript(), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.sh"), renderSharedAuthScript(), 0o755))

	skillsDir := filepath.Join(dir, "skills")
	stateDir := filepath.Join(dir, "state")
	require.NoError(t, os.MkdirAll(stateDir, 0o755))

	authFile := filepath.Join(dir, "auth.env")
	// Seed a cached hooks key matching the server URL so prepare_auth resolves
	// without a browser login. Org is left blank (legacy cache) so the org
	// binding check is skipped.
	require.NoError(t, os.WriteFile(authFile, []byte(
		"server_url="+serverURL+"\napi_key=gram_test_key\nproject=default\nemail=\norg=\n"), 0o600))

	return &skillSyncHarness{
		dir:       dir,
		scriptDir: dir,
		skillsDir: skillsDir,
		stateDir:  stateDir,
		authFile:  authFile,
		serverURL: serverURL,
	}
}

func (h *skillSyncHarness) manifestPath() string {
	return filepath.Join(h.stateDir, "skill-sync-manifest.tsv")
}

// run executes skill_sync.sh with the given hook event and returns stdout.
func (h *skillSyncHarness) run(t *testing.T, event string, extraEnv ...string) string {
	t.Helper()
	cmd := exec.Command("bash", filepath.Join(h.scriptDir, "skill_sync.sh"))
	cmd.Stdin = strings.NewReader(`{"hook_event_name":"` + event + `","session_id":"sess-1"}`)
	env := hookAuthTestEnv(h.dir,
		"GRAM_HOOKS_AUTH_FILE="+h.authFile,
		"GRAM_HOOKS_SERVER_URL="+h.serverURL,
		"GRAM_SKILLS_DIR="+h.skillsDir,
		"GRAM_HOOKS_STATE_DIR="+h.stateDir,
		"CI=1",
	)
	cmd.Env = append(env, extraEnv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// The script must always exit 0 — a sync failure may never fail a session.
	require.NoError(t, cmd.Run(), "skill_sync.sh must always exit 0; stderr: %s", stderr.String())
	return stdout.String()
}

// b64tsv encodes tab-separated rows the way the wire contract expects.
func b64tsv(rows ...[]string) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(strings.Join(r, "\t"))
		b.WriteString("\n")
	}
	return base64.StdEncoding.EncodeToString([]byte(b.String()))
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// bundleB64 builds a base64 gzip tarball whose entries are keyed by path.
func bundleB64(t *testing.T, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

// syncResponse is the plan+bundle envelope the endpoint returns on 2xx.
type syncResponse struct {
	PlanB64   string `json:"plan_b64"`
	BundleB64 string `json:"bundle_b64,omitempty"`
}

// syncServer is a stand-in for /rpc/skills.sync that serves a fixed response
// and records receipt payloads for assertions.
type syncServer struct {
	mu       sync.Mutex
	response syncResponse
	status   int
	receipts []string // decoded receipt TSVs
	requests int
}

func (s *syncServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		_ = json.Unmarshal(body, &payload)

		s.mu.Lock()
		s.requests++
		if rb, ok := payload["receipts_b64"]; ok && rb != "" {
			if dec, err := base64.StdEncoding.DecodeString(rb); err == nil {
				s.receipts = append(s.receipts, string(dec))
			}
			s.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}
		status := s.status
		resp := s.response
		s.mu.Unlock()

		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func (s *syncServer) receiptText() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.receipts, "")
}

// TestSkillSyncDistributeMaterializesAndAnnounces covers the distribute case:
// a net-new skill is written into the skills dir, recorded in the manifest,
// announced via SessionStart additionalContext, and reported live.
func TestSkillSyncDistributeMaterializesAndAnnounces(t *testing.T) {
	t.Parallel()
	srv := &syncServer{
		response: syncResponse{
			PlanB64:   b64tsv([]string{"UPDATE", "research", "sha256:aaa", "research/SKILL.md", b64("Deep research skill")}),
			BundleB64: bundleB64(t, map[string]string{"research/SKILL.md": "# Research\nBody v1\n"}),
		},
	}
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)

	h := newSkillSyncHarness(t, ts.URL)
	stdout := h.run(t, "SessionStart")

	got, err := os.ReadFile(filepath.Join(h.skillsDir, "research", "SKILL.md"))
	require.NoError(t, err, "skill file must be materialized")
	require.Equal(t, "# Research\nBody v1\n", string(got))

	manifest, err := os.ReadFile(h.manifestPath())
	require.NoError(t, err)
	require.Contains(t, string(manifest), "research\tsha256:aaa")

	require.Contains(t, stdout, "additionalContext")
	require.Contains(t, stdout, "research")
	require.Contains(t, stdout, "read before use")
	require.Contains(t, srv.receiptText(), "research\tlive\tsha256:aaa")
}

// TestSkillSyncUpdateRefreshesBody covers the update case: an owned skill's
// body is refreshed at the next SessionStart and the manifest hash advances,
// with no re-announcement (it is not net-new).
func TestSkillSyncUpdateRefreshesBody(t *testing.T) {
	t.Parallel()
	srv := &syncServer{
		response: syncResponse{
			PlanB64:   b64tsv([]string{"UPDATE", "research", "sha256:bbb", "research/SKILL.md", b64("Deep research skill")}),
			BundleB64: bundleB64(t, map[string]string{"research/SKILL.md": "# Research\nBody v2\n"}),
		},
	}
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)

	h := newSkillSyncHarness(t, ts.URL)
	// Pre-existing owned skill at the old hash.
	require.NoError(t, os.MkdirAll(filepath.Join(h.skillsDir, "research"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(h.skillsDir, "research", "SKILL.md"), []byte("# Research\nBody v1\n"), 0o644))
	require.NoError(t, os.WriteFile(h.manifestPath(), []byte("research\tsha256:aaa\n"), 0o644))

	stdout := h.run(t, "SessionStart")

	got, err := os.ReadFile(filepath.Join(h.skillsDir, "research", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "# Research\nBody v2\n", string(got), "body must be refreshed")

	manifest, err := os.ReadFile(h.manifestPath())
	require.NoError(t, err)
	require.Contains(t, string(manifest), "research\tsha256:bbb")
	require.NotContains(t, stdout, "additionalContext", "an already-owned skill must not be re-announced")
	require.Contains(t, srv.receiptText(), "research\tlive\tsha256:bbb")
}

// TestSkillSyncUndistributeRemovesFile covers undistribute: a REMOVE op deletes
// the owned skill dir and drops it from the manifest.
func TestSkillSyncUndistributeRemovesFile(t *testing.T) {
	t.Parallel()
	srv := &syncServer{
		response: syncResponse{PlanB64: b64tsv([]string{"REMOVE", "research"})},
	}
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)

	h := newSkillSyncHarness(t, ts.URL)
	require.NoError(t, os.MkdirAll(filepath.Join(h.skillsDir, "research"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(h.skillsDir, "research", "SKILL.md"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(h.manifestPath(), []byte("research\tsha256:aaa\n"), 0o644))

	h.run(t, "SessionStart")

	_, err := os.Stat(filepath.Join(h.skillsDir, "research"))
	require.True(t, os.IsNotExist(err), "removed skill dir must be gone")
	manifest, err := os.ReadFile(h.manifestPath())
	require.NoError(t, err)
	require.NotContains(t, string(manifest), "research")
	require.Contains(t, srv.receiptText(), "research\tremoved")
}

// TestSkillSyncConflictSkipsUnownedSkill covers the conflict case: a personal
// skill of the same name that the sync did not create is left untouched and
// reported shadowed, never clobbered.
func TestSkillSyncConflictSkipsUnownedSkill(t *testing.T) {
	t.Parallel()
	srv := &syncServer{
		response: syncResponse{
			PlanB64:   b64tsv([]string{"UPDATE", "research", "sha256:ccc", "research/SKILL.md", b64("desc")}),
			BundleB64: bundleB64(t, map[string]string{"research/SKILL.md": "# From server\n"}),
		},
	}
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)

	h := newSkillSyncHarness(t, ts.URL)
	// User's own skill of the same name, NOT recorded in the manifest.
	require.NoError(t, os.MkdirAll(filepath.Join(h.skillsDir, "research"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(h.skillsDir, "research", "SKILL.md"), []byte("# My own skill\n"), 0o644))

	stdout := h.run(t, "SessionStart")

	got, err := os.ReadFile(filepath.Join(h.skillsDir, "research", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "# My own skill\n", string(got), "unowned skill must not be overwritten")
	require.NotContains(t, stdout, "additionalContext", "a shadowed skill must not be announced as installed")
	require.Contains(t, srv.receiptText(), "research\tshadowed\tsha256:ccc")
}

// TestSkillSyncDegradedInlinesBodies covers degraded mode: when the skills root
// cannot be created (a path component is a regular file, which fails mkdir even
// as root), the skill body is inlined via additionalContext and reported
// fs_readonly rather than written to disk.
func TestSkillSyncDegradedInlinesBodies(t *testing.T) {
	t.Parallel()
	srv := &syncServer{
		response: syncResponse{
			PlanB64:   b64tsv([]string{"UPDATE", "research", "sha256:ddd", "research/SKILL.md", b64("desc")}),
			BundleB64: bundleB64(t, map[string]string{"research/SKILL.md": "# Inlined body content\n"}),
		},
	}
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)

	h := newSkillSyncHarness(t, ts.URL)
	// Make the skills root unresolvable: its parent component is a file, so
	// mkdir -p fails with ENOTDIR regardless of user (works even under root).
	blocker := filepath.Join(h.dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))
	unwritable := filepath.Join(blocker, "skills")

	stdout := h.run(t, "SessionStart", "GRAM_SKILLS_DIR="+unwritable)

	require.Contains(t, stdout, "additionalContext")
	require.Contains(t, stdout, "Inlined body content", "degraded mode must inline the skill body")
	require.Contains(t, srv.receiptText(), "research\tfs_readonly\tsha256:ddd")
}

// TestSkillSyncUnauthorizedRemovesManagedSkills covers the 401/403 case:
// authenticated distribution removes all managed skills when the identity is
// rejected (no fail-open ratchet for distribution).
func TestSkillSyncUnauthorizedRemovesManagedSkills(t *testing.T) {
	t.Parallel()
	srv := &syncServer{status: http.StatusUnauthorized}
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)

	h := newSkillSyncHarness(t, ts.URL)
	require.NoError(t, os.MkdirAll(filepath.Join(h.skillsDir, "research"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(h.skillsDir, "research", "SKILL.md"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(h.manifestPath(), []byte("research\tsha256:aaa\n"), 0o644))

	h.run(t, "SessionStart")

	_, err := os.Stat(filepath.Join(h.skillsDir, "research"))
	require.True(t, os.IsNotExist(err), "401 must remove managed skills")
	_, err = os.Stat(h.manifestPath())
	require.True(t, os.IsNotExist(err), "401 must clear the manifest")
}

// TestSkillSyncNetworkFailureKeepsStale covers the transport-failure case: an
// unreachable endpoint (and, equivalently, a 404 before the endpoint ships)
// leaves the last-synced set untouched.
func TestSkillSyncNetworkFailureKeepsStale(t *testing.T) {
	t.Parallel()

	// Point at a closed loopback port so curl returns a connection error.
	h := newSkillSyncHarness(t, "http://127.0.0.1:1")
	require.NoError(t, os.MkdirAll(filepath.Join(h.skillsDir, "research"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(h.skillsDir, "research", "SKILL.md"), []byte("keep me\n"), 0o644))
	require.NoError(t, os.WriteFile(h.manifestPath(), []byte("research\tsha256:aaa\n"), 0o644))

	stdout := h.run(t, "SessionStart", "GRAM_HTTP_MAX_ATTEMPTS=1")

	got, err := os.ReadFile(filepath.Join(h.skillsDir, "research", "SKILL.md"))
	require.NoError(t, err, "stale skill must survive a network failure")
	require.Equal(t, "keep me\n", string(got))
	require.Empty(t, stdout)
}

// TestSkillSync404KeepsStale covers the endpoint-not-deployed case explicitly:
// a 404 (feature/endpoint not live yet) is treated like any non-2xx and keeps
// the stale set, so shipping the client ahead of the server is inert.
func TestSkillSync404KeepsStale(t *testing.T) {
	t.Parallel()
	srv := &syncServer{status: http.StatusNotFound}
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)

	h := newSkillSyncHarness(t, ts.URL)
	require.NoError(t, os.MkdirAll(filepath.Join(h.skillsDir, "research"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(h.skillsDir, "research", "SKILL.md"), []byte("keep\n"), 0o644))
	require.NoError(t, os.WriteFile(h.manifestPath(), []byte("research\tsha256:aaa\n"), 0o644))

	h.run(t, "SessionStart")

	got, err := os.ReadFile(filepath.Join(h.skillsDir, "research", "SKILL.md"))
	require.NoError(t, err)
	require.Equal(t, "keep\n", string(got), "a 404 must not disturb the managed set")
}

// TestSkillSyncSessionEndDoesNotAnnounce covers that the best-effort SessionEnd
// refresh still materializes files but never writes additionalContext (stdout
// is not read back into the model at session end).
func TestSkillSyncSessionEndDoesNotAnnounce(t *testing.T) {
	t.Parallel()
	srv := &syncServer{
		response: syncResponse{
			PlanB64:   b64tsv([]string{"UPDATE", "research", "sha256:eee", "research/SKILL.md", b64("desc")}),
			BundleB64: bundleB64(t, map[string]string{"research/SKILL.md": "# End\n"}),
		},
	}
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)

	h := newSkillSyncHarness(t, ts.URL)
	stdout := h.run(t, "SessionEnd")

	got, err := os.ReadFile(filepath.Join(h.skillsDir, "research", "SKILL.md"))
	require.NoError(t, err, "SessionEnd still refreshes the managed set")
	require.Equal(t, "# End\n", string(got))
	require.Empty(t, stdout, "SessionEnd must not emit additionalContext")
}

// TestSkillSyncScriptWiredIntoClaudeObservabilityPlugin verifies the generator
// emits skill_sync.sh and wires it onto SessionStart/SessionEnd only when
// SkillSync is enabled, and never for other platforms.
func TestSkillSyncScriptWiredIntoClaudeObservabilityPlugin(t *testing.T) {
	t.Parallel()
	base := GenerateConfig{
		OrgName:     "Acme",
		ServerURL:   "https://app.getgram.ai",
		HooksAPIKey: "gram_local_secret_xyz",
		ProjectSlug: "default",
		Version:     "1.2.3",
	}

	off, err := GeneratePluginPackages(nil, base)
	require.NoError(t, err)
	claudeDir := ClaudeObservabilitySlug(base)
	_, present := off[filepath.Join(claudeDir, "hooks/skill_sync.sh")]
	require.False(t, present, "skill_sync.sh must not ship unless SkillSync is enabled")

	on := base
	on.SkillSync = true
	files, err := GeneratePluginPackages(nil, on)
	require.NoError(t, err)
	require.Contains(t, files, filepath.Join(claudeDir, "hooks/skill_sync.sh"))

	hooksJSON := string(files[filepath.Join(claudeDir, "hooks/hooks.json")])
	require.Contains(t, hooksJSON, "skill_sync.sh")
	// Cursor/Codex observability plugins do not get the (Claude-only) client.
	cursorHooks := files[filepath.Join(cursorPluginRoot, CursorObservabilitySlug(on), "hooks/hooks.json")]
	require.NotEmpty(t, cursorHooks, "cursor observability hooks.json should exist")
	require.NotContains(t, string(cursorHooks), "skill_sync.sh")
}
