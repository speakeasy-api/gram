package mcpmetadata

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBuildStdioConfig_ExactFormat verifies the config snippet JSON format
// matches the original template output exactly. This is a regression test
// to ensure the DRY refactoring didn't change the output format.
func TestBuildStdioConfig_ExactFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		mcpURL         string
		inputs         []securityInput
		expectedJSON   string
		expectedEnvLen int
	}{
		{
			name:   "no security inputs - basic config",
			mcpURL: "https://mcp.example.com/mcp/test-server",
			inputs: []securityInput{},
			// This matches what the old template produced for a public server
			expectedJSON: `{
  "command": "npx",
  "args": [
    "mcp-remote@0.1.25",
    "https://mcp.example.com/mcp/test-server"
  ]
}`,
			expectedEnvLen: 0,
		},
		{
			name:   "gram security mode - two headers",
			mcpURL: "https://mcp.example.com/mcp/private-server",
			inputs: []securityInput{
				{
					SystemName:  "gram_environment",
					DisplayName: "gram-environment",
					Sensitive:   false,
				},
				{
					SystemName:  "authorization",
					DisplayName: "gram-key",
					Sensitive:   true,
				},
			},
			// This matches what the old template produced for gram auth
			expectedJSON: `{
  "command": "npx",
  "args": [
    "mcp-remote@0.1.25",
    "https://mcp.example.com/mcp/private-server",
    "--header",
    "Gram-Environment:${GRAM_ENVIRONMENT}",
    "--header",
    "Authorization:${GRAM_KEY}"
  ],
  "env": {
    "GRAM_ENVIRONMENT": "<your-value-here>",
    "GRAM_KEY": "<your-value-here>"
  }
}`,
			expectedEnvLen: 2,
		},
		{
			name:   "public with custom security variable",
			mcpURL: "https://api.example.com/mcp/public-api",
			inputs: []securityInput{
				{
					SystemName:  "MCP-API_KEY",
					DisplayName: "MCP-API-KEY",
					Sensitive:   true,
				},
			},
			expectedJSON: `{
  "command": "npx",
  "args": [
    "mcp-remote@0.1.25",
    "https://api.example.com/mcp/public-api",
    "--header",
    "Mcp-Api-Key:${MCP_API_KEY}"
  ],
  "env": {
    "MCP_API_KEY": "<your-value-here>"
  }
}`,
			expectedEnvLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			config := buildStdioConfig(tt.mcpURL, tt.inputs)

			// Convert to JSON-tagged struct for serialization
			configForJSON := stdioConfigJSON{
				Command: config.Command,
				Args:    config.Args,
				Env:     config.Env,
			}

			actualBytes, err := json.MarshalIndent(configForJSON, "", "  ")
			require.NoError(t, err, "JSON marshaling should not fail")

			actualJSON := string(actualBytes)

			// Verify exact JSON match
			require.JSONEq(t, tt.expectedJSON, actualJSON,
				"Config snippet JSON should match expected format exactly.\n"+
					"Expected:\n%s\n\nActual:\n%s", tt.expectedJSON, actualJSON)

			// Also verify struct fields directly
			require.Equal(t, "npx", config.Command, "Command should be npx")
			require.Contains(t, config.Args, "mcp-remote@0.1.25", "Args should contain mcp-remote version")
			require.Contains(t, config.Args, tt.mcpURL, "Args should contain MCP URL")
			require.Len(t, config.Env, tt.expectedEnvLen, "Env should have expected number of entries")
		})
	}
}

// TestBuildInstallConfigs_AllClients verifies that all client configs
// (Claude Desktop, Cursor, VS Code, Claude Code) are generated correctly.
func TestBuildInstallConfigs_AllClients(t *testing.T) {
	t.Parallel()

	// Create a mock service (nil fields are fine for this test)
	svc := &Service{}

	inputs := []securityInput{
		{
			SystemName:  "authorization",
			DisplayName: "api-key",
			Sensitive:   true,
		},
	}

	configs := svc.buildInstallConfigs("test-mcp", "https://example.com/mcp/test-mcp", inputs)

	t.Run("ClaudeDesktop", func(t *testing.T) {
		t.Parallel()
		require.NotNil(t, configs.ClaudeDesktop, "config should not be nil")
		require.Equal(t, "npx", configs.ClaudeDesktop.Command)
		require.Contains(t, configs.ClaudeDesktop.Args, "mcp-remote@0.1.25")
		require.Contains(t, configs.ClaudeDesktop.Args, "https://example.com/mcp/test-mcp")
		require.Contains(t, configs.ClaudeDesktop.Args, "--header")
		require.Contains(t, configs.ClaudeDesktop.Args, "Authorization:${API_KEY}")
		require.Equal(t, "<your-value-here>", configs.ClaudeDesktop.Env["API_KEY"])
	})

	t.Run("Cursor", func(t *testing.T) {
		t.Parallel()
		require.NotNil(t, configs.Cursor, "config should not be nil")
		require.Equal(t, "npx", configs.Cursor.Command)
		require.Contains(t, configs.Cursor.Args, "mcp-remote@0.1.25")
		require.Contains(t, configs.Cursor.Args, "https://example.com/mcp/test-mcp")
		require.Contains(t, configs.Cursor.Args, "--header")
		require.Contains(t, configs.Cursor.Args, "Authorization:${API_KEY}")
		require.Equal(t, "<your-value-here>", configs.Cursor.Env["API_KEY"])
	})

	t.Run("VSCode", func(t *testing.T) {
		t.Parallel()
		require.NotNil(t, configs.Vscode, "config should not be nil")
		require.Equal(t, "http", configs.Vscode.Type)
		require.Equal(t, "https://example.com/mcp/test-mcp", configs.Vscode.URL)
		require.Equal(t, "${API_KEY}", configs.Vscode.Headers["Authorization"])
	})

	t.Run("ClaudeCode", func(t *testing.T) {
		t.Parallel()
		require.NotEmpty(t, configs.ClaudeCode, "command should not be empty")
		require.Contains(t, configs.ClaudeCode, "claude mcp add")
		require.Contains(t, configs.ClaudeCode, "--transport http")
		require.Contains(t, configs.ClaudeCode, `"test-mcp"`)
		require.Contains(t, configs.ClaudeCode, `"https://example.com/mcp/test-mcp"`)
		require.Contains(t, configs.ClaudeCode, "--header 'Authorization:${API_KEY}'")
	})

	t.Run("GeminiCLI", func(t *testing.T) {
		t.Parallel()
		require.NotEmpty(t, configs.GeminiCli, "command should not be empty")
		require.Contains(t, configs.GeminiCli, "gemini mcp add")
		require.Contains(t, configs.GeminiCli, "--transport http")
		require.Contains(t, configs.GeminiCli, `"test-mcp"`)
		require.Contains(t, configs.GeminiCli, `"https://example.com/mcp/test-mcp"`)
		require.Contains(t, configs.GeminiCli, "--header 'Authorization:${API_KEY}'")
	})

	t.Run("CodexCLI", func(t *testing.T) {
		t.Parallel()
		require.NotEmpty(t, configs.CodexCli, "config should not be empty")
		require.Contains(t, configs.CodexCli, "[mcp_servers.test-mcp]")
		require.Contains(t, configs.CodexCli, `url = "https://example.com/mcp/test-mcp"`)
		require.Contains(t, configs.CodexCli, "http_headers = {")
		require.Contains(t, configs.CodexCli, `"Authorization" = "your-API_KEY-value"`)
	})
}

// TestBuildInstallConfigs_NoSharedReferences verifies that ClaudeDesktop and Cursor
// configs don't share slice/map references (Devin's PR feedback).
func TestBuildInstallConfigs_NoSharedReferences(t *testing.T) {
	t.Parallel()

	svc := &Service{}
	inputs := []securityInput{
		{SystemName: "test", DisplayName: "test-key", Sensitive: true},
	}

	configs := svc.buildInstallConfigs("test", "https://example.com/mcp/test", inputs)

	// Modify ClaudeDesktop's args
	originalCursorArgs := make([]string, len(configs.Cursor.Args))
	copy(originalCursorArgs, configs.Cursor.Args)

	configs.ClaudeDesktop.Args = append(configs.ClaudeDesktop.Args, "modified")

	// Cursor's args should NOT be affected
	require.Equal(t, originalCursorArgs, configs.Cursor.Args,
		"Modifying ClaudeDesktop.Args should not affect Cursor.Args")

	// Modify ClaudeDesktop's env
	originalCursorEnv := make(map[string]string)
	for k, v := range configs.Cursor.Env {
		originalCursorEnv[k] = v
	}

	configs.ClaudeDesktop.Env["NEW_KEY"] = "new-value"

	// Cursor's env should NOT be affected
	require.Equal(t, originalCursorEnv, configs.Cursor.Env,
		"Modifying ClaudeDesktop.Env should not affect Cursor.Env")
}
