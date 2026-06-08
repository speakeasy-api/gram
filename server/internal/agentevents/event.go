package agentevents

import (
	"fmt"
	"strconv"
	"time"

	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
)

type EventContext struct {
	OrgID     string
	ProjectID string
	UserID    string
	UserEmail string

	ConversationID string
	ChatID         string
	Timestamp      time.Time

	Metadata map[string]any
}

type Event[T any] struct {
	source      *Source[T]
	Context     EventContext
	Raw         T
	BlockReason string
}

func (e Event[T]) WithContext(ctx EventContext) Event[T] {
	e.Context = ctx
	return e
}

func (e Event[T]) WithBlockReason(reason string) Event[T] {
	e.BlockReason = reason
	return e
}

func (e Event[T]) Provider() types.Provider {
	if e.source == nil {
		return ""
	}
	return e.source.Provider
}

func (e Event[T]) resolve(field types.Field) (any, bool, error) {
	if e.source == nil {
		return nil, false, fmt.Errorf("agentevents: nil source")
	}
	if field == types.FieldEventType {
		return nil, false, fmt.Errorf("agentevents: use EventType to resolve %s", field)
	}
	eventType, ok, err := e.EventType()
	if err != nil || !ok {
		return nil, false, err
	}
	return e.resolveWithEventType(eventType, field)
}

func (e Event[T]) EventType() (types.EventType, bool, error) {
	value, ok, err := e.resolveWithEventType(types.AnyEventType, types.FieldEventType)
	if err != nil || !ok {
		return "", false, err
	}
	switch v := value.(type) {
	case types.EventType:
		return e.eventType(v)
	case string:
		if v == "" {
			return "", false, nil
		}
		return e.eventType(types.EventType(v))
	default:
		return "", false, fmt.Errorf("agentevents: field %s resolved to %T, want event type", types.FieldEventType, value)
	}
}

func (e Event[T]) String(field types.Field) (string, bool, error) {
	value, ok, err := e.resolve(field)
	if err != nil || !ok || value == nil {
		return "", ok, err
	}
	switch v := value.(type) {
	case string:
		return v, true, nil
	case fmt.Stringer:
		return v.String(), true, nil
	default:
		return "", false, fmt.Errorf("agentevents: field %s resolved to %T, want string", field, value)
	}
}

func (e Event[T]) Int(field types.Field) (int, bool, error) {
	value, ok, err := e.resolve(field)
	if err != nil || !ok || value == nil {
		return 0, ok, err
	}
	switch v := value.(type) {
	case int:
		return v, true, nil
	case int8:
		return int(v), true, nil
	case int16:
		return int(v), true, nil
	case int32:
		return int(v), true, nil
	case int64:
		return int(v), true, nil
	case uint:
		return int(v), true, nil
	case uint8:
		return int(v), true, nil
	case uint16:
		return int(v), true, nil
	case uint32:
		return int(v), true, nil
	case uint64:
		return int(v), true, nil
	case float64:
		return int(v), true, nil
	case string:
		i, err := strconv.Atoi(v)
		if err != nil {
			return 0, false, fmt.Errorf("agentevents: field %s string is not an int: %w", field, err)
		}
		return i, true, nil
	default:
		return 0, false, fmt.Errorf("agentevents: field %s resolved to %T, want int", field, value)
	}
}

func (e Event[T]) Any(field types.Field) (any, bool, error) {
	return e.resolve(field)
}

func (e Event[T]) resolveWithEventType(eventType types.EventType, field types.Field) (any, bool, error) {
	if resolver := e.source.resolver(eventType, field); resolver != nil {
		return resolver(e)
	}
	if eventType == types.AnyEventType {
		return nil, false, nil
	}
	if resolver := e.source.resolver(types.AnyEventType, field); resolver != nil {
		return resolver(e)
	}
	return nil, false, nil
}

func (e Event[T]) eventType(eventType types.EventType) (types.EventType, bool, error) {
	if eventType == "" {
		return "", false, nil
	}
	if eventType == types.AnyEventType {
		return "", false, fmt.Errorf("agentevents: field %s resolved to wildcard event type", types.FieldEventType)
	}
	return eventType, true, nil
}
