package platformmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/ratelimit"

	gen "github.com/speakeasy-api/gram/server/gen/mcp_approval"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type stubMCPReviewRequests struct {
	createdProject uuid.UUID
	createdUser    string
	createdTarget  string
	readInput      mcpapproval.PlatformRequesterReviewInput
	createCalls    int
	// serverSlug is what the approval service knows the target as on its
	// Shadow MCP page; empty means it has none to offer.
	serverSlug string
}

func (s *stubMCPReviewRequests) CreatePlatformRequest(_ context.Context, _ string, projectID uuid.UUID, userID, targetKind, target, _ string) (*gen.ApprovalRequestSummary, error) {
	s.createCalls++
	s.createdProject, s.createdUser, s.createdTarget = projectID, userID, target
	summary := &gen.ApprovalRequestSummary{
		ID: uuid.NewString(), TargetKind: targetKind, TargetRaw: "https://example.test/mcp", Status: "requested", CreatedAt: "created", UpdatedAt: "updated",
	}
	if s.serverSlug != "" {
		summary.ServerSlug = &s.serverSlug
	}
	return summary, nil
}

func (s *stubMCPReviewRequests) ReadPlatformRequesterReview(_ context.Context, input mcpapproval.PlatformRequesterReviewInput) (mcpapproval.PlatformRequesterReview, error) {
	s.readInput = input
	return mcpapproval.PlatformRequesterReview{RequestID: input.RequestID.String(), TargetKind: "server_url", Target: "https://example.test/mcp", Status: "requested", NextAction: "wait_for_review", ServerSlug: s.serverSlug}, nil
}

// stubReviewLinks builds the Shadow MCP page link the way the dashboard does
// and remembers which server slugs it was asked about.
type stubReviewLinks struct{ slugs []string }

func (s *stubReviewLinks) ShadowMCPReviewURL(_ context.Context, _ Principal, project ResolvedProject, serverSlug string) string {
	s.slugs = append(s.slugs, serverSlug)
	return "https://app.example.test/acme/projects/" + project.Slug + "/shadow-ai/mcps/" + serverSlug
}

type stubMCPReviewProjects struct{ project ResolvedProject }

func (s stubMCPReviewProjects) ResolveReviewProject(_ context.Context, _ Principal, _ string) (ResolvedProject, error) {
	return s.project, nil
}

func liveReviewRequestRegistrar(service MCPReviewRequestService, projects MCPReviewProjectResolver) *Registrar {
	registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "review-request-test", Version: "test"}, nil))
	registerReviewRequestTools(registrar, service, projects, allowBudget(), nil)
	return registrar
}

func TestMCPReviewRequestToolsAreExternalMemberTools(t *testing.T) {
	t.Parallel()

	registrar := liveReviewRequestRegistrar(&stubMCPReviewRequests{}, stubMCPReviewProjects{})
	request := descriptorByName(t, registrar, "request_mcp_review")
	status := descriptorByName(t, registrar, "get_my_mcp_review_request")
	for _, descriptor := range []Descriptor{request, status} {
		require.Equal(t, ExternalAuthorizationMember, descriptor.Meta.Authorization)
		require.Equal(t, externalOnly, descriptor.Meta.Audiences)
		require.Equal(t, ProjectScopeExplicit, descriptor.Meta.ProjectScope)
		require.Empty(t, descriptor.Meta.DiscoveryScopes)
	}
	require.NotNil(t, request.Annotations)
	require.False(t, request.Annotations.ReadOnlyHint)
	require.NotNil(t, request.Annotations.DestructiveHint)
	require.False(t, *request.Annotations.DestructiveHint)
	require.NotNil(t, request.Annotations.OpenWorldHint)
	require.True(t, *request.Annotations.OpenWorldHint)
	require.NotNil(t, status.Annotations)
	require.True(t, status.Annotations.ReadOnlyHint)
}

func TestMCPReviewRequestToolsRemainDiscoverableWithoutProjectGrant(t *testing.T) {
	t.Parallel()

	registrar := liveReviewRequestRegistrar(&stubMCPReviewRequests{}, stubMCPReviewProjects{})
	principal := Principal{UserID: "requester", OrganizationID: "organization", ConnectionID: "connection", Generation: "generation", ClientID: "client", Surface: SurfacePlatformMCP}
	ctx := authz.GrantsToContext(t.Context(), []authz.Grant{})
	visible := registrar.FilterExternalTools(ctx, principal, []*mcp.Tool{{Name: "request_mcp_review"}, {Name: "get_my_mcp_review_request"}})
	require.Len(t, visible, 2)
	require.Equal(t, "request_mcp_review", visible[0].Name)
	require.Equal(t, "get_my_mcp_review_request", visible[1].Name)
}

func TestMCPReviewRequestRolloutDenialIsFeatureUnavailable(t *testing.T) {
	t.Parallel()

	result, ok := reviewRequestToolResult(oops.E(oops.CodeForbidden, nil, "MCP approval is not enabled for this organization"))
	require.True(t, ok)
	require.NotNil(t, result)
	require.True(t, result.IsError)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.JSONEq(t, `{"code":"feature_unavailable","message":"MCP review requests are not available for this organization."}`, text.Text)
}

func TestMCPReviewRequestBudgetStopsBeforeEvidenceGathering(t *testing.T) {
	t.Parallel()

	service := &stubMCPReviewRequests{}
	denied := OperationBudget{Connection: &recordingOperationLimiter{result: ratelimit.Result{Allowed: false}}, Organization: allowOperationLimiter{}}
	registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "review-request-test", Version: "test"}, nil))
	projectID := uuid.New()
	registerReviewRequestTools(registrar, service, stubMCPReviewProjects{project: ResolvedProject{ID: projectID}}, denied, nil)
	principal := Principal{UserID: "requester", OrganizationID: "organization", ConnectionID: "connection", Generation: "generation", ClientID: "client", Surface: SurfacePlatformMCP}

	_, err := descriptorByName(t, registrar, "request_mcp_review").Invoke(ContextWithPrincipal(t.Context(), principal), json.RawMessage(`{"project_id":"`+projectID.String()+`","target_kind":"server_url","target":"https://example.test/mcp","justification":"needed for work"}`))
	var refusal *ToolRefusalError
	require.ErrorAs(t, err, &refusal)
	require.Contains(t, refusal.Payload, `"code":"rate_limited"`)
	require.Zero(t, service.createCalls)
}

func TestMCPReviewRequestToolsBindProjectAndRequester(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	requestID := uuid.New()
	service := &stubMCPReviewRequests{}
	registrar := liveReviewRequestRegistrar(service, stubMCPReviewProjects{project: ResolvedProject{ID: projectID, Name: "Project", Slug: "project"}})
	principal := Principal{UserID: "requester", OrganizationID: "organization", ConnectionID: "connection", Generation: "generation", ClientID: "client", Surface: SurfacePlatformMCP}
	ctx := ContextWithPrincipal(t.Context(), principal)

	createdRaw, err := descriptorByName(t, registrar, "request_mcp_review").Invoke(ctx, json.RawMessage(`{"project_id":"`+projectID.String()+`","target_kind":"server_url","target":"https://example.test/mcp?token=fake","justification":"needed for work"}`))
	require.NoError(t, err)
	created, ok := createdRaw.(RequestMCPReviewOutput)
	require.True(t, ok)
	require.Equal(t, projectID.String(), created.ProjectID)
	require.Equal(t, "wait_for_review", created.NextAction)
	require.Equal(t, projectID, service.createdProject)
	require.Equal(t, principal.UserID, service.createdUser)

	readRaw, err := descriptorByName(t, registrar, "get_my_mcp_review_request").Invoke(ctx, json.RawMessage(`{"project_id":"`+projectID.String()+`","request_id":"`+requestID.String()+`"}`))
	require.NoError(t, err)
	read, ok := readRaw.(GetMyMCPReviewRequestOutput)
	require.True(t, ok)
	require.Equal(t, projectID.String(), read.ProjectID)
	require.Equal(t, principal.OrganizationID, service.readInput.OrganizationID)
	require.Equal(t, principal.UserID, service.readInput.UserID)
	require.Equal(t, requestID, service.readInput.RequestID)
	require.Empty(t, created.ReviewURL, "no link service, no page to offer")
	require.Empty(t, read.ReviewURL)
}

// Both review tools hand back the request's Shadow MCP page when the approval
// service knows the server's slug and a link service is wired; neither invents
// one when the slug is missing.
func TestMCPReviewRequestToolsReturnTheReviewPage(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	requestID := uuid.New()
	principal := Principal{UserID: "requester", OrganizationID: "organization", ConnectionID: "connection", Generation: "generation", ClientID: "client", Surface: SurfacePlatformMCP}
	ctx := ContextWithPrincipal(t.Context(), principal)
	createInput := json.RawMessage(`{"project_id":"` + projectID.String() + `","target_kind":"server_url","target":"https://example.test/mcp","justification":"needed for work"}`)
	readInput := json.RawMessage(`{"project_id":"` + projectID.String() + `","request_id":"` + requestID.String() + `"}`)
	register := func(service *stubMCPReviewRequests, links *stubReviewLinks) *Registrar {
		registrar := newRegistrar(mcp.NewServer(&mcp.Implementation{Name: "review-request-test", Version: "test"}, nil))
		registerReviewRequestTools(registrar, service, stubMCPReviewProjects{project: ResolvedProject{ID: projectID, Name: "Project", Slug: "project"}}, allowBudget(), links)
		return registrar
	}

	links := &stubReviewLinks{slugs: nil}
	registrar := register(&stubMCPReviewRequests{serverSlug: "example-test-mcp"}, links)
	createdRaw, err := descriptorByName(t, registrar, "request_mcp_review").Invoke(ctx, createInput)
	require.NoError(t, err)
	created, ok := createdRaw.(RequestMCPReviewOutput)
	require.True(t, ok)
	require.Equal(t, "https://app.example.test/acme/projects/project/shadow-ai/mcps/example-test-mcp", created.ReviewURL)
	readRaw, err := descriptorByName(t, registrar, "get_my_mcp_review_request").Invoke(ctx, readInput)
	require.NoError(t, err)
	read, ok := readRaw.(GetMyMCPReviewRequestOutput)
	require.True(t, ok)
	require.Equal(t, "https://app.example.test/acme/projects/project/shadow-ai/mcps/example-test-mcp", read.ReviewURL)
	require.Equal(t, []string{"example-test-mcp", "example-test-mcp"}, links.slugs)

	// No slug from the approval service: no page, and the link service is not asked.
	links = &stubReviewLinks{slugs: nil}
	registrar = register(&stubMCPReviewRequests{serverSlug: ""}, links)
	createdRaw, err = descriptorByName(t, registrar, "request_mcp_review").Invoke(ctx, createInput)
	require.NoError(t, err)
	created, ok = createdRaw.(RequestMCPReviewOutput)
	require.True(t, ok)
	require.Empty(t, created.ReviewURL)
	readRaw, err = descriptorByName(t, registrar, "get_my_mcp_review_request").Invoke(ctx, readInput)
	require.NoError(t, err)
	read, ok = readRaw.(GetMyMCPReviewRequestOutput)
	require.True(t, ok)
	require.Empty(t, read.ReviewURL)
	require.Empty(t, links.slugs)
}
