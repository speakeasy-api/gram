package assistants

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
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
	Text         string                      `json:"text"`
	UserID       string                      `json:"user_id,omitempty"`
	SkillContext []dashboardTurnSkillContext `json:"skill_context,omitempty"`
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
	return `## HARD RULE — text sent with tool calls is a heading, not speech

Whenever a reply of yours contains tool calls, the dashboard renders that reply's text as the heading above those calls — the user sees it where "Calling 3 tools" would otherwise be. It is a UI label. It is never read as a message to the user.

So when you call tools, the text you send with them must be EXACTLY this and nothing else:

- A doing-phrase, always: it MUST begin with a verb ending in -ing and name what you are doing for the user — "Searching recent chats for leaked secrets", "Investigating failing tool calls", "Breaking down spend by model". Never a statement, a question, or an announcement.
- 3 to 8 words.
- No first person. The strings "I'll", "I will", "I'm", "Let me", "Now let me", "Let's" must not appear.
- One fragment only — no second sentence, no full stop, no comma-spliced aside, no Markdown.
- Never name a tool, and never use the words "tool", "function", "API", "query", "endpoint", "filter", "limit", "pagination". Describe what the user gets, not how you get it.
- Never report a result, a surprise, or a failure here. Not "The overview shows zero failures", not "Odd — the loop broke", not "The status-code filter clearly isn't being applied". Those belong in your final reply, after the results.

Every one of these was produced by an assistant on this surface and is WRONG. The fix is on the right:

  "I'll pull recent tool-call failures and slice them by tool, server, and caller. Analyzing recent tool-call failures across servers and clients" → "Analyzing recent tool-call failures"
  "I'll pull the usage data across those dimensions. Breaking down token spend by tool, model, and client" → "Breaking down token spend by tool and model"
  "The overview shows zero failures, but it may only count a narrow definition. Let me query logs directly for non-2xx statuses" → "Checking logs for failed calls"
  "The status-code filter clearly isn't being applied (it returned everything). Let me count properly server-side" → "Counting failures by status"
  "Odd — the loop broke. Let me redo it properly" → "Gathering the remaining error data"

Note what the last three have in common: the goal did not change because an attempt failed. Retry silently under the same heading. If you truly cannot retrieve the data, say so in your final reply once you are sure — never in a heading.

If no such fragment fits, send no text with the tool calls at all. Silence is correct; a sentence of prose is not.

## Dashboard output preferences

You are answering a Gram user in the web dashboard's side panel. Your reply text is shown to the user directly — just answer in Markdown, conversationally and concisely; prefer compact tables and short summaries over long prose. This is an analyst's side panel, not a chat app. All of the above applies only to text sent alongside tool calls; your final reply, once the results are in, is normal prose to the user.

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
	b.WriteString("</message-context>\n")
	for _, skill := range payload.SkillContext {
		if skill.SkillID == uuid.Nil || skill.ResolvedVersionID == uuid.Nil || skill.Name == "" || skill.Content == "" {
			return "", fmt.Errorf("dashboard turn contains invalid skill context")
		}
		name, description := safeSkillMetadata(assistantSkillSnapshot{
			SkillID:           skill.SkillID,
			Name:              skill.Name,
			Description:       skill.Description,
			ResolvedVersionID: skill.ResolvedVersionID,
		})
		b.WriteString("\n<skill-context>\n")
		fmt.Fprintf(&b, "Name: %s\nDescription: %s\n", name, description)
		b.WriteString("<skill-content>\n")
		b.WriteString(skill.Content)
		if !strings.HasSuffix(skill.Content, "\n") {
			b.WriteByte('\n')
		}
		b.WriteString("</skill-content>\n</skill-context>\n")
	}
	b.WriteByte('\n')
	b.WriteString(payload.Text)
	return b.String(), nil
}
