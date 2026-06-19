package proxy

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// capturedEvent is a test-side copy of an SSE event, holding the raw bytes,
// the concatenated data payload, and the non-data lines. Bytes are copied
// eagerly because forEachSSEEvent reuses its internal buffers across
// callbacks.
type capturedEvent struct {
	Raw     string
	Data    string
	NonData string
}

func captureEvents(t *testing.T, body string, maxBytes int64) ([]capturedEvent, error) {
	t.Helper()

	var events []capturedEvent
	err := forEachSSEEvent(strings.NewReader(body), maxBytes, func(rawEvent []byte, data []byte, nonData []byte) error {
		events = append(events, capturedEvent{
			Raw:     string(rawEvent),
			Data:    string(data),
			NonData: string(nonData),
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
	// Comment is also captured into the non-data buffer so re-emission via
	// formatSSEEventWithData preserves it.
	require.Equal(t, ": keepalive comment\n", events[0].NonData)
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
	// Non-data fields are captured into nonData in their original order,
	// each terminated with "\n", so re-emission with a mutated payload can
	// splice them back in.
	require.Equal(t, "event: progress\nid: 42\nretry: 1000\n", events[0].NonData)
}

func TestForEachSSEEvent_NonDataEmptyWhenOnlyDataFields(t *testing.T) {
	t.Parallel()

	body := "data: payload\n\n"
	events, err := captureEvents(t, body, 1024)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Empty(t, events[0].NonData)
}

func TestForEachSSEEvent_NonDataResetBetweenEvents(t *testing.T) {
	t.Parallel()

	// Ensure the non-data buffer does not bleed between events — event #2
	// must not see event #1's id: line.
	body := "id: 1\ndata: first\n\ndata: second\n\n"
	events, err := captureEvents(t, body, 1024)
	require.NoError(t, err)
	require.Len(t, events, 2)
	require.Equal(t, "id: 1\n", events[0].NonData)
	require.Empty(t, events[1].NonData)
}

func TestForEachSSEEvent_NonDataPreservesLineOrder(t *testing.T) {
	t.Parallel()

	// Non-data lines may appear in any order around data: lines. Order is
	// preserved exactly as received so a downstream rebuild emits them in
	// the same sequence the upstream sent.
	body := "retry: 1000\nid: 7\nevent: progress\ndata: payload\n\n"
	events, err := captureEvents(t, body, 1024)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "retry: 1000\nid: 7\nevent: progress\n", events[0].NonData)
}

func TestForEachSSEEvent_NonDataCapturedWhenNoDataField(t *testing.T) {
	t.Parallel()

	// An event with only non-data fields (e.g. an id-only keepalive) is
	// still emitted to the callback. Its data is empty; its non-data
	// captures everything.
	body := "id: 1\n\n"
	events, err := captureEvents(t, body, 1024)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Empty(t, events[0].Data)
	require.Equal(t, "id: 1\n", events[0].NonData)
}

func TestForEachSSEEvent_OversizedNonDataTripsCap(t *testing.T) {
	t.Parallel()

	// A non-data field whose accumulated bytes exceed the cap must trip
	// ErrBodyTooLarge for the same reason an oversized data: field does —
	// it bounds per-event allocation.
	body := "id: " + strings.Repeat("x", 1024) + "\ndata: payload\n\n"
	_, err := captureEvents(t, body, 128)
	require.ErrorIs(t, err, ErrBodyTooLarge)
}

func TestFormatSSEEventWithData_NoNonDataFields(t *testing.T) {
	t.Parallel()

	out := formatSSEEventWithData(nil, []byte(`{"a":1}`))
	require.Equal(t, "data: {\"a\":1}\n\n", string(out))
}

func TestFormatSSEEventWithData_PreservesNonDataPrefix(t *testing.T) {
	t.Parallel()

	nonData := []byte("event: progress\nid: 42\n")
	out := formatSSEEventWithData(nonData, []byte(`{"a":1}`))
	require.Equal(t, "event: progress\nid: 42\ndata: {\"a\":1}\n\n", string(out))
}

func TestFormatSSEEventWithData_RoundTripsThroughParser(t *testing.T) {
	t.Parallel()

	// A captured event's nonData combined with its data via
	// formatSSEEventWithData must reparse identically — the parser sees
	// the same event back. This guards the contract that any mutation
	// re-emit using these primitives is parse-stable.
	original := "event: progress\nid: 42\n: keepalive\ndata: {\"a\":1}\n\n"
	captured, err := captureEvents(t, original, 1024)
	require.NoError(t, err)
	require.Len(t, captured, 1)

	rebuilt := formatSSEEventWithData([]byte(captured[0].NonData), []byte(captured[0].Data))
	reparsed, err := captureEvents(t, string(rebuilt), 1024)
	require.NoError(t, err)
	require.Len(t, reparsed, 1)
	require.Equal(t, captured[0].Data, reparsed[0].Data)
	require.Equal(t, captured[0].NonData, reparsed[0].NonData)
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
	err := forEachSSEEvent(strings.NewReader(body), 1024, func(_ []byte, _ []byte, _ []byte) error {
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
