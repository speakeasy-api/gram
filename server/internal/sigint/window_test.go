package sigint_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/sigint"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	chatrepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sigint/evaluation"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestSensorWindowSettingsRoundTripAndAudit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	sensor := createSensor(t, ctx, ti, "Context", "multi_label")
	require.Equal(t, types.SigintInputStrategy("single_message"), sensor.InputStrategy)
	require.Equal(t, 5, sensor.WindowSize)
	updated, err := ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{ID: sensor.ID, InputStrategy: new(types.SigintInputStrategy("sliding_window")), WindowSize: new(10)})
	require.NoError(t, err)
	require.Equal(t, types.SigintInputStrategy("sliding_window"), updated.InputStrategy)
	require.Equal(t, 10, updated.WindowSize)
	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSigintSensorUpdate)
	require.NoError(t, err)
	before, err := audittest.DecodeAuditData(entry.BeforeSnapshot)
	require.NoError(t, err)
	after, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, "single_message", before["InputStrategy"])
	require.Equal(t, "sliding_window", after["InputStrategy"])
	require.EqualValues(t, 10, after["WindowSize"])

	updated, err = ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{ID: sensor.ID, Name: new("Renamed context")})
	require.NoError(t, err)
	require.Equal(t, 10, updated.WindowSize)
	require.Equal(t, types.SigintInputStrategy("sliding_window"), updated.InputStrategy)
	updated, err = ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{ID: sensor.ID, InputStrategy: new(types.SigintInputStrategy("single_message"))})
	require.NoError(t, err)
	require.Equal(t, 10, updated.WindowSize)
	require.Equal(t, updated, getSensor(t, ctx, ti, sensor.ID))
	listed, err := ti.service.ListSensors(ctx, &gen.ListSensorsPayload{Limit: 50})
	require.NoError(t, err)
	require.Equal(t, updated, listed.Sensors[0])
}

func TestSensorWindowRejectsInvalidConfigurationAtomically(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, strategy string
		size           int
	}{
		{"too small", "sliding_window", 1}, {"too large", "sliding_window", 11},
		{"zero", "sliding_window", 0}, {"negative", "single_message", -1},
		{"unknown strategy", "unknown", 5}, {"empty strategy", "", 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestService(t)
			sensor := createSensor(t, ctx, ti, "Context", "multi_label")
			_, err := ti.service.CreateSensor(ctx, &gen.CreateSensorPayload{Name: "Invalid", Mode: "multi_label", InputStrategy: new(types.SigintInputStrategy(tc.strategy)), WindowSize: new(tc.size)})
			requireOopsCode(t, err, oops.CodeBadRequest)
			_, err = ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{ID: sensor.ID, Name: new("Must not persist"), InputStrategy: new(types.SigintInputStrategy(tc.strategy)), WindowSize: new(tc.size)})
			requireOopsCode(t, err, oops.CodeBadRequest)
			require.Equal(t, sensor, getSensor(t, ctx, ti, sensor.ID))
		})
	}
}

func TestEvaluationHistoryTimestampOrderingAndIsolation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	project := *auth.ProjectID
	fixtures := testrepo.New(ti.conn)
	chat, err := fixtures.SeedCapturedAgentChatFixture(ctx, testrepo.SeedCapturedAgentChatFixtureParams{ID: uuid.New(), ProjectID: project, OrganizationID: auth.ActiveOrganizationID})
	require.NoError(t, err)
	otherChat, err := fixtures.SeedCapturedAgentChatFixture(ctx, testrepo.SeedCapturedAgentChatFixtureParams{ID: uuid.New(), ProjectID: project, OrganizationID: auth.ActiveOrganizationID})
	require.NoError(t, err)
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	queries := chatrepo.New(ti.conn)
	var ids []uuid.UUID
	for _, tc := range []struct {
		role       string
		minute     int
		generation int32
		chat       uuid.UUID
	}{
		{"assistant", 3, 0, chat}, // Ingested before an older user message.
		{"system", 1, 0, chat},
		{"user", 2, 0, chat},
		{"tool", 3, 0, chat},      // Equal timestamp follows assistant by sequence.
		{"user", 4, 0, chat},      // Target.
		{"assistant", 4, 0, chat}, // Equal timestamp but after target.
		{"user", 5, 0, chat},
		{"user", 3, 1, chat},
		{"user", 3, 0, otherChat},
	} {
		id, err := queries.CreateExternalChatMessage(ctx, chatrepo.CreateExternalChatMessageParams{ID: uuid.New(), ChatID: tc.chat, ProjectID: project, Role: tc.role, Content: tc.role, Generation: tc.generation, CreatedAt: conv.ToPGTimestamptz(base.Add(time.Duration(tc.minute) * time.Minute))})
		require.NoError(t, err)
		ids = append(ids, id)
	}
	source := evaluation.NewRepository(ti.conn)
	rows, err := source.LoadMessages(ctx, auth.ActiveOrganizationID, project, []uuid.UUID{ids[4]})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	target := rows[0].ChatMessage
	history, err := source.LoadHistory(ctx, auth.ActiveOrganizationID, project, target, 9)
	require.NoError(t, err)
	require.Len(t, history, 4)
	for i, want := range []uuid.UUID{ids[3], ids[0], ids[2], ids[1]} {
		require.Equal(t, want, history[i].ChatMessage.ID)
	}
	history, err = source.LoadHistory(ctx, auth.ActiveOrganizationID, project, target, 1)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, ids[3], history[0].ChatMessage.ID)
	history, err = source.LoadHistory(ctx, "another-org", project, target, 9)
	require.NoError(t, err)
	require.Empty(t, history)
	history, err = source.LoadHistory(ctx, auth.ActiveOrganizationID, uuid.New(), target, 9)
	require.NoError(t, err)
	require.Empty(t, history)
	// A late-arriving predecessor belongs in the next evaluation, even with a newer sequence.
	late, err := queries.CreateExternalChatMessage(ctx, chatrepo.CreateExternalChatMessageParams{ID: uuid.New(), ChatID: chat, ProjectID: project, Role: "tool", Content: "late context", CreatedAt: conv.ToPGTimestamptz(base.Add(3*time.Minute + 30*time.Second))})
	require.NoError(t, err)
	history, err = source.LoadHistory(ctx, auth.ActiveOrganizationID, project, target, 9)
	require.NoError(t, err)
	require.Len(t, history, 5)
	require.Equal(t, late, history[0].ChatMessage.ID)
	require.NoError(t, fixtures.ForceSoftDeleteChat(ctx, chat))
	history, err = source.LoadHistory(ctx, auth.ActiveOrganizationID, project, target, 9)
	require.NoError(t, err)
	require.Empty(t, history)
}
