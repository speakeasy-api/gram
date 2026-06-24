package assistants

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type githubSourceRef struct {
	EventType string `json:"event_type,omitempty"`
	Action    string `json:"action,omitempty"`
	Repo      string `json:"repo,omitempty"`
}

type githubEventPayload struct {
	EventType  string          `json:"event_type,omitempty"`
	Action     string          `json:"action,omitempty"`
	Repo       string          `json:"repo,omitempty"`
	Ref        string          `json:"ref,omitempty"`
	Number     int             `json:"number,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	ReceivedAt string          `json:"received_at,omitempty"`
}

type githubAdapter struct{ deterministicChatIDAdapter }

func (githubAdapter) ThreadContext(sourceRefJSON []byte) (string, error) {
	var ref githubSourceRef
	if err := json.Unmarshal(sourceRefJSON, &ref); err != nil {
		return "", fmt.Errorf("decode github source ref: %w", err)
	}
	var b bytes.Buffer
	b.WriteString("## Conversation context\n\n")
	b.WriteString("Conversation originated on: GitHub\n")
	if ref.EventType != "" {
		fmt.Fprintf(&b, "EventType: %s\n", ref.EventType)
	}
	if ref.Action != "" {
		fmt.Fprintf(&b, "Action: %s\n", ref.Action)
	}
	if ref.Repo != "" {
		fmt.Fprintf(&b, "Repo: %s\n", ref.Repo)
	}
	return b.String(), nil
}

func (githubAdapter) OutputChannelGuidance() string {
	return `## GitHub output preferences

Text responses are not delivered to GitHub. To act on the event (e.g. comment on an issue/PR, merge a PR, create a review), call a GitHub tool. If no suitable tool is available, log the situation and stop.`
}

func (githubAdapter) DecodeTurn(event assistantThreadEventRecord) (string, error) {
	var payload githubEventPayload
	if err := json.Unmarshal(event.NormalizedPayloadJSON, &payload); err != nil {
		return "", fmt.Errorf("decode github event payload: %w", err)
	}
	var b bytes.Buffer
	b.WriteString("<message-context>\n")
	fmt.Fprintf(&b, "EventID: %s\n", event.EventID)
	if payload.EventType != "" {
		fmt.Fprintf(&b, "EventType: %s\n", payload.EventType)
	}
	if payload.Action != "" {
		fmt.Fprintf(&b, "Action: %s\n", payload.Action)
	}
	if payload.Repo != "" {
		fmt.Fprintf(&b, "Repo: %s\n", payload.Repo)
	}
	if payload.Ref != "" {
		fmt.Fprintf(&b, "Ref: %s\n", payload.Ref)
	}
	if payload.Number != 0 {
		fmt.Fprintf(&b, "Number: %d\n", payload.Number)
	}
	if payload.ReceivedAt != "" {
		fmt.Fprintf(&b, "ReceivedAt: %s\n", payload.ReceivedAt)
	}
	b.WriteString("</message-context>\n\n")
	fmt.Fprintf(&b, "GitHub webhook received: %s", payload.EventType)
	if payload.Action != "" {
		fmt.Fprintf(&b, " (%s)", payload.Action)
	}
	if payload.Repo != "" {
		fmt.Fprintf(&b, " on %s", payload.Repo)
	}
	// Inline the raw webhook body GitHub delivered so the assistant can read the
	// PR/issue/comment/review fields directly. Without it the turn only carries
	// metadata and the "inspect the event payload" instruction has nothing to
	// act on.
	if len(payload.Payload) > 0 {
		fmt.Fprintf(&b, "\n\n<event-payload>\n%s\n</event-payload>", string(payload.Payload))
	}
	b.WriteString("\n\nInspect the event payload and act on it as configured.")
	return b.String(), nil
}
