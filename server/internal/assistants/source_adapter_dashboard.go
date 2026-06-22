package assistants

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type dashboardSourceRef struct {
	// UserID is the Gram dashboard user driving the conversation (attribution).
	// The conversation thread is keyed by the caller-supplied correlation id,
	// not the user id, so a user can start a fresh thread at will.
	UserID string `json:"user_id"`
}

type dashboardEventPayload struct {
	Text   string `json:"text"`
	UserID string `json:"user_id,omitempty"`
}

type dashboardAdapter struct{}

func (dashboardAdapter) ThreadContext(sourceRefJSON []byte) (string, error) {
	var ref dashboardSourceRef
	if err := json.Unmarshal(sourceRefJSON, &ref); err != nil {
		return "", fmt.Errorf("decode dashboard source ref: %w", err)
	}
	var b bytes.Buffer
	b.WriteString("## Conversation context\n\n")
	b.WriteString("Conversation originated on: Gram dashboard\n")
	if ref.UserID != "" {
		fmt.Fprintf(&b, "UserID: %s\n", ref.UserID)
	}
	return b.String(), nil
}

func (dashboardAdapter) OutputChannelGuidance() string {
	return `## Dashboard output preferences

You are answering a Gram user in the web dashboard's side panel. Your reply text is shown to the user directly — just answer in Markdown, conversationally and concisely; prefer compact tables and short summaries over long prose. This is an analyst's side panel, not a chat app.

When relaying an "assistant_mcp_auth_required" AuthURL, render it as a clickable Markdown link in your reply (e.g. ` + "`[Authorize](<AuthURL>)`" + `) — the dashboard reader IS the owner, no tool call is needed.`
}

// ChatID: the dashboard's correlation key already IS the server-minted chat id
// (round-tripped by the client). Use it directly; fall back to a deterministic
// hash if a non-UUID correlation key ever slips through.
func (dashboardAdapter) ChatID(assistantID uuid.UUID, correlationID string) uuid.UUID {
	if parsed, err := uuid.Parse(correlationID); err == nil {
		return parsed
	}
	return deterministicChatID(assistantID, correlationID)
}

func (dashboardAdapter) DecodeTurn(event assistantThreadEventRecord) (string, error) {
	var payload dashboardEventPayload
	if err := json.Unmarshal(event.NormalizedPayloadJSON, &payload); err != nil {
		return "", fmt.Errorf("decode dashboard event payload: %w", err)
	}
	var b bytes.Buffer
	b.WriteString("<message-context>\n")
	fmt.Fprintf(&b, "EventID: %s\n", event.EventID)
	// Sourced from the event's immutable created_at, not time.Now(): DecodeTurn
	// must be byte-stable across retry/replay, or the capture matcher treats the
	// re-decoded turn as divergent and opens a spurious generation.
	if !event.CreatedAt.IsZero() {
		fmt.Fprintf(&b, "Timestamp: %s\n", event.CreatedAt.UTC().Format(time.RFC3339))
	}
	if payload.UserID != "" {
		fmt.Fprintf(&b, "UserID: %s\n", payload.UserID)
	}
	b.WriteString("</message-context>\n\n")
	b.WriteString(payload.Text)
	return b.String(), nil
}
