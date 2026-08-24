package telemetry

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// eventLogRowFixture builds a fully populated feed row so each test only has
// to override the field it exercises.
func eventLogRowFixture() repo.EventLogRow {
	return repo.EventLogRow{
		RecordID:           "rec-1",
		TimeUnixNano:       1766138400000000000,
		Kind:               "log",
		Source:             "unit-test-source",
		Name:               "unit.test.event",
		BodyPreview:        "hello world",
		TraceID:            "0123456789abcdef0123456789abcdef",
		SpanID:             "0123456789abcdef",
		ProjectID:          "11111111-1111-1111-1111-111111111111",
		Attributes:         `{"http.route":"/v1/things"}`,
		ResourceAttributes: `{"service.name":"unit-test-service"}`,
	}
}

func TestEventLogCursor_RoundTrip(t *testing.T) {
	t.Parallel()

	encoded := encodeEventLogCursor(1766138400123456789, "rec-abc")
	require.NotEmpty(t, encoded)

	decoded, err := decodeEventLogCursor(encoded)
	require.NoError(t, err)
	require.Equal(t, int64(1766138400123456789), decoded.TimeUnixNano)
	require.Equal(t, "rec-abc", decoded.RecordID)
}

func TestDecodeEventLogCursor_RejectsInvalidBase64(t *testing.T) {
	t.Parallel()

	_, err := decodeEventLogCursor("not base64!!!")
	require.Error(t, err)
}

func TestDecodeEventLogCursor_RejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	// "bm90LWpzb24" is base64 for "not-json".
	_, err := decodeEventLogCursor("bm90LWpzb24")
	require.Error(t, err)
}

func TestDecodeEventLogCursor_RejectsMissingRecordID(t *testing.T) {
	t.Parallel()

	// Base64 of {"time_unix_nano":1} — no record_id.
	_, err := decodeEventLogCursor("eyJ0aW1lX3VuaXhfbmFubyI6MX0")
	require.ErrorContains(t, err, "record_id")
}

func TestValidateEventKinds_AcceptsKnownKinds(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateEventKinds(nil))
	require.NoError(t, validateEventKinds([]string{"log"}))
	require.NoError(t, validateEventKinds([]string{"span", "log"}))
}

func TestValidateEventKinds_RejectsUnknownKind(t *testing.T) {
	t.Parallel()

	require.Error(t, validateEventKinds([]string{"metric"}))
}

func TestToEventLogEntry_ParsesAttributeJSON(t *testing.T) {
	t.Parallel()

	entry, err := toEventLogEntry(eventLogRowFixture())
	require.NoError(t, err)
	require.Equal(t, "rec-1", entry.RecordID)
	require.Equal(t, "1766138400000000000", entry.TimeUnixNano)
	require.Equal(t, "log", entry.Kind)
	require.Equal(t, map[string]any{"http.route": "/v1/things"}, entry.Attributes)
	require.Equal(t, map[string]any{"service.name": "unit-test-service"}, entry.ResourceAttributes)
}

func TestToEventLogEntry_RejectsMalformedAttributes(t *testing.T) {
	t.Parallel()

	row := eventLogRowFixture()
	row.Attributes = "{not json"
	_, err := toEventLogEntry(row)
	require.Error(t, err)
}
