package sigint_test

import (
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/sigint"
	"github.com/speakeasy-api/gram/server/internal/sigint/matching"
	"github.com/stretchr/testify/require"
)

func TestSensorMatchExpressionLifecycle(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	sensor := createSensor(t, ctx, ti, "Role matching", "multi_label")
	require.Equal(t, matching.DefaultExpression, sensor.MatchExpression)
	updated, err := ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{ID: sensor.ID, MatchExpression: new(`message.role == "assistant"`)})
	require.NoError(t, err)
	require.Equal(t, `message.role == "assistant"`, updated.MatchExpression)
	_, err = ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{ID: sensor.ID, MatchExpression: new(`message.role`)})
	require.Error(t, err)
	current, err := ti.service.GetSensor(ctx, &gen.GetSensorPayload{ID: sensor.ID})
	require.NoError(t, err)
	require.Equal(t, updated.MatchExpression, current.MatchExpression)
}
