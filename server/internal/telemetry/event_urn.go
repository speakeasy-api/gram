package telemetry

import (
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"go.opentelemetry.io/otel/attribute"
)

// eventTypeUsage is the type segment for synthesized usage-measurement rows
// (token/cost data points flattened into telemetry_logs).
const eventTypeUsage = "usage"

// Legacy gram_urn values that today double as row classifiers for rows
// arriving through the hooks OTLP endpoint. The writers own these strings
// (server/internal/hooks, server/internal/aiintegrations); they are matched
// here only to translate existing traffic onto the canonical event URN until
// those writers stamp gram.event.urn themselves.
const (
	legacyClaudeOTELLogsURN    = "claude-code:otel:logs"
	legacyClaudeUsageURN       = "claude-code:usage:metrics"
	legacyCodexUsageURN        = "codex:usage:metrics"
	legacyCursorHookUsageURN   = "cursor:usage:metrics"
	legacyClaudeOTELBodyPrefix = "claude_code."
)

// rawEventNameKey is the OTel log record EventName attribute as producers
// send it (Claude Code: api_request, user_prompt, tool_result, ...). It is a
// producer key, not a gram.* convention, so it has no attr constant.
var rawEventNameKey = attribute.Key("event.name")

// deriveEventURN computes the canonical event URN
// (urn:telemetry:<origin>:<kind>:<type>) for a row from the signals existing
// writers already stamp: gram.event.source, the legacy ToolInfo URN,
// gram.hook.event, the producer event name, and the evaluation name. Callers
// that stamp attr.EventURNKey explicitly bypass this entirely.
//
// The mapping mirrors the producer combinations we ingest today (see the
// telemetry v2 canonical model RFC): anything unrecognized classifies as
// origin "unknown" instead of being guessed, so gaps stay visible.
func deriveEventURN(legacyURN string, attrs map[attr.Key]any) string {
	eventSource := getString(attrs, attr.EventSourceKey)

	switch EventSource(eventSource) {
	case EventSourceToolCall, EventSourceResourceRead, EventSourceChatCompletion, EventSourceAssistant, EventSourceTrigger:
		return urn.NewTelemetryEvent(urn.TelemetryEventOriginGramService, urn.TelemetryEventKindLog, eventSource).String()
	case EventSourceEvaluation:
		eventType := getString(attrs, attr.GenAIEvaluationNameKey)
		if eventType == "" {
			eventType = eventSource
		}
		return urn.NewTelemetryEvent(urn.TelemetryEventOriginGramService, urn.TelemetryEventKindLog, eventType).String()
	case EventSourceAPI:
		// The only event_source=api writer today is the Cursor Admin API
		// usage poller; its rows are usage measurements.
		return urn.NewTelemetryEvent(urn.TelemetryEventOriginProviderAPI, urn.TelemetryEventKindMetric, eventTypeUsage).String()
	case EventSourceHook:
		return deriveHookEventURN(legacyURN, attrs)
	default:
		return urn.NewTelemetryEvent(urn.TelemetryEventOriginUnknown, urn.TelemetryEventKindLog, eventSource).String()
	}
}

// deriveHookEventURN classifies the rows the hooks OTLP/ingest endpoints
// write, all of which share gram.event.source=hook today despite arriving
// through different channels.
func deriveHookEventURN(legacyURN string, attrs map[attr.Key]any) string {
	switch legacyURN {
	case legacyClaudeOTELLogsURN:
		// Provider-native OTel log records. The producer's type is the OTel
		// EventName attribute; older Claude CLIs put claude_code.<name> in
		// the log body with no event.name attribute.
		eventType := getString(attrs, rawEventNameKey)
		if eventType == "" {
			if body := getString(attrs, attr.LogBodyKey); strings.HasPrefix(body, legacyClaudeOTELBodyPrefix) {
				eventType = strings.TrimPrefix(body, legacyClaudeOTELBodyPrefix)
			}
		}
		return urn.NewTelemetryEvent(urn.TelemetryEventOriginProviderOTEL, urn.TelemetryEventKindLog, eventType).String()
	case legacyClaudeUsageURN, legacyCodexUsageURN:
		// Usage data points normalized from provider OTel metrics (Claude)
		// or provider OTel logs (Codex response.completed events).
		return urn.NewTelemetryEvent(urn.TelemetryEventOriginProviderOTEL, urn.TelemetryEventKindMetric, eventTypeUsage).String()
	case legacyCursorHookUsageURN:
		// Token usage observed by the Cursor plugin's afterAgentResponse
		// hook, recorded as a synthetic usage row beside the hook event.
		return urn.NewTelemetryEvent(urn.TelemetryEventOriginAgentHook, urn.TelemetryEventKindMetric, eventTypeUsage).String()
	}

	// Plain plugin hook event rows carry the resolved hook event name
	// (PreToolUse, afterAgentResponse, skill.activated, ...).
	return urn.NewTelemetryEvent(urn.TelemetryEventOriginAgentHook, urn.TelemetryEventKindLog, getString(attrs, attr.HookEventKey)).String()
}
