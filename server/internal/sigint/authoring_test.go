package sigint_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/sigint"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/sigint"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func TestAtomicAuthoringPreviewAndCommit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	input := sigint.AuthoringInput{Operation: "create_sensor", Name: new("Support topics"), Mode: new("multi_label"), Instructions: new("Identify support topics"), Signals: &[]sigint.SignalReference{{New: &sigint.SignalDefinition{Name: "Billing", Criteria: new("Questions about invoices")}}}}
	preview, version, err := ti.service.PreviewAuthoring(ctx, input)
	require.NoError(t, err)
	require.Equal(t, "support-topics", string(preview.Sensor.Slug))
	require.Len(t, preview.Sensor.SignalIds, 1)
	state, err := ti.service.ReadAuthoringState(ctx)
	require.NoError(t, err)
	require.Equal(t, version, state.Version)
	require.Empty(t, state.Sensors)
	require.Empty(t, state.Signals)
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSigintSignalCreate)
	require.NoError(t, err)
	require.Zero(t, count)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	result, err := ti.service.AuthorInTransaction(ctx, tx, input, version)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	state, err = ti.service.ReadAuthoringState(ctx)
	require.NoError(t, err)
	require.Len(t, state.Sensors, 1)
	require.Len(t, state.Signals, 1)
	require.Equal(t, state.Signals[0].ID, result.Sensor.SignalIds[0])
	count, err = audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSigintSignalCreate)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	count, err = audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSigintSensorCreate)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}

func TestAtomicAuthoringFailureRollsBackInlineSignals(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	state, err := ti.service.ReadAuthoringState(ctx)
	require.NoError(t, err)
	input := sigint.AuthoringInput{Operation: "create_sensor", Name: new("Invalid bundle"), Mode: new("multi_label"), Signals: &[]sigint.SignalReference{{New: &sigint.SignalDefinition{Name: "Must roll back"}}, {ID: uuid.NewString()}}}
	tx := testenv.BeginTx(t, ctx, ti.conn)
	_, err = ti.service.AuthorInTransaction(ctx, tx, input, state.Version)
	require.Error(t, err)
	require.NoError(t, tx.Rollback(ctx))
	after, err := ti.service.ReadAuthoringState(ctx)
	require.NoError(t, err)
	require.Equal(t, state, after)
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSigintSignalCreate)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestAuthoringRejectsSharedDefinitionChangeSincePreview(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	signal := createSignal(t, ctx, ti, "Shared classification")
	input := sigint.AuthoringInput{Operation: "create_sensor", Name: new("Shared sensor"), Mode: new("multi_label"), Signals: &[]sigint.SignalReference{{ID: signal.ID}}}
	_, version, err := ti.service.PreviewAuthoring(ctx, input)
	require.NoError(t, err)
	_, err = ti.service.UpdateSignal(ctx, &gen.UpdateSignalPayload{ID: signal.ID, ClassifierCriteria: new("Changed criteria")})
	require.NoError(t, err)
	tx := testenv.BeginTx(t, ctx, ti.conn)
	_, err = ti.service.AuthorInTransaction(ctx, tx, input, version)
	requireOopsCode(t, err, oops.CodeConflict)
	require.NoError(t, tx.Rollback(ctx))
}

func TestAuthoringCanClearSensorMembership(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	signal := createSignal(t, ctx, ti, "Classification")
	sensor := createSensor(t, ctx, ti, "Sensor", "multi_label", signal.ID)
	input := sigint.AuthoringInput{Operation: "update_sensor", ID: sensor.ID, Signals: &[]sigint.SignalReference{}}
	preview, _, err := ti.service.PreviewAuthoring(ctx, input)
	require.NoError(t, err)
	require.Empty(t, preview.Sensor.SignalIds)
	require.Len(t, getSensor(t, ctx, ti, sensor.ID).SignalIds, 1)
}
