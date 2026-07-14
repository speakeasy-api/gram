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
	commands := strings.Split(firstNonEmpty(os.Getenv("GRAM_DEVICE_AGENT_COMMANDS"), "speakeasyd"), ",")
	timeout := tenthsDuration("GRAM_DEVICE_AGENT_TIMEOUT_TENTHS", 15)
	for _, name := range commands {
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
