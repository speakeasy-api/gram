package urn

import "strings"

// TelemetryEventOrigin identifies the channel that observed a telemetry
// record. The vocabulary is deliberately coarse: producer identity
// (claude-code vs cursor vs codex) stays in the row's existing attributes
// (gram.hook.source, service attributes); origin only says which kind of
// channel saw the event.
type TelemetryEventOrigin string

const (
	// TelemetryEventOriginProviderOTEL is telemetry emitted natively by a
	// provider's own OTel exporter (e.g. Claude Code CLI logs and metrics).
	TelemetryEventOriginProviderOTEL TelemetryEventOrigin = "provider_otel"
	// TelemetryEventOriginProviderAPI is telemetry polled from a provider's
	// compliance/admin API (e.g. the Cursor Admin API usage poller).
	TelemetryEventOriginProviderAPI TelemetryEventOrigin = "provider_api"
	// TelemetryEventOriginAgentHook is telemetry translated from Gram plugin
	// hook events running beside the agent (Claude/Codex/Cursor hooks).
	TelemetryEventOriginAgentHook TelemetryEventOrigin = "agent_hook"
	// TelemetryEventOriginGramService is telemetry recorded by Gram itself:
	// the tool proxy / MCP gateway, chat completions, assistants, triggers,
	// and background workers.
	TelemetryEventOriginGramService TelemetryEventOrigin = "gram_service"
	// TelemetryEventOriginUnknown marks rows no derivation rule recognizes.
	// These are deliberately visible rather than dropped or guessed.
	TelemetryEventOriginUnknown TelemetryEventOrigin = "unknown"
)

// TelemetryEventKind identifies the OTel signal shape of a record.
// telemetry_logs physically stores everything as log rows today, but usage
// measurements flattened into it are semantically metric data points and are
// classified as such so one prefix filter finds them.
type TelemetryEventKind string

const (
	TelemetryEventKindLog    TelemetryEventKind = "log"
	TelemetryEventKindMetric TelemetryEventKind = "metric"
)

// telemetryEventPrefix namespaces the canonical event identity. The scheme is
// urn:telemetry:<origin>:<kind>:<type>.
const telemetryEventPrefix = "urn" + delimiter + "telemetry"

// telemetryEventTypeUnknown is the type segment for rows whose producer
// supplied no usable event type. The absence is signal; it must stay
// queryable.
const telemetryEventTypeUnknown = "unknown"

// TelemetryEvent is the canonical event identity stamped on every
// telemetry_logs row: urn:telemetry:<origin>:<kind>:<type>. Unlike resource
// URNs, it identifies a class of events rather than a persistent resource, so
// it is normalized on construction instead of validated and rejected.
type TelemetryEvent struct {
	Origin TelemetryEventOrigin
	Kind   TelemetryEventKind
	Type   string
}

// NewTelemetryEvent builds a telemetry event URN. The type segment is the
// producer's own event type identifier (PreToolUse, api_request, tool_call,
// ...), sanitized so it cannot break the colon-delimited segment structure.
// The raw producer name always remains available in the row's attributes; the
// URN is a classifier, not a store.
func NewTelemetryEvent(origin TelemetryEventOrigin, kind TelemetryEventKind, eventType string) TelemetryEvent {
	return TelemetryEvent{
		Origin: origin,
		Kind:   kind,
		Type:   sanitizeTelemetryEventType(eventType),
	}
}

func (u TelemetryEvent) String() string {
	return telemetryEventPrefix + delimiter + string(u.Origin) + delimiter + string(u.Kind) + delimiter + u.Type
}

// sanitizeTelemetryEventType keeps the type segment URN-safe and
// deterministic: letters are lowercased so producer casing (PreToolUse vs
// pretooluse) cannot split one event class into several; digits, '.', '_' and
// '-' pass through; every other rune becomes '_'. Empty input maps to the
// sentinel "unknown".
func sanitizeTelemetryEventType(eventType string) string {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return telemetryEventTypeUnknown
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		case r == '.', r == '_', r == '-':
			return r
		default:
			return '_'
		}
	}, eventType)
}
