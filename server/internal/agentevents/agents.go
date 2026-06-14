package agentevents

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
)

var (
	ErrNilRegistry       = errors.New("agentevents: nil registry")
	ErrNilAgent          = errors.New("agentevents: nil agent")
	ErrNilHandle         = errors.New("agentevents: nil handle")
	ErrNilSink           = errors.New("agentevents: nil sink")
	ErrEmptyProvider     = errors.New("agentevents: empty provider")
	ErrDuplicateProvider = errors.New("agentevents: duplicate provider Agent")
)

type Sink[T any] interface {
	Write(ctx context.Context, ev Event[T]) error
}

type Agent[T any] struct {
	Provider  types.Provider
	mu        sync.RWMutex
	resolvers map[types.EventType]map[types.Field]FieldResolver[T, any]
	sinks     []Sink[T]
}

func (a *Agent[T]) ProviderID() types.Provider {
	return a.Provider
}

func NewAgent[T any](provider types.Provider) (*Agent[T], error) {
	if provider == "" {
		return nil, ErrEmptyProvider
	}

	return &Agent[T]{
		Provider:  provider,
		resolvers: make(map[types.EventType]map[types.Field]FieldResolver[T, any]),
	}, nil
}

func (a *Agent[T]) Builder() *AgentBuilder[T] {
	return &AgentBuilder[T]{Agent: a}
}

func (a *Agent[T]) Register(resolvers ...Resolver[T]) error {
	return a.registerFor([]types.EventType{types.AnyEventType}, resolvers...)
}

func (a *Agent[T]) RegisterFor(eventTypes []types.EventType, resolvers ...Resolver[T]) error {
	return a.registerFor(eventTypes, resolvers...)
}

func (a *Agent[T]) Use(sinks ...Sink[T]) error {
	if a == nil {
		return ErrNilAgent
	}
	for _, sink := range sinks {
		if sink == nil {
			return ErrNilSink
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.sinks = append(a.sinks, sinks...)
	return nil
}

func (a *Agent[T]) Write(ctx context.Context, ev Event[T]) error {
	if a == nil {
		return ErrNilAgent
	}

	a.mu.RLock()
	sinks := append([]Sink[T](nil), a.sinks...)
	a.mu.RUnlock()

	for _, sink := range sinks {
		if err := sink.Write(ctx, ev); err != nil {
			return err
		}
	}
	return nil
}

func (a *Agent[T]) registerFor(eventTypes []types.EventType, resolvers ...Resolver[T]) error {
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

func (s *Agent[T]) resolver(eventType types.EventType, field types.Field) FieldResolver[T, any] {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.resolvers == nil {
		return nil
	}
	return s.resolvers[eventType][field]
}

type AgentBuilder[T any] struct {
	Agent *Agent[T]
	err   error
}

func (b *AgentBuilder[T]) Register(resolvers ...Resolver[T]) *AgentBuilder[T] {
	if b.err == nil {
		b.err = b.Agent.Register(resolvers...)
	}
	return b
}

func (b *AgentBuilder[T]) RegisterFor(eventTypes []types.EventType, resolvers ...Resolver[T]) *AgentBuilder[T] {
	if b.err == nil {
		b.err = b.Agent.RegisterFor(eventTypes, resolvers...)
	}
	return b
}

func (b *AgentBuilder[T]) Use(sinks ...Sink[T]) *AgentBuilder[T] {
	if b.err == nil {
		b.err = b.Agent.Use(sinks...)
	}
	return b
}

func (b *AgentBuilder[T]) Build() (*Agent[T], error) {
	if b.err != nil {
		return nil, b.err
	}
	return b.Agent, nil
}

func AgentFor[T any](mux *Mux, provider types.Provider) (*Agent[T], error) {
	if mux == nil {
		return nil, errors.New("agentevents: nil mux")
	}

	mux.mu.Lock()
	handle, ok := mux.Agents[provider]
	mux.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("agentevents: unknown provider %s", provider)
	}

	agent, ok := handle.(*Agent[T])
	if !ok {
		return nil, fmt.Errorf("agentevents: provider %s registered with different payload type", provider)
	}

	return agent, nil
}

func (a *Agent[T]) NewEvent(context EventContext, raw T) Event[T] {
	context.Provider = a.Provider
	return Event[T]{agent: a, Context: context, Raw: raw}
}
