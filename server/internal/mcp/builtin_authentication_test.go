package mcp_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/agents/runtimepolicy"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/builtinmcp"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	"github.com/speakeasy-api/gram/server/internal/mcp"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcproute"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/requestorigin"
	"github.com/speakeasy-api/gram/server/internal/sessiontokens"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	usersessionsrepo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

func builtinSlug(t *testing.T, ctx context.Context, ti *testInstance) string {
	t.Helper()
	ac, _ := contextvalues.GetAuthContext(ctx)
	org, err := orgrepo.New(ti.conn).GetOrganizationMetadata(ctx, ac.ActiveOrganizationID)
	require.NoError(t, err)
	project, err := projectsrepo.New(ti.conn).GetProjectByID(ctx, *ac.ProjectID)
	require.NoError(t, err)
	return org.Slug + "/" + project.Slug + "/slack"
}

func builtinIssuerFixture(t *testing.T, ctx context.Context, ti *testInstance, organization bool) usersessionsrepo.UserSessionIssuer {
	t.Helper()
	ac, _ := contextvalues.GetAuthContext(ctx)
	q := usersessionsrepo.New(ti.conn)
	duration := pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Valid: true}
	if organization {
		issuer, err := q.CreateOrganizationUserSessionIssuer(ctx, usersessionsrepo.CreateOrganizationUserSessionIssuerParams{OrganizationID: conv.ToPGText(ac.ActiveOrganizationID), Slug: "builtin-" + uuid.NewString(), AuthnChallengeMode: "interactive", SessionDuration: duration})
		require.NoError(t, err)
		return issuer
	}
	issuer, err := q.CreateUserSessionIssuer(ctx, usersessionsrepo.CreateUserSessionIssuerParams{ProjectID: *ac.ProjectID, OrganizationID: conv.ToPGText(ac.ActiveOrganizationID), Slug: "builtin-" + uuid.NewString(), AuthnChallengeMode: "interactive", SessionDuration: duration})
	require.NoError(t, err)
	return issuer
}

func builtinSessionFixture(t *testing.T, ctx context.Context, ti *testInstance, issuer uuid.UUID, subject urn.SessionSubject, resource uuid.UUID) usersessionsrepo.UserSession {
	t.Helper()
	ac, _ := contextvalues.GetAuthContext(ctx)
	var policy []byte
	var version pgtype.Int4
	var authorizer pgtype.Text
	if subject.Kind == urn.SessionSubjectKindAgent || subject.Kind == urn.SessionSubjectKindWorkload {
		resourceID := resource.String()
		if resource == uuid.Nil {
			resourceID = authz.WildcardResource
		}
		grants, err := runtimepolicy.NewDelegatedPolicyV1([]authz.Grant{{Scope: authz.ScopeMCPConnect, Selector: authz.Selector{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: resourceID, authz.SelectorKeyProjectID: ac.ProjectID.String()}}})
		require.NoError(t, err)
		policy, err = runtimepolicy.EncodeDelegatedPolicy(runtimepolicy.CurrentDelegatedPolicyVersion, grants)
		require.NoError(t, err)
		version = pgtype.Int4{Int32: int32(runtimepolicy.CurrentDelegatedPolicyVersion), Valid: true}
	}
	if subject.Kind == urn.SessionSubjectKindAgent {
		authorizer = conv.ToPGText(ac.UserID)
	}
	jti := sha256.Sum256([]byte(uuid.NewString()))
	session, err := usersessionsrepo.New(ti.conn).CreateUserSession(ctx, usersessionsrepo.CreateUserSessionParams{UserSessionIssuerID: issuer, SubjectUrn: subject, AuthorizerUserID: authorizer, DelegatedGrants: policy, DelegatedGrantsVersion: version, Jti: base64.RawURLEncoding.EncodeToString(jti[:]), RefreshTokenHash: conv.ToPGText(uuid.NewString()), RefreshExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}})
	require.NoError(t, err)
	return session
}

func builtinBearer(t *testing.T, ti *testInstance, endpoint *mcp.ResolvedMcpEndpoint, session usersessionsrepo.UserSession, audience string) string {
	t.Helper()
	root, err := endpoint.RootURL(ti.serverURL.String())
	require.NoError(t, err)
	token, _, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{Subject: session.SubjectUrn, Audience: audience, Issuer: root, ExpiresAt: &session.ExpiresAt.Time, ClientID: sessiontokens.FirstPartyClientID, JTI: session.Jti})
	require.NoError(t, err)
	return token
}

func TestServeBuiltin_SharedMachineAuthentication(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"agent", "workload", "human"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestMCPService(t)
			ac, _ := contextvalues.GetAuthContext(ctx)
			agentID, key := seedBuiltinAgent(t, ctx, ti, true)
			issuer := builtinIssuerFixture(t, ctx, ti, false)
			slug := builtinSlug(t, ctx, ti)
			endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(t.Context(), testenv.NewLogger(t), slug, mcproute.Builtin)
			require.NoError(t, err)
			require.False(t, endpoint.McpServerID.Valid)
			require.False(t, endpoint.ToolsetID.Valid)
			require.Equal(t, builtinmcp.ID(*ac.ProjectID, "slack"), endpoint.BuiltinID.UUID)
			subject := urn.NewAgentSubject(agentID)
			if kind == "human" {
				subject = urn.NewUserSubject(ac.UserID)
			}
			if kind == "workload" {
				workloadIssuer := seedWorkloadIssuer(t, ctx, ti, ac.ActiveOrganizationID)
				assignAgentToWorkload(t, ctx, ti, ac.ActiveOrganizationID, workloadIssuer, workloadSessionSubject, agentID)
				subject = urn.NewWorkloadSubject(workloadIssuer, workloadSessionSubject)
			}
			session := builtinSessionFixture(t, ctx, ti, issuer.ID, subject, endpoint.BuiltinID.UUID)
			token := builtinBearer(t, ti, endpoint, session, endpoint.AudienceURN)
			response, err := serveBuiltinHTTP(t, ti, *ac.ProjectID, token, makeInitializeBody(), nil)
			if kind == "human" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Contains(t, response.Body.String(), "Speakeasy Built-in MCP")
			root, err := endpoint.RootURL(ti.serverURL.String())
			require.NoError(t, err)
			_, err = serveBuiltinHTTP(t, ti, *ac.ProjectID, builtinBearer(t, ti, endpoint, session, root), makeInitializeBody(), nil)
			require.NoError(t, err, "resource-bound sessions use the canonical organization/project slug URL")
			_, err = serveBuiltinHTTP(t, ti, *ac.ProjectID, key, makeInitializeBody(), nil)
			require.NoError(t, err, "same endpoint admits the existing agent key mechanism")
			wrong := builtinBearer(t, ti, endpoint, session, urn.NewUserSessionIssuer(uuid.New()).String())
			response, err = serveBuiltinHTTP(t, ti, *ac.ProjectID, wrong, makeInitializeBody(), nil)
			require.Error(t, err)
			require.Contains(t, response.Header().Get("WWW-Authenticate"), "/mcp/builtin/"+slug)
			ceiling := builtinSessionFixture(t, ctx, ti, issuer.ID, subject, uuid.New())
			_, err = serveBuiltinHTTP(t, ti, *ac.ProjectID, builtinBearer(t, ti, endpoint, ceiling, endpoint.AudienceURN), makeInitializeBody(), nil)
			require.Error(t, err, "a credential for another resource cannot inherit the live explicit grant")
			_, err = usersessionsrepo.New(ti.conn).RevokeUserSession(ctx, usersessionsrepo.RevokeUserSessionParams{ID: session.ID, ProjectID: *ac.ProjectID, OrganizationID: ac.ActiveOrganizationID})
			require.NoError(t, err)
			_, err = serveBuiltinHTTP(t, ti, *ac.ProjectID, token, makeInitializeBody(), nil)
			require.Error(t, err, "revoked principal session cannot use cached policy")
		})
	}
}

func TestBuiltinIssuerSelectionAndMetadata(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	slug := builtinSlug(t, ctx, ti)
	_, key := seedBuiltinAgent(t, ctx, ti, true)
	_, err := ti.service.LoadResolvedMcpEndpointBySlug(t.Context(), testenv.NewLogger(t), slug, mcproute.Builtin)
	requireAgentGatewayCode(t, err, oops.CodeNotFound)
	response, err := serveBuiltinHTTP(t, ti, *ac.ProjectID, key, makeInitializeBody(), nil)
	require.NoError(t, err)
	require.Empty(t, response.Header().Get("WWW-Authenticate"))
	response, err = serveBuiltinHTTP(t, ti, *ac.ProjectID, "invalid", makeInitializeBody(), nil)
	require.Error(t, err)
	require.Empty(t, response.Header().Get("WWW-Authenticate"), "no nonexistent OAuth server advertised")
	orgIssuer := builtinIssuerFixture(t, ctx, ti, true)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(t.Context(), testenv.NewLogger(t), slug, mcproute.Builtin)
	require.NoError(t, err)
	require.Equal(t, orgIssuer.ID, endpoint.UserSessionIssuerID)
	builtinIssuerFixture(t, ctx, ti, false)
	endpoint, err = ti.service.LoadResolvedMcpEndpointBySlug(t.Context(), testenv.NewLogger(t), slug, mcproute.Builtin)
	require.NoError(t, err)
	require.Equal(t, orgIssuer.ID, endpoint.UserSessionIssuerID, "the persisted binding is not shadowed by later project issuers")
	response = httptest.NewRecorder()
	require.NoError(t, ti.service.ServeGetProtectedResource(response, httptest.NewRequest("GET", ti.serverURL.String()+"/.well-known/oauth-protected-resource/mcp/builtin/"+slug, nil), endpoint))
	require.Contains(t, response.Body.String(), "/mcp/builtin/"+slug)
}

func TestServeBuiltin_CrossProjectKeyDenied(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	_, key := seedBuiltinAgent(t, ctx, ti, true)
	other, err := projectsrepo.New(ti.conn).CreateProject(ctx, projectsrepo.CreateProjectParams{Name: "Other", Slug: "other-" + uuid.NewString()[:8], OrganizationID: ac.ActiveOrganizationID})
	require.NoError(t, err)
	_, err = serveBuiltinHTTP(t, ti, other.ID, key, makeInitializeBody(), nil)
	require.Error(t, err)
	builtinIssuerFixture(t, ctx, ti, true)
	_, err = serveBuiltinHTTP(t, ti, other.ID, key, makeInitializeBody(), nil)
	require.Error(t, err, "organization issuer must not widen a project key")
}

func TestServeBuiltin_IssuerWideWorkloadSession(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	agent, _ := seedBuiltinAgent(t, ctx, ti, true)
	issuer := builtinIssuerFixture(t, ctx, ti, false)
	setIssuerMode(t, ctx, ti, issuer.ID, "shared", "")
	workloadIssuer := seedWorkloadIssuer(t, ctx, ti, ac.ActiveOrganizationID)
	assignment := assignAgentToWorkload(t, ctx, ti, ac.ActiveOrganizationID, workloadIssuer, workloadSessionSubject, agent)
	subject := urn.NewWorkloadSubject(workloadIssuer, workloadSessionSubject)
	session := builtinSessionFixture(t, ctx, ti, issuer.ID, subject, uuid.Nil)
	token, _, err := sessiontokens.NewSigner("test-jwt-secret").Mint(sessiontokens.MintParams{Subject: subject, Audience: urn.NewUserSessionIssuerMCPServers(issuer.ID).String(), Issuer: ti.serverURL.JoinPath("oauth", "usi", issuer.ID.String()).String(), ExpiresAt: &session.ExpiresAt.Time, ClientID: sessiontokens.FirstPartyClientID, JTI: session.Jti})
	require.NoError(t, err)
	response, err := serveBuiltinHTTP(t, ti, *ac.ProjectID, token, makeInitializeBody(), nil)
	require.NoError(t, err)
	require.Contains(t, response.Body.String(), "Speakeasy Built-in MCP")
	unassignWorkloadAgent(t, ctx, ti, assignment)
	_, err = serveBuiltinHTTP(t, ti, *ac.ProjectID, token, makeInitializeBody(), nil)
	require.Error(t, err, "issuer-wide reach never bypasses the live workload assignment")
}

func TestBuiltinCachedNamespaceAndSharedResource(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	issuer := builtinIssuerFixture(t, ctx, ti, false)
	setIssuerMode(t, ctx, ti, issuer.ID, "shared", "")
	slug := builtinSlug(t, ctx, ti)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(t.Context(), testenv.NewLogger(t), slug, mcproute.Builtin)
	require.NoError(t, err)
	resource, err := endpoint.RootURL(ti.serverURL.String())
	require.NoError(t, err)
	shared, rejection, err := ti.service.ResolveSharedBuiltinForTest(t.Context(), endpoint, resource)
	require.NoError(t, err)
	require.Empty(t, rejection)
	require.Equal(t, endpoint.BuiltinID, shared.BuiltinID)
	sharedRef, err := shared.EndpointRef(t.Context(), ti.conn, ti.serverURL.String())
	require.NoError(t, err)
	require.True(t, sharedRef.SharedAuthorizationServer)
	sharedRestored, err := ti.service.ResolveCachedBuiltinForTest(t.Context(), sharedRef)
	require.NoError(t, err)
	require.NoError(t, sharedRestored.ValidateRef(sharedRef))
	_, rejection, err = ti.service.ResolveSharedBuiltinForTest(t.Context(), endpoint, resource+"/extra")
	require.NoError(t, err)
	require.NotEmpty(t, rejection)

	domains := customdomainsrepo.New(ti.conn)
	domain, err := domains.CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{OrganizationID: ac.ActiveOrganizationID, Domain: "builtin-ref.example.com", IpAllowlist: []string{"192.0.2.0/24"}})
	require.NoError(t, err)
	_, err = domains.SetCustomDomainVerified(ctx, domain.ID)
	require.NoError(t, err)
	rows, err := domains.ActivateVerifiedCustomDomain(ctx, customdomainsrepo.ActivateVerifiedCustomDomainParams{ID: domain.ID, ProvisionerKind: "ingress"})
	require.NoError(t, err)
	require.EqualValues(t, 1, rows)
	origin := "https://" + domain.Domain
	domainCtx := customdomains.WithContext(t.Context(), &customdomains.Context{OrganizationID: ac.ActiveOrganizationID, DomainID: domain.ID, Domain: domain.Domain})
	domainCtx = requestorigin.WithContext(domainCtx, requestorigin.Origin{Surface: requestorigin.SurfaceCustomDomain, BaseURL: origin, OrganizationID: ac.ActiveOrganizationID})
	endpoint, err = ti.service.LoadResolvedMcpEndpointBySlug(domainCtx, testenv.NewLogger(t), slug, mcproute.Builtin)
	require.NoError(t, err)
	ref, err := endpoint.EndpointRef(domainCtx, ti.conn, origin)
	require.NoError(t, err)
	restored, err := ti.service.ResolveCachedBuiltinForTest(t.Context(), ref)
	require.NoError(t, err, "global callback must retain the custom namespace, including its lockdown exemption")
	require.Equal(t, endpoint.BuiltinID, restored.BuiltinID)
	require.Equal(t, endpoint.CustomDomainID, restored.CustomDomainID)
	ref.BuiltinID.UUID = uuid.New()
	_, err = ti.service.ResolveCachedBuiltinForTest(t.Context(), ref)
	require.Error(t, err)
}

func TestBuiltinPrivateAuthority(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	builtinIssuerFixture(t, ctx, ti, false)
	ingressID := uuid.New()
	require.NoError(t, testrepo.New(ti.conn).InsertNetworkIngressFixture(ctx, testrepo.InsertNetworkIngressFixtureParams{ID: ingressID, OrganizationID: ac.ActiveOrganizationID, DnsName: conv.ToPGText("builtin-private.example.ts.net")}))
	origin := requestorigin.Origin{Surface: requestorigin.SurfacePrivateNetwork, BaseURL: "https://builtin-private.example.ts.net", OrganizationID: ac.ActiveOrganizationID, NetworkIngressID: ingressID}
	privateCtx := requestorigin.WithContext(t.Context(), origin)
	slug := builtinSlug(t, ctx, ti)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(privateCtx, testenv.NewLogger(t), slug, mcproute.Builtin)
	require.NoError(t, err)
	ref, err := endpoint.EndpointRef(privateCtx, ti.conn, origin.BaseURL)
	require.NoError(t, err)
	restored, err := ti.service.ResolveCachedBuiltinForTest(t.Context(), ref)
	require.NoError(t, err)
	require.Equal(t, endpoint.BuiltinID, restored.BuiltinID)
	origin.OrganizationID = "org_wrong_tenant"
	_, err = ti.service.LoadResolvedMcpEndpointBySlug(requestorigin.WithContext(t.Context(), origin), testenv.NewLogger(t), slug, mcproute.Builtin)
	require.Error(t, err)
	ref.Authority.NetworkIngressID = uuid.New()
	_, err = ti.service.ResolveCachedBuiltinForTest(t.Context(), ref)
	require.Error(t, err, "stale private authority must fail cached resumption")
}

func TestBuiltinAmbiguousIssuerNeverChoosesArbitrarily(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestMCPService(t)
	builtinIssuerFixture(t, ctx, ti, true)
	builtinIssuerFixture(t, ctx, ti, true)
	slug := builtinSlug(t, ctx, ti)
	_, err := ti.service.LoadResolvedMcpEndpointBySlug(t.Context(), testenv.NewLogger(t), slug, mcproute.Builtin)
	requireAgentGatewayCode(t, err, oops.CodeUnavailable)
	projectIssuer := builtinIssuerFixture(t, ctx, ti, false)
	endpoint, err := ti.service.LoadResolvedMcpEndpointBySlug(t.Context(), testenv.NewLogger(t), slug, mcproute.Builtin)
	require.NoError(t, err, "a unique project authority takes precedence even when the lower tier is ambiguous")
	require.Equal(t, projectIssuer.ID, endpoint.UserSessionIssuerID)
	builtinIssuerFixture(t, ctx, ti, false)
	endpoint, err = ti.service.LoadResolvedMcpEndpointBySlug(t.Context(), testenv.NewLogger(t), slug, mcproute.Builtin)
	require.NoError(t, err, "later issuers cannot make an existing binding ambiguous")
	require.Equal(t, projectIssuer.ID, endpoint.UserSessionIssuerID)
}
