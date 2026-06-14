package agentevents

import (
	"context"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
)

type EventContext struct {
	Provider  types.Provider
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
	agent       *Agent[T]
	Context     EventContext
	Raw         T
	BlockReason string
}

func NewEvent[T any](mux *Mux, context EventContext, raw T) (Event[T], error) {
	agent, err := AgentFor[T](mux, context.Provider)
	if err != nil {
		return Event[T]{}, fmt.Errorf("agentevents: %w", err)
	}
	return agent.NewEvent(context, raw), nil
}

func (e Event[T]) WithContext(ctx EventContext) Event[T] {
	e.Context = ctx
	return e
}

func (e Event[T]) WithBlockReason(reason string) Event[T] {
	e.BlockReason = reason
	return e
}

func (e Event[T]) Write(ctx context.Context) error {
	if e.agent == nil {
		return ErrNilAgent
	}
	return e.agent.Write(ctx, e)
}

func (e Event[T]) Provider() types.Provider {
	return e.Context.Provider
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
	return GetValue[T, string](e, field)
}

func (e Event[T]) Int(field types.Field) (int, bool, error) {
	return GetValue[T, int](e, field)
}

func (e Event[T]) Any(field types.Field) (any, bool, error) {
	return GetValue[T, any](e, field)
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

func (e Event[T]) eventType(eventType types.EventType) (types.EventType, bool, error) {
	if eventType == "" {
		return "", false, nil
	}
	if eventType == types.AnyEventType {
		return "", false, fmt.Errorf("agentevents: field %s resolved to wildcard event type", types.FieldEventType)
	}
	return eventType, true, nil
}
