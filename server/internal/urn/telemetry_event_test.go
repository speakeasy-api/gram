package urn_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestTelemetryEventRoundTrip(t *testing.T) {
	t.Parallel()

	original := urn.NewTelemetryEvent(urn.TelemetryEventOriginGramService, urn.TelemetryEventKindLog, "tool_call")

	require.False(t, original.IsZero())
	require.Equal(t, "urn:telemetry:gram_service:log:tool_call", original.String())

	parsed, err := urn.ParseTelemetryEvent(original.String())
	require.NoError(t, err)
	require.Equal(t, original.Origin, parsed.Origin)
	require.Equal(t, original.Kind, parsed.Kind)
	require.Equal(t, original.Type, parsed.Type)

	data, err := json.Marshal(original)
	require.NoError(t, err)
	require.Equal(t, `"urn:telemetry:gram_service:log:tool_call"`, string(data))

	var fromJSON urn.TelemetryEvent
	err = json.Unmarshal(data, &fromJSON)
	require.NoError(t, err)
	require.Equal(t, original.Origin, fromJSON.Origin)
	require.Equal(t, original.Kind, fromJSON.Kind)
	require.Equal(t, original.Type, fromJSON.Type)

	text, err := original.MarshalText()
	require.NoError(t, err)
	require.Equal(t, original.String(), string(text))

	var fromText urn.TelemetryEvent
	err = fromText.UnmarshalText(text)
	require.NoError(t, err)
	require.Equal(t, original.Origin, fromText.Origin)
	require.Equal(t, original.Kind, fromText.Kind)
	require.Equal(t, original.Type, fromText.Type)
}

func TestTelemetryEventBuildsCanonicalIdentity(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		"urn:telemetry:gram_service:log:tool_call",
		urn.NewTelemetryEvent(urn.TelemetryEventOriginGramService, urn.TelemetryEventKindLog, "tool_call").String(),
	)
	require.Equal(t,
		"urn:telemetry:provider_api:metric:usage",
		urn.NewTelemetryEvent(urn.TelemetryEventOriginProviderAPI, urn.TelemetryEventKindMetric, "usage").String(),
	)
}

func TestTelemetryEventSanitizesTypeSegment(t *testing.T) {
	t.Parallel()

	// Colons and whitespace would break the colon-delimited segment
	// structure; producer casing is folded to lowercase so one event class
	// cannot split into several.
	require.Equal(t,
		"urn:telemetry:agent_hook:log:some_weird_event",
		urn.NewTelemetryEvent(urn.TelemetryEventOriginAgentHook, urn.TelemetryEventKindLog, "some:weird event").String(),
	)
	require.Equal(t,
		"urn:telemetry:agent_hook:log:pretooluse",
		urn.NewTelemetryEvent(urn.TelemetryEventOriginAgentHook, urn.TelemetryEventKindLog, "PreToolUse").String(),
	)
	require.Equal(t,
		"urn:telemetry:provider_otel:log:codex.sse_event",
		urn.NewTelemetryEvent(urn.TelemetryEventOriginProviderOTEL, urn.TelemetryEventKindLog, "codex.sse_event").String(),
	)
	require.Equal(t,
		"urn:telemetry:agent_hook:log:unknown",
		urn.NewTelemetryEvent(urn.TelemetryEventOriginAgentHook, urn.TelemetryEventKindLog, "  ").String(),
	)
}

func TestTelemetryEventTruncatesOverlongTypeSegment(t *testing.T) {
	t.Parallel()

	// A built URN must always round-trip through ParseTelemetryEvent, so
	// sanitization truncates the type segment to the parse limit.
	overlong := strings.Repeat("a", 200)
	built := urn.NewTelemetryEvent(urn.TelemetryEventOriginAgentHook, urn.TelemetryEventKindLog, overlong)
	require.Len(t, built.Type, 128)

	parsed, err := urn.ParseTelemetryEvent(built.String())
	require.NoError(t, err)
	require.Equal(t, built.Type, parsed.Type)
}

func TestTelemetryEventRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := urn.ParseTelemetryEvent("")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseTelemetryEvent("tools:http:petstore:list_pets")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseTelemetryEvent("urn:gram:telemetry:event:tool_call")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseTelemetryEvent("urn:telemetry:mystery_channel:log:tool_call")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseTelemetryEvent("urn:telemetry:agent_hook:trace:tool_call")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseTelemetryEvent("urn:telemetry:agent_hook:log:")
	require.ErrorIs(t, err, urn.ErrInvalid)

	// Stored values are sanitized (lowercase); parsing is strict.
	_, err = urn.ParseTelemetryEvent("urn:telemetry:agent_hook:log:PreToolUse")
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = urn.ParseTelemetryEvent("urn:telemetry:agent_hook:log:tool_call:extra")
	require.ErrorIs(t, err, urn.ErrInvalid)
}

func TestTelemetryEventZeroValue(t *testing.T) {
	t.Parallel()

	var zero urn.TelemetryEvent
	require.True(t, zero.IsZero())

	_, err := zero.MarshalJSON()
	require.ErrorIs(t, err, urn.ErrInvalid)

	_, err = zero.MarshalText()
	require.ErrorIs(t, err, urn.ErrInvalid)

	value, err := zero.Value()
	require.ErrorIs(t, err, urn.ErrInvalid)
	require.Nil(t, value)

	err = zero.Scan(nil)
	require.NoError(t, err)
	require.True(t, zero.IsZero())
}

func TestTelemetryEventDatabaseRoundTrip(t *testing.T) {
	t.Parallel()

	original := urn.NewTelemetryEvent(urn.TelemetryEventOriginProviderOTEL, urn.TelemetryEventKindMetric, "usage")
	value, err := original.Value()
	require.NoError(t, err)
	require.Equal(t, original.String(), value)

	var fromString urn.TelemetryEvent
	err = fromString.Scan(value)
	require.NoError(t, err)
	require.Equal(t, original.Origin, fromString.Origin)
	require.Equal(t, original.Kind, fromString.Kind)
	require.Equal(t, original.Type, fromString.Type)

	var fromBytes urn.TelemetryEvent
	err = fromBytes.Scan([]byte(original.String()))
	require.NoError(t, err)
	require.Equal(t, original.Origin, fromBytes.Origin)
	require.Equal(t, original.Kind, fromBytes.Kind)
	require.Equal(t, original.Type, fromBytes.Type)

	var invalid urn.TelemetryEvent
	err = invalid.Scan("urn:telemetry:mystery_channel:log:tool_call")
	require.ErrorIs(t, err, urn.ErrInvalid)

	err = invalid.Scan(42)
	require.Error(t, err)
}
