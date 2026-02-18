package mcp

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildMCPConfig_HTTPTransport(t *testing.T) {
	t.Parallel()

	info := &ToolsetInfo{
		Name:       "test-server",
		URL:        "https://mcp.example.com/test",
		APIKey:     "test-api-key",
		HeaderName: "Authorization",
		EnvVarName: "",
	}

	config := BuildMCPConfig(info, false)

	// Verify HTTP transport configuration
	require.Equal(t, "http", config.Type)
	require.Equal(t, "https://mcp.example.com/test", config.URL)
	require.NotNil(t, config.Headers)
	require.Equal(t, "test-api-key", config.Headers["Authorization"])

	// Should not have command-based transport fields
	require.Empty(t, config.Command)
	require.Empty(t, config.Args)
	require.Nil(t, config.Env)
}

func TestBuildMCPConfig_WithEnvVar(t *testing.T) {
	t.Parallel()

	info := &ToolsetInfo{
		Name:       "test-server",
		URL:        "https://mcp.example.com/test",
		APIKey:     "test-api-key",
		HeaderName: "X-API-Key",
		EnvVarName: "MY_API_KEY",
	}

	config := BuildMCPConfig(info, true)

	// Verify env var substitution
	require.Equal(t, "http", config.Type)
	require.Equal(t, "https://mcp.example.com/test", config.URL)
	require.NotNil(t, config.Headers)
	require.Equal(t, "${MY_API_KEY}", config.Headers["X-API-Key"])
}

func TestBuildMCPConfig_CustomHeader(t *testing.T) {
	t.Parallel()

	info := &ToolsetInfo{
		Name:       "test-server",
		URL:        "https://mcp.example.com/test",
		APIKey:     "secret-123",
		HeaderName: "X-Custom-Auth",
		EnvVarName: "",
	}

	config := BuildMCPConfig(info, false)

	require.Equal(t, "secret-123", config.Headers["X-Custom-Auth"])
	require.NotContains(t, config.Headers, "Authorization", "should not have default Authorization header")
}

func TestMarshalConfigJSON_Structure(t *testing.T) {
	t.Parallel()

	config := MCPServerConfig{
		Type: "http",
		URL:  "https://mcp.example.com/test",
		Headers: map[string]string{
			"Authorization": "test-key",
		},
		Command: "",
		Args:    nil,
		Env:     nil,
	}

	jsonStr, err := MarshalConfigJSON("test-server", config)
	require.NoError(t, err)
	require.NotEmpty(t, jsonStr)

	// Parse and verify structure
	var result map[string]map[string]MCPServerConfig
	err = json.Unmarshal([]byte(jsonStr), &result)
	require.NoError(t, err)

	// Verify wrapper structure
	require.Contains(t, result, "mcpServers")
	require.Contains(t, result["mcpServers"], "test-server")

	// Verify config content
	serverConfig := result["mcpServers"]["test-server"]
	require.Equal(t, "http", serverConfig.Type)
	require.Equal(t, "https://mcp.example.com/test", serverConfig.URL)
	require.Equal(t, "test-key", serverConfig.Headers["Authorization"])
}

func TestMarshalConfigJSON_OmitsEmptyFields(t *testing.T) {
	t.Parallel()

	config := MCPServerConfig{
		Type: "http",
		URL:  "https://mcp.example.com/test",
		Headers: map[string]string{
			"Authorization": "test-key",
		},
		// Command-based fields should be omitted
		Command: "",
		Args:    nil,
		Env:     nil,
	}

	jsonStr, err := MarshalConfigJSON("test-server", config)
	require.NoError(t, err)

	// Parse JSON
	var result map[string]any
	err = json.Unmarshal([]byte(jsonStr), &result)
	require.NoError(t, err)

	// Navigate to server config
	mcpServers, ok := result["mcpServers"].(map[string]any)
	require.True(t, ok, "mcpServers should be a map")
	serverConfig, ok := mcpServers["test-server"].(map[string]any)
	require.True(t, ok, "test-server config should be a map")

	// Verify command-based fields are omitted
	require.NotContains(t, serverConfig, "command")
	require.NotContains(t, serverConfig, "args")
	require.NotContains(t, serverConfig, "env")

	// Verify HTTP fields are present
	require.Contains(t, serverConfig, "type")
	require.Contains(t, serverConfig, "url")
	require.Contains(t, serverConfig, "headers")
}

// TestCursorDeepLinkEncoding tests the complete flow that would be used
// for Cursor deep link generation (base64 encoding of raw config)
func TestCursorDeepLinkEncoding(t *testing.T) {
	t.Parallel()

	info := &ToolsetInfo{
		Name:       "test-server",
		URL:        "https://mcp.example.com/test",
		APIKey:     "test-api-key",
		HeaderName: "Authorization",
		EnvVarName: "",
	}

	// Build config (same as in doInstallCursor)
	config := BuildMCPConfig(info, false)

	// Marshal to JSON (raw config, not wrapped)
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	// Base64 encode (as Cursor expects)
	configEncoded := base64.StdEncoding.EncodeToString(configJSON)
	require.NotEmpty(t, configEncoded)

	// Verify we can decode it back
	decoded, err := base64.StdEncoding.DecodeString(configEncoded)
	require.NoError(t, err)

	// Verify decoded JSON is valid
	var decodedConfig MCPServerConfig
	err = json.Unmarshal(decoded, &decodedConfig)
	require.NoError(t, err)

	// Verify structure matches what Cursor expects
	require.Equal(t, "http", decodedConfig.Type)
	require.Equal(t, "https://mcp.example.com/test", decodedConfig.URL)
	require.Equal(t, "test-api-key", decodedConfig.Headers["Authorization"])
}

func TestCursorDeepLinkEncoding_WithEnvVar(t *testing.T) {
	t.Parallel()

	info := &ToolsetInfo{
		Name:       "test-server",
		URL:        "https://mcp.example.com/test",
		APIKey:     "test-api-key",
		HeaderName: "Authorization",
		EnvVarName: "MY_API_KEY",
	}

	// Build config with env var
	config := BuildMCPConfig(info, true)

	// Marshal to JSON
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	// Base64 encode
	configEncoded := base64.StdEncoding.EncodeToString(configJSON)

	// Decode and verify
	decoded, err := base64.StdEncoding.DecodeString(configEncoded)
	require.NoError(t, err)

	var decodedConfig MCPServerConfig
	err = json.Unmarshal(decoded, &decodedConfig)
	require.NoError(t, err)

	// Verify env var substitution is preserved
	require.Equal(t, "${MY_API_KEY}", decodedConfig.Headers["Authorization"])
}

func TestCursorDeepLinkEncoding_NoWrapping(t *testing.T) {
	t.Parallel()

	info := &ToolsetInfo{
		Name:       "test-server",
		URL:        "https://mcp.example.com/test",
		APIKey:     "test-api-key",
		HeaderName: "Authorization",
		EnvVarName: "",
	}

	// Build config
	config := BuildMCPConfig(info, false)

	// Marshal raw config (not wrapped in mcpServers)
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	// Verify it's NOT wrapped
	var parsed map[string]any
	err = json.Unmarshal(configJSON, &parsed)
	require.NoError(t, err)

	// Should have direct fields, not wrapped in mcpServers
	require.Contains(t, parsed, "type")
	require.Contains(t, parsed, "url")
	require.Contains(t, parsed, "headers")
	require.NotContains(t, parsed, "mcpServers", "should not be wrapped for Cursor")
}
