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
	Attachments  []dashboardTurnAttachment   `json:"attachments,omitempty"`
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
- Skill: [DisplayName or Name](gram:skill/<ID>) — Skill.ID from the skill tools

Only link an entity when you actually have its id from a tool result, and the link target must be a gram:<type>/<id> reference built from that id. Never write a link with an empty, partial, or guessed URL (e.g. [name]() or [name](gram:user/) ) — if you don't have a usable id, write the name as plain text, not a link. The organization-directory users from platform_list_organization_users have no detail page, so write those as plain text; only link a user when you have their ExternalUserID (from the chats or risk-result tools).` +
		"\n\n## Elements visualizations\n\n" +
		"The dashboard renders Elements widgets from fenced code blocks. Use these formats when a visualization or structured widget helps answer the user's question. The code fence language must be exactly `chart` or `ui`; never expose the widget JSON outside its fence.\n\n" +
		elementsSystemPrompt +
		"\n### Chart code blocks\n\n" +
		elementsChartPrompt +
		"\n### Generative UI code blocks\n\n" +
		elementsGenerativeUIPrompt +
		"\n\n" + dashboardToolCallNarrationRule
}

// Appended last in OutputChannelGuidance so it lands closest to the model's
// generation point — the Elements prompts above are long, and this rule must
// not get lost in the middle of them.
const dashboardToolCallNarrationRule = `## Narrating tool calls

Every time you are about to call tools — even a single tool — the text you emit alongside must be EXACTLY ONE activity phrase and nothing else. The dashboard promotes the phrase to the tool-group heading ONLY when it matches this contract; anything else renders as stray prose and breaks the UX:

- Starts with a present-participle verb phrase: "Investigating…", "Comparing…", "Deep diving…"
- 3 to 8 words, one line, under 90 characters
- No first person (never "I", "let me", "we"), no tool names, no Markdown or backticks, no periods

Good: "Aggregating token spend server-side"
Good: "Retrying via metrics surfaces"
Bad: "That returned huge payloads. Let me aggregate compactly server-side." (narrated prose — never do this)
Bad: "The log-search endpoint returned an empty page, so pulling metrics instead" (commentary, not a phrase)

When a tool result changes your plan or fails, do NOT narrate the setback — just write a fresh activity phrase for the new approach and call the next tools. If a later batch continues the same goal, write nothing between the calls. Save ALL prose, explanations, tables, and widgets for your final answer after the tool results are in.`

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
	// Metadata only, and no minted URLs: DecodeTurn must stay byte-stable
	// across replay. The file bytes (and any download link) ride along as
	// input content parts built at turn dispatch.
	if len(payload.Attachments) > 0 {
		b.WriteString("\n<attachments>\n")
		for _, attachment := range payload.Attachments {
			fmt.Fprintf(&b, "- %s (%s, %d bytes)\n", attachment.Name, attachment.ContentType, attachment.ContentLength)
		}
		b.WriteString("</attachments>\n")
	}
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
