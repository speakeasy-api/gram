package mcpapproval_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_approval"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval/repo"
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

// The child queries carry their own project predicates. The handler's parent
// lookup 404s before they run, so the predicates are exercised directly at
// the repo layer: another project's request id under this project's bound
// must yield nothing, whatever the handler above happens to check first.
func TestGetRequest_ChildQueriesAreProjectBounded(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	otherProject := createProject(t, ctx, ti.conn, ti.organizationID)
	theirs := seedRequest(t, ctx, ti, otherProject, seededRequest{targetKey: "https://theirs.example.com", status: "", evidence: "", version: 0})
	seedRequester(t, ctx, ti, otherProject, theirs, "their-user", "their reason")

	// A decision in the other project too, so the empty result below proves
	// the predicate filters rather than the data being absent.
	otherCtx := withProject(t, ctx, ti, otherProject, authz.ScopeMCPApprovalDecide)
	_, err := ti.service.RecordDecision(otherCtx, decisionPayload(theirs.String(), "denied"))
	require.NoError(t, err)

	_, err = ti.service.GetRequest(ctx, getPayload(theirs.String()))
	requireOopsCode(t, err, oops.CodeNotFound)

	requesters, err := ti.repo.ListRequestersForApprovalRequest(ctx, repo.ListRequestersForApprovalRequestParams{
		McpApprovalRequestID: theirs,
		ProjectID:            ti.projectID,
	})
	require.NoError(t, err)
	require.Empty(t, requesters)

	decisions, err := ti.repo.ListDecisionsForApprovalRequest(ctx, repo.ListDecisionsForApprovalRequestParams{
		McpApprovalRequestID: theirs,
		ProjectID:            ti.projectID,
	})
	require.NoError(t, err)
	require.Empty(t, decisions)
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

// Research reports ride along on the detail, newest first, so the admin sees
// every run without a second call.
func TestGetRequest_IncludesResearchReports(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	requestID := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "", evidence: "", version: 0})
	seedResearchReport(t, ctx, ti, ti.projectID, requestID, "completed", `{"claims":[{"text":"vendor is real","tier":"independently_reported"}]}`)
	seedResearchReport(t, ctx, ti, ti.projectID, requestID, "running", `{}`)

	detail, err := ti.service.GetRequest(ctx, getPayload(requestID.String()))
	require.NoError(t, err)
	require.Len(t, detail.ResearchReports, 2)

	// Newest first, and the payload arrives as a structure the surface walks.
	require.Equal(t, "running", detail.ResearchReports[0].Status)
	require.Equal(t, "completed", detail.ResearchReports[1].Status)
	report, ok := detail.ResearchReports[1].Report.(map[string]any)
	require.True(t, ok)
	require.Contains(t, report, "claims")

	// A request with no runs reports none rather than failing.
	bare := seedRequest(t, ctx, ti, ti.projectID, seededRequest{targetKey: "", status: "", evidence: "", version: 0})
	bareDetail, err := ti.service.GetRequest(ctx, getPayload(bare.String()))
	require.NoError(t, err)
	require.Empty(t, bareDetail.ResearchReports)
}
