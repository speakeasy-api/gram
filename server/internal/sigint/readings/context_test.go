package readings

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestReadingIdentityNamespacesTriggersAndRuns(t *testing.T) {
	t.Parallel()
	stream, err := ParseContext("stream", "", "2026-10-17T12:00:00Z")
	require.NoError(t, err)
	manual, err := ParseContext("manual", "00000000-0000-4000-8000-000000000001", "2026-10-17T13:00:00Z")
	require.NoError(t, err)
	streamID := stream.ReadingID("example-org", "example-project", "conversation.message", "example-message", "example-sensor")
	manualID := manual.ReadingID("example-org", "example-project", "conversation.message", "example-message", "example-sensor")
	require.Equal(t, "80245444-29dc-583a-9e65-02e40e1f94ee", streamID.String())
	require.Equal(t, "4fa71bd4-c38f-552d-9e2a-e8e0ad9a5d91", manualID.String())
	require.NotEqual(t, streamID, manualID)
	require.Equal(t, manualID, manual.ReadingID("example-org", "example-project", "conversation.message", "example-message", "example-sensor"))
	manual.RunID = uuid.New()
	require.NotEqual(t, manualID, manual.ReadingID("example-org", "example-project", "conversation.message", "example-message", "example-sensor"))
	require.NotEqual(t, streamID, stream.ReadingID("another-org", "example-project", "conversation.message", "example-message", "example-sensor"))
	require.NotEqual(t, streamID, stream.ReadingID("example-org", "another-project", "conversation.message", "example-message", "example-sensor"))
	require.NotEqual(t, streamID, stream.ReadingID("example-org", "example-project", "mcp.tool_call", "example-message", "example-sensor"))
	require.NotEqual(t, streamID, stream.ReadingID("example-org", "example-project", "conversation.message", "another-message", "example-sensor"))
	require.NotEqual(t, streamID, stream.ReadingID("example-org", "example-project", "conversation.message", "example-message", "another-sensor"))
}

func TestEvaluationContextNormalizesRequestTime(t *testing.T) {
	t.Parallel()
	c, err := ParseContext("manual", "AAAAAAAA-0000-4000-8000-000000000001", "2026-10-17T13:00:00.123456789+01:00")
	require.NoError(t, err)
	require.Equal(t, "aaaaaaaa-0000-4000-8000-000000000001", c.RunID.String())
	require.Equal(t, time.Date(2026, 10, 17, 12, 0, 0, 123456789, time.UTC), c.RequestedAt)
}

func TestEvaluationContextRejectsInvalidProvenance(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, trigger, run, requested string
	}{
		{name: "it rejects an absent trigger", requested: "2026-10-17T12:00:00Z"},
		{name: "it rejects an unknown trigger", trigger: "scheduled", requested: "2026-10-17T12:00:00Z"},
		{name: "it rejects a streaming run", trigger: "stream", run: uuid.NewString(), requested: "2026-10-17T12:00:00Z"},
		{name: "it rejects a missing manual run", trigger: "manual", requested: "2026-10-17T12:00:00Z"},
		{name: "it rejects a zero manual run", trigger: "manual", run: uuid.Nil.String(), requested: "2026-10-17T12:00:00Z"},
		{name: "it rejects a malformed run", trigger: "manual", run: "invalid", requested: "2026-10-17T12:00:00Z"},
		{name: "it rejects missing request time", trigger: "stream"},
		{name: "it rejects malformed request time", trigger: "stream", requested: "yesterday"},
		{name: "it rejects pre-epoch request time", trigger: "stream", requested: "1969-01-01T00:00:00Z"},
		{name: "it rejects request time beyond retention limits", trigger: "stream", requested: "2104-01-01T00:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseContext(tc.trigger, tc.run, tc.requested)
			require.Error(t, err)
		})
	}
}
