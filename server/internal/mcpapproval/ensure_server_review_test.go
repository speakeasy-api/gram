package mcpapproval_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_approval"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func ensurePayload(target string) *gen.EnsureServerReviewPayload {
	return &gen.EnsureServerReviewPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		Target: target,
	}
}

func TestEnsureServerReview_OpensDossierWithoutRequester(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	dossier, err := ti.service.EnsureServerReview(ctx, ensurePayload("https://dossier.example.com/mcp"))
	require.NoError(t, err)

	require.Equal(t, "unreviewed", dossier.Status)
	require.Equal(t, "server_url", dossier.TargetKind)
	require.Equal(t, 0, dossier.RequesterCount)

	detail, err := ti.service.GetRequest(ctx, getPayload(dossier.ID))
	require.NoError(t, err)
	require.Empty(t, detail.Requesters)
	require.Empty(t, detail.Decisions)

	// Re-ensuring resolves the same dossier rather than opening a second.
	again, err := ti.service.EnsureServerReview(ctx, ensurePayload("https://dossier.example.com/mcp"))
	require.NoError(t, err)
	require.Equal(t, dossier.ID, again.ID)
	require.Equal(t, "unreviewed", again.Status)
}

// The server page calls EnsureServerReview on every view, so a repeat resolve
// of a dossier that already has gathered evidence must behave as a plain read:
// no evidence re-gather, no row touch, and no create audit entry.
func TestEnsureServerReview_RepeatResolveIsAPlainRead(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	dossier, err := ti.service.EnsureServerReview(ctx, ensurePayload("https://repeat.example.com/mcp"))
	require.NoError(t, err)

	first, err := ti.repo.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{ID: uuid.MustParse(dossier.ID), ProjectID: ti.projectID})
	require.NoError(t, err)
	require.True(t, first.EvidenceCollectedAt.Valid, "first open gathers evidence")

	audits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPApprovalRequestCreate)
	require.NoError(t, err)

	again, err := ti.service.EnsureServerReview(ctx, ensurePayload("https://repeat.example.com/mcp"))
	require.NoError(t, err)
	require.Equal(t, dossier.ID, again.ID)

	after, err := ti.repo.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{ID: first.ID, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.Equal(t, first.EvidenceCollectedAt.Time, after.EvidenceCollectedAt.Time, "repeat resolve must not re-gather evidence")
	require.Equal(t, first.UpdatedAt.Time, after.UpdatedAt.Time, "repeat resolve must not touch the row")

	auditsAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPApprovalRequestCreate)
	require.NoError(t, err)
	require.Equal(t, audits, auditsAfter, "repeat resolve must not audit a create")
}

// A resolve that lands on a dossier row that already exists but has no
// evidence — a previous gather failed, or a concurrent first open won the
// insert — retries the gather on the same row without auditing a second
// create for it.
func TestEnsureServerReview_ReusedRowRetriesGatherWithoutDuplicateAudit(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	target := "https://retry.example.com/mcp"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
		targetKey: target,
		status:    "unreviewed",
		evidence:  "",
		version:   0,
	})

	audits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPApprovalRequestCreate)
	require.NoError(t, err)

	ensured, err := ti.service.EnsureServerReview(ctx, ensurePayload(target))
	require.NoError(t, err)
	require.Equal(t, requestID.String(), ensured.ID, "the existing row is reused")

	row, err := ti.repo.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{ID: requestID, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.True(t, row.EvidenceCollectedAt.Valid, "the gather is retried on the reused row")

	auditsAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPApprovalRequestCreate)
	require.NoError(t, err)
	require.Equal(t, audits, auditsAfter, "a reused row must not audit a create that did not happen")
}

func TestEnsureServerReview_StaysOutOfTheQueue(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.EnsureServerReview(ctx, ensurePayload("https://quiet.example.com/mcp"))
	require.NoError(t, err)

	byDefault, err := ti.service.ListRequests(ctx, listPayload())
	require.NoError(t, err)
	require.Empty(t, byDefault.Requests)

	explicit := listPayload()
	explicit.Status = conv.PtrEmpty("unreviewed")
	named, err := ti.service.ListRequests(ctx, explicit)
	require.NoError(t, err)
	require.Len(t, named.Requests, 1)
}

func TestEnsureServerReview_RealAskUpgradesTheDossier(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	dossier, err := ti.service.EnsureServerReview(ctx, ensurePayload("https://upgrade.example.com/mcp"))
	require.NoError(t, err)

	asked, err := ti.service.CreateRequest(ctx, createPayload("server_url", "https://upgrade.example.com/mcp", "need it for release notes"))
	require.NoError(t, err)
	require.Equal(t, dossier.ID, asked.ID)
	require.Equal(t, "requested", asked.Status)
	require.Equal(t, 1, asked.RequesterCount)

	queued, err := ti.service.ListRequests(ctx, listPayload())
	require.NoError(t, err)
	require.Len(t, queued.Requests, 1)
}

func TestEnsureServerReview_NeverDowngradesADecidedRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	asked, err := ti.service.CreateRequest(ctx, createPayload("server_url", "https://decided.example.com/mcp", "need it"))
	require.NoError(t, err)

	ensured, err := ti.service.EnsureServerReview(ctx, ensurePayload("https://decided.example.com/mcp"))
	require.NoError(t, err)
	require.Equal(t, asked.ID, ensured.ID)
	require.Equal(t, "requested", ensured.Status)
}

func TestEnsureServerReview_RejectsNonURLTargets(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.EnsureServerReview(ctx, ensurePayload("npx -y some-package"))
	requireOopsCode(t, err, oops.CodeBadRequest)
}
