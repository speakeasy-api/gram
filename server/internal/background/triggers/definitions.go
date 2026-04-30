package triggers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
	gjsonschema "github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
	"github.com/robfig/cron"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

	gramjsonschema "github.com/speakeasy-api/gram/server/internal/jsonschema"
	"github.com/speakeasy-api/gram/server/internal/toolconfig"
	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

type Config interface {
	Filter(event any) (bool, error)
}

type Kind string

const (
	KindWebhook  Kind = "webhook"
	KindSchedule Kind = "schedule"
)

type EnvRequirement struct {
	Name        string
	Description string
	Required    bool
}

type Definition struct {
	Slug                 string
	Title                string
	Description          string
	Kind                 Kind
	ConfigSchema         []byte
	CompiledConfigSchema *jsonschema.Schema
	EnvRequirements      []EnvRequirement
	EventType            reflect.Type
	DecodeConfig         func(raw map[string]any) (Config, error)
	AuthenticateWebhook  func(body []byte, headers http.Header, env map[string]string, config Config) error
	HandleWebhook        func(body []byte, headers http.Header, config Config) (*WebhookIngressResult, error)
	BuildScheduledEvent  func(instance triggerrepo.TriggerInstance, config Config, firedAt time.Time) (*EventEnvelope, error)
	ExtractSchedule      func(config Config) (string, error)
}

type WebhookIngressResult struct {
	Response *WebhookResponse
	Event    *EventEnvelope
	Task     *Task
}

type WebhookResponse struct {
	Status      int
	ContentType string
	Body        []byte
}

type EventEnvelope struct {
	EventID           string
	CorrelationID     string
	TriggerInstanceID string
	DefinitionSlug    string
	Event             any
	RawPayload        []byte
	ReceivedAt        time.Time
}

type Task struct {
	TriggerInstanceID string
	DefinitionSlug    string
	TargetKind        string
	TargetRef         string
	TargetDisplay     string
	EventID           string
	CorrelationID     string
	EventJSON         []byte
	RawPayload        []byte
}

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

	// Event-type matching.
	allowed := c.EventTypes
	if len(allowed) == 0 {
		allowed = supportedSlackEventTypes
	}
	if slackEvent.EventType == "" {
		return false, nil
	}
	if !slices.Contains(allowed, slackEvent.EventType) {
		return false, nil
	}

	// CEL filter evaluation.
	if c.compiledFilter == nil {
		return true, nil
	}
	out, _, err := c.compiledFilter.Eval(map[string]any{"event": event})
	if err != nil {
		return false, fmt.Errorf("evaluate filter: %w", err)
	}
	val, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("filter result was %T, want bool", out.Value())
	}
	return val, nil
}

type cronTriggerConfig struct {
	Schedule string `json:"schedule"`
}

func (c cronTriggerConfig) Filter(_ any) (bool, error) { return true, nil }

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
		}, nil
	default:
		var ev slackEventRequestBody
		if err := json.Unmarshal(raw, &ev); err != nil {
			return slackEventRequestBody{}, fmt.Errorf("event: %w", err)
		}
		return ev, nil
	}
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
}

type cronTriggerEvent struct {
	Schedule          string `json:"schedule" cel:"schedule"`
	FiredAt           string `json:"fired_at" cel:"fired_at"`
	TriggerInstanceID string `json:"trigger_instance_id" cel:"trigger_instance_id"`
}

var supportedSlackEventTypes = []string{
	"app_home_opened",
	"app_mention",
	"app_uninstalled",
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

var registry = map[string]Definition{
	"slack": newSlackDefinition(),
	"cron":  newCronDefinition(),
}

func List() []Definition {
	definitions := make([]Definition, 0, len(registry))
	for _, definition := range registry {
		definitions = append(definitions, definition)
	}
	slices.SortFunc(definitions, func(a, b Definition) int {
		return strings.Compare(a.Slug, b.Slug)
	})
	return definitions
}

func GetDefinition(slug string) (Definition, bool) {
	definition, ok := registry[slug]
	return definition, ok
}

func compileCELFilter(eventType reflect.Type, expression string) (cel.Program, error) {
	if strings.TrimSpace(expression) == "" {
		return nil, nil
	}

	env, err := newCELEnv(eventType)
	if err != nil {
		return nil, fmt.Errorf("create CEL env: %w", err)
	}

	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compile filter: %w", issues.Err())
	}
	if ast.OutputType() != cel.BoolType {
		return nil, fmt.Errorf("filter must evaluate to bool")
	}

	prog, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("build filter program: %w", err)
	}

	return prog, nil
}

func newSlackDefinition() Definition {
	schema := buildInputSchema[slackTriggerConfig](
		withArrayItemsEnum("event_types", toAnySlice(supportedSlackEventTypes)...),
	)
	compiled := mustCompileSchema(schema)
	return Definition{
		Slug:                 "slack",
		Title:                "Slack",
		Description:          "Receive Slack Events API callbacks and map them to Gram trigger events.",
		Kind:                 KindWebhook,
		ConfigSchema:         schema,
		CompiledConfigSchema: compiled,
		EnvRequirements: []EnvRequirement{
			{
				Name:        "SLACK_SIGNING_SECRET",
				Description: "Slack signing secret used to verify webhook signatures.",
				Required:    true,
			},
		},
		EventType: reflect.TypeFor[slackTriggerEvent](),
		DecodeConfig: func(raw map[string]any) (Config, error) {
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
		},
		AuthenticateWebhook: func(body []byte, headers http.Header, env map[string]string, config Config) error {
			// Slack's URL verification handshake must echo the challenge before
			// any signing secret has necessarily been configured. Allow it
			// through auth; HandleWebhook will respond with the challenge.
			var probe slackEventRequest
			if err := json.Unmarshal(body, &probe); err == nil && probe.Type == "url_verification" && probe.Challenge != "" {
				return nil
			}
			ciEnv := toolconfig.CIEnvFrom(env)
			signingSecret := ciEnv.Get("SLACK_SIGNING_SECRET")
			if signingSecret == "" {
				return fmt.Errorf("missing SLACK_SIGNING_SECRET")
			}
			if err := validateSlackSignature(body, headers, signingSecret); err != nil {
				return err
			}
			return nil
		},
		HandleWebhook: func(body []byte, headers http.Header, config Config) (*WebhookIngressResult, error) {

			var req slackEventRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, fmt.Errorf("decode slack payload: %w", err)
			}
			if req.Type == "url_verification" && req.Challenge != "" {
				return &WebhookIngressResult{
					Response: &WebhookResponse{
						Status:      http.StatusOK,
						ContentType: "text/plain",
						Body:        []byte(req.Challenge),
					},
					Event: nil,
					Task:  nil,
				}, nil
			}

			if len(req.Event) == 0 {
				return nil, fmt.Errorf("decode slack payload: missing event")
			}
			event, err := decodeSlackEvent(req.Event)
			if err != nil {
				return nil, fmt.Errorf("decode slack payload: %w", err)
			}

			// thread_ts is only set for replies inside a thread. For top-level
			// messages we key the correlation on the channel alone so a user
			// sending multiple standalone messages in a DM or channel lands
			// on a single Gram thread rather than spawning one per message.
			threadID := event.ThreadTs

			eventID := req.EventID
			if eventID == "" {
				eventID = uuid.NewSHA1(uuid.NameSpaceURL, body).String()
			}

			// Reaction events carry the channel + ts of the reacted-to message
			// in `item`. Fall back to those so threading aligns with the
			// originating message and CEL filters / correlation work.
			channelID := event.Channel
			timestamp := event.Ts
			var (
				itemType, itemChannel, itemTs string
			)
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
			}

			correlationID := channelID
			if channelID != "" && threadID != "" {
				correlationID = channelID + ":" + threadID
			}
			if correlationID == "" {
				correlationID = req.TeamID
			}

			return &WebhookIngressResult{
				Response: nil,
				Event: &EventEnvelope{
					EventID:           eventID,
					CorrelationID:     correlationID,
					TriggerInstanceID: "",
					DefinitionSlug:    "slack",
					Event:             normalizedEvent,
					RawPayload:        body,
					ReceivedAt:        time.Now().UTC(),
				},
				Task: nil,
			}, nil
		},
		BuildScheduledEvent: nil,
		ExtractSchedule:     nil,
	}
}

func newCronDefinition() Definition {
	schema := buildInputSchema[cronTriggerConfig]()
	compiled := mustCompileSchema(schema)
	return Definition{
		Slug:                 "cron",
		Title:                "Cron",
		Description:          "Run a trigger on a Temporal-backed cron schedule.",
		Kind:                 KindSchedule,
		ConfigSchema:         schema,
		CompiledConfigSchema: compiled,
		EnvRequirements:      []EnvRequirement{},
		EventType:            reflect.TypeFor[cronTriggerEvent](),
		DecodeConfig: func(raw map[string]any) (Config, error) {
			cfg, err := decodeConfig[cronTriggerConfig](raw, compiled)
			if err != nil {
				return nil, err
			}
			if _, err := cron.ParseStandard(cfg.Schedule); err != nil {
				return nil, fmt.Errorf("parse schedule: %w", err)
			}
			return cfg, nil
		},
		AuthenticateWebhook: nil,
		HandleWebhook:       nil,
		BuildScheduledEvent: func(instance triggerrepo.TriggerInstance, config Config, firedAt time.Time) (*EventEnvelope, error) {
			cfg, ok := config.(cronTriggerConfig)
			if !ok {
				return nil, fmt.Errorf("invalid cron config")
			}
			event := cronTriggerEvent{
				Schedule:          cfg.Schedule,
				FiredAt:           firedAt.UTC().Format(time.RFC3339Nano),
				TriggerInstanceID: instance.ID.String(),
			}
			rawPayload, err := json.Marshal(event)
			if err != nil {
				return nil, fmt.Errorf("marshal cron event: %w", err)
			}
			return &EventEnvelope{
				EventID:           uuid.NewSHA1(uuid.NameSpaceURL, []byte(instance.ID.String()+":"+event.FiredAt)).String(),
				CorrelationID:     instance.ID.String(),
				TriggerInstanceID: instance.ID.String(),
				DefinitionSlug:    instance.DefinitionSlug,
				Event:             event,
				RawPayload:        rawPayload,
				ReceivedAt:        firedAt.UTC(),
			}, nil
		},
		ExtractSchedule: func(config Config) (string, error) {
			cfg, ok := config.(cronTriggerConfig)
			if !ok {
				return "", fmt.Errorf("invalid cron config")
			}
			return cfg.Schedule, nil
		},
	}
}

type inputSchemaConfig struct {
	forOptions       *gjsonschema.ForOptions
	propertyMutators map[string][]func(*gjsonschema.Schema)
}

type inputSchemaOption func(*inputSchemaConfig)

func buildInputSchema[T any](options ...inputSchemaOption) []byte {
	config := &inputSchemaConfig{
		forOptions: &gjsonschema.ForOptions{
			IgnoreInvalidTypes: false,
			TypeSchemas:        map[reflect.Type]*gjsonschema.Schema{},
		},
		propertyMutators: map[string][]func(*gjsonschema.Schema){},
	}

	for _, option := range options {
		option(config)
	}

	schema, err := gjsonschema.For[T](config.forOptions)
	if err != nil {
		panic(fmt.Errorf("build input schema: %w", err))
	}

	for propertyName, mutators := range config.propertyMutators {
		prop := schema.Properties[propertyName]
		if prop == nil {
			continue
		}
		for _, mutate := range mutators {
			mutate(prop)
		}
	}

	bs, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Errorf("marshal schema: %w", err))
	}
	return bs
}

func withArrayItemsEnum(propertyName string, values ...any) inputSchemaOption {
	return func(config *inputSchemaConfig) {
		config.propertyMutators[propertyName] = append(config.propertyMutators[propertyName], func(prop *gjsonschema.Schema) {
			if prop.Items == nil {
				prop.Items = new(gjsonschema.Schema)
			}
			prop.Items.Enum = values
		})
	}
}

func validateSlackSignature(body []byte, headers http.Header, signingSecret string) error {
	timestamp := headers.Get("X-Slack-Request-Timestamp")
	signature := headers.Get("X-Slack-Signature")
	if timestamp == "" || signature == "" {
		return fmt.Errorf("missing slack signature headers")
	}

	seconds, err := parseUnixTimestamp(timestamp)
	if err != nil {
		return fmt.Errorf("parse slack timestamp: %w", err)
	}
	now := time.Now().Unix()
	if absInt64(now-seconds) > 300 {
		return fmt.Errorf("slack timestamp too far from current time")
	}

	base := "v0:" + timestamp + ":" + string(body)
	mac := hmac.New(sha256.New, []byte(signingSecret))
	if _, err := io.WriteString(mac, base); err != nil {
		return fmt.Errorf("hash slack signature: %w", err)
	}
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("slack signature mismatch")
	}
	return nil
}

func parseUnixTimestamp(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse int: %w", err)
	}
	return parsed, nil
}

func absInt64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func newCELEnv(eventType reflect.Type) (*cel.Env, error) {
	if eventType.Kind() == reflect.Pointer {
		eventType = eventType.Elem()
	}

	env, err := cel.NewEnv(
		ext.NativeTypes(eventType, ext.ParseStructTags(true)),
		cel.Variable("event", cel.ObjectType(celTypeName(eventType))),
	)
	if err != nil {
		return nil, fmt.Errorf("create CEL env: %w", err)
	}
	return env, nil
}

func celTypeName(t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return path.Base(t.PkgPath()) + "." + t.Name()
}

func toAnySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func mustCompileSchema(schema []byte) *jsonschema.Schema {
	compiled, err := gramjsonschema.CompileSchema(schema)
	if err != nil {
		panic(fmt.Errorf("compile trigger schema: %w", err))
	}
	return compiled
}

func decodeConfig[T Config](raw map[string]any, schema *jsonschema.Schema) (T, error) {
	var zero T
	if raw == nil {
		raw = map[string]any{}
	}
	if err := gramjsonschema.ValidateAgainstSchema(schema, raw); err != nil {
		return zero, fmt.Errorf("validate config: %w", err)
	}
	bs, err := json.Marshal(raw)
	if err != nil {
		return zero, fmt.Errorf("marshal config: %w", err)
	}
	var cfg T
	if err := json.Unmarshal(bs, &cfg); err != nil {
		return zero, fmt.Errorf("decode config: %w", err)
	}
	return cfg, nil
}
