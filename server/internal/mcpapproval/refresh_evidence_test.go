package mcpapproval_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_approval"
	"github.com/speakeasy-api/gram/server/internal/authz"
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
func TestRefreshEvidence_ReadScopeSuffices(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "https://mcp.example.com/read-scope", status: "", evidence: "", version: 0})
	readOnly := withProject(t, ctx, ti, ti.projectID, authz.ScopeMCPApprovalRead)

	detail, err := ti.service.RefreshEvidence(readOnly, refreshPayload(requestID.String()))
	require.NoError(t, err)
	require.NotNil(t, detail.EvidenceCollectedAt)
}

func TestRefreshEvidence_FeatureDisabledIsForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "", evidence: "", version: 0})
	disableMCPApproval(t, ctx, ti)

	_, err := ti.service.RefreshEvidence(ctx, refreshPayload(requestID.String()))
	requireOopsCode(t, err, oops.CodeForbidden)
}
