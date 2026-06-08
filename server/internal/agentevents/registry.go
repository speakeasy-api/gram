package agentevents

import (
	"errors"
	"fmt"
	"sync"

	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
)

var (
	ErrNilRegistry       = errors.New("agentevents: nil registry")
	ErrEmptyProvider     = errors.New("agentevents: empty provider")
	ErrDuplicateProvider = errors.New("agentevents: duplicate provider source")
)

type FieldResolver[T any] func(ev Event[T]) (any, bool, error)

type Resolver[T any] struct {
	Field   types.Field
	Resolve FieldResolver[T]
}

type SourceRegistry struct {
	mu      sync.Mutex
	sources map[types.Provider]struct{}
}

type Source[T any] struct {
	Provider  types.Provider
	mu        sync.RWMutex
	resolvers map[types.EventType]map[types.Field]FieldResolver[T]
}

type SourceBuilder[T any] struct {
	source *Source[T]
	err    error
}

func NewSourceRegistry() *SourceRegistry {
	return &SourceRegistry{
		sources: make(map[types.Provider]struct{}),
	}
}

func RegisterSource[T any](a *SourceRegistry, provider types.Provider) (*Source[T], error) {
	if a == nil {
		return nil, ErrNilRegistry
	}
	if provider == "" {
		return nil, ErrEmptyProvider
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.sources == nil {
		a.sources = make(map[types.Provider]struct{})
	}
	if _, ok := a.sources[provider]; ok {
		return nil, fmt.Errorf("%w: %s", ErrDuplicateProvider, provider)
	}
	source := &Source[T]{
		Provider:  provider,
		resolvers: make(map[types.EventType]map[types.Field]FieldResolver[T]),
	}
	a.sources[provider] = struct{}{}
	return source, nil
}

func (s *Source[T]) NewEvent(context EventContext, raw T) Event[T] {
	return Event[T]{source: s, Context: context, Raw: raw}
}

func (s *Source[T]) Builder() *SourceBuilder[T] {
	return &SourceBuilder[T]{source: s}
}

func (s *Source[T]) Register(resolvers ...Resolver[T]) error {
	return s.registerFor([]types.EventType{types.AnyEventType}, resolvers...)
}

func (s *Source[T]) RegisterFor(eventTypes []types.EventType, resolvers ...Resolver[T]) error {
	return s.registerFor(eventTypes, resolvers...)
}

func (b *SourceBuilder[T]) Register(resolvers ...Resolver[T]) *SourceBuilder[T] {
	if b.err == nil {
		b.err = b.source.Register(resolvers...)
	}
	return b
}

func (b *SourceBuilder[T]) RegisterFor(eventTypes []types.EventType, resolvers ...Resolver[T]) *SourceBuilder[T] {
	if b.err == nil {
		b.err = b.source.RegisterFor(eventTypes, resolvers...)
	}
	return b
}

func (b *SourceBuilder[T]) Build() (*Source[T], error) {
	if b.err != nil {
		return nil, b.err
	}
	return b.source, nil
}

func (s *Source[T]) registerFor(eventTypes []types.EventType, resolvers ...Resolver[T]) error {
	if len(eventTypes) == 0 {
		return errors.New("agentevents: no event types")
	}
	if len(resolvers) == 0 {
		return errors.New("agentevents: no resolvers")
	}
	for _, eventType := range eventTypes {
		for _, resolver := range resolvers {
			if err := s.register(eventType, resolver); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Source[T]) register(eventType types.EventType, resolver Resolver[T]) error {
	if s == nil {
		return errors.New("agentevents: nil source")
	}
	if eventType == "" {
		return errors.New("agentevents: empty event type")
	}
	if resolver.Field == "" {
		return errors.New("agentevents: empty field")
	}
	if resolver.Resolve == nil {
		return errors.New("agentevents: nil resolver")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.resolvers == nil {
		s.resolvers = make(map[types.EventType]map[types.Field]FieldResolver[T])
	}
	if s.resolvers[eventType] == nil {
		s.resolvers[eventType] = make(map[types.Field]FieldResolver[T])
	}
	if _, ok := s.resolvers[eventType][resolver.Field]; ok {
		return fmt.Errorf("agentevents: duplicate resolver for %s/%s", eventType, resolver.Field)
	}
	s.resolvers[eventType][resolver.Field] = resolver.Resolve
	return nil
}

func (s *Source[T]) resolver(eventType types.EventType, field types.Field) FieldResolver[T] {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.resolvers == nil {
		return nil
	}
	return s.resolvers[eventType][field]
}
