package triggers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/google/cel-go/cel"
	"slices"
)

// linearTriggerConfig is the instance config for the Linear trigger. It
// exposes the shared webhook filter knobs — a CEL filter expression and an
// event-type allowlist that narrows the default-deny supportedLinearEventTypes
// set. No vendor-specific configuration is required: the Linear webhook shape
// is fixed, so everything realistically expressible is reachable through CEL
// over the typed event.
type linearTriggerConfig struct {
	FilterExpr string   `json:"filter,omitempty"`
	EventTypes []string `json:"event_types,omitempty"`

	compiledFilter cel.Program
}

func (c linearTriggerConfig) Filter(event any) (bool, error) {
	linearEvent, ok := event.(linearTriggerEvent)
	if !ok {
		return false, fmt.Errorf("expected linearTriggerEvent, got %T", event)
	}
	return evalWebhookFilter(c.compiledFilter, c.EventTypes, event, linearEvent.EventType, supportedLinearEventTypes)
}

// linearWebhookPayload is the envelope Linear delivers. `type` is the entity
// (Issue, Comment, Project, …) and `action` is the verb (create, update,
// delete, …); the two together form the normalized event type surfaced to
// filters and the assistant. `data` carries the entity snapshot and `url` the
// human-facing permalink. See https://developers.linear.app/docs/webhooks.
type linearWebhookPayload struct {
	Type      string          `json:"type"`
	Action    string          `json:"action"`
	Data      json.RawMessage `json:"data"`
	URL       string          `json:"url,omitempty"`
	CreatedAt string          `json:"createdAt,omitempty"`
	// WebhookTimestamp is the unix-millis send time Linear stamps into the
	// signed body; bounding its age guards against replay (see linearIngest).
	WebhookTimestamp int64 `json:"webhookTimestamp,omitempty"`
}

// linearWebhookMaxAge bounds how stale a Linear webhook may be. Linear signs
// only the raw body, so the Linear-Delivery header (the dedup id) can be
// mutated on a replay of an otherwise-valid signed body to dodge dedup.
// webhookTimestamp lives in the signed body and cannot, so bounding its age
// caps the replay window. The window is generous relative to Linear's ~1 min
// recommendation to tolerate clock skew. See https://linear.app/developers/webhooks.
const linearWebhookMaxAge = 5 * time.Minute

// linearEntityData is the subset of the `data` object common across Linear
// entities that the trigger cares about: an `id` to correlate on, and an
// `issueId` for comments so they fold onto the parent issue's conversation.
// Linear includes `id` on every entity; comments additionally carry `issueId`.
type linearEntityData struct {
	ID      string `json:"id,omitempty"`
	IssueID string `json:"issueId,omitempty"`
}

// linearTriggerEvent is the normalized event surfaced to CEL filters and
// downstream consumers. EventType is `<Type>.<action>` (e.g. `Issue.create`)
// so a single allowlist + CEL expression can dispatch on either dimension.
type linearTriggerEvent struct {
	EventType  string          `json:"event_type" cel:"event_type"`
	Type       string          `json:"type" cel:"type"`
	Action     string          `json:"action" cel:"action"`
	URL        string          `json:"url,omitempty" cel:"url"`
	Data       json.RawMessage `json:"data,omitempty" cel:"data"`
	ReceivedAt string          `json:"received_at,omitempty" cel:"received_at"`
}

// supportedLinearEventTypes is the default-deny allowlist of `<Type>.<action>`
// event types the Linear trigger accepts when an instance does not narrow
// `event_types`. It enumerates every data-change resource type Linear delivers,
// each with the only actions Linear sends for them — `create`, `update`, and
// `remove` (archiving surfaces as an `update` with `archivedAt` set, not a
// distinct action — see https://linear.app/developers/webhooks). Special-shaped
// event webhooks (IssueSLA, OAuthAppRevoked) follow different payload shapes
// and aren't decoded by this data-change ingest. CEL filters narrow further.
var supportedLinearEventTypes = []string{
	"Comment.create",
	"Comment.update",
	"Comment.remove",
	"Customer.create",
	"Customer.update",
	"Customer.remove",
	"CustomerRequest.create",
	"CustomerRequest.update",
	"CustomerRequest.remove",
	"Cycle.create",
	"Cycle.update",
	"Cycle.remove",
	"Document.create",
	"Document.update",
	"Document.remove",
	"Initiative.create",
	"Initiative.update",
	"Initiative.remove",
	"InitiativeUpdate.create",
	"InitiativeUpdate.update",
	"InitiativeUpdate.remove",
	"Issue.create",
	"Issue.update",
	"Issue.remove",
	"IssueAttachment.create",
	"IssueAttachment.update",
	"IssueAttachment.remove",
	"IssueLabel.create",
	"IssueLabel.update",
	"IssueLabel.remove",
	"Project.create",
	"Project.update",
	"Project.remove",
	"ProjectUpdate.create",
	"ProjectUpdate.update",
	"ProjectUpdate.remove",
	"Reaction.create",
	"Reaction.update",
	"Reaction.remove",
	"User.create",
	"User.update",
	"User.remove",
}

// linearSigningSecretEnv is the environment variable holding the Linear
// webhook signing secret. Declared as a constant so the name is referenced
// rather than inlined — gosec's G101 flags inline string literals containing
// "SECRET" as potential hardcoded credentials.
const linearSigningSecretEnv = "LINEAR_SIGNING_SECRET"

func newLinearDefinition() Definition {
	schema := buildInputSchema[linearTriggerConfig](
		withArrayItemsEnum("event_types", toAnySlice(supportedLinearEventTypes)...),
	)
	compiled := mustCompileSchema(schema)
	vendor := WebhookVendor{
		Slug:            DefinitionSlugLinear,
		Title:           "Linear",
		Description:     "Receive Linear webhooks and map them to Gram trigger events.",
		EventType:       reflect.TypeFor[linearTriggerEvent](),
		EnvRequirements: []EnvRequirement{{Name: linearSigningSecretEnv, Description: "Linear webhook signing secret.", Required: true}},
		SecretEnv:       linearSigningSecretEnv,
		Signature: HMACScheme{
			NewHash:         func(key []byte) hash.Hash { return hmac.New(sha256.New, key) },
			Header:          "Linear-Signature",
			Encoding:        "hex",
			Prefix:          "",
			Template:        "{body}",
			TimestampHeader: "",
			TimestampSkew:   0,
		},
		SupportedEventTypes: supportedLinearEventTypes,
		PreVerify:           nil,
		Ingest:              linearIngest,
	}
	return NewWebhookDefinition(vendor, schema, compiled, func(raw map[string]any) (Config, error) {
		cfg, err := decodeConfig[linearTriggerConfig](raw, compiled)
		if err != nil {
			return nil, err
		}
		for _, eventType := range cfg.EventTypes {
			if !slices.Contains(supportedLinearEventTypes, eventType) {
				return nil, fmt.Errorf("unsupported linear event type %q", eventType)
			}
		}
		prog, err := compileCELFilter(reflect.TypeFor[linearTriggerEvent](), cfg.FilterExpr)
		if err != nil {
			return nil, err
		}
		cfg.compiledFilter = prog
		return cfg, nil
	})
}

func linearIngest(body []byte, headers http.Header) (*WebhookIngest, error) {
	var payload linearWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode linear payload: %w", err)
	}
	if payload.Type == "" {
		return nil, fmt.Errorf("decode linear payload: missing type")
	}

	// Reject stale deliveries. webhookTimestamp is part of the signed body, so
	// (unlike the Linear-Delivery header used as the dedup id below) it can't be
	// tampered with on a replay; bounding its age caps the replay window.
	if payload.WebhookTimestamp != 0 {
		age := time.Now().UnixMilli() - payload.WebhookTimestamp
		if age < 0 {
			age = -age
		}
		if age > linearWebhookMaxAge.Milliseconds() {
			return nil, fmt.Errorf("linear webhook timestamp outside freshness window")
		}
	}

	eventType := payload.Type
	if payload.Action != "" {
		eventType = payload.Type + "." + payload.Action
	}

	var entity linearEntityData
	if len(payload.Data) > 0 {
		_ = json.Unmarshal(payload.Data, &entity)
	}

	// Linear-Delivery is the per-delivery id Linear sends for dedup; fall back
	// to a content hash when absent so redeliveries of the same body collapse.
	eventID := headers.Get("Linear-Delivery")

	correlationID := linearCorrelationID(payload.Type, entity)

	return &WebhookIngest{
		Response:      nil,
		EventID:       eventID,
		CorrelationID: correlationID,
		Event: linearTriggerEvent{
			EventType:  eventType,
			Type:       payload.Type,
			Action:     payload.Action,
			URL:        payload.URL,
			Data:       payload.Data,
			ReceivedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
	}, nil
}

// linearCorrelationID routes each Linear event to the assistant conversation
// that should own it. The natural unit is the Issue: comments and reactions
// fold onto their parent issue so the assistant sees the full issue thread as
// one context. Other entities (Project, Cycle, Document, …) are each their
// own conversation keyed by their entity id. Events without a usable id fall
// back to the human-facing URL, then to the event type so unrelated events
// don't all collapse onto a single conversation.
func linearCorrelationID(entityType string, entity linearEntityData) string {
	switch entityType {
	case "Comment":
		if entity.IssueID != "" {
			return "linear:issue:" + entity.IssueID
		}
	case "Reaction":
		if entity.IssueID != "" {
			return "linear:issue:" + entity.IssueID
		}
	}
	if entity.ID != "" {
		return "linear:" + strings.ToLower(entityType) + ":" + entity.ID
	}
	return "linear:" + strings.ToLower(entityType)
}
