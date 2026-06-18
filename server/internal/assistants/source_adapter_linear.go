package assistants

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type linearSourceRef struct {
	EventType string `json:"event_type,omitempty"`
	URL       string `json:"url,omitempty"`
}

type linearEventPayload struct {
	EventType   string          `json:"event_type,omitempty"`
	Type        string          `json:"type,omitempty"`
	Action      string          `json:"action,omitempty"`
	URL         string          `json:"url,omitempty"`
	Data        json.RawMessage `json:"data,omitempty"`
	UpdatedFrom json.RawMessage `json:"updated_from,omitempty"`
	ReceivedAt  string          `json:"received_at,omitempty"`
}

type linearAdapter struct{ deterministicChatIDAdapter }

func (linearAdapter) ThreadContext(sourceRefJSON []byte) (string, error) {
	var ref linearSourceRef
	if err := json.Unmarshal(sourceRefJSON, &ref); err != nil {
		return "", fmt.Errorf("decode linear source ref: %w", err)
	}
	var b bytes.Buffer
	b.WriteString("## Conversation context\n\n")
	b.WriteString("Conversation originated on: Linear\n")
	if ref.EventType != "" {
		fmt.Fprintf(&b, "EventType: %s\n", ref.EventType)
	}
	if ref.URL != "" {
		fmt.Fprintf(&b, "URL: %s\n", ref.URL)
	}
	return b.String(), nil
}

func (linearAdapter) OutputChannelGuidance() string {
	return `## Linear output preferences

Text responses are not delivered to Linear. To act on the event (e.g. update the issue, post a comment, change status), call a Linear tool. If no suitable tool is available, log the situation and stop.`
}

func (linearAdapter) DecodeTurn(event assistantThreadEventRecord) (string, error) {
	var payload linearEventPayload
	if err := json.Unmarshal(event.NormalizedPayloadJSON, &payload); err != nil {
		return "", fmt.Errorf("decode linear event payload: %w", err)
	}
	var b bytes.Buffer
	b.WriteString("<message-context>\n")
	fmt.Fprintf(&b, "EventID: %s\n", event.EventID)
	if payload.EventType != "" {
		fmt.Fprintf(&b, "EventType: %s\n", payload.EventType)
	}
	if payload.URL != "" {
		fmt.Fprintf(&b, "URL: %s\n", payload.URL)
	}
	if payload.ReceivedAt != "" {
		fmt.Fprintf(&b, "ReceivedAt: %s\n", payload.ReceivedAt)
	}
	b.WriteString("</message-context>\n\n")
	fmt.Fprintf(&b, "Linear webhook received: %s.", payload.EventType)
	if payload.URL != "" {
		fmt.Fprintf(&b, " See %s.", payload.URL)
	}
	// Inline the entity snapshot Linear delivered so the assistant can read the
	// issue/comment fields directly. Without it the turn only carries metadata
	// and the "inspect the event data" instruction below has nothing to act on.
	if len(payload.Data) > 0 {
		fmt.Fprintf(&b, "\n\n<event-data>\n%s\n</event-data>", string(payload.Data))
	}
	// On an update, surface which fields changed (their prior values) so the
	// assistant can act on the specific transition, not just the new snapshot.
	if len(payload.UpdatedFrom) > 0 {
		fmt.Fprintf(&b, "\n\n<changed-fields-previous-values>\n%s\n</changed-fields-previous-values>", string(payload.UpdatedFrom))
	}
	b.WriteString("\n\nInspect the event data and act on it as configured.")
	return b.String(), nil
}
