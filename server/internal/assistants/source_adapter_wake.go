package assistants

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type wakeSourceRef struct {
	TriggerInstanceID string `json:"trigger_instance_id"`
	ScheduledAt       string `json:"scheduled_at,omitempty"`
}

type wakeEventPayload struct {
	FiredAt           string `json:"fired_at"`
	ScheduledAt       string `json:"scheduled_at,omitempty"`
	TriggerInstanceID string `json:"trigger_instance_id"`
	Note              string `json:"note,omitempty"`
}

type wakeAdapter struct{ deterministicChatIDAdapter }

func (wakeAdapter) ThreadContext(sourceRefJSON []byte) (string, error) {
	var ref wakeSourceRef
	if err := json.Unmarshal(sourceRefJSON, &ref); err != nil {
		return "", fmt.Errorf("decode wake source ref: %w", err)
	}
	var b bytes.Buffer
	b.WriteString("## Conversation context\n\n")
	b.WriteString("Conversation originated on: Wake trigger\n")
	if ref.TriggerInstanceID != "" {
		fmt.Fprintf(&b, "TriggerInstanceID: %s\n", ref.TriggerInstanceID)
	}
	if ref.ScheduledAt != "" {
		fmt.Fprintf(&b, "ScheduledAt: %s\n", ref.ScheduledAt)
	}
	return b.String(), nil
}

func (wakeAdapter) OutputChannelGuidance() string {
	return `## Wake output preferences

Text responses are not delivered to anyone — the wake trigger fired against a thread you scheduled earlier. To make progress visible, call a tool (e.g. post to the original channel, send a follow-up message). If no suitable tool is available, log the situation and stop.`
}

func (wakeAdapter) DecodeTurn(event assistantThreadEventRecord) (string, error) {
	var payload wakeEventPayload
	if err := json.Unmarshal(event.NormalizedPayloadJSON, &payload); err != nil {
		return "", fmt.Errorf("decode wake event payload: %w", err)
	}
	var b bytes.Buffer
	b.WriteString("<message-context>\n")
	fmt.Fprintf(&b, "EventID: %s\n", event.EventID)
	if payload.FiredAt != "" {
		fmt.Fprintf(&b, "FiredAt: %s\n", payload.FiredAt)
	}
	if payload.ScheduledAt != "" {
		fmt.Fprintf(&b, "ScheduledAt: %s\n", payload.ScheduledAt)
	}
	b.WriteString("</message-context>\n\n")
	b.WriteString("Wake trigger fired. You scheduled this earlier to resume work in this thread.")
	if payload.Note != "" {
		fmt.Fprintf(&b, " Self-note: %s", payload.Note)
	}
	return b.String(), nil
}
