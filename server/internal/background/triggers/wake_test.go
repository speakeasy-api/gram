package triggers

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	triggerrepo "github.com/speakeasy-api/gram/server/internal/triggers/repo"
)

func TestWakeDecodeConfigAcceptsFutureTimestamp(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("wake")
	require.True(t, ok)

	cfg, err := definition.DecodeConfig(map[string]any{
		"fire_at":        time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339Nano),
		"correlation_id": "thread-correlation",
	})
	require.NoError(t, err)
	require.NotNil(t, cfg)
}

func TestValidateWakeFireAtRejectsPastTimestamp(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	err := ValidateWakeFireAt(now.Add(-1*time.Minute), now)
	require.Error(t, err)
	require.Contains(t, err.Error(), "fire_at must be in the future")
}

func TestValidateWakeFireAtRejectsBeyondHorizon(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	err := ValidateWakeFireAt(now.Add(MaxWakeHorizon+time.Hour), now)
	require.Error(t, err)
	require.Contains(t, err.Error(), "fire_at must be within")
}

func TestValidateWakeFireAtRejectsZero(t *testing.T) {
	t.Parallel()

	err := ValidateWakeFireAt(time.Time{}, time.Now().UTC())
	require.Error(t, err)
	require.Contains(t, err.Error(), "fire_at is required")
}

func TestValidateWakeFireAtAcceptsFuture(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	require.NoError(t, ValidateWakeFireAt(now.Add(1*time.Hour), now))
}

func TestWakeDecodeConfigRequiresCorrelationID(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("wake")
	require.True(t, ok)

	_, err := definition.DecodeConfig(map[string]any{
		"fire_at": time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339Nano),
	})
	require.Error(t, err)
}

func TestWakeBuildScheduledEventPropagatesCorrelationAndNote(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("wake")
	require.True(t, ok)

	fireAt := time.Now().UTC().Add(5 * time.Minute)
	note := "follow up on the deploy"
	cfg, err := definition.DecodeConfig(map[string]any{
		"fire_at":        fireAt.Format(time.RFC3339Nano),
		"correlation_id": "thread-xyz",
		"note":           note,
	})
	require.NoError(t, err)

	instance := triggerrepo.TriggerInstance{
		ID:             uuid.New(),
		OrganizationID: "org",
		ProjectID:      uuid.New(),
		DefinitionSlug: "wake",
		Name:           "test wake",
		EnvironmentID:  uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		TargetKind:     "assistant",
		TargetRef:      uuid.NewString(),
		TargetDisplay:  "test",
		ConfigJson:     nil,
		Status:         "active",
		CreatedAt:      pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: 0, Valid: false},
		UpdatedAt:      pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: 0, Valid: false},
		DeletedAt:      pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: 0, Valid: false},
		Deleted:        false,
	}

	firedAt := time.Now().UTC()
	envelope, err := definition.BuildScheduledEvent(instance, cfg, firedAt)
	require.NoError(t, err)
	require.Equal(t, "thread-xyz", envelope.CorrelationID)
	require.Equal(t, instance.ID.String(), envelope.TriggerInstanceID)

	var event wakeTriggerEvent
	require.NoError(t, json.Unmarshal(envelope.RawPayload, &event))
	require.Equal(t, note, event.Note)
	require.Equal(t, fireAt.Format(time.RFC3339Nano), event.ScheduledAt)
}

func TestWakeExtractScheduleReturnsTimestamp(t *testing.T) {
	t.Parallel()

	definition, ok := GetDefinition("wake")
	require.True(t, ok)

	fireAt := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Millisecond)
	cfg, err := definition.DecodeConfig(map[string]any{
		"fire_at":        fireAt.Format(time.RFC3339Nano),
		"correlation_id": "thread-1",
	})
	require.NoError(t, err)

	schedule, err := definition.ExtractSchedule(cfg)
	require.NoError(t, err)
	require.Equal(t, fireAt.Format(time.RFC3339Nano), schedule)
}
