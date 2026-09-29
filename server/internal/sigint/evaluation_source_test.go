package sigint_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/sigint"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/sigint/evaluation"
)

func TestEvaluationSourceScopesAndOrdersDefinitions(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	a := createSignal(t, ctx, ti, "first")
	b := createSignal(t, ctx, ti, "second")
	sensor := createSensor(t, ctx, ti, "sensor", "ordered_score", b.ID, a.ID)
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	source := evaluation.NewRepository(ti.conn)
	rows, err := source.Load(ctx, auth.ActiveOrganizationID, uuid.MustParse(sensor.ProjectID))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, b.ID, string(rows[0].Signals[0].Key))
	require.Equal(t, a.ID, string(rows[0].Signals[1].Key))
	rows, err = source.Load(ctx, "different-organization", uuid.MustParse(sensor.ProjectID))
	require.NoError(t, err)
	require.Empty(t, rows)
	_, err = ti.service.DeleteSignal(ctx, &gen.DeleteSignalPayload{ID: b.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil})
	require.NoError(t, err)
	rows, err = source.Load(ctx, auth.ActiveOrganizationID, uuid.MustParse(sensor.ProjectID))
	require.NoError(t, err)
	require.Len(t, rows[0].Signals, 1)
	require.Equal(t, a.ID, string(rows[0].Signals[0].Key))
}
