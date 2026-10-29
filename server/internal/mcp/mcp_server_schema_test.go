package mcp_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	mockidp "github.com/speakeasy-api/gram/dev-idp/pkg/testidp"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mcpaccess"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

// schemaServer is one private, issuer-gated, remote-backed server the test
// user can try to use over HTTP.
type schemaServer struct {
	id     string
	slug   string
	issuer uuid.UUID
}

// schemaFixture is the frozen slice 1 fixture: in the test organization,
// project P1 holds servers S1 and S2 and project P2 holds S3. A foreign
// organization holds SB in its own project. The caller is the dev user, an
// active member of the test organization only, holding the custom role R.
type schemaFixture struct {
	ti       *testInstance
	orgID    string
	p1, p2   string
	s1, s2   schemaServer
	s3, sb   schemaServer
	role     urn.Principal
	upstream string
}

func newSchemaFixture(t *testing.T) *schemaFixture {
	t.Helper()

	ctx, ti := newTestMCPService(t)
	ti.mcpServerSchema.Store(true)

	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)

	upstream := newStatelessRemoteMCPUpstream(t, "ping", nil)
	f := &schemaFixture{
		ti:       ti,
		orgID:    authCtx.ActiveOrganizationID,
		p1:       authCtx.ProjectID.String(),
		upstream: upstream.URL,
	}

	p2, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Fixture P2", Slug: "fixture-p2-" + uuid.NewString()[:8], OrganizationID: f.orgID,
	})
	require.NoError(t, err)
	f.p2 = p2.ID.String()

	foreignOrg := "fixture-foreign-org-" + uuid.NewString()[:8]
	_, err = orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID: foreignOrg, Name: "Fixture foreign organization", Slug: foreignOrg,
		WorkosID: pgtype.Text{}, Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	pb, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{
		Name: "Fixture PB", Slug: "fixture-pb-" + uuid.NewString()[:8], OrganizationID: foreignOrg,
	})
	require.NoError(t, err)

	f.s1 = f.createServer(t, ctx, *authCtx.ProjectID)
	f.s2 = f.createServer(t, ctx, *authCtx.ProjectID)
	f.s3 = f.createServer(t, ctx, p2.ID)
	f.sb = f.createServer(t, ctx, pb.ID)

	now := time.Now().UTC()
	role, err := accessrepo.New(ti.conn).CreateOrganizationRole(ctx, accessrepo.CreateOrganizationRoleParams{
		OrganizationID:    f.orgID,
		WorkosSlug:        "fixture-role-" + uuid.NewString()[:8],
		WorkosName:        "Fixture role",
		WorkosDescription: conv.ToPGTextEmpty(""),
		WorkosCreatedAt:   conv.ToPGTimestamptz(now),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(now),
		WorkosLastEventID: conv.ToPGTextEmpty(""),
	})
	require.NoError(t, err)
	_, err = accessrepo.New(ti.conn).UpsertOrganizationRoleAssignment(ctx, accessrepo.UpsertOrganizationRoleAssignmentParams{
		OrganizationID:     f.orgID,
		WorkosUserID:       mockidp.MockUserID,
		UserID:             conv.ToPGText(mockidp.MockUserID),
		WorkosMembershipID: conv.ToPGText("fixture-membership"),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(now),
		WorkosLastEventID:  conv.ToPGTextEmpty(""),
		WorkosRoleSlug:     role.WorkosSlug,
	})
	require.NoError(t, err)
	f.role, err = urn.ParsePrincipal(role.RoleUrn)
	require.NoError(t, err)

	return f
}

func (f *schemaFixture) createServer(t *testing.T, ctx context.Context, projectID uuid.UUID) schemaServer {
	t.Helper()
	issuer := createUserSessionIssuer(t, ctx, f.ti.conn, projectID)
	slug := "fixture-" + uuid.NewString()
	server, _ := createRemoteMcpEndpoint(t, ctx, f.ti.conn, projectID, f.upstream, slug, "private", issuer)
	return schemaServer{id: server.ID.String(), slug: slug, issuer: issuer}
}

func (f *schemaFixture) user() urn.Principal {
	return urn.NewPrincipal(urn.PrincipalTypeUser, mockidp.MockUserID)
}

// grant stores one principal_grants row in the test organization.
func (f *schemaFixture) grant(t *testing.T, principal urn.Principal, scope authz.Scope, selector authz.Selector) {
	t.Helper()
	selectors, err := selector.MarshalJSON()
	require.NoError(t, err)
	_, err = accessrepo.New(f.ti.conn).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{
		OrganizationID: f.orgID,
		PrincipalUrn:   principal,
		Scope:          string(scope),
		Selectors:      selectors,
	})
	require.NoError(t, err)
}

func onMCPServer(id string, kv ...string) authz.Selector {
	sel := authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: id}
	for i := 0; i+1 < len(kv); i += 2 {
		sel[kv[i]] = kv[i+1]
	}
	return sel
}

// use initializes an MCP session on server as the dev user with a real
// user-session bearer, and reports whether the endpoint admitted it.
func (f *schemaFixture) use(t *testing.T, server schemaServer) bool {
	t.Helper()
	token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{
		Subject:  urn.NewUserSubject(mockidp.MockUserID),
		Audience: urn.NewUserSessionIssuer(server.issuer).String(),
		Issuer:   f.ti.serverURL.String() + "/x/mcp/" + server.slug,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, f.ti, server.issuer, urn.NewUserSubject(mockidp.MockUserID), jti)

	resp, err := servePublicHTTP(t, context.Background(), f.ti, server.slug, makeInitializeBody(), token, nil)
	if err != nil {
		var oopsErr *oops.ShareableError
		require.ErrorAs(t, err, &oopsErr)
		require.Equal(t, oops.CodeForbidden, oopsErr.Code, "use of %s: %v", server.id, err)
		require.Contains(t, oopsErr.Error(), mcpaccess.ServerPermissionDeniedMessage, "a denial keeps the request-access guidance")
		require.Contains(t, oopsErr.Error(), "scope=mcp%3Aconnect")
		return false
	}
	require.Equal(t, http.StatusOK, resp.Code, "initialize %s: %s", server.id, resp.Body.String())
	return true
}

func TestMCPServerSchema_Use_EditPermissionDoesNotConferUse(t *testing.T) {
	t.Parallel()
	f := newSchemaFixture(t)
	f.grant(t, f.user(), authz.ScopeMCPWrite, onMCPServer(f.s1.id))
	f.grant(t, f.user(), authz.ScopeMCPRead, onMCPServer(f.s2.id))

	require.False(t, f.use(t, f.s1), "row 1: mcp:write on S1 must not allow using S1")
	require.False(t, f.use(t, f.s2), "row 1: mcp:read on S2 must not allow using S2")
}

func TestMCPServerSchema_Use_ConnectionGrantNamesOneServer(t *testing.T) {
	t.Parallel()
	f := newSchemaFixture(t)
	f.grant(t, f.user(), authz.ScopeMCPConnect, onMCPServer(f.s1.id))

	require.True(t, f.use(t, f.s1), "row 2: connect on S1 allows S1")
	require.False(t, f.use(t, f.s2), "row 2: connect on S1 does not allow S2")
}

func TestMCPServerSchema_Use_InheritedBlockDefeatsDirectAllow(t *testing.T) {
	t.Parallel()
	f := newSchemaFixture(t)
	f.grant(t, f.user(), authz.ScopeMCPConnect, onMCPServer(f.s1.id))
	f.grant(t, f.role, authz.ScopeMCPBlockedConnect, onMCPServer(f.s1.id))

	require.False(t, f.use(t, f.s1), "row 3a: role R's block on S1 defeats the user's direct allow")
}

func TestMCPServerSchema_Use_BlockLeavesOtherServers(t *testing.T) {
	t.Parallel()
	f := newSchemaFixture(t)
	f.grant(t, authz.AllUsersPrincipal(), authz.ScopeMCPConnect, onMCPServer(f.s1.id))
	f.grant(t, authz.AllUsersPrincipal(), authz.ScopeMCPConnect, onMCPServer(f.s2.id))
	f.grant(t, f.role, authz.ScopeMCPBlockedConnect, onMCPServer(f.s1.id))

	require.False(t, f.use(t, f.s1), "row 3b: blocked on S1")
	require.True(t, f.use(t, f.s2), "row 3b: S2 is not blocked")
}

func TestMCPServerSchema_Use_ProjectRestrictionOnConcreteServerHolds(t *testing.T) {
	t.Parallel()
	f := newSchemaFixture(t)
	f.grant(t, f.user(), authz.ScopeMCPConnect, onMCPServer(f.s1.id, authz.SelectorKeyProjectID, f.p1))
	f.grant(t, f.user(), authz.ScopeMCPConnect, onMCPServer(f.s3.id, authz.SelectorKeyProjectID, f.p1))

	require.True(t, f.use(t, f.s1), "row 4: S1 is in P1")
	require.False(t, f.use(t, f.s3), "row 4: S3 is in P2, outside the grant's project")
}

func TestMCPServerSchema_Use_ForeignServerFails(t *testing.T) {
	t.Parallel()
	f := newSchemaFixture(t)
	f.grant(t, f.user(), authz.ScopeMCPConnect, onMCPServer(f.sb.id))
	f.grant(t, f.user(), authz.ScopeMCPWrite, onMCPServer(f.sb.id))

	require.False(t, f.use(t, f.sb), "row 5: grants in this organization never reach a server in another")
}

func TestMCPServerSchema_Use_LegacyBroadGrantIsHonored(t *testing.T) {
	t.Parallel()
	f := newSchemaFixture(t)
	f.grant(t, f.role, authz.ScopeMCPConnect, onMCPServer(authz.WildcardResource))

	require.True(t, f.use(t, f.s1), "row 6: organization-wide connect still allows S1")
	require.True(t, f.use(t, f.s3), "row 6: organization-wide connect still allows S3")
}

func TestMCPServerSchema_Use_InactiveOrganizationKeepsLegacyRules(t *testing.T) {
	t.Parallel()
	f := newSchemaFixture(t)
	f.ti.mcpServerSchema.Store(false)
	f.grant(t, f.user(), authz.ScopeMCPWrite, onMCPServer(f.s1.id))

	require.True(t, f.use(t, f.s1), "without the schema, mcp:write still implies use")
}
