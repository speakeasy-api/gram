package mcpapproval_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_approval"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
)

func listPayload() *gen.ListRequestsPayload {
	return &gen.ListRequestsPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil,
		Status: nil, Limit: nil,
	}
}

func TestListRequests_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "https://a.example.com", status: "", evidence: "", version: 0})
	seedRequester(t, ctx, ti, ti.projectID, requestID, "user-a", "we need it for triage")
	seedRequester(t, ctx, ti, ti.projectID, requestID, "user-b", "same here")

	result, err := ti.service.ListRequests(ctx, listPayload())
	require.NoError(t, err)
	require.Len(t, result.Requests, 1)

	got := result.Requests[0]
	require.Equal(t, requestID.String(), got.ID)
	require.Equal(t, "server_url", got.TargetKind)
	require.Equal(t, "requested", got.Status)
	require.True(t, got.VersionPinned)
	require.NotNil(t, got.ArtifactRef)
	require.Equal(t, "npm:@scope/pkg@1.2.3", *got.ArtifactRef)

	// Demand is what makes the queue orderable, so the count has to reflect
	// every requester rather than collapsing to one row per request.
	require.Equal(t, 2, got.RequesterCount)
}

// A server_url summary always carries the same slug the Shadow MCP inventory
// derives from the canonical URL, so a request links to the server page it
// describes; a stdio target has no inventory page, so its slug is absent.
func TestListRequests_ServerSlugFollowsTargetKind(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	const canonicalURL = "https://sluggable.example.com/mcp"
	urlRequest := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: canonicalURL, status: "", evidence: "", version: 0})
	stdioRequest := seedUnresolvedRequest(t, ctx, ti, ti.projectID, "npx some-server")

	result, err := ti.service.ListRequests(ctx, listPayload())
	require.NoError(t, err)
	require.Len(t, result.Requests, 2)

	byID := make(map[string]*gen.ApprovalRequestSummary, len(result.Requests))
	for _, request := range result.Requests {
		byID[request.ID] = request
	}

	withSlug := byID[urlRequest.String()]
	require.NotNil(t, withSlug.ServerSlug)
	require.Equal(t, shadowmcp.ServerSlug(canonicalURL), *withSlug.ServerSlug)

	require.Nil(t, byID[stdioRequest.String()].ServerSlug)
}

// An unidentified server must reach the surface as absent rather than as an
// empty string, which would read as a blank field instead of as unknown.
func TestListRequests_UnresolvedArtifactIsAbsent(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	seedUnresolvedRequest(t, ctx, ti, ti.projectID, "npx some-server")

	result, err := ti.service.ListRequests(ctx, listPayload())
	require.NoError(t, err)
	require.Len(t, result.Requests, 1)
	require.Nil(t, result.Requests[0].ArtifactRef)
	require.False(t, result.Requests[0].VersionPinned)
}

func TestListRequests_FiltersByStatus(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "https://open.example.com", status: "requested", evidence: "", version: 0})
	approved := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "https://done.example.com", status: "approved", evidence: "", version: 0})

	payload := listPayload()
	payload.Status = new("approved")

	result, err := ti.service.ListRequests(ctx, payload)
	require.NoError(t, err)
	require.Len(t, result.Requests, 1)
	require.Equal(t, approved.String(), result.Requests[0].ID)
}

func TestListRequests_CapsPageSize(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	// One more request than the cap, so the clamp is what bounds the result
	// rather than the data running out first.
	for i := range 201 {
		seedRequest(t, ctx, ti, ti.projectID, seededRequest{
			targetKey: fmt.Sprintf("https://mcp.example.com/%03d", i), status: "", evidence: "", version: 0,
		})
	}

	payload := listPayload()
	payload.Limit = new(int32(2))

	result, err := ti.service.ListRequests(ctx, payload)
	require.NoError(t, err)
	require.Len(t, result.Requests, 2)

	// A caller-supplied limit above the cap is clamped, not honoured.
	payload.Limit = new(int32(100000))
	result, err = ti.service.ListRequests(ctx, payload)
	require.NoError(t, err)
	require.Len(t, result.Requests, 200)
}

// The queue must never carry another project's requests, whatever the caller
// holds in their own.
func TestListRequests_DoesNotLeakOtherProjects(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	otherProject := createProject(t, ctx, ti.conn, ti.organizationID)
	seedRequest(t, ctx, ti, otherProject, seededRequest{targetKey: "https://theirs.example.com", status: "", evidence: "", version: 0})
	mine := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "https://mine.example.com", status: "", evidence: "", version: 0})

	result, err := ti.service.ListRequests(ctx, listPayload())
	require.NoError(t, err)
	require.Len(t, result.Requests, 1)
	require.Equal(t, mine.String(), result.Requests[0].ID)
}

func TestListRequests_RequiresScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "", evidence: "", version: 0})
	ungranted := withProject(t, ctx, ti, ti.projectID)

	_, err := ti.service.ListRequests(ungranted, listPayload())
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeForbidden)
}

// Reviewing the queue and committing the organisation to a server are separate
// grants, and the weaker one has to be enough to look.
func TestListRequests_NonAdminIsRefused(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "", evidence: "", version: 0})
	nonAdmin := withProject(t, ctx, ti, ti.projectID, authz.ScopeProjectWrite)

	_, err := ti.service.ListRequests(nonAdmin, listPayload())
	requireOopsCode(t, err, oops.CodeForbidden)
}
