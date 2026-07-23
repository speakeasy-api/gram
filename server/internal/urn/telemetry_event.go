package urn

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
)

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

var telemetryEventOrigins = map[TelemetryEventOrigin]struct{}{
	TelemetryEventOriginProviderOTEL: {},
	TelemetryEventOriginProviderAPI:  {},
	TelemetryEventOriginAgentHook:    {},
	TelemetryEventOriginGramService:  {},
	TelemetryEventOriginUnknown:      {},
}

// TelemetryEventKind identifies the OTel signal shape of a record.
// telemetry_logs physically stores everything as log rows today, but usage
// measurements flattened into it are semantically metric data points and are
// classified as such so one prefix filter finds them.
type TelemetryEventKind string

const (
	TelemetryEventKindLog    TelemetryEventKind = "log"
	TelemetryEventKindMetric TelemetryEventKind = "metric"
)

var telemetryEventKinds = map[TelemetryEventKind]struct{}{
	TelemetryEventKindLog:    {},
	TelemetryEventKindMetric: {},
}

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
// NewTelemetryEvent normalizes the type segment instead of rejecting it;
// ParseTelemetryEvent is strict like the other Parse functions.
type TelemetryEvent struct {
	Origin TelemetryEventOrigin
	Kind   TelemetryEventKind
	Type   string

	checked bool
	err     error
}

// NewTelemetryEvent builds a telemetry event URN. The type segment is the
// producer's own event type identifier (PreToolUse, api_request, tool_call,
// ...), sanitized so it cannot break the colon-delimited segment structure.
// The raw producer name always remains available in the row's attributes; the
// URN is a classifier, not a store.
func NewTelemetryEvent(origin TelemetryEventOrigin, kind TelemetryEventKind, eventType string) TelemetryEvent {
	t := TelemetryEvent{
		Origin: origin,
		Kind:   kind,
		Type:   sanitizeTelemetryEventType(eventType),

		checked: false,
		err:     nil,
	}

	_ = t.validate()

	return t
}

func ParseTelemetryEvent(value string) (TelemetryEvent, error) {
	if value == "" {
		return TelemetryEvent{}, fmt.Errorf("%w: empty string", ErrInvalid)
	}

	parts := strings.Split(value, delimiter)
	if len(parts) != 5 {
		return TelemetryEvent{}, fmt.Errorf("%w: expected five segments (urn:telemetry:<origin>:<kind>:<type>)", ErrInvalid)
	}

	if parts[0] != "urn" || parts[1] != "telemetry" {
		prefix := parts[0] + delimiter + parts[1]
		truncated := prefix[:min(maxSegmentLength, len(prefix))]
		return TelemetryEvent{}, fmt.Errorf("%w: expected telemetry event urn (got: %q)", ErrInvalid, truncated)
	}

	t := TelemetryEvent{
		Origin: TelemetryEventOrigin(parts[2]),
		Kind:   TelemetryEventKind(parts[3]),
		Type:   parts[4],

		checked: false,
		err:     nil,
	}

	if err := t.validate(); err != nil {
		return TelemetryEvent{}, err
	}

	return t, nil
}

func (u TelemetryEvent) IsZero() bool {
	return u.Origin == "" && u.Kind == "" && u.Type == ""
}

func (u TelemetryEvent) String() string {
	return telemetryEventPrefix + delimiter + string(u.Origin) + delimiter + string(u.Kind) + delimiter + u.Type
}

func (u TelemetryEvent) MarshalJSON() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	b, err := json.Marshal(u.String())
	if err != nil {
		return nil, fmt.Errorf("telemetry event urn to json: %w", err)
	}

	return b, nil
}

func (u *TelemetryEvent) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("read telemetry event urn string from json: %w", err)
	}

	parsed, err := ParseTelemetryEvent(s)
	if err != nil {
		return fmt.Errorf("parse telemetry event urn json string: %w", err)
	}

	*u = parsed

	return nil
}

func (u *TelemetryEvent) Scan(value any) error {
	if value == nil {
		return nil
	}

	var s string
	switch v := value.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("cannot scan %T into TelemetryEvent", value)
	}

	parsed, err := ParseTelemetryEvent(s)
	if err != nil {
		return fmt.Errorf("scan database value: %w", err)
	}

	*u = parsed

	return nil
}

func (u TelemetryEvent) Value() (driver.Value, error) {
	if err := u.validate(); err != nil {
		return nil, err
	}

	return u.String(), nil
}

func (u TelemetryEvent) MarshalText() ([]byte, error) {
	if err := u.validate(); err != nil {
		return nil, fmt.Errorf("marshal telemetry event urn text: %w", err)
	}

	return []byte(u.String()), nil
}

func (u *TelemetryEvent) UnmarshalText(text []byte) error {
	parsed, err := ParseTelemetryEvent(string(text))
	if err != nil {
		return fmt.Errorf("unmarshal telemetry event urn text: %w", err)
	}

	*u = parsed

	return nil
}

func (u *TelemetryEvent) validate() error {
	if u.checked {
		return u.err
	}

	u.checked = true

	if _, ok := telemetryEventOrigins[u.Origin]; !ok {
		u.err = fmt.Errorf("%w: unknown telemetry event origin: %q", ErrInvalid, u.Origin)
		return u.err
	}

	if _, ok := telemetryEventKinds[u.Kind]; !ok {
		u.err = fmt.Errorf("%w: unknown telemetry event kind: %q", ErrInvalid, u.Kind)
		return u.err
	}

	if u.Type == "" {
		u.err = fmt.Errorf("%w: empty type", ErrInvalid)
		return u.err
	}

	if len(u.Type) > maxSegmentLength {
		u.err = fmt.Errorf("%w: type segment is too long (max %d, got %d)", ErrInvalid, maxSegmentLength, len(u.Type))
		return u.err
	}

	for _, r := range u.Type {
		if !isTelemetryEventTypeRune(r) {
			u.err = fmt.Errorf("%w: disallowed characters in type: %q", ErrInvalid, u.Type)
			return u.err
		}
	}

	return nil
}

// sanitizeTelemetryEventType keeps the type segment URN-safe and
// deterministic: letters are lowercased so producer casing (PreToolUse vs
// pretooluse) cannot split one event class into several; digits, '.', '_' and
// '-' pass through; every other rune becomes '_'. Empty input maps to the
// sentinel "unknown". The result is truncated to maxSegmentLength (safe on
// byte boundaries because the mapped output is pure ASCII) so a built URN
// always round-trips through ParseTelemetryEvent.
func sanitizeTelemetryEventType(eventType string) string {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return telemetryEventTypeUnknown
	}

	eventType = strings.Map(func(r rune) rune {
		switch {
		case isTelemetryEventTypeRune(r):
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '_'
		}
	}, eventType)

	if len(eventType) > maxSegmentLength {
		eventType = eventType[:maxSegmentLength]
	}

	return eventType
}

// isTelemetryEventTypeRune reports whether r is allowed verbatim in the type
// segment of a telemetry event URN.
func isTelemetryEventTypeRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
}
