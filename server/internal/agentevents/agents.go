package agentevents

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
)

var (
	ErrNilAgent = errors.New("agentevents: nil agent")
)

type Agent[T any] struct {
	name      types.Provider
	mu        sync.RWMutex
	resolvers map[types.EventType]map[types.Field]FieldResolver[T, any]
}

func NewAgent[T any](provider types.Provider) (*Agent[T], error) {
	return &Agent[T]{
		name:      provider,
		resolvers: make(map[types.EventType]map[types.Field]FieldResolver[T, any]),
	}, nil
}

func (a *Agent[T]) NewEvent(authContext *contextvalues.AuthContext, raw T, timestamp time.Time) Event[T] {
	return Event[T]{
		agent:       a,
		authContext: authContext,
		raw:         raw,
		Timestamp:   timestamp,
	}
}

func (a *Agent[T]) Register(resolvers ...Resolver[T]) error {
	return a.RegisterFor([]types.EventType{types.AnyEventType}, resolvers...)
}

func (a *Agent[T]) RegisterFor(eventTypes []types.EventType, resolvers ...Resolver[T]) error {
	if len(eventTypes) == 0 {
		return errors.New("agentevents: no event types")
	}
	if len(resolvers) == 0 {
		return errors.New("agentevents: no resolvers")
	}

	for _, eventType := range eventTypes {
		for _, resolver := range resolvers {
			if err := a.register(eventType, resolver); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *Agent[T]) register(eventType types.EventType, resolver Resolver[T]) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.resolvers[eventType] == nil {
		a.resolvers[eventType] = make(map[types.Field]FieldResolver[T, any])
	}
	if _, ok := a.resolvers[eventType][resolver.Field]; ok {
		return fmt.Errorf("agentevents: duplicate resolver for %s/%s", eventType, resolver.Field)
	}

	a.resolvers[eventType][resolver.Field] = resolver.ResolveFunc
	return nil
}

func (a *Agent[T]) resolver(eventType types.EventType, field types.Field) FieldResolver[T, any] {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.resolvers == nil {
		return nil
	}
	return a.resolvers[eventType][field]
}
