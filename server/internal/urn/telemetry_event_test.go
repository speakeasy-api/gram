package urn_test

import (
	"testing"

	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

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
