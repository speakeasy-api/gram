package mcpapproval_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

func newStaffReviews(t *testing.T, ti *testInstance) *mcpapproval.StaffReviews {
	t.Helper()
	return mcpapproval.NewStaffReviews(testenv.NewLogger(t), testenv.NewTracerProvider(t), ti.conn, ti.flags, audit.NewLogger(), ti.assembler, ti.research.start, ti.organizationID, ti.projectID)
}

// staffContext carries no customer session, as a staff request does not.
func staffContext(ctx context.Context) context.Context {
	return contextvalues.SetAuthContext(context.WithoutCancel(ctx), nil)
}

func TestStaffReviewsFindReportsNothingBeforeStart(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)

	review, err := newStaffReviews(t, ti).Find(staffContext(ctx), "https://mcp.example.com/mcp")

	require.NoError(t, err)
	require.Nil(t, review)
}

func TestStaffReviewsStartUsesTheOrdinaryReviewInTheHomeProject(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	reviews := newStaffReviews(t, ti)
	staffCtx := staffContext(ctx)
	createdBefore, err := audittest.AuditLogCountByAction(staffCtx, ti.conn, audit.ActionMCPApprovalRequestCreate)
	require.NoError(t, err)

	review, err := reviews.Start(staffCtx, "https://mcp.example.com/mcp", "synthetic-staff", nil)

	require.NoError(t, err)
	require.NotEmpty(t, review.RequestID)
	require.Equal(t, "running", review.Research.Status)
	runs := ti.research.started()
	require.Len(t, runs, 1)
	require.Equal(t, ti.organizationID, runs[0].OrgID)
	require.Equal(t, ti.projectID, runs[0].ProjectID)
	created, err := audittest.AuditLogCountByAction(staffCtx, ti.conn, audit.ActionMCPApprovalRequestCreate)
	require.NoError(t, err)
	require.Equal(t, createdBefore+1, created)
	event, err := audittest.LatestAuditLogByAction(staffCtx, ti.conn, audit.ActionMCPApprovalRequestResearchStart)
	require.NoError(t, err)
	require.Equal(t, ti.organizationID, event.OrganizationID)

	// A second start returns the run in flight rather than paying again.
	again, err := reviews.Start(staffCtx, "https://mcp.example.com/mcp", "synthetic-staff", nil)
	require.NoError(t, err)
	require.Equal(t, review.RequestID, again.RequestID)
	require.Len(t, ti.research.started(), 1)

	// Traffic in the home project says nothing about the source account.
	var evidence map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(review.Evidence, &evidence))
	require.NotContains(t, evidence, "exposure")

	found, err := reviews.Find(staffCtx, "https://mcp.example.com/mcp")
	require.NoError(t, err)
	require.Equal(t, review.RequestID, found.RequestID)
}

func TestStaffReviewsStartRespectsRolloutGates(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestService(t)
	ti.flags.SetFlag(feature.FlagMCPResearchKill, ti.organizationID, true)

	_, err := newStaffReviews(t, ti).Start(staffContext(ctx), "https://mcp.example.com/mcp", "synthetic-staff", nil)

	require.ErrorIs(t, err, mcpapproval.ErrStaffReviewDisabled)
	require.Empty(t, ti.research.started())
}
