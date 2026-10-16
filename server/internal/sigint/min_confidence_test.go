package sigint_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/sigint"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestSensorMinConfidenceRoundTripClearAndAudit(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	sensor := createSensor(t, ctx, ti, "Off by default", "multi_label")
	require.Nil(t, sensor.MinConfidence)

	created, err := ti.service.CreateSensor(ctx, &gen.CreateSensorPayload{Name: "Strict", Mode: "exclusive", MinConfidence: new(0.6)})
	require.NoError(t, err)
	require.InDelta(t, 0.6, *created.MinConfidence, 1e-9)

	updated, err := ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{ID: created.ID, Name: new("Renamed")})
	require.NoError(t, err)
	require.InDelta(t, 0.6, *updated.MinConfidence, 1e-9, "omission preserves the value")

	updated, err = ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{ID: created.ID, MinConfidence: new(0.8)})
	require.NoError(t, err)
	require.InDelta(t, 0.8, *updated.MinConfidence, 1e-9)

	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSigintSensorUpdate)
	require.NoError(t, err)
	before, err := audittest.DecodeAuditData(entry.BeforeSnapshot)
	require.NoError(t, err)
	after, err := audittest.DecodeAuditData(entry.AfterSnapshot)
	require.NoError(t, err)
	require.InDelta(t, 0.6, before["MinConfidence"], 1e-9)
	require.InDelta(t, 0.8, after["MinConfidence"], 1e-9)

	updated, err = ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{ID: created.ID, ClearMinConfidence: new(true)})
	require.NoError(t, err)
	require.Nil(t, updated.MinConfidence)
	require.Equal(t, updated, getSensor(t, ctx, ti, created.ID))

	listed, err := ti.service.ListSensors(ctx, &gen.ListSensorsPayload{Limit: 50})
	require.NoError(t, err)
	for _, s := range listed.Sensors {
		require.Nil(t, s.MinConfidence)
	}
}

func TestSensorMinConfidenceRejectsInvalidValuesAtomically(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	sensor, err := ti.service.CreateSensor(ctx, &gen.CreateSensorPayload{Name: "Bounded", Mode: "multi_label", MinConfidence: new(0.5)})
	require.NoError(t, err)

	for _, value := range []float64{-0.01, 1.01, math.NaN(), math.Inf(1)} {
		_, err := ti.service.CreateSensor(ctx, &gen.CreateSensorPayload{Name: "Invalid", Mode: "multi_label", MinConfidence: new(value)})
		requireOopsCode(t, err, oops.CodeBadRequest)
		_, err = ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{ID: sensor.ID, Name: new("Must not persist"), MinConfidence: new(value)})
		requireOopsCode(t, err, oops.CodeBadRequest)
	}

	_, err = ti.service.UpdateSensor(ctx, &gen.UpdateSensorPayload{ID: sensor.ID, MinConfidence: new(0.7), ClearMinConfidence: new(true)})
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.Equal(t, sensor, getSensor(t, ctx, ti, sensor.ID))
}

func TestSensorMinConfidenceBoundariesAreAccepted(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	for name, value := range map[string]float64{"Lower bound": 0, "Upper bound": 1} {
		created, err := ti.service.CreateSensor(ctx, &gen.CreateSensorPayload{Name: name, Mode: "multi_label", MinConfidence: new(value)})
		require.NoError(t, err)
		require.InDelta(t, value, *created.MinConfidence, 1e-9)
	}
}
