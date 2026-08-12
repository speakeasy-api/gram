package mcpapproval_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
)

// Recording a decision is the only thing that clears an outstanding
// evidence-change flag: the fresh snapshot it freezes is the answer to the
// drift.
func TestRecordDecision_ClearsEvidenceChangeFlag(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
		targetKey: "", status: "requested",
		evidence: `{"identity": {"kind": "server_url"}, "authority": {"mode": "oauth"}}`, version: 1,
	})

	require.NoError(t, ti.repo.MarkApprovalRequestEvidenceChanged(ctx, repo.MarkApprovalRequestEvidenceChangedParams{
		ID:          requestID,
		ProjectID:   ti.projectID,
		Fingerprint: conv.ToPGText("fp-drift-1"),
	}))

	flagged, err := ti.service.GetRequest(ctx, getPayload(requestID.String()))
	require.NoError(t, err)
	require.NotNil(t, flagged.Request.EvidenceChangedAt)

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

// The detail's diff compares the latest decision's frozen snapshot against
// the current gather on read, so drift is visible even before the daily
// sweep flags it.
func TestGetRequest_EvidenceDiffAfterDrift(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
		targetKey: "", status: "requested",
		evidence: `{"identity": {"kind": "server_url"}, "authority": {"mode": "oauth", "scopes": ["read:messages"]}}`,
		version:  1,
	})

	payload := decisionPayload(requestID.String(), "approved")
	payload.Rationale = "narrow read scope at decision time"
	_, err := ti.service.RecordDecision(ctx, payload)
	require.NoError(t, err)

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
