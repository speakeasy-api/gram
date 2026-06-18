package agentevents

import (
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

type Event[T any] struct {
	agent       *Agent[T]
	authContext *contextvalues.AuthContext
	raw         T
	Timestamp   time.Time
}

func (e Event[T]) Raw() T {
	return e.raw
}

func (e Event[T]) AuthContext() *contextvalues.AuthContext {
	return e.authContext
}

func (e Event[T]) EventType() (types.EventType, bool, error) {
	if e.agent == nil {
		return "", false, ErrNilAgent
	}
	value, ok, err := e.resolveWithEventType(types.AnyEventType, types.FieldEventType)
	if err != nil || !ok {
		return "", false, err
	}

	switch v := value.(type) {
	case types.EventType:
		return normalizeEventType(v)
	case string:
		return normalizeEventType(types.EventType(v))
	default:
		return "", false, fmt.Errorf("agentevents: field %s resolved to %T, want event type", types.FieldEventType, value)
	}
}

func (e Event[T]) String(field types.Field) (string, bool, error) {
	return GetValue[T, string](e, field)
}

func (e Event[T]) Any(field types.Field) (any, bool, error) {
	return GetValue[T, any](e, field)
}

func (e Event[T]) resolve(field types.Field) (any, bool, error) {
	if e.agent == nil {
		return nil, false, ErrNilAgent
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

func (e Event[T]) resolveWithEventType(eventType types.EventType, field types.Field) (any, bool, error) {
	if e.agent == nil {
		return nil, false, ErrNilAgent
	}
	if resolver := e.agent.resolver(eventType, field); resolver != nil {
		return resolver(e)
	}
	if eventType == types.AnyEventType {
		return nil, false, nil
	}
	if resolver := e.agent.resolver(types.AnyEventType, field); resolver != nil {
		return resolver(e)
	}
	return nil, false, nil
}

func normalizeEventType(eventType types.EventType) (types.EventType, bool, error) {
	if eventType == "" {
		return "", false, nil
	}
	if eventType == types.AnyEventType {
		return "", false, fmt.Errorf("agentevents: field %s resolved to wildcard event type", types.FieldEventType)
	}
	return eventType, true, nil
}
