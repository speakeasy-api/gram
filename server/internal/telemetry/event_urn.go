package telemetry

import (
	"strings"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"go.opentelemetry.io/otel/attribute"
)

// EventOrigin identifies the channel that observed a telemetry record. The
// vocabulary is deliberately coarse: producer identity (claude-code vs cursor
// vs codex) stays in its existing attributes (gram.hook.source, service
// attributes); origin only says which kind of channel saw the event.
type EventOrigin string

const (
	// EventOriginProviderOTEL is telemetry emitted natively by a provider's
	// own OTel exporter (e.g. Claude Code CLI logs and metrics).
	EventOriginProviderOTEL EventOrigin = "provider_otel"
	// EventOriginProviderAPI is telemetry polled from a provider's
	// compliance/admin API (e.g. the Cursor Admin API usage poller).
	EventOriginProviderAPI EventOrigin = "provider_api"
	// EventOriginAgentHook is telemetry translated from Gram plugin hook
	// events running beside the agent (Claude/Codex/Cursor hooks).
	EventOriginAgentHook EventOrigin = "agent_hook"
	// EventOriginGramGateway is telemetry recorded by the Gram tool proxy /
	// MCP gateway around proxied tool executions and resource reads.
	EventOriginGramGateway EventOrigin = "gram_gateway"
	// EventOriginGramRuntime is telemetry recorded by Gram's own runtimes:
	// chat completions, assistants, and triggers.
	EventOriginGramRuntime EventOrigin = "gram_runtime"
	// EventOriginGramWorker is telemetry recorded by Gram background workers
	// (e.g. chat resolution evaluations).
	EventOriginGramWorker EventOrigin = "gram_worker"
	// EventOriginUnknown marks rows no derivation rule recognizes. These are
	// deliberately visible rather than dropped or guessed.
	EventOriginUnknown EventOrigin = "unknown"
)

// EventKind identifies the OTel signal shape of a record. telemetry_logs
// physically stores everything as log rows today, but usage measurements
// flattened into it are semantically metric data points and are classified as
// such so one prefix filter finds them.
type EventKind string

const (
	EventKindLog    EventKind = "log"
	EventKindMetric EventKind = "metric"
)

// eventURNPrefix namespaces the canonical event identity. The scheme is
// urn:gram:telemetry:event:<origin>:<kind>:<type>.
const eventURNPrefix = "urn:gram:telemetry:event"

// eventTypeUnknown is the type segment for rows whose producer supplied no
// usable event type. The absence is signal; it must stay queryable.
const eventTypeUnknown = "unknown"

// eventTypeUsage is the type segment for synthesized usage-measurement rows
// (token/cost data points flattened into telemetry_logs).
const eventTypeUsage = "usage"

// NewEventURN builds the canonical event identity URN stamped on every
// telemetry_logs row: urn:gram:telemetry:event:<origin>:<kind>:<type>. The
// type segment is the producer's own event type identifier (PreToolUse,
// api_request, tool_call, ...), sanitized so it cannot break the
// colon-delimited segment structure. The raw producer name always remains
// available in the row's attributes; the URN is a classifier, not a store.
func NewEventURN(origin EventOrigin, kind EventKind, eventType string) string {
	return eventURNPrefix + ":" + string(origin) + ":" + string(kind) + ":" + sanitizeEventType(eventType)
}

// sanitizeEventType keeps the type segment URN-safe and deterministic:
// letters, digits, '.', '_' and '-' pass through verbatim (case preserved so
// PreToolUse stays recognizable); every other rune becomes '_'. Empty input
// maps to the sentinel "unknown".
func sanitizeEventType(eventType string) string {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return eventTypeUnknown
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.', r == '_', r == '-':
			return r
		default:
			return '_'
		}
	}, eventType)
}

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

// deriveEventURN computes the canonical event URN for a row from the signals
// existing writers already stamp: gram.event.source, the legacy ToolInfo URN,
// gram.hook.event, the producer event name, and the evaluation name. Callers
// that stamp attr.EventURNKey explicitly bypass this entirely.
//
// The mapping mirrors the producer combinations we ingest today (see the
// telemetry v2 canonical model RFC): anything unrecognized classifies as
// origin "unknown" instead of being guessed, so gaps stay visible.
func deriveEventURN(legacyURN string, attrs map[attr.Key]any) string {
	eventSource := getString(attrs, attr.EventSourceKey)

	switch EventSource(eventSource) {
	case EventSourceToolCall, EventSourceResourceRead:
		return NewEventURN(EventOriginGramGateway, EventKindLog, eventSource)
	case EventSourceChatCompletion, EventSourceAssistant, EventSourceTrigger:
		return NewEventURN(EventOriginGramRuntime, EventKindLog, eventSource)
	case EventSourceEvaluation:
		eventType := getString(attrs, attr.GenAIEvaluationNameKey)
		if eventType == "" {
			eventType = eventSource
		}
		return NewEventURN(EventOriginGramWorker, EventKindLog, eventType)
	case EventSourceAPI:
		// The only event_source=api writer today is the Cursor Admin API
		// usage poller; its rows are usage measurements.
		return NewEventURN(EventOriginProviderAPI, EventKindMetric, eventTypeUsage)
	case EventSourceHook:
		return deriveHookEventURN(legacyURN, attrs)
	default:
		return NewEventURN(EventOriginUnknown, EventKindLog, eventSource)
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
		return NewEventURN(EventOriginProviderOTEL, EventKindLog, eventType)
	case legacyClaudeUsageURN, legacyCodexUsageURN:
		// Usage data points normalized from provider OTel metrics (Claude)
		// or provider OTel logs (Codex response.completed events).
		return NewEventURN(EventOriginProviderOTEL, EventKindMetric, eventTypeUsage)
	case legacyCursorHookUsageURN:
		// Token usage observed by the Cursor plugin's afterAgentResponse
		// hook, recorded as a synthetic usage row beside the hook event.
		return NewEventURN(EventOriginAgentHook, EventKindMetric, eventTypeUsage)
	}

	// Plain plugin hook event rows carry the resolved hook event name
	// (PreToolUse, afterAgentResponse, skill.activated, ...).
	return NewEventURN(EventOriginAgentHook, EventKindLog, getString(attrs, attr.HookEventKey))
}
