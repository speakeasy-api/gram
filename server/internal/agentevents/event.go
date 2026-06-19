package agentevents

import (
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
	return GetValue[T, types.EventType](e, types.AnyEventType, types.FieldEventType)
}

func (e Event[T]) String(eventType types.EventType, field types.Field) (string, bool, error) {
	return GetValue[T, string](e, eventType, field)
}

func (e Event[T]) Any(eventType types.EventType, field types.Field) (any, bool, error) {
	return GetValue[T, any](e, eventType, field)
}

type resolveparams struct {
	eventType types.EventType
	field     types.Field
}

func (e Event[T]) resolve(params resolveparams) (any, bool, error) {
	if resolver := e.agent.resolver(params.eventType, params.field); resolver != nil {
		return resolver(e)
	}
	return nil, false, nil
}
