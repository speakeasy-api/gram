package triggers

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

func TestDashboardDefinitionBuildDirectEvent(t *testing.T) {
	t.Parallel()

	def := newDashboardDefinition()
	require.Equal(t, KindDirect, def.Kind)
	require.Nil(t, def.HandleWebhook)
	require.Nil(t, def.BuildScheduledEvent)
	require.NotNil(t, def.BuildDirectEvent)

	instance := triggerrepo.TriggerInstance{ID: uuid.New(), DefinitionSlug: "dashboard"}
	receivedAt := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)

	envelope, err := def.BuildDirectEvent(instance, dashboardTriggerConfig{}, []byte(`{"text":"top errors?","user_id":"user-1","correlation_id":"conv-1","idempotency_key":"key-1"}`), receivedAt)
	require.NoError(t, err)
	require.Equal(t, "conv-1", envelope.CorrelationID, "correlation id keys the thread, independent of the user")
	require.Equal(t, instance.ID.String(), envelope.TriggerInstanceID)
	require.Equal(t, "dashboard", envelope.DefinitionSlug)
	require.NotEmpty(t, envelope.EventID)
	require.Equal(t, receivedAt, envelope.ReceivedAt)

	// Event id is derived from instance + idempotency key so retries dedupe.
	retry, err := def.BuildDirectEvent(instance, dashboardTriggerConfig{}, []byte(`{"text":"top errors?","user_id":"user-1","correlation_id":"conv-1","idempotency_key":"key-1"}`), receivedAt.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, envelope.EventID, retry.EventID, "same idempotency key yields same event id")

	other, err := def.BuildDirectEvent(instance, dashboardTriggerConfig{}, []byte(`{"text":"top errors?","user_id":"user-1","correlation_id":"conv-1","idempotency_key":"key-2"}`), receivedAt)
	require.NoError(t, err)
	require.NotEqual(t, envelope.EventID, other.EventID, "different idempotency key yields different event id")

	_, err = def.BuildDirectEvent(instance, dashboardTriggerConfig{}, []byte(`{"user_id":"user-1","correlation_id":"conv-1","idempotency_key":"key-1"}`), receivedAt)
	require.Error(t, err, "empty text rejected")

	_, err = def.BuildDirectEvent(instance, dashboardTriggerConfig{}, []byte(`{"text":"hi","correlation_id":"conv-1","idempotency_key":"key-1"}`), receivedAt)
	require.Error(t, err, "empty user id rejected")

	_, err = def.BuildDirectEvent(instance, dashboardTriggerConfig{}, []byte(`{"text":"hi","user_id":"user-1","correlation_id":"conv-1"}`), receivedAt)
	require.Error(t, err, "empty idempotency key rejected")

	_, err = def.BuildDirectEvent(instance, dashboardTriggerConfig{}, []byte(`{"text":"hi","user_id":"user-1","idempotency_key":"key-1"}`), receivedAt)
	require.Error(t, err, "empty correlation id rejected")
}

func TestDashboardDefinitionBuildDirectEventWarm(t *testing.T) {
	t.Parallel()

	def := newDashboardDefinition()
	instance := triggerrepo.TriggerInstance{ID: uuid.New(), DefinitionSlug: "dashboard"}
	receivedAt := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)

	// Warm events carry no user message, so text/user_id are not required.
	env, err := def.BuildDirectEvent(instance, dashboardTriggerConfig{}, []byte(`{"warm":true,"correlation_id":"__warm__","idempotency_key":"w-1"}`), receivedAt)
	require.NoError(t, err, "warm event accepted without text/user_id")
	require.Equal(t, "__warm__", env.CorrelationID)

	// Warm must survive into the envelope's Event, or it's lost before reaching
	// the normalized payload (the bug that sank the earlier `hidden` flag).
	ev, ok := env.Event.(dashboardTriggerEvent)
	require.True(t, ok)
	require.True(t, ev.Warm)

	// Idempotency key is still required even for warm events.
	_, err = def.BuildDirectEvent(instance, dashboardTriggerConfig{}, []byte(`{"warm":true,"correlation_id":"__warm__"}`), receivedAt)
	require.Error(t, err, "warm still requires an idempotency key")
}
