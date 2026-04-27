package proxy

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// capturedEvent is a test-side copy of an SSE event, holding the raw bytes
// and the concatenated data payload. Raw bytes are copied eagerly because
// forEachSSEEvent reuses its internal buffers across callbacks.
type capturedEvent struct {
	Raw  string
	Data string
}

func captureEvents(t *testing.T, body string, maxBytes int64) ([]capturedEvent, error) {
	t.Helper()

	var events []capturedEvent
	err := forEachSSEEvent(strings.NewReader(body), maxBytes, func(rawEvent []byte, data []byte) error {
		events = append(events, capturedEvent{
			Raw:  string(rawEvent),
			Data: string(data),
		})
		return nil
	})
	return events, err
}

func TestForEachSSEEvent_SingleEvent(t *testing.T) {
	t.Parallel()

	body := "data: hello\n\n"
	events, err := captureEvents(t, body, 1024)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "hello", events[0].Data)
	require.Equal(t, body, events[0].Raw, "raw bytes must be preserved verbatim for relay")
}

func TestForEachSSEEvent_MultipleEvents(t *testing.T) {
	t.Parallel()

	body := "data: first\n\ndata: second\n\ndata: third\n\n"
	events, err := captureEvents(t, body, 1024)
	require.NoError(t, err)
	require.Len(t, events, 3)
	require.Equal(t, "first", events[0].Data)
	require.Equal(t, "second", events[1].Data)
	require.Equal(t, "third", events[2].Data)
}

func TestForEachSSEEvent_MultiLineData(t *testing.T) {
	t.Parallel()

	// Per SSE spec, multiple data: lines in a single event are joined with
	// newlines when assembling the payload.
	body := "data: line one\ndata: line two\ndata: line three\n\n"
	events, err := captureEvents(t, body, 1024)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "line one\nline two\nline three", events[0].Data)
}

func TestForEachSSEEvent_CommentsAreIgnoredInData(t *testing.T) {
	t.Parallel()

	body := ": keepalive comment\ndata: payload\n\n"
	events, err := captureEvents(t, body, 1024)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "payload", events[0].Data)
	// Comment line stays in raw so downstream clients see what upstream sent.
	require.Contains(t, events[0].Raw, ": keepalive comment")
}

func TestForEachSSEEvent_OtherFieldsIgnoredForData(t *testing.T) {
	t.Parallel()

	// event:, id:, and retry: fields are preserved in raw bytes but not
	// contributed to the data payload.
	body := "event: progress\nid: 42\ndata: payload\nretry: 1000\n\n"
	events, err := captureEvents(t, body, 1024)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "payload", events[0].Data)
	require.Contains(t, events[0].Raw, "event: progress")
	require.Contains(t, events[0].Raw, "id: 42")
	require.Contains(t, events[0].Raw, "retry: 1000")
}

func TestForEachSSEEvent_TrailingEventWithoutBlankLineIsEmitted(t *testing.T) {
	t.Parallel()

	// Some implementations may close the stream without a trailing blank
	// line after the final event. Ensure we still emit it so terminal
	// tools/call responses are detected even on abrupt close.
	body := "data: final\n"
	events, err := captureEvents(t, body, 1024)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "final", events[0].Data)
}

func TestForEachSSEEvent_OversizedEventTripsCap(t *testing.T) {
	t.Parallel()

	// Build an event whose accumulated raw size exceeds the cap.
	body := "data: " + strings.Repeat("x", 1024) + "\n\n"
	_, err := captureEvents(t, body, 128)
	require.ErrorIs(t, err, ErrBodyTooLarge)
}

func TestForEachSSEEvent_CallbackErrorStopsScan(t *testing.T) {
	t.Parallel()

	body := "data: first\n\ndata: second\n\ndata: third\n\n"
	stopErr := errors.New("stop")
	var count int
	err := forEachSSEEvent(strings.NewReader(body), 1024, func(_ []byte, _ []byte) error {
		count++
		if count == 2 {
			return stopErr
		}
		return nil
	})
	require.ErrorIs(t, err, stopErr)
	require.Equal(t, 2, count)
}

func TestForEachSSEEvent_EmptyInputEmitsNoEvents(t *testing.T) {
	t.Parallel()

	events, err := captureEvents(t, "", 1024)
	require.NoError(t, err)
	require.Empty(t, events)
}

func TestForEachSSEEvent_DataFieldWithoutSpaceAfterColon(t *testing.T) {
	t.Parallel()

	// SSE spec permits "data:value" without a leading space; only a single
	// leading space is stripped when present.
	body := "data:hello\n\n"
	events, err := captureEvents(t, body, 1024)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "hello", events[0].Data)
}
