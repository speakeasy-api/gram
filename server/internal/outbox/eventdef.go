package outbox

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/google/uuid"
)

// defaultTypeSchemas maps commonly-used Go types to semantically correct JSON
// Schema representations that jsonschema-go cannot infer from struct layout alone
// (custom JSON marshalers, type aliases, nullable wrappers, etc.).
var defaultTypeSchemas = map[reflect.Type]*jsonschema.Schema{
	// uuid.UUID marshals to a hyphenated UUID string, not a 16-byte array.
	reflect.TypeFor[uuid.UUID](): {Type: "string", Format: "uuid"},

	// uuid.NullUUID marshals to a UUID string or JSON null.
	reflect.TypeFor[uuid.NullUUID](): {Types: []string{"string", "null"}, Format: "uuid"},

	// json.RawMessage is pre-encoded JSON; allow any value.
	reflect.TypeFor[json.RawMessage](): {AdditionalProperties: &jsonschema.Schema{}},
}

// EventType identifies a webhook event kind (e.g. "audit_log.created").
type EventType string

// EventRegistration is implemented by every declared event type and exposes
// the metadata needed to build the OpenAPI webhook catalog.
type EventRegistration interface {
	EventType() EventType
	Description() string
	FeatureFlag() string // x-svix-feature-flag; empty = omit
	GroupName() string   // x-svix-group-name; empty = omit
	JSONSchema() []byte  // JSON-encoded JSON Schema for the event payload
}

// EventDefOption configures optional Svix fields on an EventDef.
type EventDefOption func(*eventDefConfig)

type eventDefConfig struct {
	featureFlag string
	groupName   string
}

// WithFeatureFlag sets the Svix feature flag (x-svix-feature-flag).
func WithFeatureFlag(flag string) EventDefOption {
	return func(c *eventDefConfig) {
		c.featureFlag = flag
	}
}

// WithGroupName sets the Svix group name (x-svix-group-name).
func WithGroupName(name string) EventDefOption {
	return func(c *eventDefConfig) {
		c.groupName = name
	}
}

// EventDef is a typed, self-describing event definition. T is the Go type of
// the event payload and determines the generated JSON Schema.
type EventDef[T any] struct {
	typ         EventType
	description string
	featureFlag string
	groupName   string
}

// NewEventDef declares a typed outbox event.
func NewEventDef[T any](typ EventType, description string, opts ...EventDefOption) *EventDef[T] {
	cfg := &eventDefConfig{
		featureFlag: "",
		groupName:   "",
	}
	for _, o := range opts {
		o(cfg)
	}
	return &EventDef[T]{
		typ:         typ,
		description: description,
		featureFlag: cfg.featureFlag,
		groupName:   cfg.groupName,
	}
}

func (e *EventDef[T]) EventType() EventType { return e.typ }
func (e *EventDef[T]) Description() string  { return e.description }
func (e *EventDef[T]) FeatureFlag() string  { return e.featureFlag }
func (e *EventDef[T]) GroupName() string    { return e.groupName }

// JSONSchema returns a JSON-encoded JSON Schema derived from payload type T.
// Panics if schema generation fails (a programming error, not a runtime one).
func (e *EventDef[T]) JSONSchema() []byte {
	schema, err := jsonschema.For[T](&jsonschema.ForOptions{
		IgnoreInvalidTypes: false,
		TypeSchemas:        defaultTypeSchemas,
	})
	if err != nil {
		panic(fmt.Errorf("generate json schema for event %q: %w", e.typ, err))
	}
	bs, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Errorf("marshal json schema for event %q: %w", e.typ, err))
	}
	return bs
}
