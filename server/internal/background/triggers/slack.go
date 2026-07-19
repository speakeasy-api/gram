package triggers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"slices"
)

// slackTriggerConfig is the instance config for the Slack trigger. It exposes
// the shared webhook filter knobs — a CEL filter expression and an event-type
// allowlist that narrows the default-deny supportedSlackEventTypes set.
type slackTriggerConfig struct {
	FilterExpr string   `json:"filter,omitempty"`
	EventTypes []string `json:"event_types,omitempty"`

	// compiledFilter is set during DecodeConfig when FilterExpr is non-empty.
	compiledFilter cel.Program
}

func (c slackTriggerConfig) Filter(event any) (bool, error) {
	slackEvent, ok := event.(slackTriggerEvent)
	if !ok {
		return false, fmt.Errorf("expected slackTriggerEvent, got %T", event)
	}
	return evalWebhookFilter(c.compiledFilter, c.EventTypes, event, slackEvent.EventType, supportedSlackEventTypes)
}

type slackEventRequest struct {
	Type      string          `json:"type"`
	Challenge string          `json:"challenge,omitempty"`
	TeamID    string          `json:"team_id,omitempty"`
	EventID   string          `json:"event_id,omitempty"`
	EventTime int64           `json:"event_time,omitempty"`
	Event     json.RawMessage `json:"event,omitempty"`
}

// slackEventRequestBody is the normalized intermediate shape produced by
// decodeSlackEvent. JSON tags also let it deserialize directly from the
// "string user / string channel" majority of Slack events; per-event-type
// branches in decodeSlackEvent handle the polymorphic shapes.
type slackEventRequestBody struct {
	Type     string `json:"type"`
	Subtype  string `json:"subtype,omitempty"`
	Text     string `json:"text,omitempty"`
	User     string `json:"user,omitempty"`
	Inviter  string `json:"inviter,omitempty"`
	BotID    string `json:"bot_id,omitempty"`
	AppID    string `json:"app_id,omitempty"`
	Channel  string `json:"channel,omitempty"`
	ThreadTs string `json:"thread_ts,omitempty"`
	Ts       string `json:"ts,omitempty"`

	// Reaction-event fields. Slack puts the channel + ts of the reacted-to
	// message inside `item`, not on the event body itself, so reaction_added
	// / reaction_removed arrive with empty top-level Channel/Ts.
	Reaction string              `json:"reaction,omitempty"`
	ItemUser string              `json:"item_user,omitempty"`
	Item     *slackEventItemBody `json:"item,omitempty"`

	// Files carries the attachments of a message event (e.g. the file_share
	// subtype). See https://docs.slack.dev/reference/events/message/file_share.
	Files []slackEventFile `json:"files,omitempty"`
}

// slackEventFile models the subset of Slack's file object
// (https://docs.slack.dev/reference/objects/file-object) surfaced downstream.
// Metadata only — file contents are never fetched here.
type slackEventFile struct {
	ID                 string `json:"id" cel:"id"`
	Name               string `json:"name,omitempty" cel:"name"`
	Title              string `json:"title,omitempty" cel:"title"`
	Mimetype           string `json:"mimetype,omitempty" cel:"mimetype"`
	Size               int64  `json:"size,omitempty" cel:"size"`
	URLPrivateDownload string `json:"url_private_download,omitempty" cel:"url_private_download"`
}

// slackUserChangeEventBody matches the team_join and user_change payloads,
// where Slack sends event.user as a User object rather than a user ID.
// See https://docs.slack.dev/reference/events/team_join and
// https://docs.slack.dev/reference/events/user_change.
type slackUserChangeEventBody struct {
	Type string    `json:"type"`
	User slackUser `json:"user"`
}

// slackUser models the subset of Slack's User object
// (https://docs.slack.dev/reference/objects/user-object) that we surface
// downstream.
type slackUser struct {
	ID string `json:"id"`
}

// slackChannelObjectEventBody matches channel_created, channel_rename, and
// group_rename, where Slack sends event.channel as a channel object rather
// than a channel ID. See https://docs.slack.dev/reference/events/channel_created,
// https://docs.slack.dev/reference/events/channel_rename, and
// https://docs.slack.dev/reference/events/group_rename.
type slackChannelObjectEventBody struct {
	Type    string             `json:"type"`
	Channel slackChannelObject `json:"channel"`
}

type slackChannelObject struct {
	ID      string `json:"id"`
	Creator string `json:"creator,omitempty"`
}

// slackFileSharedEventBody matches file_shared, where the actor is carried in
// `event.user_id` (not `event.user`) and the channel in `event.channel_id`.
// See https://docs.slack.dev/reference/events/file_shared.
type slackFileSharedEventBody struct {
	Type      string `json:"type"`
	UserID    string `json:"user_id,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
}

// slackChannelIDChangedEventBody matches channel_id_changed, which carries
// new_channel_id instead of an `event.channel` field.
// See https://docs.slack.dev/reference/events/channel_id_changed.
type slackChannelIDChangedEventBody struct {
	Type         string `json:"type"`
	OldChannelID string `json:"old_channel_id,omitempty"`
	NewChannelID string `json:"new_channel_id,omitempty"`
}

type slackEventItemBody struct {
	Type    string `json:"type,omitempty"`
	Channel string `json:"channel,omitempty"`
	Ts      string `json:"ts,omitempty"`
}

// slackLinkSharedEventBody matches link_shared, where the message timestamp
// is carried in `event.message_ts` (not `event.ts`) alongside the shared
// links and the unfurl handle used by chat.unfurl.
// See https://docs.slack.dev/reference/events/link_shared.
type slackLinkSharedEventBody struct {
	Type      string                `json:"type"`
	User      string                `json:"user,omitempty"`
	Channel   string                `json:"channel,omitempty"`
	MessageTs string                `json:"message_ts,omitempty"`
	ThreadTs  string                `json:"thread_ts,omitempty"`
	Links     []slackSharedLinkBody `json:"links,omitempty"`
	UnfurlID  string                `json:"unfurl_id,omitempty"`
	Source    string                `json:"source,omitempty"`
}

type slackSharedLinkBody struct {
	Domain string `json:"domain,omitempty"`
	URL    string `json:"url,omitempty"`
}

// decodeSlackEvent decodes the inner `event` payload of an event_callback,
// dispatching by event type because Slack's `event.user` and `event.channel`
// are sometimes objects (e.g. team_join, channel_rename) and sometimes string
// IDs (e.g. message, app_mention). Each branch normalizes the payload back
// into slackEventRequestBody for downstream code.
func decodeSlackEvent(raw json.RawMessage) (slackEventRequestBody, error) {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return slackEventRequestBody{}, fmt.Errorf("event type: %w", err)
	}
	switch probe.Type {
	case "team_join", "user_change":
		var ev slackUserChangeEventBody
		if err := json.Unmarshal(raw, &ev); err != nil {
			return slackEventRequestBody{}, fmt.Errorf("%s event: %w", probe.Type, err)
		}
		return slackEventRequestBody{
			Type:     ev.Type,
			Subtype:  "",
			Text:     "",
			User:     ev.User.ID,
			Inviter:  "",
			BotID:    "",
			AppID:    "",
			Channel:  "",
			ThreadTs: "",
			Ts:       "",
			Reaction: "",
			ItemUser: "",
			Item:     nil,
			Files:    nil,
		}, nil
	case "channel_created":
		var ev slackChannelObjectEventBody
		if err := json.Unmarshal(raw, &ev); err != nil {
			return slackEventRequestBody{}, fmt.Errorf("%s event: %w", probe.Type, err)
		}
		// channel_created carries the actor inside channel.creator; surface it
		// as the normalized actor user so downstream consumers see a unified shape.
		return slackEventRequestBody{
			Type:     ev.Type,
			Subtype:  "",
			Text:     "",
			User:     ev.Channel.Creator,
			Inviter:  "",
			BotID:    "",
			AppID:    "",
			Channel:  ev.Channel.ID,
			ThreadTs: "",
			Ts:       "",
			Reaction: "",
			ItemUser: "",
			Item:     nil,
			Files:    nil,
		}, nil
	case "channel_rename", "group_rename":
		var ev slackChannelObjectEventBody
		if err := json.Unmarshal(raw, &ev); err != nil {
			return slackEventRequestBody{}, fmt.Errorf("%s event: %w", probe.Type, err)
		}
		return slackEventRequestBody{
			Type:     ev.Type,
			Subtype:  "",
			Text:     "",
			User:     "",
			Inviter:  "",
			BotID:    "",
			AppID:    "",
			Channel:  ev.Channel.ID,
			ThreadTs: "",
			Ts:       "",
			Reaction: "",
			ItemUser: "",
			Item:     nil,
			Files:    nil,
		}, nil
	case "channel_id_changed":
		var ev slackChannelIDChangedEventBody
		if err := json.Unmarshal(raw, &ev); err != nil {
			return slackEventRequestBody{}, fmt.Errorf("%s event: %w", probe.Type, err)
		}
		return slackEventRequestBody{
			Type:     ev.Type,
			Subtype:  "",
			Text:     "",
			User:     "",
			Inviter:  "",
			BotID:    "",
			AppID:    "",
			Channel:  ev.NewChannelID,
			ThreadTs: "",
			Ts:       "",
			Reaction: "",
			ItemUser: "",
			Item:     nil,
			Files:    nil,
		}, nil
	case "link_shared":
		var ev slackLinkSharedEventBody
		if err := json.Unmarshal(raw, &ev); err != nil {
			return slackEventRequestBody{}, fmt.Errorf("%s event: %w", probe.Type, err)
		}
		return slackEventRequestBody{
			Type:     ev.Type,
			Subtype:  "",
			Text:     "",
			User:     ev.User,
			Inviter:  "",
			BotID:    "",
			AppID:    "",
			Channel:  ev.Channel,
			ThreadTs: ev.ThreadTs,
			Ts:       ev.MessageTs,
			Reaction: "",
			ItemUser: "",
			Item:     nil,
			Files:    nil,
		}, nil
	case "file_shared":
		var ev slackFileSharedEventBody
		if err := json.Unmarshal(raw, &ev); err != nil {
			return slackEventRequestBody{}, fmt.Errorf("%s event: %w", probe.Type, err)
		}
		return slackEventRequestBody{
			Type:     ev.Type,
			Subtype:  "",
			Text:     "",
			User:     ev.UserID,
			Inviter:  "",
			BotID:    "",
			AppID:    "",
			Channel:  ev.ChannelID,
			ThreadTs: "",
			Ts:       "",
			Reaction: "",
			ItemUser: "",
			Item:     nil,
			Files:    nil,
		}, nil
	default:
		var ev slackEventRequestBody
		if err := json.Unmarshal(raw, &ev); err != nil {
			return slackEventRequestBody{}, fmt.Errorf("event: %w", err)
		}
		return ev, nil
	}
}

// slackCorrelationID derives the assistant-thread correlation key from a
// Slack event. Top-level messages (empty thread_ts) fall back to the event's
// own ts so each top-level message maps 1:1 to a Gram thread — Slack itself
// promotes a top-level ts into thread_ts the moment anyone replies, so this
// mirrors Slack's threading semantic. Channel-less events (e.g. team_join)
// fall back to the workspace ID.
func slackCorrelationID(channelID, threadID, fallbackTs, teamID string) string {
	if threadID == "" {
		threadID = fallbackTs
	}
	if channelID != "" && threadID != "" {
		return channelID + ":" + threadID
	}
	if channelID != "" {
		return channelID
	}
	return teamID
}

// slackInteractionPayload is the JSON body of a Block Kit interaction
// envelope (e.g. block_actions). Slack form-encodes this under the "payload"
// field of the webhook request body. We only model the fields needed to
// route the click back to the originating thread + surface action metadata.
// See https://docs.slack.dev/interactivity/handling-user-interaction.
type slackInteractionPayload struct {
	Type     string `json:"type"`
	APIAppID string `json:"api_app_id,omitempty"`
	Team     struct {
		ID string `json:"id"`
	} `json:"team"`
	User struct {
		ID string `json:"id"`
	} `json:"user"`
	Channel struct {
		ID string `json:"id"`
	} `json:"channel"`
	Message struct {
		Ts       string `json:"ts"`
		ThreadTs string `json:"thread_ts,omitempty"`
	} `json:"message"`
	Container struct {
		ChannelID string `json:"channel_id,omitempty"`
		ThreadTs  string `json:"thread_ts,omitempty"`
		MessageTs string `json:"message_ts,omitempty"`
	} `json:"container"`
	Actions []struct {
		ActionID string `json:"action_id"`
		BlockID  string `json:"block_id"`
		Value    string `json:"value,omitempty"`
		Type     string `json:"type,omitempty"`
	} `json:"actions"`
}

func isSlackInteractionRequest(headers http.Header) bool {
	// Events API requests are application/json; interactivity requests are
	// always application/x-www-form-urlencoded with a single `payload` field.
	// HasPrefix tolerates an optional ;charset= parameter without paying for
	// mime.ParseMediaType on every webhook.
	contentType := strings.ToLower(headers.Get("Content-Type"))
	return strings.HasPrefix(contentType, "application/x-www-form-urlencoded")
}

func handleSlackInteraction(body []byte) (*WebhookIngest, error) {
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, fmt.Errorf("decode slack interaction body: %w", err)
	}
	rawPayload := values.Get("payload")
	if rawPayload == "" {
		return nil, fmt.Errorf("decode slack interaction body: missing payload")
	}
	var payload slackInteractionPayload
	if err := json.Unmarshal([]byte(rawPayload), &payload); err != nil {
		return nil, fmt.Errorf("decode slack interaction payload: %w", err)
	}
	if payload.Type != "block_actions" {
		// Other interaction envelopes (view_submission, shortcut, etc.) are
		// not wired through Gram triggers yet. Ack without dispatching so
		// Slack stops retrying.
		return &WebhookIngest{Response: nil, Event: nil, EventID: "", CorrelationID: ""}, nil
	}
	if len(payload.Actions) == 0 {
		return nil, fmt.Errorf("decode slack interaction payload: no actions")
	}

	// Block Kit button clicks deliver exactly one entry in actions[].
	// Multi-select / checkbox payloads fan out across entries; we only
	// surface the first because button is the only interactive element
	// the outbound model exposes today.
	action := payload.Actions[0]

	channelID := payload.Channel.ID
	if channelID == "" {
		channelID = payload.Container.ChannelID
	}
	threadID := payload.Message.ThreadTs
	if threadID == "" {
		threadID = payload.Container.ThreadTs
	}
	messageTs := payload.Message.Ts
	if messageTs == "" {
		messageTs = payload.Container.MessageTs
	}

	normalized := slackTriggerEvent{
		EnvelopeType: "interactive",
		EventType:    payload.Type,
		Subtype:      "",
		TeamID:       payload.Team.ID,
		ChannelID:    channelID,
		ThreadID:     threadID,
		UserID:       payload.User.ID,
		InviterID:    "",
		BotID:        "",
		AppID:        payload.APIAppID,
		Text:         action.Value,
		Timestamp:    messageTs,
		Reaction:     "",
		ItemUserID:   "",
		ItemChannel:  "",
		ItemTs:       "",
		ItemType:     "",
		ActionID:     action.ActionID,
		ActionValue:  action.Value,
		BlockID:      action.BlockID,
		Files:        nil,
	}

	// A block_actions interaction anchored to a channel message folds onto that
	// message's conversation. An anchorless one (e.g. an App Home or modal
	// button, which carries no channel) would otherwise fall back to a single
	// team-level correlation id and mix every user's unrelated clicks into one
	// conversation, so scope those to the acting user instead.
	correlationID := slackCorrelationID(channelID, threadID, messageTs, payload.Team.ID)
	if channelID == "" {
		correlationID = "slack:" + payload.Team.ID + ":user:" + payload.User.ID
	}

	// Slack interaction envelopes carry no event_id; leave it empty so the
	// dispatcher derives an instance-scoped content-hash fallback. A bare body
	// hash here would be identical across trigger instances and collide on the
	// downstream dedup keys.
	return &WebhookIngest{
		Response:      nil,
		EventID:       "",
		CorrelationID: correlationID,
		Event:         normalized,
	}, nil
}

type slackTriggerEvent struct {
	EnvelopeType string `json:"envelope_type" cel:"envelope_type"`
	EventType    string `json:"event_type" cel:"event_type"`
	Subtype      string `json:"subtype,omitempty" cel:"subtype"`
	TeamID       string `json:"team_id,omitempty" cel:"team_id"`
	ChannelID    string `json:"channel_id,omitempty" cel:"channel_id"`
	ThreadID     string `json:"thread_id,omitempty" cel:"thread_id"`
	// UserID is the normalized actor for the event: the user who took the
	// action that produced it. Source varies by event type (event.user,
	// event.user.id, event.user_id, event.channel.creator).
	UserID string `json:"user_id,omitempty" cel:"user_id"`
	// InviterID is set on member_joined_channel when the join was the result
	// of an invitation. Empty if the user joined themselves or was added by
	// default channel rules.
	InviterID string `json:"inviter_id,omitempty" cel:"inviter_id"`
	BotID     string `json:"bot_id,omitempty" cel:"bot_id"`
	AppID     string `json:"app_id,omitempty" cel:"app_id"`
	Text      string `json:"text,omitempty" cel:"text"`
	Timestamp string `json:"timestamp,omitempty" cel:"timestamp"`

	// Reaction-event fields exposed to CEL filters and the assistant adapter.
	// Empty for non-reaction events.
	Reaction    string `json:"reaction,omitempty" cel:"reaction"`
	ItemUserID  string `json:"item_user_id,omitempty" cel:"item_user_id"`
	ItemChannel string `json:"item_channel,omitempty" cel:"item_channel"`
	ItemTs      string `json:"item_ts,omitempty" cel:"item_ts"`
	ItemType    string `json:"item_type,omitempty" cel:"item_type"`

	// Block Kit interaction fields, set when EnvelopeType is "interactive"
	// and EventType is "block_actions". Empty for events-API callbacks.
	ActionID    string `json:"action_id,omitempty" cel:"action_id"`
	ActionValue string `json:"action_value,omitempty" cel:"action_value"`
	BlockID     string `json:"block_id,omitempty" cel:"block_id"`

	// Files carries attachment metadata for message events that share files
	// (e.g. the file_share subtype). Empty for all other events.
	Files []slackEventFile `json:"files,omitempty" cel:"files"`
}

var supportedSlackEventTypes = []string{
	"app_home_opened",
	"app_mention",
	"app_uninstalled",
	// Interactivity envelope, not an Events API event. Delivered to the same
	// webhook by Slack when a user clicks a Block Kit button. See
	// https://docs.slack.dev/interactivity/handling-user-interaction.
	"block_actions",
	"channel_archive",
	"channel_created",
	"channel_deleted",
	"channel_id_changed",
	"channel_left",
	"channel_rename",
	"channel_unarchive",
	"emoji_changed",
	"file_change",
	"file_created",
	"file_deleted",
	"file_public",
	"file_shared",
	"file_unshared",
	"group_archive",
	"group_deleted",
	"group_left",
	"group_rename",
	"group_unarchive",
	"link_shared",
	"member_joined_channel",
	"member_left_channel",
	"message",
	"pin_added",
	"pin_removed",
	"reaction_added",
	"reaction_removed",
	"team_join",
	"tokens_revoked",
	"user_change",
}

// slackEventExpectsReply reports whether a slack event is one the assistant
// always replies to: an explicit @-mention, a DM (channel IDs start with "D"),
// or a Block Kit interaction. Ambient channel events (plain messages,
// reactions, joins) may legitimately end in a silent turn, so the ingress
// loading indicator is suppressed for them — a silent turn would otherwise
// strand the indicator until Slack's two-minute timeout.
func slackEventExpectsReply(event EventEnvelope) bool {
	evt, isSlack := event.Event.(slackTriggerEvent)
	if !isSlack {
		return false
	}
	return evt.EventType == "app_mention" ||
		evt.EnvelopeType == "interactive" ||
		strings.HasPrefix(evt.ChannelID, "D")
}

// slackThreadStatusTarget extracts the channel + thread to anchor a loading
// status on. Returns ok=false for events without a threadable target (e.g.
// user_change, channel_created).
func slackThreadStatusTarget(event EventEnvelope) (channelID, threadTS string, ok bool) {
	evt, isSlack := event.Event.(slackTriggerEvent)
	if !isSlack {
		return "", "", false
	}
	threadTS = evt.ThreadID
	if threadTS == "" {
		threadTS = evt.Timestamp
	}
	if evt.ChannelID == "" || threadTS == "" {
		return "", "", false
	}
	return evt.ChannelID, threadTS, true
}

// slackThinkingStatus and slackInitialLoadingMessages drive the loading
// indicator shown the moment a slack-triggered event is dispatched, before
// the assistant runtime is up to refine it via the set_thread_status platform
// tool.
const slackThinkingStatus = "is thinking…"

var slackInitialLoadingMessages = []string{"Routing…"}

// slackSigningSecretEnv is the environment variable holding the Slack
// signing secret. Declared as a constant so the name is referenced rather
// than inlined — gosec's G101 flags inline string literals containing
// "SECRET" as potential hardcoded credentials.
const slackSigningSecretEnv = "SLACK_SIGNING_SECRET" //nolint:gosec // env var name, not a credential

func newSlackDefinition() Definition {
	schema := buildInputSchema[slackTriggerConfig](
		withArrayItemsEnum("event_types", toAnySlice(supportedSlackEventTypes)...),
	)
	compiled := mustCompileSchema(schema)
	vendor := WebhookVendor{
		Slug:            DefinitionSlugSlack,
		Title:           "Slack",
		Description:     "Receive Slack Events API callbacks and map them to Gram trigger events.",
		EventType:       reflect.TypeFor[slackTriggerEvent](),
		EnvRequirements: []EnvRequirement{{Name: slackSigningSecretEnv, Description: "Slack signing secret used to verify webhook signatures.", Required: true}},
		SecretEnv:       slackSigningSecretEnv,
		Signature: HMACScheme{
			NewHash:         func(key []byte) hash.Hash { return hmac.New(sha256.New, key) },
			Header:          "X-Slack-Signature",
			Encoding:        "hex",
			Prefix:          "v0=",
			Template:        "v0:{timestamp}:{body}",
			TimestampHeader: "X-Slack-Request-Timestamp",
			TimestampSkew:   300 * time.Second,
		},
		SupportedEventTypes: supportedSlackEventTypes,
		PreVerify: func(body []byte, _ http.Header) (bool, error) {
			// Slack's URL verification handshake must echo the challenge before
			// any signing secret has necessarily been configured. Allow it
			// through auth; Ingest will respond with the challenge.
			var probe slackEventRequest
			if err := json.Unmarshal(body, &probe); err == nil && probe.Type == "url_verification" && probe.Challenge != "" {
				return true, nil
			}
			return false, nil
		},
		Ingest: slackIngest,
	}
	return NewWebhookDefinition(vendor, schema, compiled, func(raw map[string]any) (Config, error) {
		cfg, err := decodeConfig[slackTriggerConfig](raw, compiled)
		if err != nil {
			return nil, err
		}
		for _, eventType := range cfg.EventTypes {
			if !slices.Contains(supportedSlackEventTypes, eventType) {
				return nil, fmt.Errorf("unsupported slack event type %q", eventType)
			}
		}
		prog, err := compileCELFilter(reflect.TypeFor[slackTriggerEvent](), cfg.FilterExpr)
		if err != nil {
			return nil, err
		}
		cfg.compiledFilter = prog
		return cfg, nil
	})
}

func slackIngest(body []byte, headers http.Header) (*WebhookIngest, error) {
	if isSlackInteractionRequest(headers) {
		return handleSlackInteraction(body)
	}

	var req slackEventRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("decode slack payload: %w", err)
	}
	if req.Type == "url_verification" && req.Challenge != "" {
		return &WebhookIngest{
			Response: &WebhookResponse{
				Status:      200,
				ContentType: "text/plain",
				Body:        []byte(req.Challenge),
			},
			Event:         nil,
			EventID:       "",
			CorrelationID: "",
		}, nil
	}

	if len(req.Event) == 0 {
		return nil, fmt.Errorf("decode slack payload: missing event")
	}
	event, err := decodeSlackEvent(req.Event)
	if err != nil {
		return nil, fmt.Errorf("decode slack payload: %w", err)
	}

	threadID := event.ThreadTs

	eventID := req.EventID

	// Reaction events carry the channel + ts of the reacted-to message
	// in `item`. Fall back to those so threading aligns with the
	// originating message and CEL filters / correlation work.
	channelID := event.Channel
	timestamp := event.Ts
	var itemType, itemChannel, itemTs string
	if event.Item != nil {
		itemType = event.Item.Type
		itemChannel = event.Item.Channel
		itemTs = event.Item.Ts
		if channelID == "" {
			channelID = itemChannel
		}
		if threadID == "" {
			threadID = itemTs
		}
		if timestamp == "" {
			timestamp = itemTs
		}
	}

	normalizedEvent := slackTriggerEvent{
		EnvelopeType: req.Type,
		EventType:    event.Type,
		Subtype:      event.Subtype,
		TeamID:       req.TeamID,
		ChannelID:    channelID,
		ThreadID:     threadID,
		UserID:       event.User,
		InviterID:    event.Inviter,
		BotID:        event.BotID,
		AppID:        event.AppID,
		Text:         event.Text,
		Timestamp:    timestamp,
		Reaction:     event.Reaction,
		ItemUserID:   event.ItemUser,
		ItemChannel:  itemChannel,
		ItemTs:       itemTs,
		ItemType:     itemType,
		ActionID:     "",
		ActionValue:  "",
		BlockID:      "",
		Files:        event.Files,
	}

	return &WebhookIngest{
		Response:      nil,
		EventID:       eventID,
		CorrelationID: slackCorrelationID(channelID, threadID, timestamp, req.TeamID),
		Event:         normalizedEvent,
	}, nil
}
