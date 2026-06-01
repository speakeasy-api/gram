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

	envelope, err := def.BuildDirectEvent(instance, dashboardTriggerConfig{}, []byte(`{"text":"top errors?","user_id":"user-1"}`), receivedAt)
	require.NoError(t, err)
	require.Equal(t, "user-1", envelope.CorrelationID, "user id doubles as the thread correlation id")
	require.Equal(t, instance.ID.String(), envelope.TriggerInstanceID)
	require.Equal(t, "dashboard", envelope.DefinitionSlug)
	require.NotEmpty(t, envelope.EventID)
	require.Equal(t, receivedAt, envelope.ReceivedAt)

	_, err = def.BuildDirectEvent(instance, dashboardTriggerConfig{}, []byte(`{"user_id":"user-1"}`), receivedAt)
	require.Error(t, err, "empty text rejected")

	_, err = def.BuildDirectEvent(instance, dashboardTriggerConfig{}, []byte(`{"text":"hi"}`), receivedAt)
	require.Error(t, err, "empty user id rejected")
}
