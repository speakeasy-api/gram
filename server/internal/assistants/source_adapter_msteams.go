package assistants

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type msteamsSourceRef struct {
	TenantID       string `json:"tenant_id,omitempty"`
	ConversationID string `json:"conversation_id"`
	ServiceURL     string `json:"service_url,omitempty"`
	UserID         string `json:"user_id,omitempty"`
}

// msteamsEventPayload mirrors the JSON shape of the trigger package's
// msteamsTriggerEvent.
type msteamsEventPayload struct {
	EventType        string   `json:"event_type,omitempty"`
	ActivityID       string   `json:"activity_id,omitempty"`
	ConversationID   string   `json:"conversation_id,omitempty"`
	ConversationType string   `json:"conversation_type,omitempty"`
	TenantID         string   `json:"tenant_id,omitempty"`
	ServiceURL       string   `json:"service_url,omitempty"`
	TeamsChannelID   string   `json:"teams_channel_id,omitempty"`
	TeamsTeamID      string   `json:"teams_team_id,omitempty"`
	UserID           string   `json:"user_id,omitempty"`
	UserName         string   `json:"user_name,omitempty"`
	UserAADObjectID  string   `json:"user_aad_object_id,omitempty"`
	BotID            string   `json:"bot_id,omitempty"`
	Text             string   `json:"text,omitempty"`
	ReplyToID        string   `json:"reply_to_id,omitempty"`
	Timestamp        string   `json:"timestamp,omitempty"`
	Action           string   `json:"action,omitempty"`
	ReactionsAdded   []string `json:"reactions_added,omitempty"`
	ReactionsRemoved []string `json:"reactions_removed,omitempty"`
	MembersAdded     []string `json:"members_added,omitempty"`
	MembersRemoved   []string `json:"members_removed,omitempty"`
}

type msteamsAdapter struct{ deterministicChatIDAdapter }

func (msteamsAdapter) ThreadContext(sourceRefJSON []byte) (string, error) {
	var ref msteamsSourceRef
	if err := json.Unmarshal(sourceRefJSON, &ref); err != nil {
		return "", fmt.Errorf("decode msteams source ref: %w", err)
	}
	var b bytes.Buffer
	b.WriteString("## Conversation context\n\n")
	b.WriteString("Conversation originated on: Microsoft Teams\n")
	if ref.TenantID != "" {
		fmt.Fprintf(&b, "TenantID: %s\n", ref.TenantID)
	}
	if ref.ConversationID != "" {
		fmt.Fprintf(&b, "ConversationID: %s\n", ref.ConversationID)
	}
	if ref.ServiceURL != "" {
		fmt.Fprintf(&b, "ServiceURL: %s\n", ref.ServiceURL)
	}
	if ref.UserID != "" {
		fmt.Fprintf(&b, "UserID: %s\n", ref.UserID)
	}
	return b.String(), nil
}

func (msteamsAdapter) OutputChannelGuidance() string {
	return `## Microsoft Teams output preferences

Text responses are not delivered to Microsoft Teams. To communicate, call a Microsoft Teams post tool if one is available. If no suitable tool is available, the user will not see your reply — log the situation and stop.

## Deciding whether to respond

Not every Teams activity needs a reply. ALWAYS reply when the message clearly addresses you (an @-mention, a personal chat message, a direct question to you, or a follow-up to your last reply). For ambient channel messages and passive events (reactions, membership changes), first evaluate whether a reply adds value. Stay silent — call no Teams post tool — when the message is clearly directed at another human, when it asks nothing you can help with, or when you would only restate what has already been said. When staying silent, end the turn without posting anything. Never post a message explaining a tool error or announcing your decision to stay silent.`
}

func (msteamsAdapter) DecodeTurn(event assistantThreadEventRecord) (string, error) {
	var payload msteamsEventPayload
	if err := json.Unmarshal(event.NormalizedPayloadJSON, &payload); err != nil {
		return "", fmt.Errorf("decode msteams event payload: %w", err)
	}
	var b bytes.Buffer
	b.WriteString("<message-context>\n")
	fmt.Fprintf(&b, "EventID: %s\n", event.EventID)
	if payload.EventType != "" {
		fmt.Fprintf(&b, "EventType: %s\n", payload.EventType)
	}
	// The output guidance keys "always reply" on personal chats, so the turn
	// must carry the conversation type for the model to apply it.
	if payload.ConversationType != "" {
		fmt.Fprintf(&b, "ConversationType: %s\n", payload.ConversationType)
	}
	if payload.UserID != "" {
		fmt.Fprintf(&b, "UserID: %s\n", payload.UserID)
	}
	if payload.UserName != "" {
		fmt.Fprintf(&b, "UserName: %s\n", payload.UserName)
	}
	if payload.Timestamp != "" {
		fmt.Fprintf(&b, "Timestamp: %s\n", payload.Timestamp)
	}
	if payload.ReplyToID != "" {
		fmt.Fprintf(&b, "ReplyToID: %s\n", payload.ReplyToID)
	}
	if payload.Action != "" {
		fmt.Fprintf(&b, "Action: %s\n", payload.Action)
	}
	if len(payload.ReactionsAdded) > 0 {
		fmt.Fprintf(&b, "ReactionsAdded: %s\n", strings.Join(payload.ReactionsAdded, ", "))
	}
	if len(payload.ReactionsRemoved) > 0 {
		fmt.Fprintf(&b, "ReactionsRemoved: %s\n", strings.Join(payload.ReactionsRemoved, ", "))
	}
	if len(payload.MembersAdded) > 0 {
		fmt.Fprintf(&b, "MembersAdded: %s\n", strings.Join(payload.MembersAdded, ", "))
	}
	if len(payload.MembersRemoved) > 0 {
		fmt.Fprintf(&b, "MembersRemoved: %s\n", strings.Join(payload.MembersRemoved, ", "))
	}
	b.WriteString("</message-context>\n\n")
	b.WriteString(payload.Text)
	return b.String(), nil
}
