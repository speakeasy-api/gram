package deploy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/cli/internal/must"
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
		Runtime:  "nodejs:22",
	}

	reader := NewSourceReader(source)

	// Test type method
	require.Equal(t, "openapiv3", string(reader.Source.Type))

	// Test content type method
	require.Equal(t, "application/yaml", reader.GetContentType())

	// Test read method
	rc, size, err := reader.Read(t.Context())
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
		Runtime:  "nodejs:22",
	}

	reader := NewSourceReader(source)

	// Test content type for JSON
	require.Equal(t, "application/json", reader.GetContentType())

	// Test read method
	rc, size, err := reader.Read(t.Context())
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
		Runtime:  "nodejs:22",
	}

	reader := NewSourceReader(source)

	// Should fail to read nonexistent file
	_, _, err := reader.Read(t.Context())
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "no such file")
}

func TestSourceReader_RemoteURL(t *testing.T) {
	t.Parallel()

	stubSpec := `openapi: 3.0.0
			info:
			title: Remote Test API
			version: 1.0.0`

	staticHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.WriteHeader(http.StatusOK)
		must.Value(w.Write([]byte(stubSpec)))
	}
	staticServer := httptest.NewServer(http.HandlerFunc(staticHandler))
	defer staticServer.Close()

	source := Source{
		Type:     SourceTypeOpenAPIV3,
		Location: staticServer.URL + "/api-spec.yaml",
		Name:     "Remote API",
		Slug:     "remote-api",
		Runtime:  "nodejs:22",
	}

	reader := NewSourceReader(source)
	require.Equal(t, "application/yaml", reader.GetContentType())

	rc, size, err := reader.Read(t.Context())
	require.NoError(t, err)
	require.Positive(t, size)

	defer func() {
		require.NoError(t, rc.Close())
	}()

	content, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, stubSpec, string(content))
}

func TestSourceReader_RemoteURL_Error(t *testing.T) {
	t.Parallel()

	// Create a test HTTP server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		must.Value(w.Write([]byte("Not Found")))
	}))
	defer server.Close()

	source := Source{
		Type:     SourceTypeOpenAPIV3,
		Location: server.URL + "/nonexistent.yaml",
		Name:     "Remote API",
		Slug:     "remote-api",
		Runtime:  "nodejs:22",
	}

	reader := NewSourceReader(source)

	// Should fail with HTTP error
	_, _, err := reader.Read(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), "404")
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
