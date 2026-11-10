package sigint_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/sigint"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/sigint/evaluation"
)

func TestDisabledSensorExcludedFromEvaluation(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	sensor := createSensor(t, ctx, ti, "evaluation toggle", "multi_label")
	auth, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	source := evaluation.NewRepository(ti.conn)

	loaded, err := source.Load(ctx, auth.ActiveOrganizationID, *auth.ProjectID, evaluation.ConversationMessageKind)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Equal(t, sensor.ID, loaded[0].ID)

	_, err = ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{ID: sensor.ID, Enabled: new(false)})
	require.NoError(t, err)
	loaded, err = source.Load(ctx, auth.ActiveOrganizationID, *auth.ProjectID, evaluation.ConversationMessageKind)
	require.NoError(t, err)
	require.Empty(t, loaded)
	require.False(t, getSensor(t, ctx, ti, sensor.ID).Enabled)

	_, err = ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{ID: sensor.ID, Enabled: new(true)})
	require.NoError(t, err)
	loaded, err = source.Load(ctx, auth.ActiveOrganizationID, *auth.ProjectID, evaluation.ConversationMessageKind)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
}
