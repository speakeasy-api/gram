package agentevents_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agentevents"
	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
)

type testPayload struct {
	EventType types.EventType
	Prompt    string
	Count     int
}

func testResolver(field types.Field, fn agentevents.FieldResolver[testPayload]) agentevents.Resolver[testPayload] {
	return agentevents.Resolver[testPayload]{Field: field, Resolve: fn}
}

func TestRegistrySourceAndResolve(t *testing.T) {
	t.Parallel()

	registry := agentevents.NewSourceRegistry()
	source, err := agentevents.RegisterSource[testPayload](registry, "test")
	require.NoError(t, err)

	err = source.Register(testResolver(types.FieldEventType, func(ev agentevents.Event[testPayload]) (any, bool, error) {
		return ev.Raw.EventType, true, nil
	}))
	require.NoError(t, err)
	err = source.RegisterFor([]types.EventType{types.UserPromptSubmit},
		testResolver(types.FieldPrompt, func(ev agentevents.Event[testPayload]) (any, bool, error) {
			return ev.Raw.Prompt, ev.Raw.Prompt != "", nil
		}),
		testResolver(types.FieldUsageInputTokens, func(ev agentevents.Event[testPayload]) (any, bool, error) {
			return ev.Raw.Count, true, nil
		}),
	)
	require.NoError(t, err)

	ev := source.NewEvent(agentevents.EventContext{}, testPayload{
		EventType: types.UserPromptSubmit,
		Prompt:    "hello",
		Count:     42,
	})

	eventType, ok, err := ev.EventType()
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, types.UserPromptSubmit, eventType)

	_, ok, err = ev.Any(types.FieldEventType)
	require.Error(t, err)
	assert.False(t, ok)

	prompt, ok, err := ev.String(types.FieldPrompt)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "hello", prompt)

	count, ok, err := ev.Int(types.FieldUsageInputTokens)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 42, count)

	_, ok, err = ev.Any(types.FieldToolName)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestRegistryValidation(t *testing.T) {
	t.Parallel()

	registry := agentevents.NewSourceRegistry()
	source, err := agentevents.RegisterSource[testPayload](registry, "test")
	require.NoError(t, err)

	_, err = agentevents.RegisterSource[testPayload](registry, "test")
	require.ErrorIs(t, err, agentevents.ErrDuplicateProvider)

	err = source.Register(testResolver(types.FieldEventType, func(ev agentevents.Event[testPayload]) (any, bool, error) {
		return ev.Raw.EventType, true, nil
	}))
	require.NoError(t, err)
	err = source.Register(testResolver(types.FieldEventType, func(ev agentevents.Event[testPayload]) (any, bool, error) {
		return ev.Raw.EventType, true, nil
	}))
	require.Error(t, err)

	err = source.RegisterFor([]types.EventType{""}, testResolver(types.FieldPrompt, func(agentevents.Event[testPayload]) (any, bool, error) {
		return nil, false, nil
	}))
	require.Error(t, err)
}

func TestRegisterForRegistersFieldAcrossEventTypes(t *testing.T) {
	t.Parallel()

	registry := agentevents.NewSourceRegistry()
	source, err := agentevents.RegisterSource[testPayload](registry, "test")
	require.NoError(t, err)

	require.NoError(t, source.Register(testResolver(types.FieldEventType, func(ev agentevents.Event[testPayload]) (any, bool, error) {
		return ev.Raw.EventType, true, nil
	})))
	require.NoError(t, source.RegisterFor([]types.EventType{types.UserPromptSubmit, types.AssistantResponseComplete},
		testResolver(types.FieldPrompt, func(ev agentevents.Event[testPayload]) (any, bool, error) {
			return ev.Raw.Prompt, ev.Raw.Prompt != "", nil
		}),
	))

	for _, eventType := range []types.EventType{types.UserPromptSubmit, types.AssistantResponseComplete} {
		ev := source.NewEvent(agentevents.EventContext{}, testPayload{
			EventType: eventType,
			Prompt:    string(eventType),
		})
		got, ok, err := ev.String(types.FieldPrompt)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, string(eventType), got)
	}
}

func TestAnyEventTypeResolverFallback(t *testing.T) {
	t.Parallel()

	registry := agentevents.NewSourceRegistry()
	source, err := agentevents.RegisterSource[testPayload](registry, "test")
	require.NoError(t, err)

	require.NoError(t, source.Register(
		testResolver(types.FieldEventType, func(ev agentevents.Event[testPayload]) (any, bool, error) {
			return ev.Raw.EventType, true, nil
		}),
		testResolver(types.FieldPrompt, func(ev agentevents.Event[testPayload]) (any, bool, error) {
			return "fallback", true, nil
		}),
	))
	require.NoError(t, source.RegisterFor([]types.EventType{types.AssistantResponseComplete},
		testResolver(types.FieldPrompt, func(ev agentevents.Event[testPayload]) (any, bool, error) {
			return "specific", true, nil
		}),
	))

	prompt, ok, err := source.NewEvent(agentevents.EventContext{}, testPayload{
		EventType: types.UserPromptSubmit,
	}).String(types.FieldPrompt)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "fallback", prompt)

	prompt, ok, err = source.NewEvent(agentevents.EventContext{}, testPayload{
		EventType: types.AssistantResponseComplete,
	}).String(types.FieldPrompt)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "specific", prompt)
}

func TestCommonResolversApplyToFutureEventTypes(t *testing.T) {
	t.Parallel()

	registry := agentevents.NewSourceRegistry()
	source, err := agentevents.RegisterSource[testPayload](registry, "test")
	require.NoError(t, err)

	require.NoError(t, source.Register(
		testResolver(types.FieldEventType, func(ev agentevents.Event[testPayload]) (any, bool, error) {
			return ev.Raw.EventType, true, nil
		}),
		testResolver(types.FieldPrompt, func(ev agentevents.Event[testPayload]) (any, bool, error) {
			return ev.Raw.Prompt, ev.Raw.Prompt != "", nil
		}),
	))

	futureEventType := types.EventType("future.event")
	prompt, ok, err := source.NewEvent(agentevents.EventContext{}, testPayload{
		EventType: futureEventType,
		Prompt:    "future",
	}).String(types.FieldPrompt)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "future", prompt)

	eventType, ok, err := source.NewEvent(agentevents.EventContext{}, testPayload{
		EventType: futureEventType,
	}).EventType()
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, futureEventType, eventType)
}

func TestRegisterForValidation(t *testing.T) {
	t.Parallel()

	registry := agentevents.NewSourceRegistry()
	source, err := agentevents.RegisterSource[testPayload](registry, "test")
	require.NoError(t, err)

	err = source.RegisterFor(nil, testResolver(types.FieldPrompt, func(agentevents.Event[testPayload]) (any, bool, error) {
		return nil, false, nil
	}))
	require.Error(t, err)

	err = source.RegisterFor([]types.EventType{types.UserPromptSubmit},
		testResolver(types.FieldPrompt, func(agentevents.Event[testPayload]) (any, bool, error) {
			return "first", true, nil
		}),
	)
	require.NoError(t, err)
	err = source.RegisterFor([]types.EventType{types.UserPromptSubmit, types.AssistantResponseComplete},
		testResolver(types.FieldPrompt, func(agentevents.Event[testPayload]) (any, bool, error) {
			return "duplicate", true, nil
		}),
	)
	require.Error(t, err)
}

func TestSourceBuilderReturnsFirstRegistrationError(t *testing.T) {
	t.Parallel()

	registry := agentevents.NewSourceRegistry()
	source, err := agentevents.RegisterSource[testPayload](registry, "test")
	require.NoError(t, err)

	require.NoError(t, source.Register(testResolver(types.FieldEventType, func(ev agentevents.Event[testPayload]) (any, bool, error) {
		return ev.Raw.EventType, true, nil
	})))

	got, err := source.Builder().
		Register(testResolver(types.FieldEventType, func(ev agentevents.Event[testPayload]) (any, bool, error) {
			return ev.Raw.EventType, true, nil
		})).
		RegisterFor([]types.EventType{types.UserPromptSubmit},
			testResolver(types.FieldPrompt, func(ev agentevents.Event[testPayload]) (any, bool, error) {
				return ev.Raw.Prompt, ev.Raw.Prompt != "", nil
			}),
		).
		Build()
	require.Error(t, err)
	assert.Nil(t, got)

	prompt, ok, err := source.NewEvent(agentevents.EventContext{}, testPayload{
		EventType: types.UserPromptSubmit,
		Prompt:    "not registered",
	}).String(types.FieldPrompt)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, prompt)
}

func TestResolverErrorPropagates(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("resolver failed")
	registry := agentevents.NewSourceRegistry()
	source, err := agentevents.RegisterSource[testPayload](registry, "test")
	require.NoError(t, err)
	require.NoError(t, source.Register(testResolver(types.FieldEventType, func(agentevents.Event[testPayload]) (any, bool, error) {
		return nil, false, wantErr
	})))

	_, ok, err := source.NewEvent(agentevents.EventContext{}, testPayload{}).EventType()
	require.ErrorIs(t, err, wantErr)
	assert.False(t, ok)
}
