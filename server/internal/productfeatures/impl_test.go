package productfeatures_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/features"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
)

func TestService_SetAndListSessionCaptureExclusions(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestProductFeaturesService(t)

	// set {u1, u2} — payload has duplicate u1 and empty string; should be normalized.
	res, err := ti.service.SetSessionCaptureExclusions(ctx, &gen.SetSessionCaptureExclusionsPayload{UserIds: []string{ti.u2, ti.u1, ti.u1, ""}})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{ti.u1, ti.u2}, res.UserIds) // de-duped, empties dropped

	got, err := ti.service.ListSessionCaptureExclusions(ctx, &gen.ListSessionCaptureExclusionsPayload{})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{ti.u1, ti.u2}, got.UserIds)

	// audit: two adds emitted (u1 and u2 both newly added)
	adds, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSessionCaptureExclusionAdd)
	require.NoError(t, err)
	require.EqualValues(t, 2, adds)

	// shrink to {u1}: one remove for u2, zero new adds (u1 unchanged)
	_, err = ti.service.SetSessionCaptureExclusions(ctx, &gen.SetSessionCaptureExclusionsPayload{UserIds: []string{ti.u1}})
	require.NoError(t, err)

	rems, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSessionCaptureExclusionRemove)
	require.NoError(t, err)
	require.EqualValues(t, 1, rems)

	addsAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionSessionCaptureExclusionAdd)
	require.NoError(t, err)
	require.EqualValues(t, 2, addsAfter) // u1 unchanged → no new add
}
