package agentevents_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agentevents"
)

type reflectedPayload struct {
	Name  *string
	Count *int
	Input any
}

func TestStringField(t *testing.T) {
	t.Parallel()

	source := newReflectedSource(t)
	name := "cursor"
	ev := source.NewEvent(agentevents.EventContext{}, reflectedPayload{Name: &name})

	value, ok, err := agentevents.StringField[reflectedPayload]("Name")(ev)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "cursor", value)

	empty := ""
	value, ok, err = agentevents.StringField[reflectedPayload]("Name")(source.NewEvent(agentevents.EventContext{}, reflectedPayload{Name: &empty}))
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, value)
}

func TestIntField(t *testing.T) {
	t.Parallel()

	source := newReflectedSource(t)
	count := 12
	ev := source.NewEvent(agentevents.EventContext{}, reflectedPayload{Count: &count})

	value, ok, err := agentevents.IntField[reflectedPayload]("Count")(ev)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, 12, value)
}

func TestAnyField(t *testing.T) {
	t.Parallel()

	source := newReflectedSource(t)
	input := map[string]any{"ok": true}
	ev := source.NewEvent(agentevents.EventContext{}, reflectedPayload{Input: input})

	value, ok, err := agentevents.AnyField[reflectedPayload]("Input")(ev)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, input, value)
}

func TestReflectedFieldResolverErrors(t *testing.T) {
	t.Parallel()

	source := newReflectedSource(t)
	name := "not-an-int"
	ev := source.NewEvent(agentevents.EventContext{}, reflectedPayload{Name: &name})

	_, ok, err := agentevents.IntField[reflectedPayload]("Name")(ev)
	require.Error(t, err)
	assert.False(t, ok)

	_, ok, err = agentevents.StringField[reflectedPayload]("Missing")(ev)
	require.Error(t, err)
	assert.False(t, ok)
}

func newReflectedSource(t *testing.T) *agentevents.Source[reflectedPayload] {
	t.Helper()

	source, err := agentevents.RegisterSource[reflectedPayload](agentevents.NewSourceRegistry(), "reflected")
	require.NoError(t, err)
	return source
}
