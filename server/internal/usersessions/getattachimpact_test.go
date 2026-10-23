package usersessions_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	projectgen "github.com/speakeasy-api/gram/server/gen/user_session_issuers"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	metamcprepo "github.com/speakeasy-api/gram/server/internal/metamcp/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/authserver"
)

func TestGetAttachImpactBetweenPerServerIssuers(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	currentID := seedIssuer(t, ctx, ti, "impact-current")
	candidateID := seedIssuer(t, ctx, ti, "impact-candidate")
	toolset := attachIssuerToolset(t, ctx, ti, *authCtx.ProjectID, currentID, "impact-toolset")

	impact, err := ti.service.GetAttachImpact(ctx, &projectgen.GetAttachImpactPayload{ToolsetID: new(toolset.ID.String()), UserSessionIssuerID: new(candidateID.String())})
	require.NoError(t, err)
	require.Equal(t, currentID.String(), impact.CurrentIssuer.ID)
	require.Equal(t, candidateID.String(), impact.CandidateIssuer.ID)
	// Clients belong to the current issuer, but the server keeps advertising
	// its own per-server authorization server.
	require.True(t, impact.ReauthorizationRequired)
	require.False(t, impact.AuthorizationServerChanges)
}

func TestGetAttachImpactBetweenPerServerIssuersOnDifferentHosts(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	currentID := seedIssuer(t, ctx, ti, "impact-host-current")
	candidateID := seedIssuer(t, ctx, ti, "impact-host-candidate")
	useAuthenticationHost(t, ctx, ti, candidateID)
	toolset := attachIssuerToolset(t, ctx, ti, *authCtx.ProjectID, currentID, "impact-host-toolset")

	impact, err := ti.service.GetAttachImpact(ctx, &projectgen.GetAttachImpactPayload{ToolsetID: new(toolset.ID.String()), UserSessionIssuerID: new(candidateID.String())})
	require.NoError(t, err)
	// The server's own authorization server moves from the server URL to the
	// authentication host.
	require.True(t, impact.ReauthorizationRequired)
	require.True(t, impact.AuthorizationServerChanges)
}

// A custom domain keeps the server's own authorization server on the domain,
// whichever host the issuer announces platform-host servers on.
func TestGetAttachImpactBetweenPerServerIssuersOnCustomDomain(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	currentID := seedIssuer(t, ctx, ti, "impact-domain-current")
	candidateID := seedIssuer(t, ctx, ti, "impact-domain-candidate")
	useAuthenticationHost(t, ctx, ti, candidateID)
	domainID := createCustomDomain(t, ctx, ti)
	toolset := attachIssuerToolset(t, ctx, ti, projectID, currentID, "impact-domain-toolset")
	require.NoError(t, testrepo.New(ti.conn).SetToolsetCustomDomain(ctx, testrepo.SetToolsetCustomDomainParams{
		CustomDomainID: uuid.NullUUID{UUID: domainID, Valid: true}, Slug: toolset.Slug, ProjectID: projectID,
	}))

	impact, err := ti.service.GetAttachImpact(ctx, &projectgen.GetAttachImpactPayload{ToolsetID: new(toolset.ID.String()), UserSessionIssuerID: new(candidateID.String())})
	require.NoError(t, err)
	require.True(t, impact.ReauthorizationRequired)
	require.False(t, impact.AuthorizationServerChanges)

	// An MCP server reached only through custom domains keeps its authorization
	// server too, until it gains an address on the platform host.
	serverID := attachIssuerMCPServer(t, ctx, ti, projectID, currentID, toolset, "impact-domain-server")
	createMCPServerEndpoint(t, ctx, ti, projectID, serverID, uuid.NullUUID{UUID: domainID, Valid: true}, "impact-domain-endpoint")
	payload := &projectgen.GetAttachImpactPayload{McpServerID: new(serverID.String()), UserSessionIssuerID: new(candidateID.String())}
	impact, err = ti.service.GetAttachImpact(ctx, payload)
	require.NoError(t, err)
	require.False(t, impact.AuthorizationServerChanges)

	createMCPServerEndpoint(t, ctx, ti, projectID, serverID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "impact-platform-endpoint")
	impact, err = ti.service.GetAttachImpact(ctx, payload)
	require.NoError(t, err)
	require.True(t, impact.AuthorizationServerChanges)
}

// A toolset with a canonical server previews that server's issuer, and is on
// the platform host through its legacy alias or any live endpoint.
func TestGetAttachImpactCanonicalServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	projectID := *authCtx.ProjectID
	currentID := seedIssuer(t, ctx, ti, "impact-canonical-current")
	candidateID := seedIssuer(t, ctx, ti, "impact-canonical-candidate")
	useAuthenticationHost(t, ctx, ti, candidateID)
	domainID := createCustomDomain(t, ctx, ti)
	toolset := attachIssuerToolset(t, ctx, ti, projectID, currentID, "impact-canonical-toolset")
	_, err := mcpserversrepo.New(ti.conn).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: toolset.ID, ProjectID: projectID,
		Name: pgtype.Text{String: toolset.Slug, Valid: true}, Slug: pgtype.Text{String: toolset.Slug, Valid: true},
		EnvironmentID: uuid.NullUUID{}, UserSessionIssuerID: uuid.NullUUID{UUID: currentID, Valid: true}, RemoteMcpServerID: uuid.NullUUID{},
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, ToolVariationsGroupID: uuid.NullUUID{}, Visibility: mcpservers.VisibilityDisabled,
	})
	require.NoError(t, err)
	payload := &projectgen.GetAttachImpactPayload{ToolsetID: new(toolset.ID.String()), UserSessionIssuerID: new(candidateID.String())}

	// The legacy alias on the platform host answers beside a custom-domain endpoint.
	createMCPServerEndpoint(t, ctx, ti, projectID, toolset.ID, uuid.NullUUID{UUID: domainID, Valid: true}, "impact-canonical-domain")
	impact, err := ti.service.GetAttachImpact(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, currentID.String(), impact.CurrentIssuer.ID)
	require.True(t, impact.AuthorizationServerChanges)

	require.NoError(t, testrepo.New(ti.conn).SetToolsetCustomDomain(ctx, testrepo.SetToolsetCustomDomainParams{
		CustomDomainID: uuid.NullUUID{UUID: domainID, Valid: true}, Slug: toolset.Slug, ProjectID: projectID,
	}))
	impact, err = ti.service.GetAttachImpact(ctx, payload)
	require.NoError(t, err)
	require.False(t, impact.AuthorizationServerChanges)

	createMCPServerEndpoint(t, ctx, ti, projectID, toolset.ID, uuid.NullUUID{UUID: uuid.Nil, Valid: false}, "impact-canonical-platform")
	impact, err = ti.service.GetAttachImpact(ctx, payload)
	require.NoError(t, err)
	require.True(t, impact.AuthorizationServerChanges)
}

// useAuthenticationHost opts an issuer in to announcing the authentication
// host.
func useAuthenticationHost(t *testing.T, ctx context.Context, ti *testInstance, issuerID uuid.UUID) {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	updated, err := testrepo.New(ti.conn).SetUserSessionIssuerUseAuthenticationHostFixture(ctx, testrepo.SetUserSessionIssuerUseAuthenticationHostFixtureParams{
		UseAuthenticationHost: true,
		IssuerID:              issuerID,
		OrganizationID:        authCtx.ActiveOrganizationID,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), updated)
}

func createCustomDomain(t *testing.T, ctx context.Context, ti *testInstance) uuid.UUID {
	t.Helper()

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	domain, err := customdomainsrepo.New(ti.conn).CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID:  authCtx.ActiveOrganizationID,
		Domain:          uuid.NewString() + ".mcp.example.test",
		IngressName:     pgtype.Text{},
		CertSecretName:  pgtype.Text{},
		ProvisionerKind: "ingress",
		IpAllowlist:     []string{},
	})
	require.NoError(t, err)
	return domain.ID
}

func createMCPServerEndpoint(t *testing.T, ctx context.Context, ti *testInstance, projectID, serverID uuid.UUID, customDomainID uuid.NullUUID, slug string) {
	t.Helper()

	_, err := mcpendpointsrepo.New(ti.conn).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID:       projectID,
		CustomDomainID:  customDomainID,
		McpServerID:     uuid.NullUUID{UUID: serverID, Valid: true},
		MetaMcpServerID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		Slug:            slug,
	})
	require.NoError(t, err)
}

func TestGetAttachImpactOntoSharedIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	currentID := seedIssuer(t, ctx, ti, "impact-endpoint")
	sharedID := createOrganizationIssuer(t, ctx, ti, "impact-shared")
	setIssuerShared(t, ctx, ti, sharedID, "")
	backing := attachIssuerToolset(t, ctx, ti, *authCtx.ProjectID, currentID, "impact-backing")
	serverID := attachIssuerMCPServer(t, ctx, ti, *authCtx.ProjectID, currentID, backing, "impact-server")

	impact, err := ti.service.GetAttachImpact(ctx, &projectgen.GetAttachImpactPayload{McpServerID: new(serverID.String()), UserSessionIssuerID: new(sharedID.String())})
	require.NoError(t, err)
	require.True(t, impact.ReauthorizationRequired)
	require.True(t, impact.AuthorizationServerChanges)
	require.Equal(t, string(authserver.ModeShared), impact.CandidateIssuer.AuthorizationServerMode)
	require.Equal(t, testServerURL+authserver.SharedPath(sharedID), *impact.CandidateIssuer.IssuerURL)
}

func TestGetAttachImpactKeepingTheIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := seedIssuer(t, ctx, ti, "impact-same")
	toolset := attachIssuerToolset(t, ctx, ti, *authCtx.ProjectID, issuerID, "impact-same-toolset")

	impact, err := ti.service.GetAttachImpact(ctx, &projectgen.GetAttachImpactPayload{ToolsetID: new(toolset.ID.String()), UserSessionIssuerID: new(issuerID.String())})
	require.NoError(t, err)
	require.False(t, impact.ReauthorizationRequired)
	require.False(t, impact.AuthorizationServerChanges)
}

func TestGetAttachImpactFirstIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := seedIssuer(t, ctx, ti, "impact-first")
	toolset, err := toolsetsrepo.New(ti.conn).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: authCtx.ActiveOrganizationID, ProjectID: *authCtx.ProjectID,
		Name: "impact-unattached", Slug: "impact-unattached", Description: pgtype.Text{}, DefaultEnvironmentSlug: pgtype.Text{}, McpSlug: pgtype.Text{}, McpEnabled: false,
	})
	require.NoError(t, err)

	impact, err := ti.service.GetAttachImpact(ctx, &projectgen.GetAttachImpactPayload{ToolsetID: new(toolset.ID.String()), UserSessionIssuerID: new(issuerID.String())})
	require.NoError(t, err)
	require.Nil(t, impact.CurrentIssuer)
	require.False(t, impact.ReauthorizationRequired, "no client is registered with an issuer yet")
	require.True(t, impact.AuthorizationServerChanges)
}

func TestGetAttachImpactDetach(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := createOrganizationIssuer(t, ctx, ti, "impact-detach")
	meta, err := metamcprepo.New(ti.conn).CreateMetaMCPServer(ctx, metamcprepo.CreateMetaMCPServerParams{
		OrganizationID:      authCtx.ActiveOrganizationID,
		ProjectID:           *authCtx.ProjectID,
		Name:                "impact-meta",
		UserSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
	})
	require.NoError(t, err)

	impact, err := ti.service.GetAttachImpact(ctx, &projectgen.GetAttachImpactPayload{MetaMcpServerID: new(meta.ID.String())})
	require.NoError(t, err)
	require.Equal(t, issuerID.String(), impact.CurrentIssuer.ID)
	require.Nil(t, impact.CandidateIssuer)
	require.True(t, impact.ReauthorizationRequired)
	require.True(t, impact.AuthorizationServerChanges)
}

func TestGetAttachImpactNamesExactlyOneServer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)

	_, err := ti.service.GetAttachImpact(ctx, &projectgen.GetAttachImpactPayload{})
	requireOopsCode(t, err, oops.CodeBadRequest)

	_, err = ti.service.GetAttachImpact(ctx, &projectgen.GetAttachImpactPayload{ToolsetID: new(uuid.NewString()), MetaMcpServerID: new(uuid.NewString())})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestGetAttachImpactRefusesUnknownServerAndHiddenIssuer(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := seedIssuer(t, ctx, ti, "impact-visible")
	toolset := attachIssuerToolset(t, ctx, ti, *authCtx.ProjectID, issuerID, "impact-visible-toolset")
	sibling := seedSiblingProject(t, ctx, ti, "impact-sibling")

	_, err := ti.service.GetAttachImpact(ctx, &projectgen.GetAttachImpactPayload{McpServerID: new(uuid.NewString()), UserSessionIssuerID: new(issuerID.String())})
	requireOopsCode(t, err, oops.CodeNotFound)

	// Another project's issuer cannot be attached here, so it is not found.
	_, err = ti.service.GetAttachImpact(ctx, &projectgen.GetAttachImpactPayload{ToolsetID: new(toolset.ID.String()), UserSessionIssuerID: new(sibling.issuerID.String())})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestGetAttachImpactRBAC(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	issuerID := seedIssuer(t, ctx, ti, "impact-rbac")
	toolset := attachIssuerToolset(t, ctx, ti, *authCtx.ProjectID, issuerID, "impact-rbac-toolset")
	payload := &projectgen.GetAttachImpactPayload{ToolsetID: new(toolset.ID.String()), UserSessionIssuerID: new(issuerID.String())}

	writeCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeMCPWrite, toolset.ID.String()))
	_, err := ti.service.GetAttachImpact(writeCtx, payload)
	require.NoError(t, err)

	readCtx := withExactAuthzGrants(t, ctx, ti.conn, authz.NewGrant(authz.ScopeProjectRead, authCtx.ProjectID.String()))
	_, err = ti.service.GetAttachImpact(readCtx, payload)
	requireOopsCode(t, err, oops.CodeForbidden)
}

// The preview honors project-scoped MCP grants and exclusions as the toolset
// update it previews does.
func TestGetAttachImpactHonorsProjectScopedAuthorization(t *testing.T) {
	t.Parallel()

	projectGrant := func(projectID string, scope authz.Scope) authz.Grant {
		return authz.NewGrantWithSelector(scope, authz.Selector{
			authz.SelectorKeyResourceKind: authz.ResourceKindMCP,
			authz.SelectorKeyResourceID:   authz.WildcardResource,
			authz.SelectorKeyProjectID:    projectID,
		})
	}

	for _, tc := range []struct {
		name    string
		grants  func(string) []authz.Grant
		allowed bool
	}{
		{name: "matching project grant", grants: func(projectID string) []authz.Grant {
			return []authz.Grant{projectGrant(projectID, authz.ScopeMCPWrite)}
		}, allowed: true},
		{name: "other project grant", grants: func(string) []authz.Grant {
			return []authz.Grant{projectGrant(uuid.NewString(), authz.ScopeMCPWrite)}
		}},
		{name: "matching project exclusion", grants: func(projectID string) []authz.Grant {
			return []authz.Grant{
				authz.NewGrant(authz.ScopeMCPWrite, authz.WildcardResource),
				projectGrant(projectID, authz.ScopeMCPBlockedWrite),
			}
		}},
		{name: "other project exclusion", grants: func(string) []authz.Grant {
			return []authz.Grant{
				authz.NewGrant(authz.ScopeMCPWrite, authz.WildcardResource),
				projectGrant(uuid.NewString(), authz.ScopeMCPBlockedWrite),
			}
		}, allowed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestService(t)
			authCtx, ok := contextvalues.GetAuthContext(ctx)
			require.True(t, ok)
			issuerID := seedIssuer(t, ctx, ti, "impact-project-authz")
			toolset := attachIssuerToolset(t, ctx, ti, *authCtx.ProjectID, issuerID, "impact-project-authz-toolset")

			grantedCtx := withExactAuthzGrants(t, ctx, ti.conn, tc.grants(authCtx.ProjectID.String())...)
			_, err := ti.service.GetAttachImpact(grantedCtx, &projectgen.GetAttachImpactPayload{ToolsetID: new(toolset.ID.String()), UserSessionIssuerID: nil})
			if tc.allowed {
				require.NoError(t, err)
			} else {
				requireOopsCode(t, err, oops.CodeForbidden)
			}
		})
	}
}
