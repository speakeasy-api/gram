package assistants

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type sourceAdapter interface {
	ThreadContext(sourceRefJSON []byte) (string, error)
	DecodeTurn(event assistantThreadEventRecord) (string, error)
}

var sourceAdapters = map[string]sourceAdapter{
	sourceKindSlack: slackAdapter{},
	sourceKindCron:  cronAdapter{},
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
}

type slackAdapter struct{}

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
}

type cronAdapter struct{}

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
	return b.String(), nil
}
