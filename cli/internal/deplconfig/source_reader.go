package deplconfig

import (
	"context"
	"io"
	"mime"
	"path/filepath"
	"strings"
)

const (
	// MaxRemoteFileSizeMB is the maximum size allowed for remote files (10MB)
	MaxRemoteFileSizeMB = 10
	MaxRemoteFileSize   = MaxRemoteFileSizeMB * 1024 * 1024
)

// SourceReader reads source content from local files or remote URLs.
type SourceReader struct {
	source Source
}

// NewSourceReader creates a new SourceReader for the given source.
func NewSourceReader(source Source) *SourceReader {
	return &SourceReader{
		source: source,
	}
}

// GetType returns the source type (e.g., "openapiv3").
func (sr *SourceReader) GetType() string {
	return string(sr.source.Type)
}

// GetContentType returns the MIME type of the content based on file extension.
func (sr *SourceReader) GetContentType() string {
	if isRemoteURL(sr.source.Location) {
		// NOTE(cjea): For remote URLs, we'll need to determine content type
		// differently. For now, default to common OpenAPI types based on
		// extension.
		return getContentTypeFromPath(sr.source.Location)
	}

	return getContentTypeFromPath(sr.source.Location)
}

// Read returns a reader for the asset content and its size.
func (sr *SourceReader) Read(ctx context.Context) (io.ReadCloser, int64, error) {
	if isRemoteURL(sr.source.Location) {
		return sr.readRemote(ctx)
	}
	return sr.readLocal()
}

// isRemoteURL checks if the location is a remote URL.
func isRemoteURL(location string) bool {
	return strings.HasPrefix(location, "http://") || strings.HasPrefix(location, "https://")
}

// getContentTypeFromPath determines content type from file path/extension.
func getContentTypeFromPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "application/yaml"
	default:
		if mimeType := mime.TypeByExtension(ext); mimeType != "" {
			return mimeType
		}
		defaultForOpenAPISpecs := "application/yaml"
		return defaultForOpenAPISpecs
	}
}
