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

When relaying an "assistant_mcp_auth_required" AuthURL, render it as a clickable Markdown link in your reply (e.g. ` + "`[Authorize](<AuthURL>)`" + `) — the dashboard reader IS the owner, no tool call is needed.

## Linking entities

Your reply renders in the Gram dashboard, which turns Markdown links written as [label](gram:<type>/<id>) into clickable links to that entity's page (opened in a new tab). Whenever you mention a specific entity, link it this way using its id from the tool result, with a human-readable label (a name or title, not the raw id) — including the name cell in tables. Never leave a bare id like 9399393 as plain text when you can link it instead; a bare id is a dead end for the reader.

Id values come from the tool results (their JSON field names are PascalCase). Use:
- Chat / agent session: [Title](gram:chat/<ID>) — the chat's ID, or ChatID from the risk tools
- Risk policy: [Name](gram:risk_policy/<ID>) — the policy's ID, or PolicyID
- User: [name or email](gram:risk_user/<ExternalUserID>) — chats expose ExternalUserID; the risk-result tools expose the same value under the name UserID
- Deployment: [label](gram:deployment/<deployment id>)
- Environment: [slug](gram:environment/<environment_slug>)

Only link an entity when you actually have its id from a tool result, and the link target must be a gram:<type>/<id> reference built from that id. Never write a link with an empty, partial, or guessed URL (e.g. [name]() or [name](gram:user/) ) — if you don't have a usable id, write the name as plain text, not a link. The organization-directory users from platform_list_organization_users have no detail page, so write those as plain text; only link a user when you have their ExternalUserID (from the chats or risk-result tools).` +
		"\n\n## Elements visualizations\n\n" +
		"The dashboard renders Elements widgets from fenced code blocks. Use these formats when a visualization or structured widget helps answer the user's question. The code fence language must be exactly `chart` or `ui`; never expose the widget JSON outside its fence.\n\n" +
		elementsSystemPrompt +
		"\n### Chart code blocks\n\n" +
		elementsChartPrompt +
		"\n### Generative UI code blocks\n\n" +
		elementsGenerativeUIPrompt
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
