package assistants

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type slackSourceRef struct {
	TeamID    string `json:"team_id"`
	ChannelID string `json:"channel_id"`
	ThreadID  string `json:"thread_id"`
	UserID    string `json:"user_id,omitempty"`
}

type slackEventPayload struct {
	EnvelopeType string `json:"envelope_type,omitempty"`
	EventType    string `json:"event_type,omitempty"`
	Subtype      string `json:"subtype,omitempty"`
	TeamID       string `json:"team_id"`
	ChannelID    string `json:"channel_id"`
	ThreadID     string `json:"thread_id"`
	UserID       string `json:"user_id,omitempty"`
	InviterID    string `json:"inviter_id,omitempty"`
	BotID        string `json:"bot_id,omitempty"`
	AppID        string `json:"app_id,omitempty"`
	Text         string `json:"text"`
	Timestamp    string `json:"timestamp,omitempty"`
	Reaction     string `json:"reaction,omitempty"`
	ItemUserID   string `json:"item_user_id,omitempty"`
	ItemChannel  string `json:"item_channel,omitempty"`
	ItemTs       string `json:"item_ts,omitempty"`
	ItemType     string `json:"item_type,omitempty"`
	ActionID     string `json:"action_id,omitempty"`
	ActionValue  string `json:"action_value,omitempty"`
	BlockID      string `json:"block_id,omitempty"`

	Files []slackFilePayload `json:"files,omitempty"`
}

// slackFilePayload mirrors the file attachment metadata the slack trigger
// carries on message events (e.g. the file_share subtype).
type slackFilePayload struct {
	ID                 string `json:"id"`
	Name               string `json:"name,omitempty"`
	Title              string `json:"title,omitempty"`
	Mimetype           string `json:"mimetype,omitempty"`
	Size               int64  `json:"size,omitempty"`
	URLPrivateDownload string `json:"url_private_download,omitempty"`
}

type slackAdapter struct{ deterministicChatIDAdapter }

func (slackAdapter) ThreadContext(sourceRefJSON []byte) (string, error) {
	var ref slackSourceRef
	if err := json.Unmarshal(sourceRefJSON, &ref); err != nil {
		return "", fmt.Errorf("decode slack source ref: %w", err)
	}
	var b bytes.Buffer
	b.WriteString("## Conversation context\n\n")
	b.WriteString("Conversation originated on: Slack\n")
	if ref.TeamID != "" {
		fmt.Fprintf(&b, "TeamID: %s\n", ref.TeamID)
	}
	if ref.ChannelID != "" {
		fmt.Fprintf(&b, "ChannelID: %s\n", ref.ChannelID)
	}
	if ref.ThreadID != "" {
		fmt.Fprintf(&b, "ThreadID: %s\n", ref.ThreadID)
	}
	if ref.UserID != "" {
		fmt.Fprintf(&b, "UserID: %s\n", ref.UserID)
	}
	return b.String(), nil
}

func (slackAdapter) OutputChannelGuidance() string {
	return `## Slack output preferences

Text responses are not delivered to the user. To communicate, call a Slack post tool (e.g. platform_slack_post_message, platform_slack_post_ephemeral). If no suitable tool is available, the user will not see your reply.

## Deciding whether to respond

Not every Slack message needs a reply. ALWAYS reply when the turn's EventType is "app_mention" or the message clearly addresses you (a direct question to you, a follow-up to your last reply). For ambient thread messages (EventType "message") and other passive events, first evaluate whether a reply adds value. Stay silent — call no Slack post tool — when the message is clearly directed at another human in the thread, when it asks nothing you can help with, or when you would only restate what has already been said. When staying silent, end the turn without posting anything. Never post a message explaining a tool error or announcing your decision to stay silent.

When relaying an "assistant_mcp_auth_required" AuthURL, deliver it to the OWNER (per the Owner entry in your instructions, when recorded), not whoever triggered this turn. Prefer platform_slack_post_ephemeral targeted at the owner's Slack user ID so only they see it, and render the AuthURL as a Block Kit actions block containing a single primary button rather than as plain text. If ephemeral can't reach the owner in the current channel, DM the owner instead — but AuthURL expires, so first ask if they're ready to authenticate now and only then re-attempt the tool call to mint a fresh URL for the DM. If the requester is someone other than the owner, also tell them (without the URL) that the owner has to complete the authentication. If neither ephemeral nor DM can reach the owner, don't post the URL.`
}

func (slackAdapter) DecodeTurn(event assistantThreadEventRecord) (string, error) {
	var payload slackEventPayload
	if err := json.Unmarshal(event.NormalizedPayloadJSON, &payload); err != nil {
		return "", fmt.Errorf("decode slack event payload: %w", err)
	}
	var b bytes.Buffer
	b.WriteString("<message-context>\n")
	fmt.Fprintf(&b, "EventID: %s\n", event.EventID)
	if payload.EventType != "" {
		fmt.Fprintf(&b, "EventType: %s\n", payload.EventType)
	}
	if payload.Subtype != "" {
		fmt.Fprintf(&b, "Subtype: %s\n", payload.Subtype)
	}
	if payload.UserID != "" {
		fmt.Fprintf(&b, "UserID: %s\n", payload.UserID)
	}
	if payload.InviterID != "" {
		fmt.Fprintf(&b, "InviterID: %s\n", payload.InviterID)
	}
	if payload.Timestamp != "" {
		fmt.Fprintf(&b, "Timestamp: %s\n", payload.Timestamp)
	}
	if payload.Reaction != "" {
		fmt.Fprintf(&b, "Reaction: :%s:\n", payload.Reaction)
	}
	if payload.ItemUserID != "" {
		fmt.Fprintf(&b, "ItemUserID: %s\n", payload.ItemUserID)
	}
	if payload.ItemChannel != "" {
		fmt.Fprintf(&b, "ItemChannel: %s\n", payload.ItemChannel)
	}
	if payload.ItemTs != "" {
		fmt.Fprintf(&b, "ItemTs: %s\n", payload.ItemTs)
	}
	if payload.ItemType != "" {
		fmt.Fprintf(&b, "ItemType: %s\n", payload.ItemType)
	}
	if payload.ActionID != "" {
		fmt.Fprintf(&b, "ActionID: %s\n", payload.ActionID)
	}
	if payload.BlockID != "" {
		fmt.Fprintf(&b, "BlockID: %s\n", payload.BlockID)
	}
	if payload.ActionValue != "" {
		fmt.Fprintf(&b, "ActionValue: %s\n", payload.ActionValue)
	}
	if len(payload.Files) > 0 {
		b.WriteString("Attachments:\n")
		for _, file := range payload.Files {
			name := file.Name
			if name == "" {
				name = file.Title
			}
			fmt.Fprintf(&b, "- id: %s, name: %s, type: %s, size: %s\n", file.ID, name, file.Mimetype, humanFileSize(file.Size))
		}
		b.WriteString("Attachment contents are not directly visible here; pass an attachment's file id to the Slack platform tools to access it.\n")
	}
	b.WriteString("</message-context>\n\n")
	b.WriteString(payload.Text)
	return b.String(), nil
}

// humanFileSize renders a byte count with binary (1024) unit steps, e.g.
// 2048 -> "2.0 KB".
func humanFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}
