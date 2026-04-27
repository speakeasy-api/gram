package proxy

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// forEachSSEEvent scans an SSE stream from r, calling fn once per event. The
// raw event bytes (including SSE framing, terminated with the blank-line
// separator) and the event's concatenated "data:" payload are both passed to
// fn so the caller can relay the raw bytes to the user verbatim while
// separately inspecting or decoding the payload.
//
// maxEventBytes bounds the accepted size of a single event. If an event's
// raw bytes or concatenated data payload exceeds this, [ErrBodyTooLarge] is
// returned. This mirrors the buffered-body cap applied to non-streamed JSON
// bodies — each SSE event is an independent allocation bounded by the same
// rule.
//
// Only "data:" fields are accumulated for the payload; "event:", "id:",
// "retry:", and comment lines are relayed verbatim but ignored for payload
// purposes. If an event ends without a trailing blank line (abrupt stream
// close), any pending buffered event is emitted before return.
func forEachSSEEvent(r io.Reader, maxEventBytes int64, fn func(rawEvent []byte, data []byte) error) error {
	scanner := bufio.NewScanner(r)
	// Initial + max line buffer sizes. Max is set to maxEventBytes+1 so a
	// single oversized line (i.e. one gigantic data: value) trips the
	// scanner and we can surface ErrBodyTooLarge.
	scanner.Buffer(make([]byte, 0, 64*1024), int(maxEventBytes+1))

	var (
		eventBuf bytes.Buffer // raw event bytes as received, for verbatim relay
		dataBuf  bytes.Buffer // concatenated data: fields for parsing
	)

	emit := func() error {
		defer func() {
			eventBuf.Reset()
			dataBuf.Reset()
		}()
		if eventBuf.Len() == 0 {
			return nil
		}
		if err := fn(eventBuf.Bytes(), dataBuf.Bytes()); err != nil {
			return err
		}
		return nil
	}

	for scanner.Scan() {
		line := scanner.Bytes()

		// Early overflow check on the raw event buffer. The scanner caps
		// single-line size; we cap total event size (sum of all lines).
		if int64(eventBuf.Len())+int64(len(line))+1 > maxEventBytes {
			return ErrBodyTooLarge
		}

		eventBuf.Write(line)
		eventBuf.WriteByte('\n')

		if len(line) == 0 {
			if err := emit(); err != nil {
				return err
			}
			continue
		}

		if line[0] == ':' {
			// Comment line — retained in raw event for relay, not part of payload.
			continue
		}

		if bytes.HasPrefix(line, []byte("data:")) {
			val := line[len("data:"):]
			if len(val) > 0 && val[0] == ' ' {
				val = val[1:]
			}
			if int64(dataBuf.Len())+int64(len(val))+1 > maxEventBytes {
				return ErrBodyTooLarge
			}
			if dataBuf.Len() > 0 {
				dataBuf.WriteByte('\n')
			}
			dataBuf.Write(val)
		}
		// Other SSE fields (event:, id:, retry:) are preserved in the raw
		// event bytes for relay but not interpreted here.
	}

	if err := scanner.Err(); err != nil {
		// bufio.Scanner returns bufio.ErrTooLong when a single line exceeds
		// the configured max. Surface as ErrBodyTooLarge for a consistent
		// caller signal.
		if errors.Is(err, bufio.ErrTooLong) {
			return ErrBodyTooLarge
		}
		return fmt.Errorf("scan sse stream: %w", err)
	}

	// Flush any event that was left open because the stream ended without a
	// terminating blank line.
	return emit()
}

// isEventStream reports whether the given response headers indicate an MCP
// Streamable HTTP SSE response. Per MCP spec § Sending Messages to the Server
// (step 5), the upstream chooses between application/json and
// text/event-stream at response time.
func isEventStream(header http.Header) bool {
	ct := header.Get("Content-Type")
	if ct == "" {
		return false
	}
	// Header may include parameters like "text/event-stream; charset=utf-8".
	if idx := strings.Index(ct, ";"); idx >= 0 {
		ct = ct[:idx]
	}
	return strings.EqualFold(strings.TrimSpace(ct), "text/event-stream")
}
