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

	withSkill, err := def.BuildDirectEvent(instance, dashboardTriggerConfig{}, []byte(`{"text":"use it","user_id":"user-1","correlation_id":"conv-1","idempotency_key":"skill-key","skill_context":[{"name":"incident-analysis","content":"verbatim"}]}`), receivedAt)
	require.NoError(t, err)
	skillEvent, ok := withSkill.Event.(dashboardTriggerEvent)
	require.True(t, ok)
	require.JSONEq(t, `[{"name":"incident-analysis","content":"verbatim"}]`, string(skillEvent.SkillContext))

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

// An attachment-only turn carries no text, so the has-content gate has to read
// the attachments array rather than reject the message outright.
func TestDashboardDefinitionAcceptsAttachmentOnlyTurn(t *testing.T) {
	t.Parallel()

	def := newDashboardDefinition()
	instance := triggerrepo.TriggerInstance{ID: uuid.New(), DefinitionSlug: "dashboard"}
	receivedAt := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)

	envelope, err := def.BuildDirectEvent(instance, dashboardTriggerConfig{}, []byte(`{"text":"","user_id":"user-1","correlation_id":"conv-1","idempotency_key":"key-1","attachments":[{"asset_id":"a1","name":"spec.yaml"}]}`), receivedAt)
	require.NoError(t, err)
	require.Equal(t, "conv-1", envelope.CorrelationID)

	// An empty array is not content: `attachments` is raw JSON, so a byte-length
	// check would have let this through with nothing to say.
	_, err = def.BuildDirectEvent(instance, dashboardTriggerConfig{}, []byte(`{"text":"","user_id":"user-1","correlation_id":"conv-1","idempotency_key":"key-2","attachments":[]}`), receivedAt)
	require.Error(t, err)

	_, err = def.BuildDirectEvent(instance, dashboardTriggerConfig{}, []byte(`{"text":"","user_id":"user-1","correlation_id":"conv-1","idempotency_key":"key-3","attachments":null}`), receivedAt)
	require.Error(t, err)
}

func TestCountRawJSONArray(t *testing.T) {
	t.Parallel()

	require.Equal(t, 0, countRawJSONArray(nil))
	require.Equal(t, 0, countRawJSONArray([]byte(`[]`)))
	require.Equal(t, 0, countRawJSONArray([]byte(`null`)))
	// Not an array, and not valid JSON: both mean "no attachments", never a panic.
	require.Equal(t, 0, countRawJSONArray([]byte(`{"asset_id":"a1"}`)))
	require.Equal(t, 0, countRawJSONArray([]byte(`{`)))
	require.Equal(t, 2, countRawJSONArray([]byte(`[{"asset_id":"a1"},{"asset_id":"a2"}]`)))
}
