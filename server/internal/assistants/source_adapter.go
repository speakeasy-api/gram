package assistants

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type sourceAdapter interface {
	ThreadContext(sourceRefJSON []byte) (string, error)
	OutputChannelGuidance() string
	DecodeTurn(event assistantThreadEventRecord) (string, error)
	// ChatID derives the conversation identity from a turn's correlation key.
	// External sources hash an opaque correlation key into a stable id; the
	// dashboard's correlation key already IS the chat id (server-minted on the
	// first turn and round-tripped by the client).
	ChatID(assistantID uuid.UUID, correlationID string) uuid.UUID
}

// deterministicChatIDAdapter is the default ChatID strategy embedded by every
// source whose correlation key is opaque (Slack, cron, wake). The dashboard
// source overrides ChatID because its correlation key already IS the chat id.
type deterministicChatIDAdapter struct{}

func (deterministicChatIDAdapter) ChatID(assistantID uuid.UUID, correlationID string) uuid.UUID {
	return deterministicChatID(assistantID, correlationID)
}

var sourceAdapters = map[string]sourceAdapter{
	sourceKindSlack:     slackAdapter{deterministicChatIDAdapter: deterministicChatIDAdapter{}},
	sourceKindCron:      cronAdapter{deterministicChatIDAdapter: deterministicChatIDAdapter{}},
	sourceKindWake:      wakeAdapter{deterministicChatIDAdapter: deterministicChatIDAdapter{}},
	sourceKindDashboard: dashboardAdapter{},
}

func getSourceAdapter(kind string) (sourceAdapter, error) {
	adapter, ok := sourceAdapters[kind]
	if !ok {
		return nil, fmt.Errorf("assistant source %q is not supported", kind)
	}
	return adapter, nil
}

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

When relaying an "assistant_mcp_auth_required" AuthURL, prefer platform_slack_post_ephemeral so only the requesting user sees it, and render the AuthURL as a Block Kit actions block containing a single primary button rather than as plain text. If ephemeral isn't available in the current channel, DM the owner instead — but AuthURL expires, so first ask if they're ready to authenticate now and only then re-attempt the tool call to mint a fresh URL for the DM. If neither ephemeral nor DM is reachable, don't post the URL.`
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
	b.WriteString("</message-context>\n\n")
	b.WriteString(payload.Text)
	return b.String(), nil
}

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
	// Stamp the turn's wall-clock so the assistant has temporal grounding for
	// relative-time queries ("errors since Monday"). Per-turn and append-only —
	// it rides on the user message rather than the cached system prompt, so it
	// stays fresh across long sessions without busting the prompt cache. Sourced
	// from the event's immutable created_at so re-decoding on retry/replay is
	// byte-stable (the capture matcher compares stored vs replayed content).
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
