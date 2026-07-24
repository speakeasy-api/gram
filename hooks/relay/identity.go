package relay

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
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
	return ""
}

// deviceAgentCommands returns the device-agent commands to try, in order.
// GRAM_DEVICE_AGENT_COMMANDS (comma-separated) replaces the default set
// entirely. The default order — the device agent's HOOK_IDENTITY contract —
// is: the path the daemon advertises about itself, then a bare PATH lookup,
// then static well-known install locations for daemons too old to advertise.
// speakeasyd is typically NOT on PATH (MDM installs place it under the
// per-user Speakeasy directory without touching PATH, and GUI-spawned hook
// hosts see a minimal PATH anyway), so a bare lookup alone silently loses
// managed identity attribution.
func deviceAgentCommands() []string {
	if env := strings.TrimSpace(os.Getenv("GRAM_DEVICE_AGENT_COMMANDS")); env != "" {
		return strings.Split(env, ",")
	}
	var commands []string
	if advertised := advertisedAgentPath(); advertised != "" {
		commands = append(commands, advertised)
	}
	commands = append(commands, "speakeasyd")
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
			candidates = append(candidates, filepath.Join(home, ".local", "bin", "speakeasyd"))
		}
		candidates = append(candidates, "/usr/local/bin/speakeasyd")
	}
	for _, candidate := range candidates {
		if executableFile(candidate) && candidate != commands[0] {
			commands = append(commands, candidate)
		}
	}
	return commands
}

// advertisedAgentPath reads the daemon's self-advertisement: on every startup
// speakeasyd writes its own executable path to a fixed per-OS file (the
// device agent's core/paths.AgentPathFile — keep the locations in sync). This
// is the only discovery mode that works regardless of where IT installed the
// daemon, on every OS. Windows is machine-wide (%ProgramData%) because the
// daemon runs as LocalSystem there, whose profile dirs per-user hook
// processes can't see. Returns "" when the file is absent (older daemon),
// unreadable, or names something that isn't an executable file.
func advertisedAgentPath() string {
	var file string
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		file = filepath.Join(home, "Library", "Application Support", "Speakeasy", "agent-path")
	case "windows":
		programData := os.Getenv("PROGRAMDATA")
		if programData == "" {
			programData = `C:\ProgramData`
		}
		file = filepath.Join(programData, "Speakeasy", "agent-path")
	default:
		stateHome := os.Getenv("XDG_STATE_HOME")
		if stateHome == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return ""
			}
			stateHome = filepath.Join(home, ".local", "state")
		}
		file = filepath.Join(stateHome, "speakeasy", "agent-path")
	}
	content, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	advertised := strings.TrimSpace(string(content))
	if !filepath.IsAbs(advertised) || !executableFile(advertised) {
		return ""
	}
	return advertised
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
	for _, candidate := range []string{
		filepath.Join(codexHome, "packages", "standalone", "current", "bin", "codex"),
		filepath.Join(home, ".local", "bin", "codex"),
		"/usr/local/bin/codex",
		"/Applications/Codex.app/Contents/Resources/codex",
	} {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return candidate
		}
	}
	return ""
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
