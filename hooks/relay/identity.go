package relay

import (
	"bufio"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/speakeasy-api/agenthooks"
)

var emailPattern = regexp.MustCompile(`^[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}$`)

// resolveUserEmail mirrors the released hook's best-effort attribution chain:
// managed device identity first, then a provider-owned account identity.
// Failures never affect delivery or policy enforcement.
func resolveUserEmail(ctx context.Context, typed any) string {
	if email := deviceAgentEmail(ctx); email != "" {
		return email
	}
	base := agenthooks.EventOf(typed)
	if base.Provider == agenthooks.ProviderCodex && base.Kind == agenthooks.KindSessionStart {
		if email := codexAppServerEmail(ctx); email != "" {
			return email
		}
		if email := codexAuthFileEmail(); email != "" {
			return email
		}
	}
	return topLevelPayloadEmail(base.Raw)
}

func deviceAgentEmail(ctx context.Context) string {
	timeout := tenthsDuration("GRAM_DEVICE_AGENT_TIMEOUT_TENTHS", 15)
	for _, name := range deviceAgentCommands() {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		commandCtx, cancel := context.WithTimeout(ctx, timeout)
		output, err := exec.CommandContext(commandCtx, name, "identity").Output()
		cancel()
		if err != nil {
			continue
		}
		if email := identityOutputEmail(output); email != "" {
			return email
		}
	}
	// Rescue tier: a daemon running from a location the exec chain can't
	// guess still owns a socket at a fixed per-OS path — ask it directly.
	// The env override is a real off switch and silences this tier too.
	if strings.TrimSpace(os.Getenv("GRAM_DEVICE_AGENT_COMMANDS")) != "" {
		return ""
	}
	return socketIdentityEmail(ctx)
}

// socketAgentTimeout bounds the whole socket-rescue request. Only a *hung*
// daemon ever pays it — a missing socket fails the dial instantly — and hooks
// must never hold an editor hostage to a wedged one.
const socketAgentTimeout = 150 * time.Millisecond

// socketIdentityEmail asks the running daemon for identity over its IPC
// socket (GET /v1/identity — the device agent's api.SocketPath and
// IdentityResponse contracts; keep the paths below in sync). This is the
// rescue, not the primary, on purpose: `speakeasyd identity` re-reads the
// config files per call, while the daemon serves its boot-time snapshot —
// exec is fresher whenever both are available. One attempt, no retry.
func socketIdentityEmail(ctx context.Context) string {
	socket := agentSocketPath()
	if socket == "" {
		return ""
	}
	requestCtx, cancel := context.WithTimeout(ctx, socketAgentTimeout)
	defer cancel()
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(dialCtx context.Context, _, _ string) (net.Conn, error) {
			return dialAgentSocket(dialCtx, socket)
		},
		// One-shot client: without this, the transport pools the socket
		// connection (plus its read/write goroutines) for the process
		// lifetime — a per-invocation leak, since the transport is never
		// reused or closed.
		DisableKeepAlives: true,
	}}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, "http://speakeasy-agent/v1/identity", nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return ""
	}
	var identity struct {
		V        int    `json:"v"`
		Enrolled bool   `json:"enrolled"`
		Email    string `json:"email"`
	}
	if json.Unmarshal(body, &identity) != nil {
		return ""
	}
	// Contract rule: a document version newer than we understand is
	// unreadable, same as the exec path's consumers.
	if identity.V > 1 || !identity.Enrolled || !validEmail(identity.Email) {
		return ""
	}
	return identity.Email
}

// agentSocketPath mirrors the device agent's api.SocketPath, including the
// SPEAKEASY_SOCKET override.
func agentSocketPath() string {
	if v := os.Getenv("SPEAKEASY_SOCKET"); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		return `\\.\pipe\speakeasy-agent`
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/speakeasy-agent.sock"
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "Speakeasy", "agent.sock")
	}
	return filepath.Join(home, ".speakeasy", "agent.sock")
}

// deviceAgentCommands returns the device-agent commands to exec, in order.
// GRAM_DEVICE_AGENT_COMMANDS (comma-separated) replaces the default set
// entirely — and silences the socket rescue in deviceAgentEmail, so it stays
// a complete off switch. The default is a bare PATH lookup, then static
// well-known install locations: speakeasyd is typically NOT on PATH (MDM
// installs place it under the per-user Speakeasy directory without touching
// PATH, and GUI-spawned hook hosts see a minimal PATH anyway), so a bare
// lookup alone silently loses managed identity attribution. A daemon running
// somewhere none of these cover is handled by the socket rescue.
func deviceAgentCommands() []string {
	if env := strings.TrimSpace(os.Getenv("GRAM_DEVICE_AGENT_COMMANDS")); env != "" {
		return strings.Split(env, ",")
	}
	commands := []string{"speakeasyd"}
	home, _ := os.UserHomeDir()
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		if home != "" {
			candidates = append(candidates, filepath.Join(home, "Library", "Application Support", "Speakeasy", "bin", "speakeasyd"))
		}
		candidates = append(candidates,
			"/usr/local/bin/speakeasyd",
			"/Library/Application Support/Speakeasy/speakeasyd",
		)
	case "windows":
		candidates = append(candidates, `C:\Program Files\Speakeasy\speakeasyd.exe`)
	default:
		if home != "" {
			candidates = append(candidates,
				// ~/.speakeasy is the agent's conventional dotdir on Linux
				// (its IPC socket lives there); kept as a supported install
				// location by maintainer decision even though no packaged
				// rollout writes it today.
				filepath.Join(home, ".speakeasy", "bin", "speakeasyd"),
				filepath.Join(home, ".local", "bin", "speakeasyd"),
			)
		}
		candidates = append(candidates, "/usr/local/bin/speakeasyd")
	}
	for _, candidate := range candidates {
		if executableFile(candidate) {
			commands = append(commands, candidate)
		}
	}
	return commands
}

func executableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode()&0o111 != 0
}

func identityOutputEmail(output []byte) string {
	if email := strings.TrimSpace(string(output)); validEmail(email) {
		return email
	}
	var value any
	if json.Unmarshal(output, &value) != nil {
		return ""
	}
	return emailFromJSON(value)
}

func emailFromJSON(value any) string {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range []string{"email", "user_email", "userEmail", "mail", "preferred_username"} {
			if email, ok := v[key].(string); ok && validEmail(email) {
				return email
			}
		}
		for _, child := range v {
			if email := emailFromJSON(child); email != "" {
				return email
			}
		}
	case []any:
		for _, child := range v {
			if email := emailFromJSON(child); email != "" {
				return email
			}
		}
	}
	return ""
}

func topLevelPayloadEmail(raw json.RawMessage) string {
	var payload struct {
		UserEmail string `json:"user_email"`
	}
	if json.Unmarshal(raw, &payload) != nil || !validEmail(payload.UserEmail) {
		return ""
	}
	return payload.UserEmail
}

func codexAppServerEmail(ctx context.Context) string {
	binary := findCodexBinary()
	if binary == "" {
		return ""
	}
	commandCtx, cancel := context.WithTimeout(ctx, tenthsDuration("GRAM_CODEX_IDENTITY_TIMEOUT_TENTHS", 10))
	defer cancel()
	cmd := exec.CommandContext(commandCtx, binary, "app-server", "--stdio")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return ""
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return ""
	}
	if cmd.Start() != nil {
		_ = stdin.Close()
		return ""
	}
	defer func() {
		_ = stdin.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()

	scanner := bufio.NewScanner(stdout)
	if _, err := fmt.Fprintln(stdin, `{"id":71001,"method":"initialize","params":{"clientInfo":{"name":"gram_hooks","title":"Gram Hooks","version":"1.0.0"},"capabilities":{"optOutNotificationMethods":["remoteControl/status/changed"]}}}`); err != nil {
		return ""
	}
	if codexResponse(scanner, 71001) == nil {
		return ""
	}
	if _, err := fmt.Fprintln(stdin, `{"method":"initialized"}`); err != nil {
		return ""
	}
	if _, err := fmt.Fprintln(stdin, `{"id":71002,"method":"account/read","params":{"refreshToken":false}}`); err != nil {
		return ""
	}
	return codexAccountEmail(codexResponse(scanner, 71002))
}

func codexAccountEmail(result any) string {
	object, ok := result.(map[string]any)
	if !ok {
		return ""
	}
	account, ok := object["account"].(map[string]any)
	if !ok {
		return ""
	}
	email, _ := account["email"].(string)
	if !validEmail(email) {
		return ""
	}
	return email
}

func codexResponse(scanner *bufio.Scanner, id int) any {
	for scanner.Scan() {
		var response struct {
			ID     json.RawMessage `json:"id"`
			Result any             `json:"result"`
		}
		if json.Unmarshal(scanner.Bytes(), &response) == nil && string(response.ID) == strconv.Itoa(id) {
			return response.Result
		}
	}
	return nil
}

func findCodexBinary() string {
	if path, err := exec.LookPath("codex"); err == nil {
		return path
	}
	home, _ := os.UserHomeDir()
	codexHome := firstNonEmpty(os.Getenv("CODEX_HOME"), filepath.Join(home, ".codex"))
	return probeCodexBinary(codexBinaryCandidates(home, codexHome))
}

// probeCodexBinary returns the first candidate that is a runnable file. A
// present-but-non-executable path is skipped rather than returned: the caller
// execs the result, so a path it cannot run is worse than none.
func probeCodexBinary(candidates []string) string {
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return candidate
		}
	}
	return ""
}

// codexBinaryCandidates lists the install locations to probe, in preference
// order, when codex is not on PATH. A managed or user install outranks the
// copies bundled inside an app: OpenAI merged the standalone Codex app into
// the ChatGPT app, so ChatGPT.app is probed ahead of the frozen Codex.app and
// a machine carrying both prefers the maintained copy. Mirrors the ordering
// the generated install script's find_codex uses.
func codexBinaryCandidates(home, codexHome string) []string {
	candidates := []string{
		filepath.Join(codexHome, "packages", "standalone", "current", "bin", "codex"),
		filepath.Join(home, ".local", "bin", "codex"),
		"/usr/local/bin/codex",
		"/Applications/ChatGPT.app/Contents/Resources/codex",
	}
	// The editor extensions ship their own copy, and on a machine that runs
	// Codex only from an editor it is the only one there is.
	candidates = append(candidates, codexEditorExtensionBinaries(home)...)
	// Frozen: superseded by the unified ChatGPT app, so it ranks last.
	return append(candidates, "/Applications/Codex.app/Contents/Resources/codex")
}

// codexEditorExtensionBinaries finds the codex copies bundled with the ChatGPT
// editor extensions. Both path segments vary — the extension directory carries
// the version and platform, the inner directory the target triple — so they are
// globbed rather than listed. Matches come back newest-first by the version
// parsed out of the extension directory name: an editor leaves the superseded
// build on disk across an upgrade, and probing a stale codex first means
// running a binary the user already replaced. The whole set is ordered at once
// rather than per editor, because the newest build on a machine may live under
// any of them.
func codexEditorExtensionBinaries(home string) []string {
	if home == "" {
		return nil
	}
	var found []string
	versions := map[string][]int{}
	for _, editorDir := range []string{".vscode", ".vscode-insiders", ".vscode-server", ".cursor", ".windsurf"} {
		matches, err := filepath.Glob(filepath.Join(home, editorDir, "extensions", "openai.chatgpt-*", "bin", "*", "codex"))
		if err != nil {
			continue
		}
		for _, match := range matches {
			versions[match] = codexExtensionVersion(match)
			found = append(found, match)
		}
	}
	// Stable so equally-versioned copies keep glob order and the probe order
	// stays reproducible run to run.
	slices.SortStableFunc(found, func(a, b string) int {
		return compareCodexExtensionVersions(versions[a], versions[b])
	})
	return found
}

// codexExtensionVersion reads the version out of an
// openai.chatgpt-<version>-<platform> extension directory as its numeric
// components, so 26.10.0 can outrank 26.9.0 — a lexical comparison gets that
// backwards the moment a component grows a digit. A name we cannot read yields
// nil, which the ordering treats as unknown rather than as version zero.
func codexExtensionVersion(binary string) []int {
	// <extensions>/openai.chatgpt-<version>-<platform>/bin/<triple>/codex
	name := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(binary))))
	rest, ok := strings.CutPrefix(name, "openai.chatgpt-")
	if !ok {
		return nil
	}
	version, _, _ := strings.Cut(rest, "-")
	var components []int
	for field := range strings.SplitSeq(version, ".") {
		number, err := strconv.Atoi(field)
		if err != nil {
			return nil
		}
		components = append(components, number)
	}
	return components
}

// compareCodexExtensionVersions orders parsed versions newest-first. An
// unreadable version sorts last but is never dropped: a copy that exists on
// disk is still worth probing when nothing better is installed.
func compareCodexExtensionVersions(a, b []int) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1
	case len(b) == 0:
		return -1
	}
	for i := range max(len(a), len(b)) {
		var left, right int
		if i < len(a) {
			left = a[i]
		}
		if i < len(b) {
			right = b[i]
		}
		if left != right {
			return cmp.Compare(right, left)
		}
	}
	return 0
}

func codexAuthFileEmail() string {
	home, _ := os.UserHomeDir()
	codexHome := firstNonEmpty(os.Getenv("CODEX_HOME"), filepath.Join(home, ".codex"))
	b, err := os.ReadFile(filepath.Join(codexHome, "auth.json"))
	if err != nil {
		return ""
	}
	var auth struct {
		Tokens map[string]string `json:"tokens"`
	}
	if json.Unmarshal(b, &auth) != nil {
		return ""
	}
	for _, key := range []string{"access_token", "id_token"} {
		if email := jwtEmail(auth.Tokens[key]); email != "" {
			return email
		}
	}
	return ""
}

func jwtEmail(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}
	claims, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var value any
	if json.Unmarshal(claims, &value) != nil {
		return ""
	}
	return emailFromJSON(value)
}

func tenthsDuration(name string, fallback int) time.Duration {
	tenths := fallback
	if parsed, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name))); err == nil && parsed > 0 {
		tenths = parsed
	}
	return time.Duration(tenths) * 100 * time.Millisecond
}

func validEmail(email string) bool {
	return emailPattern.MatchString(strings.TrimSpace(email))
}
