package deplconfig

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
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
		// For remote URLs, we'll need to determine content type differently.
		// For now, default to common OpenAPI types based on extension.
		return getContentTypeFromPath(sr.source.Location)
	}

	return getContentTypeFromPath(sr.source.Location)
}

// Read returns a reader for the asset content and its size.
func (sr *SourceReader) Read() (io.ReadCloser, int64, error) {
	if isRemoteURL(sr.source.Location) {
		return sr.readRemote()
	}
	return sr.readLocal()
}

// readLocal reads from a local file path.
func (sr *SourceReader) readLocal() (io.ReadCloser, int64, error) {
	data, err := readFile(sr.source.Location)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read local file: %w", err)
	}

	fi, err := os.Stat(sr.source.Location)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get file info: %w", err)
	}

	reader := strings.NewReader(string(data))
	return io.NopCloser(reader), fi.Size(), nil
}

// readRemote reads from a remote URL.
func (sr *SourceReader) readRemote() (io.ReadCloser, int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", sr.source.Location, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "gram-cli/1.0")
	req.Header.Set("Accept", "application/yaml, application/json, text/yaml, text/plain, */*")

	resp, err := sharedRetryHTTPClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to fetch remote file: %w", err)
	}
	defer func() {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("remote file request failed with status %d: %s", resp.StatusCode, resp.Status)
	}

	contentLength := resp.ContentLength
	if contentLength > MaxRemoteFileSize {
		return nil, 0, fmt.Errorf("remote file too large: %d bytes (max: %d bytes)", contentLength, MaxRemoteFileSize)
	}

	var buf bytes.Buffer
	writer := bufio.NewWriterSize(&buf, 4096)

	limitedReader := io.LimitReader(resp.Body, MaxRemoteFileSize+1) // +1 to detect if file exceeds limit
	bytesRead, err := io.Copy(writer, limitedReader)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read remote file content: %w", err)
	}

	if bytesRead > MaxRemoteFileSize {
		return nil, 0, fmt.Errorf("remote file too large: exceeds %d bytes", MaxRemoteFileSize)
	}

	if err := writer.Flush(); err != nil {
		return nil, 0, fmt.Errorf("failed to flush content: %w", err)
	}

	reader := strings.NewReader(buf.String())

	return io.NopCloser(reader), bytesRead, nil
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
