package sigint_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/sigint"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func TestDeleteSignalDetachesEverySensorAndAuditsOrder(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	calm := createSignal(t, ctx, ti, "calm")
	frustrated := createSignal(t, ctx, ti, "frustrated")
	angry := createSignal(t, ctx, ti, "angry")
	scale := createSensor(t, ctx, ti, "severity", "ordered_score", calm.ID, frustrated.ID, angry.ID)
	choice := createSensor(t, ctx, ti, "disposition", "exclusive", frustrated.ID, calm.ID)
	beforeCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSigintSensorUpdate)
	require.NoError(t, err)

	_, err = ti.service.DeleteSignal(ctx, &gen.DeleteSignalPayload{
		ID: frustrated.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, []string{calm.ID, angry.ID}, getSensor(t, ctx, ti, scale.ID).SignalIds)
	require.Equal(t, []string{calm.ID}, getSensor(t, ctx, ti, choice.ID).SignalIds)
	_, err = ti.service.GetSignal(ctx, &gen.GetSignalPayload{
		ID: frustrated.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	afterCount, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSigintSensorUpdate)
	require.NoError(t, err)
	require.Equal(t, beforeCount+2, afterCount)
	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionSigintSensorUpdate)
	require.NoError(t, err)
	var before, after struct {
		SignalIds []string `json:"SignalIds"`
	}
	require.NoError(t, json.Unmarshal(entry.BeforeSnapshot, &before))
	require.NoError(t, json.Unmarshal(entry.AfterSnapshot, &after))
	original := map[string][]string{scale.ID: scale.SignalIds, choice.ID: choice.SignalIds}
	require.Equal(t, original[entry.SubjectID], before.SignalIds)
	require.Equal(t, getSensor(t, ctx, ti, entry.SubjectID).SignalIds, after.SignalIds)
}

func TestDeleteSignalRollsBackWhenSensorAuditFails(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	signal := createSignal(t, ctx, ti, "api")
	first := createSensor(t, ctx, ti, "services", "multi_label", signal.ID)
	second := createSensor(t, ctx, ti, "routing", "exclusive", signal.ID)
	require.NoError(t, audittest.RejectAction(ctx, ti.conn, audit.ActionSigintSensorUpdate))

	_, err := ti.service.DeleteSignal(ctx, &gen.DeleteSignalPayload{
		ID: signal.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeUnexpected)
	stored, err := ti.service.GetSignal(ctx, &gen.GetSignalPayload{
		ID: signal.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, signal.ID, stored.ID)
	require.Equal(t, []string{signal.ID}, getSensor(t, ctx, ti, first.ID).SignalIds)
	require.Equal(t, []string{signal.ID}, getSensor(t, ctx, ti, second.ID).SignalIds)
	count, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSigintSignalDelete)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestDeleteSensorPreservesSharedSignalsAndOtherSensors(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	signal := createSignal(t, ctx, ti, "campaign")
	removed := createSensor(t, ctx, ti, "marketing", "multi_label", signal.ID)
	retained := createSensor(t, ctx, ti, "attribution", "exclusive", signal.ID)

	_, err := ti.service.DeleteSensor(ctx, &gen.DeleteSensorPayload{
		ID: removed.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	_, err = ti.service.GetSensor(ctx, &gen.GetSensorPayload{
		ID: removed.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	requireOopsCode(t, err, oops.CodeNotFound)
	stored, err := ti.service.GetSignal(ctx, &gen.GetSignalPayload{
		ID: signal.ID, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.Equal(t, signal.Name, stored.Name)
	require.Equal(t, []string{signal.ID}, getSensor(t, ctx, ti, retained.ID).SignalIds)
}
