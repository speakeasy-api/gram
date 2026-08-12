package mcpapproval_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
)

const driftEvidence = `{"identity": {"kind": "server_url"}, "authority": {"mode": "oauth", "scopes": ["read:messages"]}}`

func pgNow(offset time.Duration) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().Add(offset), Valid: true, InfinityModifier: pgtype.Finite}
}

// approveSeededRequest raises a request, decides it, and returns its id — the
// state every drift flag is written against.
func approveSeededRequest(t *testing.T, ctx context.Context, ti *testInstance, evidence string) uuid.UUID {
	t.Helper()

	id := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
		targetKey: "", status: "requested", evidence: evidence, version: 1,
	})

	payload := decisionPayload(id.String(), "approved")
	payload.Rationale = "narrow read scope at decision time"
	_, err := ti.service.RecordDecision(ctx, payload)
	require.NoError(t, err)

	return id
}

// Recording a decision is the only thing that clears an outstanding
// evidence-change flag: the fresh snapshot it freezes is the answer to the
// drift.
func TestRecordDecision_ClearsEvidenceChangeFlag(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	requestID := approveSeededRequest(t, ctx, ti, driftEvidence)

	flagged, err := ti.repo.MarkApprovalRequestEvidenceChanged(ctx, repo.MarkApprovalRequestEvidenceChangedParams{
		ID:                 requestID,
		ProjectID:          ti.projectID,
		Fingerprint:        conv.ToPGText("fp-drift-1"),
		ComparedDecisionAt: pgNow(0),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), flagged)

	detail, err := ti.service.GetRequest(ctx, getPayload(requestID.String()))
	require.NoError(t, err)
	require.NotNil(t, detail.Request.EvidenceChangedAt)

	payload := decisionPayload(requestID.String(), "approved")
	payload.Rationale = "re-reviewed after the drift flag"
	_, err = ti.service.RecordDecision(ctx, payload)
	require.NoError(t, err)

	cleared, err := ti.service.GetRequest(ctx, getPayload(requestID.String()))
	require.NoError(t, err)
	require.Nil(t, cleared.Request.EvidenceChangedAt)

	row, err := ti.repo.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{ID: requestID, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.False(t, row.NotifiedChangeFingerprint.Valid, "the announce-once fingerprint clears with the flag")
}

// The flag write is the announce-once arbiter: a fingerprint that already
// matches writes nothing, so an activity retry cannot emit a second webhook
// for a drift that was already announced.
func TestMarkEvidenceChanged_IsIdempotentPerFingerprint(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	requestID := approveSeededRequest(t, ctx, ti, driftEvidence)

	params := repo.MarkApprovalRequestEvidenceChangedParams{
		ID:                 requestID,
		ProjectID:          ti.projectID,
		Fingerprint:        conv.ToPGText("fp-same"),
		ComparedDecisionAt: pgNow(0),
	}

	first, err := ti.repo.MarkApprovalRequestEvidenceChanged(ctx, params)
	require.NoError(t, err)
	require.Equal(t, int64(1), first)

	again, err := ti.repo.MarkApprovalRequestEvidenceChanged(ctx, params)
	require.NoError(t, err)
	require.Equal(t, int64(0), again, "the same drift is not news twice")

	// A materially different drift is news again, and keeps the original
	// first-noticed time.
	before, err := ti.repo.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{ID: requestID, ProjectID: ti.projectID})
	require.NoError(t, err)

	params.Fingerprint = conv.ToPGText("fp-different")
	third, err := ti.repo.MarkApprovalRequestEvidenceChanged(ctx, params)
	require.NoError(t, err)
	require.Equal(t, int64(1), third)

	after, err := ti.repo.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{ID: requestID, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.Equal(t, before.EvidenceChangedAt.Time, after.EvidenceChangedAt.Time, "first noticed survives a later drift")
}

// A sweep whose comparison predates a decision must not re-flag: the admin
// already answered this drift, and nothing but a decision clears the flag,
// so a stale write would strand it permanently.
func TestMarkEvidenceChanged_RefusesAfterANewerDecision(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	requestID := approveSeededRequest(t, ctx, ti, driftEvidence)

	// The sweep read its snapshot an hour ago; the admin decided since.
	comparedAt := pgNow(-time.Hour)

	flagged, err := ti.repo.MarkApprovalRequestEvidenceChanged(ctx, repo.MarkApprovalRequestEvidenceChangedParams{
		ID:                 requestID,
		ProjectID:          ti.projectID,
		Fingerprint:        conv.ToPGText("fp-stale"),
		ComparedDecisionAt: comparedAt,
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), flagged)

	row, err := ti.repo.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{ID: requestID, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.False(t, row.EvidenceChangedAt.Valid)
}

// A request that is no longer approved has no standing approval to drift
// from, so the flag write declines it.
func TestMarkEvidenceChanged_RefusesUnapprovedRequests(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
		targetKey: "", status: "requested", evidence: driftEvidence, version: 1,
	})

	flagged, err := ti.repo.MarkApprovalRequestEvidenceChanged(ctx, repo.MarkApprovalRequestEvidenceChangedParams{
		ID:                 requestID,
		ProjectID:          ti.projectID,
		Fingerprint:        conv.ToPGText("fp-pending"),
		ComparedDecisionAt: pgNow(0),
	})
	require.NoError(t, err)
	require.Equal(t, int64(0), flagged)
}

// The recheck loads both comparison sides itself, and takes the newest
// decision's snapshot — not the first one ever recorded.
func TestGetApprovalRequestForRecheck_UsesNewestDecision(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	requestID := approveSeededRequest(t, ctx, ti, driftEvidence)

	// A second decision on a widened gather is what a recheck must compare
	// against.
	widened := `{"identity": {"kind": "server_url"}, "authority": {"mode": "oauth", "scopes": ["read:messages", "write:messages"]}}`
	seedEvidence(t, ctx, ti, ti.projectID, requestID, widened, 1)

	payload := decisionPayload(requestID.String(), "approved")
	payload.Rationale = "accepted the write scope"
	newest, err := ti.service.RecordDecision(ctx, payload)
	require.NoError(t, err)

	row, err := ti.repo.GetApprovalRequestForRecheck(ctx, repo.GetApprovalRequestForRecheckParams{
		ID:        requestID,
		ProjectID: ti.projectID,
	})
	require.NoError(t, err)
	require.JSONEq(t, widened, string(row.DecisionEvidenceSnapshot))
	require.Equal(t, newest.DecidedAt, row.DecisionDecidedAt.Time.Format(time.RFC3339))
}

// The scan is global by design; the per-request load is not. A request id
// from another project resolves to nothing.
func TestGetApprovalRequestForRecheck_IsProjectScoped(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	requestID := approveSeededRequest(t, ctx, ti, driftEvidence)

	otherProject := createProject(t, ctx, ti.conn, ti.organizationID)
	_, err := ti.repo.GetApprovalRequestForRecheck(ctx, repo.GetApprovalRequestForRecheckParams{
		ID:        requestID,
		ProjectID: otherProject,
	})
	require.Error(t, err)
}

// The detail's diff compares the latest decision's frozen snapshot against
// the current gather on read, so drift is visible even before the daily
// sweep flags it.
func TestGetRequest_EvidenceDiffAfterDrift(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	requestID := approveSeededRequest(t, ctx, ti, driftEvidence)

	// The server's published authority widens after the approval.
	seedEvidence(t, ctx, ti, ti.projectID, requestID,
		`{"identity": {"kind": "server_url"}, "authority": {"mode": "oauth", "scopes": ["read:messages", "write:messages"]}}`, 1)

	detail, err := ti.service.GetRequest(ctx, getPayload(requestID.String()))
	require.NoError(t, err)
	require.NotNil(t, detail.EvidenceDiff)
	require.True(t, detail.EvidenceDiff.Changed)
	require.Equal(t, []string{"write:messages"}, detail.EvidenceDiff.ScopesAdded)
	require.Empty(t, detail.EvidenceDiff.ScopesRemoved)
}

// With no decision there is nothing to compare against: the section is
// absent rather than an empty diff pretending nothing moved.
func TestGetRequest_NoDiffWithoutDecision(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
		targetKey: "", status: "requested",
		evidence: `{"identity": {"kind": "server_url"}}`, version: 1,
	})

	detail, err := ti.service.GetRequest(ctx, getPayload(requestID.String()))
	require.NoError(t, err)
	require.Nil(t, detail.EvidenceDiff)
}
