package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agentevents"
	telemetrysink "github.com/speakeasy-api/gram/server/internal/agentevents/eventsink/sinks/telemetry"
	"github.com/speakeasy-api/gram/server/internal/agentevents/types"
	gramtelemetry "github.com/speakeasy-api/gram/server/internal/telemetry"
)

type testPayload struct{}

type recordingLogger struct {
	logs []gramtelemetry.LogParams
}

func (l *recordingLogger) Log(_ context.Context, params gramtelemetry.LogParams) {
	l.logs = append(l.logs, params)
}

func TestTelemetrySinkNoopsWithoutLogger(t *testing.T) {
	t.Parallel()

	ev := newTelemetryEvent(t)
	sink := telemetrysink.New[*testPayload](nil)

	require.NoError(t, sink.Write(t.Context(), ev))
}

func TestTelemetrySinkWritesBuiltLogs(t *testing.T) {
	t.Parallel()

	ev := newTelemetryEvent(t)
	logger := &recordingLogger{}
	sink := telemetrysink.New[*testPayload](logger)

	require.NoError(t, sink.Write(t.Context(), ev))
	require.Len(t, logger.logs, 1)
	assert.Equal(t, "org", logger.logs[0].ToolInfo.OrganizationID)
	assert.Equal(t, "22222222-2222-2222-2222-222222222222", logger.logs[0].ToolInfo.ProjectID)
}

func newTelemetryEvent(t *testing.T) agentevents.Event[*testPayload] {
	t.Helper()

	agent, err := agentevents.NewAgent[*testPayload]("cursor")
	require.NoError(t, err)
	require.NoError(t, agent.Register(
		agentevents.Resolve[*testPayload, types.EventType](types.FieldEventType, func(agentevents.Event[*testPayload]) (types.EventType, bool, error) {
			return types.SessionStarted, true, nil
		}),
	))
	return agent.NewEvent(agentevents.EventContext{
		OrgID:          "org",
		ProjectID:      "22222222-2222-2222-2222-222222222222",
		UserEmail:      "dev@example.com",
		ConversationID: "conversation",
		Timestamp:      time.Unix(123, 0),
	}, &testPayload{})
}
