package platformmcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/aivendors"
	"github.com/speakeasy-api/gram/server/internal/authz"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featuresrepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
)

func TestOrganizationGatewayProjectDiscovery(t *testing.T) {
	t.Parallel()
	ctx, conn, service, principal, slug := newOrganizationGatewayFixture(t)
	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Gateway project", Slug: "gateway-project", OrganizationID: principal.OrganizationID,
	})
	require.NoError(t, err)
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, project.ID.String())})
	input := GetMyProjectGatewayInput{ProjectSlug: project.Slug}
	output, err := service.GetProject(ctx, principal, input)
	require.NoError(t, err)
	require.False(t, output.Available)
	require.Empty(t, output.URL)
	require.Equal(t, "no_organization_issuer", output.UnavailableReason)

	createOrganizationIssuerForGateway(t, ctx, conn, principal.OrganizationID)
	output, err = service.GetProject(ctx, principal, input)
	require.NoError(t, err)
	require.True(t, output.Available)
	require.Equal(t, "https://app.example.test/mcp/gateway/"+slug+"/projects/"+project.Slug, output.URL)

	server := newTestMCPServer()
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, project.ID.String())})
			return next(contextWithPrincipal(ctx, principal), method, req)
		}
	})
	reg := newRegistrar(aivendors.NewStore(aivendors.NewSnapshot(aivendors.Products())), server)
	reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
	registerOrganizationGatewayTool(reg, service)
	session := connectTestClient(t, server)
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "get_my_project_gateway", Arguments: map[string]any{"project_slug": project.Slug}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	body, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	var actual GetMyOrganizationGatewayOutput
	require.NoError(t, json.Unmarshal(body, &actual))
	require.Equal(t, output, actual)
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "get_my_project_gateway", Arguments: map[string]any{}})
	require.True(t, err != nil || result.IsError)
	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: getMyOrganizationGatewayName, Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, result.IsError)
	body, err = json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, &actual))
	require.Equal(t, "https://app.example.test/mcp/gateway/"+slug, actual.URL)

	_, err = featuresrepo.New(conn).EnableFeature(ctx, featuresrepo.EnableFeatureParams{OrganizationID: principal.OrganizationID, FeatureName: string(productfeatures.FeatureOrganizationGatewayDisabled)})
	require.NoError(t, err)
	output, err = service.GetProject(ctx, principal, input)
	require.NoError(t, err)
	require.False(t, output.Available)
	require.Empty(t, output.URL)
	require.Equal(t, "turned_off", output.UnavailableReason)
}

func TestOrganizationGatewayProjectRejectsMissingDeletedAndForeign(t *testing.T) {
	t.Parallel()
	ctx, conn, service, principal, _ := newOrganizationGatewayFixture(t)
	foreignID := "org-" + uuid.NewString()
	_, err := organizationsrepo.New(conn).UpsertOrganizationMetadata(ctx, organizationsrepo.UpsertOrganizationMetadataParams{ID: foreignID, Name: "Other organization", Slug: "other-" + uuid.NewString()[:8]})
	require.NoError(t, err)
	_, err = projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Other project", Slug: "foreign-project", OrganizationID: foreignID})
	require.NoError(t, err)
	deleted, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Deleted project", Slug: "deleted-project", OrganizationID: principal.OrganizationID})
	require.NoError(t, err)
	_, err = projectsrepo.New(conn).DeleteProject(ctx, deleted.ID)
	require.NoError(t, err)
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, authz.WildcardResource)})
	for _, slug := range []string{"missing-project", "foreign-project", "deleted-project"} {
		output, err := service.GetProject(ctx, principal, GetMyProjectGatewayInput{ProjectSlug: slug})
		require.ErrorIs(t, err, ErrUnavailable)
		require.Empty(t, output.URL)
	}
	_, err = service.GetProject(ctx, principal, GetMyProjectGatewayInput{})
	require.ErrorContains(t, err, "project_slug is required")
}

func TestOrganizationGatewayProjectContract(t *testing.T) {
	t.Parallel()
	_, _, service, _, _ := newOrganizationGatewayFixture(t)
	var liveSchema []byte
	for _, svc := range []*OrganizationGatewayService{service, nil} {
		reg := newRegistrar(aivendors.NewStore(aivendors.NewSnapshot(aivendors.Products())), newTestMCPServer())
		registerOrganizationGatewayTool(reg, svc)
		require.Empty(t, reg.For(AudienceAssistant))
		require.Len(t, reg.Descriptors(), 2)
		for _, d := range reg.Descriptors() {
			require.Equal(t, ExternalAuthorizationMember, d.Meta.Authorization)
			require.Equal(t, externalOnly, d.Meta.Audiences)
			require.True(t, d.Annotations.ReadOnlyHint)
			if d.Name == "get_my_project_gateway" {
				require.Equal(t, ProjectScopeExplicit, d.Meta.ProjectScope)
				require.Contains(t, string(d.InputSchema), "project_slug")
				if liveSchema == nil {
					liveSchema = d.InputSchema
				} else {
					require.JSONEq(t, string(liveSchema), string(d.InputSchema))
				}
			} else {
				require.Equal(t, getMyOrganizationGatewayName, d.Name)
				require.Equal(t, ProjectScopeNone, d.Meta.ProjectScope)
			}
		}
	}
}

func TestOrganizationGatewayProjectReadRequired(t *testing.T) {
	t.Parallel()
	ctx, conn, service, principal, _ := newOrganizationGatewayFixture(t)
	project, err := projectsrepo.New(conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Restricted project", Slug: "restricted", OrganizationID: principal.OrganizationID})
	require.NoError(t, err)
	createOrganizationIssuerForGateway(t, ctx, conn, principal.OrganizationID)
	input := GetMyProjectGatewayInput{ProjectSlug: project.Slug}
	for _, grants := range [][]authz.Grant{
		nil,
		{authz.NewGrant(authz.ScopeOrgRead, principal.OrganizationID)},
		{authz.NewGrant(authz.ScopeProjectRead, uuid.NewString())},
		{authz.NewGrant(authz.ScopeMCPConnect, authz.WildcardResource)},
	} {
		deniedCtx := authz.GrantsToContext(ctx, grants)
		output, err := service.GetProject(deniedCtx, principal, input)
		require.ErrorIs(t, err, ErrUnavailable)
		require.Empty(t, output.URL)
		_, missingErr := service.GetProject(deniedCtx, principal, GetMyProjectGatewayInput{ProjectSlug: "missing"})
		require.ErrorIs(t, missingErr, ErrUnavailable)
	}
	output, err := service.GetProject(ctx, principal, input)
	require.ErrorIs(t, err, ErrUnavailable)
	require.Empty(t, output.URL)
	allowedCtx := authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, project.ID.String())})
	output, err = service.GetProject(allowedCtx, principal, input)
	require.NoError(t, err)
	require.True(t, output.Available)
}

func TestOrganizationGatewayProjectReadableRefusals(t *testing.T) {
	t.Parallel()
	ctx, _, service, principal, _ := newOrganizationGatewayFixture(t)
	ctx = contextWithPrincipal(ctx, principal)
	for _, svc := range []*OrganizationGatewayService{nil, service} {
		reg := newRegistrar(aivendors.NewStore(aivendors.NewSnapshot(aivendors.Products())), newTestMCPServer())
		reg.withExternalAuthorizer(allowExternalCallAuthorizer{})
		registerOrganizationGatewayTool(reg, svc)
		d := descriptorByName(t, reg, "get_my_project_gateway")
		_, err := d.Invoke(ctx, json.RawMessage(`{"project_slug":""}`))
		var refusal *ToolRefusalError
		require.ErrorAs(t, err, &refusal)
		if svc == nil {
			require.Equal(t, unavailableCode, refusal.Code)
			var payload featureUnavailableResult
			require.NoError(t, json.Unmarshal([]byte(refusal.Payload), &payload))
			require.Equal(t, "organization_gateway", payload.Feature)
			require.Equal(t, "This is not switched on for your organization yet.", payload.Message)
		} else {
			require.Equal(t, "invalid_request", refusal.Code)
			require.JSONEq(t, `{"code":"invalid_request","message":"project_slug is required"}`, refusal.Payload)
		}
	}
}
