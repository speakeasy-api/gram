package deploy

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// readLocal reads a source from a local file path.
func (sr *SourceReader) readLocal() (io.ReadCloser, int64, error) {
	data, err := os.ReadFile(sr.source.Location)
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

// readRemote reads a source from a remote URL.
func (sr *SourceReader) readRemote(ctx context.Context) (io.ReadCloser, int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
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
