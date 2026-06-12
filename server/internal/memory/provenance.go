package memory

import (
	"encoding/json"
	"time"

	"github.com/speakeasy-api/gram/server/internal/conv"
)

// Source kinds mirror the assistant thread source surfaces defined by the
// assistants package adapters (slack|cron|wake|dashboard). They are duplicated
// here as plain strings because the assistants constants are unexported and
// importing that package would invert the dependency direction.
const (
	sourceKindSlack     = "slack"
	sourceKindCron      = "cron"
	sourceKindWake      = "wake"
	sourceKindDashboard = "dashboard"
)

// provenance captures who said the remembered fact, in what channel, and when
// it was recorded. All fields are optional: rows written outside an assistant
// thread (or before provenance existed) carry none, and each source surface
// only fills the fields it can attest to.
type provenance struct {
	Kind      *string
	UserID    *string
	Channel   *string
	Timestamp *time.Time
}

// extractProvenance maps an origin thread's source surface onto memory
// provenance. Per source kind:
//
//   - slack: user_id is the Slack user who triggered the turn and channel_id
//     is the Slack channel it happened in.
//   - dashboard: user_id is the Gram dashboard user driving the conversation;
//     there is no channel concept.
//   - cron/wake: no human speaker; the trigger instance id is recorded as the
//     channel so memories written by automated runs remain attributable to a
//     specific trigger.
//
// Timestamp is the time of write: the triggering event's own timestamp is not
// available at Remember() time (the tool call carries only the thread
// principal), and the write happens within the same turn as the event, so the
// write time is a faithful proxy.
func extractProvenance(kind string, sourceRefJSON []byte) provenance {
	now := time.Now()
	out := provenance{
		Kind:      conv.PtrEmpty(kind),
		UserID:    nil,
		Channel:   nil,
		Timestamp: &now,
	}

	switch kind {
	case sourceKindSlack:
		var ref struct {
			ChannelID string `json:"channel_id"`
			UserID    string `json:"user_id"`
		}
		if err := json.Unmarshal(sourceRefJSON, &ref); err == nil {
			out.UserID = conv.PtrEmpty(ref.UserID)
			out.Channel = conv.PtrEmpty(ref.ChannelID)
		}
	case sourceKindDashboard:
		var ref struct {
			UserID string `json:"user_id"`
		}
		if err := json.Unmarshal(sourceRefJSON, &ref); err == nil {
			out.UserID = conv.PtrEmpty(ref.UserID)
		}
	case sourceKindCron, sourceKindWake:
		var ref struct {
			TriggerInstanceID string `json:"trigger_instance_id"`
		}
		if err := json.Unmarshal(sourceRefJSON, &ref); err == nil {
			out.Channel = conv.PtrEmpty(ref.TriggerInstanceID)
		}
	}

	return out
}
