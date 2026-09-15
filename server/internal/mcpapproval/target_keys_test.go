package mcpapproval_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
)

// ListApprovalRequestsByTargetKeys backs the Shadow MCP inventory join
// (server/internal/access/shadow_mcp_inventory.go). Canonical URLs are
// identical across tenants — two projects observing the same public server
// share the same key — so the project predicate, not the key, is what keeps
// one tenant's approval state off another's inventory.
func TestListApprovalRequestsByTargetKeys_ProjectBounded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	const sharedURL = "https://shared.example.com/mcp"

	mine := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: sharedURL, status: "requested", evidence: "", version: 0})
	seedRequester(t, ctx, ti, ti.projectID, mine, "my-user", "mine")

	// A second project tracks the very same canonical URL, with its own
	// request in a different status and more requesters, so any bleed would be
	// visible in the id, the status, or the count.
	otherProject := createProject(t, ctx, ti.conn, ti.organizationID)
	theirs := seedRequest(t, ctx, ti, otherProject, seededRequest{targetKey: sharedURL, status: "approved", evidence: "", version: 0})
	seedRequester(t, ctx, ti, otherProject, theirs, "their-user-a", "theirs")
	seedRequester(t, ctx, ti, otherProject, theirs, "their-user-b", "theirs too")

	rows, err := ti.repo.ListApprovalRequestsByTargetKeys(ctx, repo.ListApprovalRequestsByTargetKeysParams{
		ProjectID:  ti.projectID,
		TargetKeys: []string{sharedURL},
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, mine, rows[0].ID)
	require.Equal(t, "requested", rows[0].Status)
	require.Equal(t, int64(1), rows[0].RequesterCount)
}

// The query resolves server_url targets only: a stdio request that happens to
// carry the same key must not surface as approval state for a URL, however
// the key collides.
func TestListApprovalRequestsByTargetKeys_IgnoresOtherTargetKinds(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// The uniqueness key is (project, kind, key), so the same key can exist
	// under both kinds within one project.
	const key = "https://collides.example.com/mcp"
	urlRequest := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: key, status: "requested", evidence: "", version: 0})
	seedUnresolvedRequest(t, ctx, ti, ti.projectID, key)

	rows, err := ti.repo.ListApprovalRequestsByTargetKeys(ctx, repo.ListApprovalRequestsByTargetKeysParams{
		ProjectID:  ti.projectID,
		TargetKeys: []string{key},
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, urlRequest, rows[0].ID)
}
