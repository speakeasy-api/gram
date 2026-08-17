package mcpapproval_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_approval"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func refreshPayload(id string) *gen.RefreshEvidencePayload {
	return &gen.RefreshEvidencePayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: id,
	}
}

// A refresh replaces the stored evidence with a fresh gather and returns the
// full detail, so the caller renders the new document without a second fetch.
func TestRefreshEvidence_ReplacesStoredEvidence(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	stale := `{"identity": {"kind": "remote"}, "note": "stale gather"}`
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
		targetKey: "https://mcp.example.com/refresh", status: "requested", evidence: stale, version: 1,
	})

	detail, err := ti.service.RefreshEvidence(ctx, refreshPayload(requestID.String()))
	require.NoError(t, err)

	require.Equal(t, requestID.String(), detail.Request.ID)
	require.NotNil(t, detail.EvidenceCollectedAt)

	decoded, ok := detail.Evidence.(map[string]any)
	require.True(t, ok, "refreshed evidence should decode to an object")
	require.NotContains(t, decoded, "note", "the stale document must be replaced, not merged")
	require.Contains(t, decoded, "identity", "the fresh gather carries a resolved identity")
}

// Two refreshes race the network for seconds, so the write is a compare-and-
// set against the gather that was current at the start: a gather that ran
// against a row someone else has since refreshed is discarded, and the
// concurrent write — not whichever request happened to finish last — is what
// stays stored and what the detail returns.
func TestRefreshEvidence_StaleGatherLosesToConcurrentWrite(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
		targetKey: "https://mcp.example.com/cas-race", status: "requested",
		evidence: `{"identity": {"kind": "remote"}, "note": "original"}`, version: 1,
	})

	// Fires mid-gather — after RefreshEvidence read the row, before it
	// writes — standing in for a concurrent refresh landing first.
	concurrent := `{"identity": {"kind": "remote"}, "note": "concurrent"}`
	ti.probes.onGather = func() {
		seedEvidence(t, ctx, ti, ti.projectID, requestID, concurrent, 1)
	}

	detail, err := ti.service.RefreshEvidence(ctx, refreshPayload(requestID.String()))
	require.NoError(t, err, "losing the race is not an error; the winner's evidence is returned")

	row, err := ti.repo.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{ID: requestID, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.JSONEq(t, concurrent, string(row.CurrentEvidence), "the stale gather must be discarded, not written over the concurrent refresh")

	decoded, ok := detail.Evidence.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "concurrent", decoded["note"], "the detail returns the winning evidence")
}

// A refresh that gapped on every remote source has learned nothing: writing
// it over a stored document that did consult those sources would replace real
// evidence with a page of failures, so the refresh reports the outage and
// keeps the stored document.
func TestRefreshEvidence_AllGapsGatherKeepsRicherStoredEvidence(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	stored := `{"identity": {"kind": "remote"}, "authority": {"mode": "none"}}`
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
		targetKey: "https://mcp.example.com/all-gaps", status: "requested",
		evidence: stored, version: 1,
	})
	ti.probes.fail = true

	_, err := ti.service.RefreshEvidence(ctx, refreshPayload(requestID.String()))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeUnexpected)

	row, err := ti.repo.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{ID: requestID, ProjectID: ti.projectID})
	require.NoError(t, err)
	require.JSONEq(t, stored, string(row.CurrentEvidence), "the richer stored document must survive an all-gaps refresh")
}

// With nothing better stored, an all-gaps gather still lands: honest gaps
// beat no evidence, and a later refresh can replace them.
func TestRefreshEvidence_AllGapsGatherLandsWhenNothingBetterIsStored(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
		targetKey: "https://mcp.example.com/all-gaps-first", status: "requested", evidence: "", version: 0,
	})
	ti.probes.fail = true

	detail, err := ti.service.RefreshEvidence(ctx, refreshPayload(requestID.String()))
	require.NoError(t, err)
	require.NotNil(t, detail.EvidenceCollectedAt)

	decoded, ok := detail.Evidence.(map[string]any)
	require.True(t, ok)
	gaps, ok := decoded["gaps"].([]any)
	require.True(t, ok)
	require.Contains(t, gaps, "authority_probe_failed")
	require.Contains(t, gaps, "tool_declarations_probe_failed")
	require.Contains(t, gaps, "catalog_lookup_failed")
}

// A request id is guessable from a dashboard URL, so the project predicate —
// not the id alone — decides whether it may be refreshed.
func TestRefreshEvidence_OtherProjectIsNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	otherProject := createProject(t, ctx, ti.conn, ti.organizationID)
	theirs := seedRequest(t, ctx, ti, otherProject, seededRequest{targetKey: "https://theirs.example.com", status: "", evidence: "", version: 0})

	_, err := ti.service.RefreshEvidence(ctx, refreshPayload(theirs.String()))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestRefreshEvidence_InvalidIDIsBadRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.RefreshEvidence(ctx, refreshPayload("not-a-uuid"))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.RefreshEvidence(ctx, refreshPayload(uuid.NewString()))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestRefreshEvidence_RequiresScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "", evidence: "", version: 0})
	ungranted := withProject(t, ctx, ti, ti.projectID)

	_, err := ti.service.RefreshEvidence(ungranted, refreshPayload(requestID.String()))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

// Refreshing is part of reviewing, not deciding: the read scope must be
// enough, matching intake where any authenticated member triggers the same
// gather.
func TestRefreshEvidence_NonAdminIsRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "https://mcp.example.com/read-scope", status: "", evidence: "", version: 0})
	nonAdmin := withProject(t, ctx, ti, ti.projectID, authz.ScopeProjectWrite)

	_, err := ti.service.RefreshEvidence(nonAdmin, refreshPayload(requestID.String()))
	requireOopsCode(t, err, oops.CodeForbidden)
}
