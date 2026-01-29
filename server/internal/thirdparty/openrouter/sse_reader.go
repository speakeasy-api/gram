package openrouter

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// SSEEvent represents a server-sent event
type SSEEvent struct {
	Event string
	Data  string
	ID    string
	Retry int
}

// SSEReader reads server-sent events from an io.Reader
type SSEReader struct {
	scanner *bufio.Scanner
}

// NewSSEReader creates a new SSE reader
func NewSSEReader(r io.Reader) *SSEReader {
	return &SSEReader{
		scanner: bufio.NewScanner(r),
	}
}

// ReadEvent reads the next SSE event from the stream
// Returns io.EOF when the stream is exhausted
func (r *SSEReader) ReadEvent() (*SSEEvent, error) {
	event := &SSEEvent{
		Event: "",
		Data:  "",
		ID:    "",
		Retry: 0,
	}
	var dataLines []string

	for r.scanner.Scan() {
		line := r.scanner.Text()

		// Empty line signals end of event
		if line == "" {
			if len(dataLines) > 0 {
				event.Data = strings.Join(dataLines, "\n")
				return event, nil
			}
			continue
		}

		// Parse field
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimPrefix(data, " ") // Optional space after colon
			dataLines = append(dataLines, data)
		} else if strings.HasPrefix(line, "event:") {
			event.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "id:") {
			event.ID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
		}
		// Ignore retry and comments for now
	}

	if err := r.scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error: %w", err)
	}

	// Return any remaining data
	if len(dataLines) > 0 {
		event.Data = strings.Join(dataLines, "\n")
		return event, nil
	}

	return nil, io.EOF
}
