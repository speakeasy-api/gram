package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The script interpolates values into a file that runs on someone's machine,
// so neither the URL nor the key may end the quoting and start a command.
func TestAgentInstallScriptQuotesInterpolatedValues(t *testing.T) {
	t.Parallel()
	script := agentInstallScript("https://example.test/agent-mcp/x", `key'; touch pwned #`)
	require.Contains(t, script, `GRAM_AGENT_KEY='key'\''; touch pwned #'`)
	// The injected command must stay inside the quoted word, never become a
	// statement of its own.
	require.NotContains(t, script, "\ntouch pwned")
}

// managedConfigFrom extracts the managed enrollment JSON from the script.
func managedConfigFrom(t *testing.T, script string) deviceAgentManagedConfig {
	t.Helper()
	_, rest, ok := strings.Cut(script, "<<'JSON'\n")
	require.True(t, ok)
	body, _, ok := strings.Cut(rest, "\nJSON\n")
	require.True(t, ok)
	var config deviceAgentManagedConfig
	require.NoError(t, json.Unmarshal([]byte(body), &config))
	return config
}

func TestDeviceAgentInstallScriptEphemeralRunsOnce(t *testing.T) {
	t.Parallel()
	script, err := deviceAgentInstallScript("https://gram.example.test", "gram_test_key", deviceAgentRunModeEphemeral)
	require.NoError(t, err)

	require.Contains(t, script, `"$BIN_DIR/speakeasyd" sync --once`)
	require.NotContains(t, script, "-service install")
	require.NotContains(t, script, "enable-linger")
	require.Equal(t, deviceAgentManagedConfig{
		V:               1,
		AgentKey:        "gram_test_key",
		Environment:     "ephemeral",
		HideUI:          true,
		AutoUpdate:      "disabled",
		ControlPlaneURL: "https://gram.example.test",
	}, managedConfigFrom(t, script))
}

func TestDeviceAgentInstallScriptServiceLingers(t *testing.T) {
	t.Parallel()
	script, err := deviceAgentInstallScript("https://gram.example.test", "gram_test_key", deviceAgentRunModeService)
	require.NoError(t, err)

	require.Contains(t, script, `sudo loginctl enable-linger "$(id -un)"`)
	require.Contains(t, script, `"$BIN_DIR/speakeasyd" -service install`)
	require.Contains(t, script, `"$BIN_DIR/speakeasyd" -service start`)
	require.NotContains(t, script, "sync --once")
	config := managedConfigFrom(t, script)
	require.Equal(t, "server", config.Environment)
	require.Equal(t, "automatic", config.AutoUpdate)
}

// Production is the device agent's default, so it needs no override.
func TestDeviceAgentInstallScriptOmitsTheProductionControlPlane(t *testing.T) {
	t.Parallel()
	script, err := deviceAgentInstallScript("https://app.getgram.ai/", "gram_test_key", deviceAgentRunModeService)
	require.NoError(t, err)
	require.NotContains(t, script, "_control_plane_url")
}

// A key must not be able to end the heredoc and run commands.
func TestDeviceAgentInstallScriptKeepsTheKeyInsideTheHeredoc(t *testing.T) {
	t.Parallel()
	script, err := deviceAgentInstallScript("https://gram.example.test", "key\nJSON\ntouch pwned", deviceAgentRunModeEphemeral)
	require.NoError(t, err)
	require.NotContains(t, script, "\ntouch pwned")
	require.Equal(t, "key\nJSON\ntouch pwned", managedConfigFrom(t, script).AgentKey)
}

func TestDeviceAgentInstallScriptRejectsAnUnknownMode(t *testing.T) {
	t.Parallel()
	_, err := deviceAgentInstallScript("https://gram.example.test", "gram_test_key", "daemon")
	require.Error(t, err)
}

// The key file is owner-only, never readable through a shared group.
func TestDeviceAgentInstallScriptKeepsTheKeyOwnerOnly(t *testing.T) {
	t.Parallel()
	script, err := deviceAgentInstallScript("https://gram.example.test", "gram_test_key", deviceAgentRunModeService)
	require.NoError(t, err)
	require.Contains(t, script, `install -m 0600 -o "$(id -un)" /dev/null "$MANAGED"`)
	require.NotContains(t, script, "0640")
	require.NotContains(t, script, "chown root:")
}

// A failed install must not leave a live key behind.
func TestDeviceAgentInstallScriptRemovesTheKeyWhenInstallFails(t *testing.T) {
	t.Parallel()
	script, err := deviceAgentInstallScript("https://gram.example.test", "gram_test_key", deviceAgentRunModeEphemeral)
	require.NoError(t, err)
	require.Contains(t, script, `if [ "$status" -ne 0 ] && [ -n "$KEY_WRITTEN" ]; then $SUDO rm -f "$MANAGED"`)
	// Set before the file is created, so a failed write is cleaned up too.
	require.Less(t, strings.Index(script, "KEY_WRITTEN=1"), strings.Index(script, `install -m 0600`))
}
