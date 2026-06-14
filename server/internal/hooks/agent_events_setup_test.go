package hooks

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/hooks"
	"github.com/speakeasy-api/gram/server/internal/agentevents"
	cursoragent "github.com/speakeasy-api/gram/server/internal/agentevents/providers/cursor"
)

func newTestCursorAgentEventSource(t *testing.T) *agentevents.Mux {
	t.Helper()

	source, err := newTestCursorAgentEventSourceWithError()
	require.NoError(t, err)
	return source
}

func newTestCursorAgentEventSourceWithError() (*agentevents.Mux, error) {
	mux := agentevents.NewMux()
	agent, err := newTestCursorAgentWithError()
	if err != nil {
		return nil, err
	}
	if err := mux.Register(agent, nil); err != nil {
		return nil, err
	}
	return mux, nil
}

func newTestCursorAgentWithError() (*agentevents.Agent[*gen.CursorPayload], error) {
	return cursoragent.Spec().Agent()
}
