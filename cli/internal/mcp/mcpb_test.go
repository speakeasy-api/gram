package mcp

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateMCPBManifest_CreatesValidZIP(t *testing.T) {
	t.Parallel()

	info := &ToolsetInfo{
		Name:       "test-server",
		URL:        "https://mcp.example.com/test",
		APIKey:     "test-api-key",
		HeaderName: "Authorization",
		EnvVarName: "",
	}

	result, err := GenerateMCPBManifest(info, false)
	require.NoError(t, err)
	require.NotEmpty(t, result)

	// Verify it's a valid ZIP archive
	zipReader, err := zip.NewReader(bytes.NewReader(result), int64(len(result)))
	require.NoError(t, err, "should be a valid ZIP archive")

	// Verify it contains manifest.json
	require.Len(t, zipReader.File, 1, "should contain exactly one file")
	require.Equal(t, "manifest.json", zipReader.File[0].Name)
}

func TestGenerateMCPBManifest_ManifestStructure(t *testing.T) {
	t.Parallel()

	info := &ToolsetInfo{
		Name:       "test-server",
		URL:        "https://mcp.example.com/test",
		APIKey:     "test-api-key",
		HeaderName: "Authorization",
		EnvVarName: "",
	}

	result, err := GenerateMCPBManifest(info, false)
	require.NoError(t, err)

	// Extract and parse manifest.json
	zipReader, err := zip.NewReader(bytes.NewReader(result), int64(len(result)))
	require.NoError(t, err)

	manifestFile, err := zipReader.File[0].Open()
	require.NoError(t, err)
	defer func() { _ = manifestFile.Close() }()

	manifestJSON, err := io.ReadAll(manifestFile)
	require.NoError(t, err)

	// Parse manifest
	var manifest MCPBManifest
	err = json.Unmarshal(manifestJSON, &manifest)
	require.NoError(t, err, "manifest.json should be valid JSON")

	// Verify manifest structure
	require.Equal(t, "0.3", manifest.ManifestVersion, "should use current manifest version")
	require.Equal(t, "test-server", manifest.Name)
	require.Equal(t, "1.0.0", manifest.Version)
	require.Equal(t, "Gram MCP server for test-server", manifest.Description)
	require.Equal(t, "Gram", manifest.Author.Name)

	// Verify server configuration
	require.Equal(t, "node", manifest.Server.Type)
	require.Equal(t, "npx", manifest.Server.EntryPoint)
	require.Equal(t, "npx", manifest.Server.MCPConfig.Command)
	require.Equal(t, []string{
		"mcp-remote@0.1.25",
		"https://mcp.example.com/test",
		"--header",
		"Authorization:test-api-key",
	}, manifest.Server.MCPConfig.Args)

	// Verify command-based transport (not HTTP)
	require.Empty(t, manifest.Server.MCPConfig.Type, "should not use HTTP transport")
	require.Empty(t, manifest.Server.MCPConfig.URL, "should not have URL field")
	require.Nil(t, manifest.Server.MCPConfig.Headers, "should not have Headers field")

	// Should not have user_config when not using env var
	require.Nil(t, manifest.UserConfig)
}

func TestGenerateMCPBManifest_WithEnvVar(t *testing.T) {
	t.Parallel()

	info := &ToolsetInfo{
		Name:       "test-server",
		URL:        "https://mcp.example.com/test",
		APIKey:     "test-api-key",
		HeaderName: "X-API-Key",
		EnvVarName: "MY_API_KEY",
	}

	result, err := GenerateMCPBManifest(info, true)
	require.NoError(t, err)

	// Extract and parse manifest
	zipReader, err := zip.NewReader(bytes.NewReader(result), int64(len(result)))
	require.NoError(t, err)

	manifestFile, err := zipReader.File[0].Open()
	require.NoError(t, err)
	defer func() { _ = manifestFile.Close() }()

	manifestJSON, err := io.ReadAll(manifestFile)
	require.NoError(t, err)

	var manifest MCPBManifest
	err = json.Unmarshal(manifestJSON, &manifest)
	require.NoError(t, err)

	// Verify env var substitution in header
	require.Equal(t, []string{
		"mcp-remote@0.1.25",
		"https://mcp.example.com/test",
		"--header",
		"X-API-Key:${user_config.MY_API_KEY}",
	}, manifest.Server.MCPConfig.Args)

	// Verify user_config is present
	require.NotNil(t, manifest.UserConfig)
	require.Contains(t, manifest.UserConfig, "MY_API_KEY")

	userVar := manifest.UserConfig["MY_API_KEY"]
	require.Equal(t, "string", userVar.Type)
	require.Equal(t, "MY_API_KEY", userVar.Title)
	require.Equal(t, "API key for authentication", userVar.Description)
	require.True(t, userVar.Sensitive)
	require.True(t, userVar.Required)
}

func TestGenerateMCPBManifest_SpecialCharactersInName(t *testing.T) {
	t.Parallel()

	info := &ToolsetInfo{
		Name:       "test-server-with-special-chars!@#",
		URL:        "https://mcp.example.com/test",
		APIKey:     "test-api-key",
		HeaderName: "Authorization",
		EnvVarName: "",
	}

	result, err := GenerateMCPBManifest(info, false)
	require.NoError(t, err)

	// Extract and parse manifest
	zipReader, err := zip.NewReader(bytes.NewReader(result), int64(len(result)))
	require.NoError(t, err)

	manifestFile, err := zipReader.File[0].Open()
	require.NoError(t, err)
	defer func() { _ = manifestFile.Close() }()

	manifestJSON, err := io.ReadAll(manifestFile)
	require.NoError(t, err)

	var manifest MCPBManifest
	err = json.Unmarshal(manifestJSON, &manifest)
	require.NoError(t, err)

	// Name should be preserved as-is in manifest
	require.Equal(t, "test-server-with-special-chars!@#", manifest.Name)
}

func TestGenerateMCPBManifest_DifferentHeaderName(t *testing.T) {
	t.Parallel()

	info := &ToolsetInfo{
		Name:       "test-server",
		URL:        "https://mcp.example.com/test",
		APIKey:     "secret-key-123",
		HeaderName: "X-Custom-Auth",
		EnvVarName: "",
	}

	result, err := GenerateMCPBManifest(info, false)
	require.NoError(t, err)

	// Extract and parse manifest
	zipReader, err := zip.NewReader(bytes.NewReader(result), int64(len(result)))
	require.NoError(t, err)

	manifestFile, err := zipReader.File[0].Open()
	require.NoError(t, err)
	defer func() { _ = manifestFile.Close() }()

	manifestJSON, err := io.ReadAll(manifestFile)
	require.NoError(t, err)

	var manifest MCPBManifest
	err = json.Unmarshal(manifestJSON, &manifest)
	require.NoError(t, err)

	// Verify custom header is used
	require.Equal(t, []string{
		"mcp-remote@0.1.25",
		"https://mcp.example.com/test",
		"--header",
		"X-Custom-Auth:secret-key-123",
	}, manifest.Server.MCPConfig.Args)
}

func TestGenerateMCPBManifest_ZIPIsExtractable(t *testing.T) {
	t.Parallel()

	info := &ToolsetInfo{
		Name:       "test-server",
		URL:        "https://mcp.example.com/test",
		APIKey:     "test-api-key",
		HeaderName: "Authorization",
		EnvVarName: "",
	}

	result, err := GenerateMCPBManifest(info, false)
	require.NoError(t, err)

	// Try to extract all files (should only be manifest.json)
	zipReader, err := zip.NewReader(bytes.NewReader(result), int64(len(result)))
	require.NoError(t, err)

	for _, file := range zipReader.File {
		rc, err := file.Open()
		require.NoError(t, err, "should be able to open file in ZIP")

		content, err := io.ReadAll(rc)
		require.NoError(t, err, "should be able to read file content")
		require.NotEmpty(t, content, "file should have content")

		err = rc.Close()
		require.NoError(t, err, "should be able to close file")
	}
}
