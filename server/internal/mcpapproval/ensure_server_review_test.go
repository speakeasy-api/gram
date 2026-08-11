package mcpapproval_test

import (
	"strings"
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

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPApprovalRequestCreate)
	require.NoError(t, err)

	dossier, err := ti.service.EnsureServerReview(ctx, ensurePayload("https://dossier.example.com/mcp"))
	require.NoError(t, err)

	require.Equal(t, "unreviewed", dossier.Status)
	require.Equal(t, "server_url", dossier.TargetKind)
	require.Equal(t, 0, dossier.RequesterCount)

	// The first open inserts the dossier row, and that insert audits a
	// create attributed to whoever opened the page.
	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPApprovalRequestCreate)
	require.NoError(t, err)
	require.Equal(t, before+1, after, "a fresh dossier insert audits a create")

	entry, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionMCPApprovalRequestCreate)
	require.NoError(t, err)
	require.Equal(t, ti.authContext.UserID, entry.ActorID)
	require.Equal(t, dossier.TargetRaw, entry.SubjectDisplay)

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

// EnsureServerReview must never move a row's status, whatever state the row
// is in. The rows are seeded WITHOUT evidence so the resolve falls all the way
// through to the upsert — the early return for gathered evidence must not be
// what protects a decided row. Changing the upsert's status CASE to take the
// incoming status unconditionally fails this test: every row would become
// unreviewed.
func TestEnsureServerReview_NeverDowngradesADecidedRow(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	for _, status := range []string{"requested", "approved", "denied"} {
		target := "https://" + status + ".downgrade.example.com/mcp"
		requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
			targetKey: target,
			status:    status,
			evidence:  "",
			version:   0,
		})

		ensured, err := ti.service.EnsureServerReview(ctx, ensurePayload(target))
		require.NoError(t, err)
		require.Equal(t, requestID.String(), ensured.ID, "the existing %s row is reused", status)
		require.Equal(t, status, ensured.Status, "an incoming dossier must not move a %s row", status)
		require.Equal(t, status, requestStatus(t, ctx, ti, ti.projectID, requestID))
	}
}

// A dossier whose stored document recorded source gaps — gathered during a
// registry or ClickHouse outage — re-gathers on the next view instead of
// keeping its gaps forever. The refresh is still the read path: it writes no
// audit entry and never moves the row's status. TestEnsureServerReview_
// RepeatResolveIsAPlainRead pins the complementary property: a complete
// document is never re-gathered, so a gapped retry can never replace a richer
// document — the fresh document is stored only when it closed every gap.
func TestEnsureServerReview_GappedDossierRetriesTheGather(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	target := "https://gapped.example.com/mcp"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
		targetKey: target,
		status:    "unreviewed",
		evidence:  `{"identity":{"kind":"remote","version_pinned":false},"gaps":["exposure_lookup_failed"]}`,
		version:   1,
	})

	seeded, err := ti.repo.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{ID: requestID, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.True(t, seeded.EvidenceCollectedAt.Valid, "the gapped gather still stamped a collection time")

	audits, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPApprovalRequestCreate)
	require.NoError(t, err)

	ensured, err := ti.service.EnsureServerReview(ctx, ensurePayload(target))
	require.NoError(t, err)
	require.Equal(t, requestID.String(), ensured.ID)
	require.Equal(t, "unreviewed", ensured.Status, "the refresh never moves the status")

	refreshed, err := ti.repo.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{ID: requestID, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.True(t, refreshed.EvidenceCollectedAt.Time.After(seeded.EvidenceCollectedAt.Time), "the gather is re-run")

	detail, err := ti.service.GetRequest(ctx, getPayload(requestID.String()))
	require.NoError(t, err)
	doc, ok := detail.Evidence.(map[string]any)
	require.True(t, ok)
	require.NotContains(t, doc, "gaps", "the refreshed document closed the gaps")

	auditsAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionMCPApprovalRequestCreate)
	require.NoError(t, err)
	require.Equal(t, audits, auditsAfter, "the refresh path must not audit a create")
}

func TestEnsureServerReview_RejectsNonURLTargets(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.EnsureServerReview(ctx, ensurePayload("npx -y some-package"))
	requireOopsCode(t, err, oops.CodeBadRequest)

	// The target is bounded, mirroring the design-level MaxLength.
	_, err = ti.service.EnsureServerReview(ctx, ensurePayload("https://long.example.com/"+strings.Repeat("a", 2100)))
	requireOopsCode(t, err, oops.CodeBadRequest)
}
