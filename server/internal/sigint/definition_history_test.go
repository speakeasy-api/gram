package sigint_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/sigint"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/sigint/evaluation"
	"github.com/speakeasy-api/gram/server/internal/sigint/repo"
)

func TestEvaluatedDefinitionSnapshotsSurviveEdits(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	signal := createSignal(t, ctx, ti, "topic")
	sensor, err := ti.service.CreateSensor(ctx, &gen.CreateSensorPayload{Name: "topic sensor", Mode: "multi_label", SignalIds: []string{signal.ID}, Instructions: new("Identify the topic")})
	require.NoError(t, err)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	source := evaluation.NewRepository(ti.conn)
	loaded, err := source.Load(ctx, auth.ActiveOrganizationID, *auth.ProjectID, evaluation.ConversationMessageKind)
	require.NoError(t, err)
	first, ready := evaluation.Snapshot(loaded[0])
	require.True(t, ready)
	_, err = source.Load(ctx, auth.ActiveOrganizationID, *auth.ProjectID, evaluation.ConversationMessageKind)
	require.NoError(t, err)
	_, err = ti.service.UpdateSignal(ctx, &gen.UpdateSignalPayload{ID: signal.ID, ClassifierCriteria: new("Different question criteria")})
	require.NoError(t, err)
	loaded, err = source.Load(ctx, auth.ActiveOrganizationID, *auth.ProjectID, evaluation.ConversationMessageKind)
	require.NoError(t, err)
	second, ready := evaluation.Snapshot(loaded[0])
	require.True(t, ready)
	require.NotEqual(t, first.Hash, second.Hash)
	q := repo.New(ti.conn)
	rows, err := q.ListDefinitionSnapshots(ctx, repo.ListDefinitionSnapshotsParams{ProjectID: *auth.ProjectID, SensorID: uuid.MustParse(sensor.ID), LimitValue: 10})
	require.NoError(t, err)
	require.Len(t, rows, 2, "repeated evaluations must not duplicate definitions")
	saved, err := q.GetDefinitionSnapshot(ctx, repo.GetDefinitionSnapshotParams{ProjectID: *auth.ProjectID, OrganizationID: auth.ActiveOrganizationID, SensorID: uuid.MustParse(sensor.ID), DefinitionHash: first.Hash})
	require.NoError(t, err)
	require.JSONEq(t, string(first.Definition), string(saved.Definition))
	_, err = q.GetDefinitionSnapshot(ctx, repo.GetDefinitionSnapshotParams{ProjectID: *auth.ProjectID, OrganizationID: "other-organization", SensorID: uuid.MustParse(sensor.ID), DefinitionHash: first.Hash})
	require.Error(t, err)
}

func TestSensorActivationHistoryIncludesCreationAndDeletion(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	sensor := createSensor(t, ctx, ti, "activation", "multi_label")
	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSigintSensorCreate)
	require.NoError(t, err)
	after, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, true, after["Enabled"])
	_, err = ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{ID: sensor.ID, Enabled: new(false)})
	require.NoError(t, err)
	entry, err = audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSigintSensorUpdate)
	require.NoError(t, err)
	before, err := audittest.DecodeAuditData(entry.BeforeSnapshot)
	require.NoError(t, err)
	after, err = audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.Equal(t, true, before["Enabled"])
	require.Equal(t, false, after["Enabled"])
	_, err = ti.service.DeleteSensor(ctx, &gen.DeleteSensorPayload{ID: sensor.ID})
	require.NoError(t, err)
	entry, err = audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSigintSensorDelete)
	require.NoError(t, err)
	before, err = audittest.DecodeAuditData(entry.BeforeSnapshot)
	require.NoError(t, err)
	require.Equal(t, false, before["Enabled"])
}
