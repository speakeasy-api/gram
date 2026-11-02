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

	"github.com/speakeasy-api/gram/server/internal/hooks"
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

// The hooks self-test must use the session id the hooks API answers without
// recording, or every install leaves an empty session under the agent.
func TestScriptSelfCheckUsesTheUnrecordedSession(t *testing.T) {
	t.Parallel()
	// The hooks API matches the whole call, not the session id alone, so the
	// whole frame is pinned.
	require.Contains(t, renderScript(t, testParams()), `frame='{"seq":1,"hook":"before_tool_call","event":{"toolName":"exec","toolCallId":"speakeasy-smoke-1","params":{"command":"true"}},"ctx":{"sessionId":"`+hooks.InstallCheckSessionID+`"},"timeoutMs":10000}'`)
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
	require.Contains(t, script, `OPENCLAW_MCP_CONFIG='[{"path":"mcp.servers.speakeasy","value":{"url":"https://app.example.test/agent-mcp/x'\''; touch pwned #"`)
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
		AgentKey    bool   `json:"agent_key"`
	}
	require.NoError(t, json.Unmarshal(zipFile(t, archive, "speakeasy.json"), &config))
	require.Equal(t, testKey, config.HooksAPIKey)
	require.True(t, config.AgentKey)
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

// baseTools are the system tools the script and the stubs call.
var baseTools = []string{"base64", "bash", "cat", "chmod", "cp", "dirname", "grep", "mkdir", "mktemp", "rm", "sed", "tail", "touch", "tr"}

// Fallbacks the script reaches for when the installed plugin is not where it
// expects, to test the archive's own copy.
const (
	toolUnzip   = "unzip"
	toolPython3 = "python3"
)

// toolsDir links the named system tools into a fresh directory, so a PATH
// built from it reaches those tools and no others: no real claude, codex,
// openclaw or curl on the test machine. Optional tools the machine lacks are
// left out.
func toolsDir(t *testing.T, required []string, optional ...string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "tools")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	link := func(name string, mustExist bool) {
		target, err := exec.LookPath(name)
		if err != nil {
			require.False(t, mustExist, "the install script tests need %s on PATH", name)
			return
		}
		require.NoError(t, os.Symlink(target, filepath.Join(dir, name)))
	}
	for _, name := range required {
		link(name, true)
	}
	for _, name := range optional {
		link(name, false)
	}
	return dir
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

	// skipLayout makes the stub install nothing where the script looks for
	// the plugin, so the script falls back to testing the archive.
	skipLayout bool

	// optionalTools are the fallbacks on the script's PATH.
	optionalTools []string

	// env selects isolated profile and failure scenarios for the stub.
	env []string
}

const (
	helpWithAcceptCapabilities    = "Usage: openclaw plugins install [options] <path-or-spec-or-plugin>\n  --force\n  --accept-capabilities  Accept the plugin's declared capabilities\n"
	helpWithoutAcceptCapabilities = "Usage: openclaw plugins install [options] <path-or-spec>\n  --force  Overwrite an existing installed plugin or hook pack\n"
)

// The stub records its argv, answers --help with $STUB_INSTALL_HELP, and on a
// real install keeps the archive, prints $STUB_INSTALL_OUTPUT and, unless
// $STUB_SKIP_LAYOUT is set, lays
// out the installed plugin the way OpenClaw does, world-readable, with the
// stub bootstrapper in place of the real one.
const stubOpenClaw = `#!/bin/sh
printf '%s\n' "$*" >>"$STUB_DIR/openclaw.log"
if [ "$1 $2" = "config set" ]; then
  if [ "$3" = "--help" ]; then
    [ "${STUB_NO_BATCH:-}" = "1" ] || echo --batch-file
    exit 0
  fi
  if [ "$3" = "--batch-file" ]; then
    [ "${STUB_GATEWAY_FAIL:-}" = "1" ] && exit 1
    cp "$4" "$STUB_DIR/mcp-config.json"
  fi
fi
if [ "$1 $2" = "plugins install" ]; then
  case " $* " in
    *" --help "*) printf '%s' "$STUB_INSTALL_HELP"; exit 0 ;;
  esac
  for archive in "$@"; do :; done
  cp "$archive" "$STUB_DIR/installed.zip"
  [ -n "${STUB_INSTALL_OUTPUT:-}" ] && printf '%s\n' "$STUB_INSTALL_OUTPUT"
  [ -n "$STUB_SKIP_LAYOUT" ] && exit 0
  plugin="${STUB_PLUGIN_DIR:-$OPENCLAW_STATE_DIR/extensions/speakeasy-observability}"
  mkdir -p "$plugin/hooks"
  printf '{}\n' >"$plugin/speakeasy.json"
  chmod 644 "$plugin/speakeasy.json"
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
  printf 'org_key=%s\n' "${SPEAKEASY_AI_HOOKS_ORG_KEY:-unset}"
  printf 'old_key=%s\n' "${GRAM_HOOKS_API_KEY:-unset}"
  printf 'old_org_key=%s\n' "${GRAM_HOOKS_ORG_KEY:-unset}"
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
		stubDir:       filepath.Join(root, "stubs"),
		stateDir:      filepath.Join(root, "openclaw-state"),
		workDir:       filepath.Join(root, "work"),
		home:          filepath.Join(root, "home"),
		help:          help,
		reply:         reply,
		skipLayout:    false,
		optionalTools: []string{toolUnzip, toolPython3},
	}
	for _, dir := range []string{h.stubDir, h.stateDir, h.workDir, h.home} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}
	require.NoError(t, os.WriteFile(filepath.Join(h.stubDir, "openclaw"), []byte(stubOpenClaw), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(h.stubDir, "bootstrap.sh"), []byte(stubBootstrap), 0o600))
	return h
}

// run executes the script with only the stubs and the linked system tools on
// PATH, so no real claude, codex, openclaw or curl is reachable.
func (h *openClawHarness) run(t *testing.T, script string) string {
	t.Helper()
	out, err := h.runResult(t, script)
	require.NoError(t, err, "install script failed: %s", out)
	return out
}

func (h *openClawHarness) runResult(t *testing.T, script string) (string, error) {
	t.Helper()
	skipLayout := ""
	if h.skipLayout {
		skipLayout = "1"
	}
	cmd := exec.CommandContext(t.Context(), "/bin/sh", writeScript(t, script))
	cmd.Dir = h.workDir
	cmd.Env = []string{
		"PATH=" + h.stubDir + ":" + toolsDir(t, baseTools, h.optionalTools...),
		"HOME=" + h.home,
		"TMPDIR=" + h.workDir,
		"OPENCLAW_STATE_DIR=" + h.stateDir,
		"STUB_DIR=" + h.stubDir,
		"STUB_INSTALL_HELP=" + h.help,
		"STUB_REPLY=" + h.reply,
		"STUB_SKIP_LAYOUT=" + skipLayout,
		// The self-check must test the plugin's key, not this one, and must
		// not let an outage pass.
		"SPEAKEASY_AI_HOOKS_API_KEY=key-from-the-shell",
		"SPEAKEASY_AI_HOOKS_ORG_KEY=org-key-from-the-shell",
		"GRAM_HOOKS_API_KEY=old-key-from-the-shell",
		"GRAM_HOOKS_ORG_KEY=old-org-key-from-the-shell",
		"SPEAKEASY_AI_HOOKS_FAIL_OPEN=1",
	}
	cmd.Env = append(cmd.Env, h.env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
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
	require.Contains(t, out, "Next: restart the OpenClaw Gateway.")

	calls := h.calls(t)
	require.Len(t, calls, 6, "openclaw calls: %v", calls)
	require.Equal(t, "config set --help", calls[0])
	require.True(t, strings.HasPrefix(calls[1], "config set --batch-file "), calls[1])
	require.Equal(t, "mcp unset gram", calls[2])
	require.NotContains(t, strings.Join(calls, "\n"), testKey, "no client argv may carry the key")
	config := h.stubFile(t, "mcp-config.json")
	require.JSONEq(t, `[{"path":"mcp.servers.speakeasy","value":{"url":"`+testGatewayURL+`","transport":"streamable-http","headers":{"Authorization":"Bearer `+testKey+`"}}}]`, config)
	info, err := os.Stat(filepath.Join(h.stubDir, "mcp-config.json"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
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

	// The installed copy of the key is owner-only, whatever mode the
	// installer gave it.
	plugin := filepath.Join(h.stateDir, "extensions", "speakeasy-observability")
	info, err = os.Stat(filepath.Join(plugin, "speakeasy.json"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	// The self-check ran the installed plugin the way OpenClaw does.
	require.Equal(t, "agenthooks serve --provider=openclaw --timeout=15s --config="+plugin+"/speakeasy.json", h.stubFile(t, "bootstrap.args"))
	require.Contains(t, h.stubFile(t, "bootstrap.stdin"), `"hook":"before_tool_call"`)
	// The plugin's own key, no cached sign-in, and an outage reads as a
	// refusal: only Speakeasy accepting the key can pass.
	require.Equal(t, "key=unset\norg_key=unset\nold_key=unset\nold_org_key=unset\nfail_open=unset\norg_settings=present", h.stubFile(t, "bootstrap.env"))
}

// Current OpenClaw applies the installed plugin to the running Gateway, and a
// needless restart note sends the agent off to restart its own Gateway.
func TestScriptSkipsTheRestartNoteWhenOpenClawAppliedThePlugin(t *testing.T) {
	t.Parallel()
	h := newOpenClawHarness(t, helpWithAcceptCapabilities, `{"seq":1}`)
	h.env = append(h.env, "STUB_INSTALL_OUTPUT=Installed plugin: speakeasy-observability\nApplied in Gateway generation 4.")
	out := h.run(t, renderScript(t, testParams()))

	require.Contains(t, out, "Applied in Gateway generation 4.", "the installer's own output is shown")
	require.Contains(t, out, "Speakeasy hooks: PASS")
	require.NotContains(t, out, "restart the OpenClaw Gateway")
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
// answers 403, which it does for a missing hooks grant.
func TestScriptExplainsAForbiddenHooksKey(t *testing.T) {
	t.Parallel()
	reply := `{"seq":1,"output":{"block":true,"blockReason":"permission denied"}}`
	h := newOpenClawHarness(t, helpWithAcceptCapabilities, reply)
	out := h.run(t, renderScript(t, testParams()))

	require.Contains(t, out, "Speakeasy hooks: FAIL: permission denied")
	require.Contains(t, out, "org:hooks_ingest")
	require.NotContains(t, out, "project:read")
	require.NotContains(t, out, "PASS")
}

// A refusal is reported with the hooks binary's own reason, here the one it
// gives for an unreachable server. That an outage is a refusal rather than a
// pass rests on the posture the self-check pins, which
// TestScriptConfiguresOpenClawAndPassesTheSelfCheck asserts.
func TestScriptReportsTheHooksBinarysBlockReason(t *testing.T) {
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

// When the installed plugin is not where this release is expected to keep it,
// the script tests the archive's own copy, and python3 unpacks it on images
// without unzip. That copy runs the real bootstrapper, which, with no uname or
// curl on this PATH, stops offline with its own diagnostic: reaching it is the
// proof the archive was unpacked.
func TestScriptUnpacksTheArchiveWithPython3WhenUnzipIsMissing(t *testing.T) {
	t.Parallel()
	_, err := exec.LookPath(toolPython3)
	require.NoError(t, err, "this test needs python3 on PATH")
	h := newOpenClawHarness(t, helpWithAcceptCapabilities, `{"seq":1}`)
	h.skipLayout = true
	h.optionalTools = []string{toolPython3}
	out := h.run(t, renderScript(t, testParams()))

	require.NotContains(t, out, "could not find it to test it")
	require.Contains(t, out, "Speakeasy hooks: FAIL: the hooks binary did not answer. speakeasy-hooks:")
}

func TestScriptPythonFallbacksIgnoreModulesInTheWorkingDirectory(t *testing.T) {
	t.Parallel()
	h := newOpenClawHarness(t, helpWithAcceptCapabilities, `{"seq":1}`)
	h.skipLayout = true
	h.optionalTools = []string{toolPython3}
	// Force both decoding and extraction through Python rather than base64.
	require.NoError(t, os.WriteFile(filepath.Join(h.stubDir, "base64"), []byte("#!/bin/sh\nexit 1\n"), 0o700))
	for _, name := range []string{"base64.py", "zipfile.py"} {
		require.NoError(t, os.WriteFile(filepath.Join(h.workDir, name), []byte("from pathlib import Path\nPath('python-shadow-ran').touch()\nraise RuntimeError('shadow import')\n"), 0o600))
	}
	out := h.run(t, renderScript(t, testParams()))
	require.NotContains(t, out, "could not decode the plugin")
	require.NotContains(t, out, "could not find it to test it")
	require.Contains(t, out, "Speakeasy hooks: FAIL: the hooks binary did not answer. speakeasy-hooks:")
	_, err := os.Stat(filepath.Join(h.workDir, "python-shadow-ran"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestScriptOnlyWarnsAboutExplicitCredentialOverrides(t *testing.T) {
	t.Parallel()
	h := newOpenClawHarness(t, helpWithAcceptCapabilities, `{"seq":1}`)
	cached := filepath.Join(h.home, ".config", "speakeasy-ai", "hooks-auth.env")
	require.NoError(t, os.MkdirAll(filepath.Dir(cached), 0o700))
	require.NoError(t, os.WriteFile(cached, []byte("api_key=cached-personal-key\norg="+testOrgID+"\n"), 0o600))
	out := h.run(t, renderScript(t, testParams()))
	require.Contains(t, out, "SPEAKEASY_AI_HOOKS_API_KEY is set")
	require.Contains(t, out, "SPEAKEASY_AI_HOOKS_ORG_KEY is set")
	require.NotContains(t, out, "cached Speakeasy sign-in")
	require.NotContains(t, h.stubFile(t, "bootstrap.env"), "sign_in=present")
}

func TestScriptReportsAPluginItCannotTest(t *testing.T) {
	t.Parallel()
	h := newOpenClawHarness(t, helpWithAcceptCapabilities, `{"seq":1}`)
	h.skipLayout = true
	h.optionalTools = nil
	out := h.run(t, renderScript(t, testParams()))

	require.Contains(t, out, "Speakeasy hooks: FAIL: the plugin is installed, but this script could not find it to test it.")
	require.Contains(t, out, "Install unzip, bsdtar or python3")
	// The plugin is installed all the same, so the Gateway still needs a restart.
	require.Contains(t, out, "Next: restart the OpenClaw Gateway.")
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
	require.Contains(t, out, "Speakeasy hooks: FAIL: this agent has no project, or its project was deleted")
	// OpenClaw applies a gateway entry by itself; only a plugin needs a restart.
	require.NotContains(t, out, "restart the OpenClaw Gateway")

	calls := h.calls(t)
	require.Len(t, calls, 3, "openclaw calls: %v", calls)
	require.True(t, strings.HasPrefix(calls[1], "config set --batch-file "), calls[1])
}

func TestScriptWithNoClientsExplainsManualSetup(t *testing.T) {
	t.Parallel()
	cmd := exec.CommandContext(t.Context(), "/bin/sh", writeScript(t, renderScript(t, testParams())))
	cmd.Dir = t.TempDir()
	cmd.Env = []string{"PATH=" + toolsDir(t, baseTools), "HOME=" + cmd.Dir}
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "%s", out)
	require.Equal(t, 1, exitErr.ExitCode())
	require.Contains(t, string(out), "No supported MCP client (Claude Code, Codex, OpenClaw) found on PATH.")
	require.Contains(t, string(out), testGatewayURL)
}

func TestScriptRejectsUnsafeCredentialOrigins(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"http://example.test", "ftp://example.test", "https:///missing-host", "https://user:pass@example.test", "/relative", ":bad"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			p := testParams()
			p.GatewayURL = raw
			script, err := Script(p)
			require.ErrorContains(t, err, "gateway URL must use HTTPS")
			require.Empty(t, script)
			p = testParams()
			p.ServerURL = raw
			script, err = Script(p)
			require.ErrorContains(t, err, "hooks server URL must use HTTPS")
			require.Empty(t, script)
		})
	}
}

func TestScriptKeepsPreviousGatewayWhenConfigIsRejected(t *testing.T) {
	t.Parallel()
	h := newOpenClawHarness(t, helpWithAcceptCapabilities, `{"seq":1}`)
	h.env = []string{"STUB_GATEWAY_FAIL=1"}
	out, err := h.runResult(t, renderScript(t, testParams()))
	require.Error(t, err)
	require.Contains(t, out, "Previous gateway entries were kept")
	require.Contains(t, out, "Speakeasy hooks: PASS")
	require.Contains(t, out, "Supported clients were found, but no gateway was configured")
	require.NotContains(t, out, "No supported MCP client")
	require.NotContains(t, strings.Join(h.calls(t), "\n"), "mcp unset")
}

func TestScriptExplainsUnsupportedSafeConfiguration(t *testing.T) {
	t.Parallel()
	h := newOpenClawHarness(t, helpWithAcceptCapabilities, `{"seq":1}`)
	h.env = []string{"STUB_NO_BATCH=1"}
	out, err := h.runResult(t, renderScript(t, testParams()))
	require.Error(t, err)
	require.Contains(t, out, "update OpenClaw")
	require.Contains(t, out, "Speakeasy hooks: PASS")
	require.NotContains(t, strings.Join(h.calls(t), "\n"), "mcp unset")
}

func TestScriptContinuesAfterClaudeFailsWithoutLeakingKeyInArguments(t *testing.T) {
	t.Parallel()
	h := newOpenClawHarness(t, helpWithAcceptCapabilities, `{"seq":1}`)
	stub := "#!/bin/sh\nprintf '%s\\n' \"$*\" >>\"$STUB_DIR/claude.log\"\nexit 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(h.stubDir, "claude"), []byte(stub), 0o700))
	out := h.run(t, renderScript(t, testParams()))
	require.Contains(t, out, "Could not configure Claude Code")
	require.Contains(t, out, "Configured OpenClaw")
	args := h.stubFile(t, "claude.log")
	require.NotContains(t, args, testKey)
	require.NotContains(t, args, "mcp remove")
	require.Contains(t, args, "Authorization: Bearer ${SPEAKEASY_AGENT_KEY}")
}

func TestScriptProtectsAndTestsTheNamedProfilesInstalledPlugin(t *testing.T) {
	t.Parallel()
	h := newOpenClawHarness(t, helpWithAcceptCapabilities, `{"seq":1}`)
	plugin := filepath.Join(h.home, ".openclaw-testing", "extensions", "speakeasy-observability")
	h.env = []string{"OPENCLAW_STATE_DIR=", "OPENCLAW_PROFILE=testing", "STUB_PLUGIN_DIR=" + plugin}
	out := h.run(t, renderScript(t, testParams()))
	require.Contains(t, out, "Speakeasy hooks: PASS")
	info, err := os.Stat(filepath.Join(plugin, "speakeasy.json"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	require.Contains(t, h.stubFile(t, "bootstrap.args"), "--config="+plugin+"/speakeasy.json")
}

func TestScriptOmitsUnsupportedPluginFlags(t *testing.T) {
	t.Parallel()
	h := newOpenClawHarness(t, "Usage: openclaw plugins install [path]", `{"seq":1}`)
	out := h.run(t, renderScript(t, testParams()))
	require.Contains(t, out, "Speakeasy hooks: PASS")
	calls := h.calls(t)
	require.Len(t, calls, 6, "openclaw calls: %v", calls)
	require.NotContains(t, calls[4], "--force")
	require.NotContains(t, calls[4], "--accept-capabilities")
}

func TestScriptPreservesCodexConfigurationWhenAddingFails(t *testing.T) {
	t.Parallel()
	h := newOpenClawHarness(t, helpWithAcceptCapabilities, `{"seq":1}`)
	stub := "#!/bin/sh\nprintf '%s\\n' \"$*\" >>\"$STUB_DIR/codex.log\"\nexit 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(h.stubDir, "codex"), []byte(stub), 0o700))
	out := h.run(t, renderScript(t, testParams()))
	require.Contains(t, out, "Could not configure Codex. Its existing entry was kept")
	require.Contains(t, out, "Configured OpenClaw")
	require.NotContains(t, h.stubFile(t, "codex.log"), "mcp remove")
	// Only OpenClaw was configured, and it keeps the key in its own config.
	require.NotContains(t, out, "was exported for this run only")
}

// Claude Code and Codex read the key from the environment when they start, so
// the script says to store it and names only the clients it configured. An
// OpenClaw-only run says nothing: an agent relays that advice.
func TestScriptAsksToStoreTheKeyOnlyForClientsThatReadIt(t *testing.T) {
	t.Parallel()
	stub := []byte("#!/bin/sh\nexit 0\n")
	for _, tc := range []struct {
		clients []string
		names   string
	}{
		{clients: []string{"claude"}, names: "Claude Code can read it"},
		{clients: []string{"codex"}, names: "Codex can read it"},
		{clients: []string{"claude", "codex"}, names: "Claude Code and Codex can read it"},
	} {
		h := newOpenClawHarness(t, helpWithAcceptCapabilities, `{"seq":1}`)
		for _, client := range tc.clients {
			require.NoError(t, os.WriteFile(filepath.Join(h.stubDir, client), stub, 0o700))
		}
		out := h.run(t, renderScript(t, testParams()))
		require.Contains(t, out, "SPEAKEASY_AGENT_KEY was exported for this run only. Store it where\n"+tc.names+" when started.", "clients %v", tc.clients)
	}

	openClawOnly := newOpenClawHarness(t, helpWithAcceptCapabilities, `{"seq":1}`)
	out := openClawOnly.run(t, renderScript(t, testParams()))
	require.Contains(t, out, "Done.")
	require.NotContains(t, out, "SPEAKEASY_AGENT_KEY")
}

func TestScriptUsesBSDTarWhenOtherExtractorsAreMissing(t *testing.T) {
	t.Parallel()
	h := newOpenClawHarness(t, helpWithAcceptCapabilities, `{"seq":1}`)
	h.skipLayout = true
	h.optionalTools = nil
	stub := `#!/bin/sh
printf '%s\n' "$*" >"$STUB_DIR/bsdtar.log"
[ "$1" = "-xf" ] && [ "$3" = "-C" ] || exit 1
mkdir -p "$4/hooks"
printf '{}\n' >"$4/speakeasy.json"
cp "$STUB_DIR/bootstrap.sh" "$4/hooks/bootstrap.sh"
`
	require.NoError(t, os.WriteFile(filepath.Join(h.stubDir, "bsdtar"), []byte(stub), 0o700))
	out := h.run(t, renderScript(t, testParams()))
	require.Contains(t, out, "Speakeasy hooks: PASS")
	require.Contains(t, h.stubFile(t, "bsdtar.log"), "-xf ")
}
