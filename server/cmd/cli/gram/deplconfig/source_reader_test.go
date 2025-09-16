package deplconfig

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceReader_LocalFile(t *testing.T) {
	t.Parallel()

	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test-spec.yaml")
	testContent := `openapi: 3.0.0
info:
  title: Test API
  version: 1.0.0`

	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0600))

	// Create source and reader
	source := Source{
		Type:     SourceTypeOpenAPIV3,
		Location: testFile,
		Name:     "Test API",
		Slug:     "test-api",
	}

	reader := NewSourceReader(source)

	// Test type method
	require.Equal(t, "openapiv3", reader.GetType())

	// Test content type method
	require.Equal(t, "application/yaml", reader.GetContentType())

	// Test read method
	rc, size, err := reader.Read()
	require.NoError(t, err)
	require.Positive(t, size)

	defer func() {
		require.NoError(t, rc.Close())
	}()

	// Read the content
	content, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, testContent, string(content))
}

func TestSourceReader_JSONFile(t *testing.T) {
	t.Parallel()

	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test-spec.json")
	testContent := `{"openapi": "3.0.0", "info": {"title": "Test API", "version": "1.0.0"}}`

	require.NoError(t, os.WriteFile(testFile, []byte(testContent), 0600))

	// Create source and reader
	source := Source{
		Type:     SourceTypeOpenAPIV3,
		Location: testFile,
		Name:     "Test JSON API",
		Slug:     "test-json-api",
	}

	reader := NewSourceReader(source)

	// Test content type for JSON
	require.Equal(t, "application/json", reader.GetContentType())

	// Test read method
	rc, size, err := reader.Read()
	require.NoError(t, err)
	require.Positive(t, size)

	defer func() {
		require.NoError(t, rc.Close())
	}()

	content, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, testContent, string(content))
}

func TestSourceReader_NonexistentFile(t *testing.T) {
	t.Parallel()
	source := Source{
		Type:     SourceTypeOpenAPIV3,
		Location: "/nonexistent/path/file.yaml",
		Name:     "Nonexistent API",
		Slug:     "nonexistent-api",
	}

	reader := NewSourceReader(source)

	// Should fail to read nonexistent file
	_, _, err := reader.Read()
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "no such file")
}

func TestSourceReader_RemoteURL(t *testing.T) {
	t.Parallel()
	source := Source{
		Type:     SourceTypeOpenAPIV3,
		Location: "https://example.com/api-spec.yaml",
		Name:     "Remote API",
		Slug:     "remote-api",
	}

	reader := NewSourceReader(source)

	// Test content type detection from URL
	require.Equal(t, "application/yaml", reader.GetContentType())

	// Remote URL reading should fail (not implemented yet)
	_, _, err := reader.Read()
	require.Error(t, err)
	require.Contains(t, err.Error(), "remote URL reading not yet implemented")
}

func TestGetContentTypeFromPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path     string
		expected string
	}{
		{"file.json", "application/json"},
		{"file.yaml", "application/yaml"},
		{"file.yml", "application/yaml"},
		{"file.unknown", "application/yaml"}, // default
		{"https://example.com/spec.json", "application/json"},
		{"https://example.com/spec.yaml", "application/yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			result := getContentTypeFromPath(tt.path)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestIsRemoteURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		location string
		expected bool
	}{
		{"https://example.com/spec.yaml", true},
		{"http://example.com/spec.json", true},
		{"/local/path/file.yaml", false},
		{"./relative/path/file.json", false},
		{"file.yaml", false},
	}

	for _, tt := range tests {
		t.Run(tt.location, func(t *testing.T) {
			t.Parallel()
			result := isRemoteURL(tt.location)
			require.Equal(t, tt.expected, result)
		})
	}
}
