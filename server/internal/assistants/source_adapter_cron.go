package assistants

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type cronSourceRef struct {
	TriggerInstanceID string `json:"trigger_instance_id"`
	Schedule          string `json:"schedule"`
}

type cronEventPayload struct {
	Schedule          string `json:"schedule"`
	FiredAt           string `json:"fired_at"`
	TriggerInstanceID string `json:"trigger_instance_id"`
	Note              string `json:"note,omitempty"`
}

type cronAdapter struct{ deterministicChatIDAdapter }

func (cronAdapter) ThreadContext(sourceRefJSON []byte) (string, error) {
	var ref cronSourceRef
	if err := json.Unmarshal(sourceRefJSON, &ref); err != nil {
		return "", fmt.Errorf("decode cron source ref: %w", err)
	}
	var b bytes.Buffer
	b.WriteString("## Conversation context\n\n")
	b.WriteString("Conversation originated on: Cron schedule\n")
	if ref.Schedule != "" {
		fmt.Fprintf(&b, "Schedule: %s\n", ref.Schedule)
	}
	if ref.TriggerInstanceID != "" {
		fmt.Fprintf(&b, "TriggerInstanceID: %s\n", ref.TriggerInstanceID)
	}
	return b.String(), nil
}

func (cronAdapter) OutputChannelGuidance() string {
	return `## Cron output preferences

Text responses are not delivered to anyone — no human is watching the cron tick. To produce a side effect or notify someone, call a tool (e.g. post a Slack message, send an email, write to an external system). If no suitable tool is available for the work this tick requires, log the situation and stop.`
}

func (cronAdapter) DecodeTurn(event assistantThreadEventRecord) (string, error) {
	var payload cronEventPayload
	if err := json.Unmarshal(event.NormalizedPayloadJSON, &payload); err != nil {
		return "", fmt.Errorf("decode cron event payload: %w", err)
	}
	var b bytes.Buffer
	b.WriteString("<message-context>\n")
	fmt.Fprintf(&b, "EventID: %s\n", event.EventID)
	if payload.FiredAt != "" {
		fmt.Fprintf(&b, "FiredAt: %s\n", payload.FiredAt)
	}
	if payload.Schedule != "" {
		fmt.Fprintf(&b, "Schedule: %s\n", payload.Schedule)
	}
	b.WriteString("</message-context>\n\n")
	if payload.Schedule != "" {
		fmt.Fprintf(&b, "Scheduled run fired for schedule %q at %s. Execute the assistant's configured task for this tick.", payload.Schedule, payload.FiredAt)
	} else {
		fmt.Fprintf(&b, "Scheduled run fired at %s. Execute the assistant's configured task for this tick.", payload.FiredAt)
	}
	if payload.Note != "" {
		fmt.Fprintf(&b, " Note: %s", payload.Note)
	}
	return b.String(), nil
}
