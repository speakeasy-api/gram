package mcpapproval_test

import (
	"github.com/jackc/pgx/v5/pgtype"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// A redeemed block link admits like any other ask: the review opens as
// requested, keyed on the canonical URL, with the blocked employee attached
// as the requester and the token-bearing query stripped from what is stored.
func TestAdmitBlockedServer_AttachesRequesterOnCanonicalReview(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	id, status, err := ti.service.AdmitBlockedServer(
		ctx,
		ti.organizationID,
		ti.projectID,
		"https://MCP.Example.com:443/blocked?token=supersecret#fragment",
		"blocked-user",
		"blocked-user@example.test",
		"  hit the block during oncall  ",
	)
	require.NoError(t, err)
	require.Equal(t, "requested", status)

	row, err := ti.repo.GetApprovalRequest(ctx, repo.GetApprovalRequestParams{
		ID:        uuid.MustParse(id),
		ProjectID: ti.projectID,
	})
	require.NoError(t, err)
	// The dedupe key is the canonical inventory URL — lowercased host,
	// default port dropped, query gone — the same key a block rule and the
	// org's own traffic converge on.
	require.Equal(t, "https://mcp.example.com/blocked", row.TargetKey)
	// The stored reference is the redacted form: the token must not reach
	// every reader of the queue or the audit feed.
	require.NotContains(t, row.TargetRaw, "supersecret")
	require.Equal(t, "requested", row.Status)

	detail, err := ti.service.GetRequest(ctx, getPayload(id))
	require.NoError(t, err)
	require.Len(t, detail.Requesters, 1)
	require.Equal(t, "blocked-user", detail.Requesters[0].UserID)
	require.NotNil(t, detail.Requesters[0].UserEmail)
	require.Equal(t, "blocked-user@example.test", *detail.Requesters[0].UserEmail)
	require.NotNil(t, detail.Requesters[0].Note)
	require.Equal(t, "hit the block during oncall", *detail.Requesters[0].Note)
}

// A block-link ask for a server someone already proactively requested joins
// the existing review — one server, one review, whatever the entry point and
// however the URL was spelled.
func TestAdmitBlockedServer_DedupesOntoExistingReview(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	created, err := ti.service.CreateRequest(ctx, createPayload("server_url", "https://mcp.example.com/sse", "proactive ask"))
	require.NoError(t, err)

	id, status, err := ti.service.AdmitBlockedServer(
		ctx,
		ti.organizationID,
		ti.projectID,
		"https://mcp.example.com:443/sse",
		"blocked-user",
		"blocked-user@example.test",
		"hit the block",
	)
	require.NoError(t, err)
	require.Equal(t, created.ID, id)
	require.Equal(t, "requested", status)

	detail, err := ti.service.GetRequest(ctx, getPayload(id))
	require.NoError(t, err)
	require.Len(t, detail.Requesters, 2)
}

// With the gate off the intake reports exactly oops.CodeForbidden — the
// documented signal the risk service's redemption uses to fall back to the
// legacy bypass request, so any other code here would break the fallback.
func TestAdmitBlockedServer_GateOffIsForbidden(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	disableMCPApproval(ti)

	_, _, err := ti.service.AdmitBlockedServer(
		ctx,
		ti.organizationID,
		ti.projectID,
		"https://mcp.example.com/gated",
		"blocked-user",
		"blocked-user@example.test",
		"",
	)
	requireOopsCode(t, err, oops.CodeForbidden)

	// Nothing was admitted.
	result, err := ti.repo.ListApprovalRequests(ctx, repo.ListApprovalRequestsParams{
		ProjectID: ti.projectID,
		Status:    pgtype.Text{},
		PageLimit: 50,
	})
	require.NoError(t, err)
	require.Empty(t, result)
}

// The intake only admits URLs the MCP backend could reach; anything else is
// the caller's error, not a review.
func TestAdmitBlockedServer_NonHTTPURLIsBadRequest(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, _, err := ti.service.AdmitBlockedServer(
		ctx,
		ti.organizationID,
		ti.projectID,
		"ws://mcp.example.com/socket",
		"blocked-user",
		"blocked-user@example.test",
		"",
	)
	requireOopsCode(t, err, oops.CodeBadRequest)
}

// A repeat ask on an approved review attaches the requester without reopening
// the decision: re-asking changes nothing about an approval, and an admin can
// re-decide at any time.
func TestAdmitBlockedServer_RepeatAskOnApprovedReviewDoesNotReopen(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	serverURL := "https://mcp.example.com/approved-already"
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: serverURL, status: "approved", evidence: "", version: 0})

	id, status, err := ti.service.AdmitBlockedServer(
		ctx,
		ti.organizationID,
		ti.projectID,
		serverURL,
		"late-asker",
		"late-asker@example.test",
		"still want it",
	)
	require.NoError(t, err)
	require.Equal(t, requestID.String(), id)
	require.Equal(t, "approved", status)
	require.Equal(t, "approved", requestStatus(t, ctx, ti, ti.projectID, requestID))

	detail, err := ti.service.GetRequest(ctx, getPayload(id))
	require.NoError(t, err)
	require.Len(t, detail.Requesters, 1)
	require.Equal(t, "late-asker", detail.Requesters[0].UserID)
}
