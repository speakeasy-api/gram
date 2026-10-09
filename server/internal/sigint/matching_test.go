package sigint_test

import (
	"strings"
	"testing"

	gen "github.com/speakeasy-api/gram/server/gen/sigint"
	"github.com/speakeasy-api/gram/server/internal/oops"
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
	requireOopsCode(t, err, oops.CodeBadRequest)

	current, err := ti.service.GetSensor(ctx, &gen.GetSensorPayload{ID: sensor.ID})
	require.NoError(t, err)
	require.Equal(t, updated.MatchExpression, current.MatchExpression)
}

func TestSensorMatchExpressionUTF8ByteLimit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	expression := `message.role == "` + strings.Repeat("界", 1359) + `a"`
	require.Len(t, expression, matching.MaxExpressionBytes)

	sensor, err := ti.service.CreateSensor(ctx, &gen.CreateSensorPayload{Name: "Unicode matching", Mode: "multi_label", MatchExpression: &expression})
	require.NoError(t, err)
	require.Equal(t, expression, sensor.MatchExpression)

	expression += " "
	_, err = ti.service.CreateSensor(ctx, &gen.CreateSensorPayload{Name: "Oversized matching", Mode: "multi_label", MatchExpression: &expression})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{ID: sensor.ID, MatchExpression: &expression})
	requireOopsCode(t, err, oops.CodeBadRequest)

	current, err := ti.service.GetSensor(ctx, &gen.GetSensorPayload{ID: sensor.ID})
	require.NoError(t, err)
	require.Equal(t, sensor.MatchExpression, current.MatchExpression)
}
