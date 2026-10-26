package agentinstall

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	testGatewayURL = "https://app.example.test/agent-mcp/00000000-0000-0000-0000-000000000001"
	testKey        = "gram_live_agent_key_for_tests"
	testServerURL  = "https://app.example.test"
	testOrgID      = "org-for-tests"
	testProject    = "agents-project"
)

func testParams() Params {
	return Params{
		GatewayURL:      testGatewayURL,
		Key:             testKey,
		ServerURL:       testServerURL,
		OrganizationID:  testOrgID,
		ProjectSlug:     testProject,
		InstallFailOpen: false,
	}
}

func renderScript(t *testing.T, p Params) string {
	t.Helper()
	script, err := Script(p)
	require.NoError(t, err)
	return script
}

func writeScript(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "install.sh")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o600))
	return path
}

// embeddedPlugin decodes the base64 heredoc the script writes the OpenClaw
// plugin from, and opens it as the ZIP OpenClaw receives.
func embeddedPlugin(t *testing.T, script string) *zip.Reader {
	t.Helper()
	const start = "cat <<'SPEAKEASY_OPENCLAW_PLUGIN'\n"
	const end = "\nSPEAKEASY_OPENCLAW_PLUGIN\n"
	from := strings.Index(script, start)
	require.NotEqual(t, -1, from, "script must embed the OpenClaw plugin")
	body := script[from+len(start):]
	to := strings.Index(body, end)
	require.NotEqual(t, -1, to, "the plugin heredoc must be closed")

	archive, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(body[:to], "\n", ""))
	require.NoError(t, err)
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	require.NoError(t, err)
	return reader
}

func zipFile(t *testing.T, archive *zip.Reader, name string) []byte {
	t.Helper()
	f, err := archive.Open(name)
	require.NoError(t, err, "plugin archive must contain %s", name)
	defer func() { require.NoError(t, f.Close()) }()
	content, err := io.ReadAll(f)
	require.NoError(t, err)
	return content
}

func TestScriptIsValidShell(t *testing.T) {
	t.Parallel()
	withoutProject := testParams()
	withoutProject.ProjectSlug = ""

	for name, p := range map[string]Params{"with hooks": testParams(), "without hooks": withoutProject} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, err := exec.CommandContext(t.Context(), "/bin/sh", "-n", writeScript(t, renderScript(t, p))).CombinedOutput()
			require.NoError(t, err, "sh -n: %s", out)
		})
	}
}

// The script interpolates values into a file that runs on someone's machine,
// so no value may end the quoting and start a command.
func TestScriptQuotesInterpolatedValues(t *testing.T) {
	t.Parallel()
	hostile := `key'; touch pwned #$(touch pwned)` + "`touch pwned`"
	p := testParams()
	p.GatewayURL = "https://app.example.test/agent-mcp/x'; touch pwned #"
	p.Key = hostile

	script := renderScript(t, p)
	require.Contains(t, script, `SPEAKEASY_AGENT_KEY='key'\''; touch pwned #$(touch pwned)`+"`touch pwned`'")
	require.Contains(t, script, `GRAM_AGENT_URL='https://app.example.test/agent-mcp/x'\''; touch pwned #'`)
	require.Contains(t, script, `OPENCLAW_MCP_SERVER='{"url":"https://app.example.test/agent-mcp/x'\''; touch pwned #"`)
	require.NotContains(t, script, "\ntouch pwned")

	// Run it for real: whatever a client stub sees, nothing may execute.
	h := newOpenClawHarness(t, helpWithAcceptCapabilities, `{"seq":1}`)
	out := h.run(t, script)
	require.Contains(t, out, "Configured OpenClaw.")
	_, err := os.Stat(filepath.Join(h.workDir, "pwned"))
	require.ErrorIs(t, err, os.ErrNotExist, "an interpolated value ran as a command")
}

func TestScriptEmbedsTheAgentsOpenClawPlugin(t *testing.T) {
	t.Parallel()
	archive := embeddedPlugin(t, renderScript(t, testParams()))

	var config struct {
		ServerURL   string `json:"server_url"`
		Project     string `json:"project"`
		Org         string `json:"org"`
		HooksAPIKey string `json:"hooks_api_key"`
	}
	require.NoError(t, json.Unmarshal(zipFile(t, archive, "speakeasy.json"), &config))
	require.Equal(t, testKey, config.HooksAPIKey)
	require.Equal(t, testProject, config.Project)
	require.Equal(t, testServerURL, config.ServerURL)
	require.Equal(t, testOrgID, config.Org)

	var manifest struct {
		ID         string `json:"id"`
		Activation struct {
			OnCapabilities []string `json:"onCapabilities"`
		} `json:"activation"`
	}
	require.NoError(t, json.Unmarshal(zipFile(t, archive, "openclaw.plugin.json"), &manifest))
	require.Equal(t, "speakeasy-observability", manifest.ID)
	require.Contains(t, manifest.Activation.OnCapabilities, "hook")

	zipFile(t, archive, "index.js")
	zipFile(t, archive, "hooks/bootstrap.sh")
}

// openClawHarness runs a rendered script against a stub openclaw that records
// every invocation, and a stub hooks bootstrapper that answers the self-check.
type openClawHarness struct {
	stubDir  string
	stateDir string
	workDir  string
	home     string
	help     string
	reply    string
}

const (
	helpWithAcceptCapabilities    = "Usage: openclaw plugins install [options] <path-or-spec-or-plugin>\n  --force\n  --accept-capabilities  Accept the plugin's declared capabilities\n"
	helpWithoutAcceptCapabilities = "Usage: openclaw plugins install [options] <path-or-spec>\n  --force  Overwrite an existing installed plugin or hook pack\n"
)

// The stub records its argv, answers --help with $STUB_INSTALL_HELP, and on a
// real install keeps the archive and lays out the installed plugin the way
// OpenClaw does, with the stub bootstrapper in place of the real one.
const stubOpenClaw = `#!/bin/sh
printf '%s\n' "$*" >>"$STUB_DIR/openclaw.log"
if [ "$1 $2" = "plugins install" ]; then
  case " $* " in
    *" --help "*) printf '%s' "$STUB_INSTALL_HELP"; exit 0 ;;
  esac
  for archive in "$@"; do :; done
  cp "$archive" "$STUB_DIR/installed.zip"
  plugin="$OPENCLAW_STATE_DIR/extensions/speakeasy-observability"
  mkdir -p "$plugin/hooks"
  printf '{}\n' >"$plugin/speakeasy.json"
  cp "$STUB_DIR/bootstrap.sh" "$plugin/hooks/bootstrap.sh"
fi
exit 0
`

// The stub bootstrapper stands in for the hooks binary: it records how it was
// called, which credentials and outage posture it would have seen, and
// answers with $STUB_REPLY.
const stubBootstrap = `printf '%s\n' "$*" >"$STUB_DIR/bootstrap.args"
{
  printf 'key=%s\n' "${SPEAKEASY_AI_HOOKS_API_KEY:-unset}"
  printf 'fail_open=%s\n' "${SPEAKEASY_AI_HOOKS_FAIL_OPEN:-unset}"
  if [ -f "$SPEAKEASY_AI_HOOKS_AUTH_FILE.org-settings.json" ]; then
    echo "org_settings=present"
  fi
  if [ -f "$SPEAKEASY_AI_HOOKS_AUTH_FILE" ]; then
    echo "sign_in=present"
  fi
} >"$STUB_DIR/bootstrap.env"
cat >"$STUB_DIR/bootstrap.stdin"
printf '%s\n' "$STUB_REPLY"
`

func newOpenClawHarness(t *testing.T, help, reply string) *openClawHarness {
	t.Helper()
	root := t.TempDir()
	h := &openClawHarness{
		stubDir:  filepath.Join(root, "stubs"),
		stateDir: filepath.Join(root, "openclaw-state"),
		workDir:  filepath.Join(root, "work"),
		home:     filepath.Join(root, "home"),
		help:     help,
		reply:    reply,
	}
	for _, dir := range []string{h.stubDir, h.stateDir, h.workDir, h.home} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}
	require.NoError(t, os.WriteFile(filepath.Join(h.stubDir, "openclaw"), []byte(stubOpenClaw), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(h.stubDir, "bootstrap.sh"), []byte(stubBootstrap), 0o600))
	return h
}

// run executes the script with only the stubs and the base system on PATH, so
// no real claude, codex, openclaw or curl is reachable.
func (h *openClawHarness) run(t *testing.T, script string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "/bin/sh", writeScript(t, script))
	cmd.Dir = h.workDir
	cmd.Env = []string{
		"PATH=" + h.stubDir + ":/usr/bin:/bin",
		"HOME=" + h.home,
		"TMPDIR=" + h.workDir,
		"OPENCLAW_STATE_DIR=" + h.stateDir,
		"STUB_DIR=" + h.stubDir,
		"STUB_INSTALL_HELP=" + h.help,
		"STUB_REPLY=" + h.reply,
		// The self-check must test the plugin's key, not this one, and must
		// not let an outage pass.
		"SPEAKEASY_AI_HOOKS_API_KEY=key-from-the-shell",
		"SPEAKEASY_AI_HOOKS_FAIL_OPEN=1",
	}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "install script failed: %s", out)
	return string(out)
}

func (h *openClawHarness) calls(t *testing.T) []string {
	t.Helper()
	log, err := os.ReadFile(filepath.Join(h.stubDir, "openclaw.log"))
	require.NoError(t, err)
	return strings.Split(strings.TrimSpace(string(log)), "\n")
}

func (h *openClawHarness) stubFile(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(h.stubDir, name))
	require.NoError(t, err)
	return strings.TrimSpace(string(content))
}

// installedArchive is the last argument the stub saw on a real install.
func installedArchive(t *testing.T, call string) string {
	t.Helper()
	fields := strings.Fields(call)
	return fields[len(fields)-1]
}

func TestScriptConfiguresOpenClawAndPassesTheSelfCheck(t *testing.T) {
	t.Parallel()
	h := newOpenClawHarness(t, helpWithAcceptCapabilities, `{"seq":1}`)
	out := h.run(t, renderScript(t, testParams()))

	require.Contains(t, out, "Configured OpenClaw.")
	require.Contains(t, out, "Speakeasy hooks: PASS")
	require.NotContains(t, out, "FAIL")

	mcpServer := `{"url":"` + testGatewayURL + `","transport":"streamable-http","headers":{"Authorization":"Bearer ` + testKey + `"}}`
	calls := h.calls(t)
	require.Len(t, calls, 6, "openclaw calls: %v", calls)
	require.Equal(t, "mcp unset gram", calls[0])
	require.Equal(t, "mcp unset speakeasy", calls[1])
	require.Equal(t, "mcp set speakeasy "+mcpServer, calls[2])
	require.Equal(t, "plugins install --help", calls[3])
	require.True(t, strings.HasPrefix(calls[4], "plugins install --force --accept-capabilities "), calls[4])
	require.True(t, strings.HasSuffix(calls[4], ".zip"), calls[4])
	require.Equal(t, "config set plugins.entries.speakeasy-observability.hooks.allowConversationAccess true", calls[5])

	// OpenClaw got the agent's plugin, and the script cleaned up after itself.
	installed, err := os.ReadFile(filepath.Join(h.stubDir, "installed.zip"))
	require.NoError(t, err)
	archive, err := zip.NewReader(bytes.NewReader(installed), int64(len(installed)))
	require.NoError(t, err)
	require.Contains(t, string(zipFile(t, archive, "speakeasy.json")), testKey)
	_, err = os.Stat(installedArchive(t, calls[4]))
	require.ErrorIs(t, err, os.ErrNotExist, "the archive carries the key and must not outlive the script")

	// The self-check ran the installed plugin the way OpenClaw does.
	plugin := filepath.Join(h.stateDir, "extensions", "speakeasy-observability")
	require.Equal(t, "agenthooks serve --provider=openclaw --timeout=15s --config="+plugin+"/speakeasy.json", h.stubFile(t, "bootstrap.args"))
	require.Contains(t, h.stubFile(t, "bootstrap.stdin"), `"hook":"before_tool_call"`)
	// The plugin's own key, no cached sign-in, and an outage reads as a
	// refusal: only Speakeasy accepting the key can pass.
	require.Equal(t, "key=unset\nfail_open=unset\norg_settings=present", h.stubFile(t, "bootstrap.env"))
}

// Releases before 2026.8.1 reject --accept-capabilities, so the script asks
// the installed CLI before passing it.
func TestScriptOmitsAcceptCapabilitiesForOlderOpenClaw(t *testing.T) {
	t.Parallel()
	h := newOpenClawHarness(t, helpWithoutAcceptCapabilities, `{"seq":1}`)
	out := h.run(t, renderScript(t, testParams()))
	require.Contains(t, out, "Speakeasy hooks: PASS")

	calls := h.calls(t)
	require.Len(t, calls, 6, "openclaw calls: %v", calls)
	require.True(t, strings.HasPrefix(calls[4], "plugins install --force "), calls[4])
	require.NotContains(t, calls[4], "--accept-capabilities")
}

// The reply is what the pinned hooks binary returns when the hooks API
// answers 403, which it does for a missing hooks or project grant.
func TestScriptExplainsAForbiddenHooksKey(t *testing.T) {
	t.Parallel()
	reply := `{"seq":1,"output":{"block":true,"blockReason":"permission denied"}}`
	h := newOpenClawHarness(t, helpWithAcceptCapabilities, reply)
	out := h.run(t, renderScript(t, testParams()))

	require.Contains(t, out, "Speakeasy hooks: FAIL: permission denied")
	require.Contains(t, out, "org:hooks_ingest")
	require.Contains(t, out, "project:read")
	require.NotContains(t, out, "PASS")
}

// With the outage posture pinned closed, an unreachable server comes back as
// a refusal naming the cause, never as a pass.
func TestScriptReportsAnUnreachableServer(t *testing.T) {
	t.Parallel()
	reply := `{"seq":1,"output":{"block":true,"blockReason":"Speakeasy hooks could not resolve the Speakeasy AI Control Plane host (dns)."}}`
	h := newOpenClawHarness(t, helpWithAcceptCapabilities, reply)
	out := h.run(t, renderScript(t, testParams()))

	require.Contains(t, out, "Speakeasy hooks: FAIL: Speakeasy hooks could not resolve the Speakeasy AI Control Plane host (dns).")
	require.NotContains(t, out, "org:hooks_ingest")
}

func TestScriptReportsAHooksBinaryThatNeverAnswers(t *testing.T) {
	t.Parallel()
	h := newOpenClawHarness(t, helpWithAcceptCapabilities, "")
	out := h.run(t, renderScript(t, testParams()))
	require.Contains(t, out, "Speakeasy hooks: FAIL: the hooks binary did not answer.")
}

// Without a project the hooks API has nowhere to record sessions, so the
// script still connects OpenClaw to the gateway but says hooks are missing.
func TestScriptWithoutAProjectConfiguresOpenClawWithoutHooks(t *testing.T) {
	t.Parallel()
	p := testParams()
	p.ProjectSlug = ""
	script := renderScript(t, p)
	require.NotContains(t, script, "SPEAKEASY_OPENCLAW_PLUGIN")

	h := newOpenClawHarness(t, helpWithAcceptCapabilities, `{"seq":1}`)
	out := h.run(t, script)
	require.Contains(t, out, "Configured OpenClaw.")
	require.Contains(t, out, "Speakeasy hooks: FAIL: this agent belongs to no project")

	calls := h.calls(t)
	require.Len(t, calls, 3, "openclaw calls: %v", calls)
	require.True(t, strings.HasPrefix(calls[2], "mcp set speakeasy "), calls[2])
}

func TestScriptWithNoClientsExplainsManualSetup(t *testing.T) {
	t.Parallel()
	cmd := exec.CommandContext(t.Context(), "/bin/sh", writeScript(t, renderScript(t, testParams())))
	cmd.Dir = t.TempDir()
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + cmd.Dir}
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "%s", out)
	require.Equal(t, 1, exitErr.ExitCode())
	require.Contains(t, string(out), "No supported MCP client (Claude Code, Codex, OpenClaw) found on PATH.")
	require.Contains(t, string(out), testGatewayURL)
}
