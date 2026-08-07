package mcpapproval_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_approval"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

func getPayload(id string) *gen.GetRequestPayload {
	return &gen.GetRequestPayload{
		SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil, ID: id,
	}
}

func TestGetRequest_Success(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	evidence := `{"authority": {"mode": "oauth"}, "capability": {"declares_destructive": true}}`
	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{
		targetKey: "https://mcp.example.com/x", status: "requested", evidence: evidence, version: 3,
	})
	seedRequester(t, ctx, ti, ti.projectID, requestID, "user-a", "needed for oncall")

	detail, err := ti.service.GetRequest(ctx, getPayload(requestID.String()))
	require.NoError(t, err)

	require.Equal(t, requestID.String(), detail.Request.ID)
	require.Len(t, detail.Requesters, 1)
	require.Equal(t, "user-a", detail.Requesters[0].UserID)
	require.NotNil(t, detail.Requesters[0].Note)
	require.Equal(t, "needed for oncall", *detail.Requesters[0].Note)
	require.Empty(t, detail.Decisions)

	// The evidence has to arrive as a structure the surface can walk, and its
	// version has to travel with it so an older snapshot stays interpretable.
	require.NotNil(t, detail.EvidenceVersion)
	require.Equal(t, 3, *detail.EvidenceVersion)
	require.NotNil(t, detail.EvidenceCollectedAt)

	decoded, ok := detail.Evidence.(map[string]any)
	require.True(t, ok, "evidence should decode to an object")
	require.Contains(t, decoded, "authority")
	require.Contains(t, decoded, "capability")
}

// A request id is guessable from a dashboard URL, so the project predicate —
// not the id alone — is what decides whether it may be read.
func TestGetRequest_OtherProjectIsNotFound(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	otherProject := createProject(t, ctx, ti.conn, ti.organizationID)
	theirs := seedRequest(t, ctx, ti, otherProject, seededRequest{targetKey: "https://theirs.example.com", status: "", evidence: "", version: 0})

	_, err := ti.service.GetRequest(ctx, getPayload(theirs.String()))
	require.Error(t, err)
	requireOopsCode(t, err, oops.CodeNotFound)
}

// Requesters and decisions are fetched under their own predicates, so a
// request id belonging to another project must not drag its children out
// even when the parent lookup is what fails.
func TestGetRequest_DoesNotLeakOtherProjectRequesters(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	otherProject := createProject(t, ctx, ti.conn, ti.organizationID)
	theirs := seedRequest(t, ctx, ti, otherProject, seededRequest{targetKey: "https://theirs.example.com", status: "", evidence: "", version: 0})
	seedRequester(t, ctx, ti, otherProject, theirs, "their-user", "their reason")

	_, err := ti.service.GetRequest(ctx, getPayload(theirs.String()))
	requireOopsCode(t, err, oops.CodeNotFound)

	// And the same id read from the granted project yields nothing either.
	mine := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "https://mine.example.com", status: "", evidence: "", version: 0})
	detail, err := ti.service.GetRequest(ctx, getPayload(mine.String()))
	require.NoError(t, err)
	require.Empty(t, detail.Requesters)
}

func TestGetRequest_UnknownID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetRequest(ctx, getPayload("0195c1f1-0000-7000-8000-000000000000"))
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetRequest_InvalidID(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetRequest(ctx, getPayload("not-a-uuid"))
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestGetRequest_RequiresScope(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "", evidence: "", version: 0})
	ungranted := withProject(t, ctx, ti, ti.projectID)

	_, err := ti.service.GetRequest(ungranted, getPayload(requestID.String()))
	requireOopsCode(t, err, oops.CodeForbidden)
}

func TestGetRequest_ReadScopeSuffices(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "", evidence: "", version: 0})
	readOnly := withProject(t, ctx, ti, ti.projectID, authz.ScopeMCPApprovalRead)

	detail, err := ti.service.GetRequest(readOnly, getPayload(requestID.String()))
	require.NoError(t, err)
	require.Equal(t, requestID.String(), detail.Request.ID)
}
