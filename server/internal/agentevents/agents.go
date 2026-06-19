package agentevents

import (
	"errors"
	"fmt"
	"time"

	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

type fieldResolvers[T any] map[types.Field]FieldResolver[T, any]
type resolverRegistry[T any] map[types.EventType]fieldResolvers[T]

type Agent[T any] struct {
	name      types.Provider
	resolvers resolverRegistry[T]
}

type Registration[T any] struct {
	eventTypes []types.EventType
	resolvers  []Resolver[T]
}

func Register[T any](resolvers ...Resolver[T]) Registration[T] {
	return RegisterFor([]types.EventType{types.AnyEventType}, resolvers...)
}

func RegisterFor[T any](eventTypes []types.EventType, resolvers ...Resolver[T]) Registration[T] {
	return Registration[T]{
		eventTypes: eventTypes,
		resolvers:  resolvers,
	}
}

func NewAgent[T any](provider types.Provider, registrations ...Registration[T]) (*Agent[T], error) {
	resolvers := make(resolverRegistry[T])
	for _, registration := range registrations {
		if err := register(resolvers, registration); err != nil {
			return nil, err
		}
	}

	return &Agent[T]{
		name:      provider,
		resolvers: resolvers,
	}, nil
}

func (a *Agent[T]) resolver(eventType types.EventType, field types.Field) FieldResolver[T, any] {
	return a.resolvers[eventType][field]
}

func (a *Agent[T]) NewEvent(authContext *contextvalues.AuthContext, raw T, timestamp time.Time) Event[T] {
	return Event[T]{
		agent:       a,
		authContext: authContext,
		raw:         raw,
		Timestamp:   timestamp,
	}
}

func register[T any](registry resolverRegistry[T], registration Registration[T]) error {
	eventTypes := registration.eventTypes
	resolvers := registration.resolvers
	if len(eventTypes) == 0 {
		return errors.New("agentevents: no event types")
	}
	if len(resolvers) == 0 {
		return errors.New("agentevents: no resolvers")
	}

	for _, eventType := range eventTypes {
		if registry[eventType] == nil {
			registry[eventType] = make(fieldResolvers[T])
		}
		for _, resolver := range resolvers {
			if registry[eventType][resolver.Field] != nil {
				return fmt.Errorf("agentevents: duplicate resolver for %s/%s", eventType, resolver.Field)
			}
			registry[eventType][resolver.Field] = resolver.ResolveFunc
		}
	}
	return nil
}
