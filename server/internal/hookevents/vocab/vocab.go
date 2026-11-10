// Package vocab is the canonical hook wire vocabulary. The Goa design builds
// its enums from these lists and the ingest service checks in-process payloads
// against the same lists, so the HTTP edge and the Go path cannot drift. It
// has no dependencies on purpose: the design package compiles it too.
package vocab

// EventTypeClientDeliveryFailed is the event type a hook sender uses to
// report that an earlier event could not reach the control plane.
const EventTypeClientDeliveryFailed = "client.delivery_failed"

// activityEventTypes are the events a hook sender emits about agent activity.
var activityEventTypes = []string{
	"session.started",
	"session.updated",
	"session.ended",
	"mcp.inventory",
	"prompt.submitted",
	"tool.requested",
	"tool.completed",
	"tool.failed",
	"assistant.responded",
	"assistant.thought",
	"usage.reported",
	"skill.activated",
	"notification.reported",
}

// EventTypes is the vocabulary of HookIngestEvent.type: every activity event
// plus the delivery failure report.
var EventTypes = append(append([]string{}, activityEventTypes...), EventTypeClientDeliveryFailed)

// FailedEventTypes is the vocabulary of HookDeliveryFailureData.failed_event_type.
// It excludes the report type itself so a report can never describe another
// report: a drain that fails while replaying reports leaves them spooled for
// the next drain instead of chaining.
var FailedEventTypes = append([]string{}, activityEventTypes...)

// DeliveryPhases is the vocabulary of HookDeliveryFailureData.delivery_phase.
var DeliveryPhases = []string{"live", "drain"}

// DeliveryCauses is the vocabulary of HookDeliveryFailureData.cause.
var DeliveryCauses = []string{"dns", "tls", "timeout", "canceled", "connection", "unreadable-response", "unknown", "http"}

// Set indexes a vocabulary for membership checks.
func Set(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		set[v] = struct{}{}
	}
	return set
}

// Any widens a vocabulary for the Goa Enum DSL.
func Any(values []string) []any {
	out := make([]any, 0, len(values))
	for _, v := range values {
		out = append(out, v)
	}
	return out
}
