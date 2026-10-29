package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/remote_mcp"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/remotemcp"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersrepo "github.com/speakeasy-api/gram/server/internal/users/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions"
)

// Configure a server without using it.
//
// One organization (A) has two projects. P1 holds the private, issuer-gated,
// remote-backed servers S1 and S2, plus S1b, which shares S1's upstream URL
// and therefore S1's scope pin. A second organization (B) holds SB. Every
// grant below is a stored principal_grants row and every caller is resolved
// by the production PrepareContext, so the test reads grants exactly as a
// request does.
//
// Expected outcomes (configure = setServerScopePin, capability = can_pin on
// getServerScopes, use = an initialize request on the server's endpoint):
//
//	caller        grants                                   pin S1  can_pin S1  use S1  use S2
//	operator      user mcp:write {S1}, {S1b}               allow   true        deny    deny
//	connector     user mcp:connect {S1}                    deny    (no read)   allow   deny
//	blocked       user mcp:connect {S1}, mcp:write {S1,S1b} deny   false       deny    deny
//	              + role mcp:blocked_connect {S1},
//	                     mcp:blocked_write {S1}
//	projscoped    user mcp:connect {S1, project_id:P1},    deny    (no read)   allow   deny
//	                   mcp:connect {S2, project_id:P2}
//	legacyMember  built-in Member role (wildcard mcp:connect) –     –           allow   allow
//	foreign       org A rows naming SB: mcp:write, connect  pin SB from P1: not found, nothing written; use SB: denied
//
// The legacy member row pins a shared contract: existing wildcard connection
// grants keep allowing use until they are migrated to named servers.
func TestConfigureWithoutUse(t *testing.T) {
	t.Parallel()

	f := newConfigureWithoutUseFixture(t)

	t.Run("operator configures S1 but cannot use it", func(t *testing.T) {
		t.Parallel()
		ctx := f.session(t, f.operator)

		scopes, err := f.getScopes(ctx, f.s1)
		require.NoError(t, err)
		require.True(t, scopes.CanPin)

		pinned, err := f.setPin(ctx, f.s1, "read")
		require.NoError(t, err)
		require.True(t, pinned.CanPin)

		f.requireUseDenied(t, f.operator, f.s1)
		f.requireUseDenied(t, f.operator, f.s2)
	})

	t.Run("connection grant permits S1 only", func(t *testing.T) {
		t.Parallel()
		ctx := f.session(t, f.connector)

		_, err := f.setPin(ctx, f.s1, "read")
		requireOopsCodeFromErr(t, err, oops.CodeForbidden)
		_, err = f.getScopes(ctx, f.s1)
		requireOopsCodeFromErr(t, err, oops.CodeForbidden)

		f.requireUseAllowed(t, f.connector, f.s1)
		f.requireUseDenied(t, f.connector, f.s2)
	})

	t.Run("matching block defeats direct allows", func(t *testing.T) {
		t.Parallel()
		ctx := f.session(t, f.blocked)

		scopes, err := f.getScopes(ctx, f.s1)
		require.NoError(t, err, "a write block does not block reading")
		require.False(t, scopes.CanPin, "the capability applies the inherited block")

		_, err = f.setPin(ctx, f.s1, "read")
		requireOopsCodeFromErr(t, err, oops.CodeForbidden)

		f.requireUseDenied(t, f.blocked, f.s1)
		f.requireUseDenied(t, f.blocked, f.s2)
	})

	t.Run("project-restricted grant keeps its project", func(t *testing.T) {
		t.Parallel()
		ctx := f.session(t, f.projScoped)

		_, err := f.setPin(ctx, f.s1, "read")
		requireOopsCodeFromErr(t, err, oops.CodeForbidden)

		f.requireUseAllowed(t, f.projScoped, f.s1)
		f.requireUseDenied(t, f.projScoped, f.s2)
	})

	t.Run("legacy wildcard connection grant still allows use", func(t *testing.T) {
		t.Parallel()

		f.requireUseAllowed(t, f.legacyMember, f.s1)
		f.requireUseAllowed(t, f.legacyMember, f.s2)
	})

	t.Run("foreign tenant target fails", func(t *testing.T) {
		t.Parallel()
		ctx := f.session(t, f.foreign)

		before := pinAuditCount(t, f)
		_, err := f.setPin(ctx, f.sb, "read")
		requireOopsCodeFromErr(t, err, oops.CodeNotFound)
		_, err = remotemcprepo.New(f.ti.conn).GetRemoteProtectedResource(ctx, remotemcprepo.GetRemoteProtectedResourceParams{ProjectID: f.sb.projectID, ResourceIdentifier: f.sb.upstream.URL})
		require.Error(t, err, "nothing is written to the other organization")
		require.Equal(t, before, pinAuditCount(t, f))

		f.requireUseDenied(t, f.foreign, f.sb)
	})
}

type slice1Server struct {
	id        uuid.UUID
	projectID uuid.UUID
	slug      string
	issuerID  uuid.UUID
	upstream  *httptest.Server
	hits      *atomic.Int64
}

type configureWithoutUseFixture struct {
	ti       *testInstance
	base     contextvalues.AuthContext
	remote   *remotemcp.Service
	s1, s1b  slice1Server
	s2, sb   slice1Server
	operator string
	connector,
	blocked,
	projScoped,
	legacyMember,
	foreign string
}

func newConfigureWithoutUseFixture(t *testing.T) *configureWithoutUseFixture {
	t.Helper()

	ctx, ti := newTestMCPServiceWithoutTemporal(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authCtx.ProjectID)
	orgA := authCtx.ActiveOrganizationID
	p1 := *authCtx.ProjectID

	p2, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Slice one P2", Slug: "slice1-p2-" + uuid.NewString()[:8], OrganizationID: orgA})
	require.NoError(t, err)

	orgB := "org-slice1-b-" + uuid.NewString()[:8]
	_, err = orgrepo.New(ti.conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:             orgB,
		Name:           "Slice one B",
		Slug:           orgB,
		WorkosID:       pgtype.Text{String: orgB, Valid: true},
		Whitelisted:    pgtype.Bool{Bool: false, Valid: false},
		CreationSource: pgtype.Text{String: "", Valid: false},
		DefaultHost:    pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	pb, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Slice one PB", Slug: "slice1-pb-" + uuid.NewString()[:8], OrganizationID: orgB})
	require.NoError(t, err)

	policy, err := guardian.NewUnsafePolicy(ti.tracerProvider, []string{})
	require.NoError(t, err)

	f := &configureWithoutUseFixture{
		ti:           ti,
		base:         *authCtx,
		remote:       remotemcp.NewService(ti.logger, ti.tracerProvider, ti.conn, ti.sessionManager, ti.enc, ti.authzEngine, policy, ti.audit, nil, ti.features),
		operator:     "user-slice1-operator-" + uuid.NewString()[:8],
		connector:    "user-slice1-connector-" + uuid.NewString()[:8],
		blocked:      "user-slice1-blocked-" + uuid.NewString()[:8],
		projScoped:   "user-slice1-projscoped-" + uuid.NewString()[:8],
		legacyMember: "user-slice1-legacy-" + uuid.NewString()[:8],
		foreign:      "user-slice1-foreign-" + uuid.NewString()[:8],
	}
	f.s1 = seedSlice1Server(t, ctx, ti, p1, nil)
	f.s1b = seedSlice1Server(t, ctx, ti, p1, &f.s1)
	f.s2 = seedSlice1Server(t, ctx, ti, p1, nil)
	f.sb = seedSlice1Server(t, ctx, ti, pb.ID, nil)

	for _, user := range []string{f.operator, f.connector, f.blocked, f.projScoped, f.legacyMember, f.foreign} {
		seedSlice1Member(t, ctx, ti, orgA, user)
	}

	// The built-in Member role holds a wildcard mcp:connect grant today.
	require.NoError(t, authz.SeedSystemRoleGrants(ctx, ti.conn, orgA))
	assignSlice1Role(t, ctx, ti, orgA, f.legacyMember, authz.SystemRoleMember)

	role, err := accessrepo.New(ti.conn).UpsertOrganizationRole(ctx, accessrepo.UpsertOrganizationRoleParams{
		OrganizationID:    orgA,
		WorkosSlug:        "slice1-blockers-" + uuid.NewString()[:8],
		WorkosName:        "Slice one blockers",
		WorkosDescription: pgtype.Text{String: "", Valid: false},
		WorkosCreatedAt:   conv.ToPGTimestamptz(time.Now()),
		WorkosUpdatedAt:   conv.ToPGTimestamptz(time.Now()),
		WorkosLastEventID: pgtype.Text{String: "", Valid: false},
	})
	require.NoError(t, err)
	assignSlice1Role(t, ctx, ti, orgA, f.blocked, role.WorkosSlug)
	rolePrincipal, err := urn.ParsePrincipal(role.RoleUrn)
	require.NoError(t, err)

	user := func(id string) urn.Principal { return urn.NewPrincipal(urn.PrincipalTypeUser, id) }
	server := func(scope authz.Scope, s slice1Server) authz.Selector { return authz.NewSelector(scope, s.id.String()) }
	inProject := func(scope authz.Scope, s slice1Server, project uuid.UUID) authz.Selector {
		sel := authz.NewSelector(scope, s.id.String())
		sel[authz.SelectorKeyProjectID] = project.String()
		return sel
	}
	for _, g := range []struct {
		principal urn.Principal
		scope     authz.Scope
		selector  authz.Selector
	}{
		{user(f.operator), authz.ScopeMCPWrite, server(authz.ScopeMCPWrite, f.s1)},
		{user(f.operator), authz.ScopeMCPWrite, server(authz.ScopeMCPWrite, f.s1b)},
		{user(f.connector), authz.ScopeMCPConnect, server(authz.ScopeMCPConnect, f.s1)},
		{user(f.blocked), authz.ScopeMCPConnect, server(authz.ScopeMCPConnect, f.s1)},
		{user(f.blocked), authz.ScopeMCPWrite, server(authz.ScopeMCPWrite, f.s1)},
		{user(f.blocked), authz.ScopeMCPWrite, server(authz.ScopeMCPWrite, f.s1b)},
		{rolePrincipal, authz.ScopeMCPBlockedConnect, server(authz.ScopeMCPBlockedConnect, f.s1)},
		{rolePrincipal, authz.ScopeMCPBlockedWrite, server(authz.ScopeMCPBlockedWrite, f.s1)},
		{user(f.projScoped), authz.ScopeMCPConnect, inProject(authz.ScopeMCPConnect, f.s1, p1)},
		{user(f.projScoped), authz.ScopeMCPConnect, inProject(authz.ScopeMCPConnect, f.s2, p2.ID)},
		{user(f.foreign), authz.ScopeMCPWrite, server(authz.ScopeMCPWrite, f.sb)},
		{user(f.foreign), authz.ScopeMCPConnect, server(authz.ScopeMCPConnect, f.sb)},
	} {
		selectors, err := g.selector.MarshalJSON()
		require.NoError(t, err)
		_, err = accessrepo.New(ti.conn).UpsertPrincipalGrant(ctx, accessrepo.UpsertPrincipalGrantParams{
			OrganizationID: orgA,
			PrincipalUrn:   g.principal,
			Scope:          string(g.scope),
			Selectors:      selectors,
		})
		require.NoError(t, err)
	}

	return f
}

// seedSlice1Server creates a private, issuer-gated, remote-backed server with
// its own endpoint and upstream. With shareWith set, the server reuses that
// server's upstream URL, so both share one scope pin.
func seedSlice1Server(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, shareWith *slice1Server) slice1Server {
	t.Helper()

	srv := slice1Server{projectID: projectID, slug: "slice1-" + uuid.NewString(), hits: &atomic.Int64{}}
	if shareWith != nil {
		srv.upstream = shareWith.upstream
		srv.hits = shareWith.hits
	} else {
		hits := srv.hits
		srv.upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"slice1","version":"1"}}}`))
		}))
		t.Cleanup(srv.upstream.Close)
	}
	srv.issuerID = createUserSessionIssuer(t, ctx, ti.conn, projectID)
	server, _ := createRemoteMcpEndpoint(t, ctx, ti.conn, projectID, srv.upstream.URL, srv.slug, "private", srv.issuerID)
	srv.id = server.ID
	return srv
}

func seedSlice1Member(t *testing.T, ctx context.Context, ti *testInstance, organizationID, userID string) {
	t.Helper()

	_, err := usersrepo.New(ti.conn).UpsertUser(ctx, usersrepo.UpsertUserParams{
		ID:          userID,
		Email:       userID + "@example.com",
		DisplayName: userID,
		PhotoUrl:    conv.PtrToPGText(nil),
		Admin:       false,
	})
	require.NoError(t, err)
	_, err = orgrepo.New(ti.conn).UpsertOrganizationUserRelationship(ctx, orgrepo.UpsertOrganizationUserRelationshipParams{
		OrganizationID: organizationID,
		UserID:         conv.ToPGText(userID),
	})
	require.NoError(t, err)
}

func assignSlice1Role(t *testing.T, ctx context.Context, ti *testInstance, organizationID, userID, roleSlug string) {
	t.Helper()

	_, err := accessrepo.New(ti.conn).UpsertOrganizationRoleAssignment(ctx, accessrepo.UpsertOrganizationRoleAssignmentParams{
		OrganizationID:     organizationID,
		WorkosUserID:       userID,
		UserID:             conv.ToPGText(userID),
		WorkosMembershipID: conv.ToPGText("membership_" + userID),
		WorkosUpdatedAt:    conv.ToPGTimestamptz(time.Now().UTC()),
		WorkosLastEventID:  conv.ToPGTextEmpty(""),
		WorkosRoleSlug:     roleSlug,
	})
	require.NoError(t, err)
}

// session is a dashboard session for userID in project P1, with grants
// prepared by the production engine from the stored rows.
func (f *configureWithoutUseFixture) session(t *testing.T, userID string) context.Context {
	t.Helper()

	authCtx := f.base
	authCtx.UserID = userID
	email := userID + "@example.com"
	authCtx.Email = &email
	ctx, err := f.ti.authzEngine.PrepareContext(contextvalues.SetAuthContext(t.Context(), &authCtx))
	require.NoError(t, err)
	return ctx
}

func (f *configureWithoutUseFixture) getScopes(ctx context.Context, srv slice1Server) (*gen.RemoteMcpServerScopes, error) {
	return f.remote.GetServerScopes(ctx, &gen.GetServerScopesPayload{McpServerID: srv.id.String(), SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}) //nolint:wrapcheck // returned for oops code assertions
}

func (f *configureWithoutUseFixture) setPin(ctx context.Context, srv slice1Server, scopes ...string) (*gen.RemoteMcpServerScopes, error) {
	return f.remote.SetServerScopePin(ctx, &gen.SetServerScopePinPayload{McpServerID: srv.id.String(), Scopes: scopes, SessionToken: nil, ApikeyToken: nil, ProjectSlugInput: nil}) //nolint:wrapcheck // returned for oops code assertions
}

// use sends initialize to srv's endpoint as userID, authenticated only by a
// user-session JWT from srv's issuer, as a remote MCP client would.
func (f *configureWithoutUseFixture) use(t *testing.T, userID string, srv slice1Server) (*httptest.ResponseRecorder, error) {
	t.Helper()

	token, jti, err := usersessions.NewSigner("test-jwt-secret").Mint(usersessions.MintParams{
		Subject:  urn.NewUserSubject(userID),
		Audience: urn.NewUserSessionIssuer(srv.issuerID).String(),
		Issuer:   f.ti.serverURL.String() + "/x/mcp/" + srv.slug,
		Lifetime: time.Hour,
	})
	require.NoError(t, err)
	persistTestUserSession(t, f.ti, srv.issuerID, urn.NewUserSubject(userID), jti)
	return servePublicHTTP(t, context.Background(), f.ti, srv.slug, makeInitializeBody(), token, nil)
}

func (f *configureWithoutUseFixture) requireUseAllowed(t *testing.T, userID string, srv slice1Server) {
	t.Helper()

	before := srv.hits.Load()
	w, err := f.use(t, userID, srv)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, w.Code, "initialize: %s", w.Body.String())
	require.Greater(t, srv.hits.Load(), before, "the upstream serves an allowed caller")
}

// requireUseDenied asserts the request is refused before any byte reaches the
// upstream. The shared upstream counter is per server, so a sibling's
// concurrent allowed call could only make this assertion stricter, never
// looser: S1's subtests that expect an allow run against S1 itself.
func (f *configureWithoutUseFixture) requireUseDenied(t *testing.T, userID string, srv slice1Server) {
	t.Helper()

	w, err := f.use(t, userID, srv)
	if err == nil {
		require.NotEqual(t, http.StatusOK, w.Code, "initialize must be refused: %s", w.Body.String())
		return
	}
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Contains(t, []oops.Code{oops.CodeForbidden, oops.CodeUnauthorized}, oopsErr.Code, "denied: %v", err)
}

func requireOopsCodeFromErr(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code, "error: %v", err)
}

func pinAuditCount(t *testing.T, f *configureWithoutUseFixture) int64 {
	t.Helper()

	count, err := audittest.AuditLogCountByAction(t.Context(), f.ti.conn, audit.ActionMcpServerScopePinUpdate)
	require.NoError(t, err)
	return count
}
