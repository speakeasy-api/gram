package oktaresourceconnections_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/okta_resource_connections"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections"
	"github.com/speakeasy-api/gram/server/internal/identityproviderconnections/provisiontest"
	idprepo "github.com/speakeasy-api/gram/server/internal/identityproviderconnections/repo"
	oktaapprepo "github.com/speakeasy-api/gram/server/internal/oktaapplications/repo"
	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections"
	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	infra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

type instance struct {
	svc          *oktaresourceconnections.Service
	conn         *pgxpool.Pool
	q            *repo.Queries
	orgID        string
	connectionID uuid.UUID
	flags        *feature.InMemory
	authCtx      *contextvalues.AuthContext
}

func createOrganization(t *testing.T, ctx context.Context, conn *pgxpool.Pool) string {
	t.Helper()
	orgID := "org_" + uuid.NewString()
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Okta Resource Connections Test Org",
		Slug:        "xaa-test-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
	return orgID
}

// newTestService builds the API over a fresh database with a live Okta
// connection (no agent yet) and org:admin for the organization.
func newTestService(t *testing.T) (context.Context, *instance) {
	t.Helper()
	ctx := t.Context()
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)
	orgID := createOrganization(t, ctx, conn)

	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billing.NewStubClient(logger, tracerProvider))
	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	authCtx.ActiveOrganizationID = orgID
	ctx = contextvalues.SetAuthContext(ctx, authCtx)

	flags := &feature.InMemory{}
	flags.SetFlag(feature.FlagOktaConnections, orgID, true)
	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	svc := oktaresourceconnections.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, audit.NewLogger(), flags)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, orgID), authz.NewGrant(authz.ScopeMCPRead, authz.WildcardResource))

	si := &instance{svc: svc, conn: conn, q: repo.New(conn), orgID: orgID, connectionID: uuid.Nil, flags: flags, authCtx: authCtx}
	si.connectionID = createConnection(t, ctx, si)
	return ctx, si
}

func createConnection(t *testing.T, ctx context.Context, si *instance) uuid.UUID {
	t.Helper()
	connectionID := provisiontest.CreateConnection(t, ctx, si.conn, si.orgID, identityproviderconnections.ProviderOkta)
	issuerID := provisiontest.CreateIssuer(t, ctx, si.conn, si.orgID, uuid.NullUUID{}, "https://tenant.okta.com/oauth2/v1/token")
	issuer, err := si.q.GetIssuerFixture(ctx, issuerID)
	require.NoError(t, err)
	clientID, err := si.q.CreateIssuerClientFixture(ctx, repo.CreateIssuerClientFixtureParams{
		ProjectID:             uuid.NullUUID{},
		OrganizationID:        conv.ToPGText(si.orgID),
		RemoteSessionIssuerID: issuerID,
		ClientID:              "0oaconnectionclient000",
		Scope:                 []string{},
		ResourceIdentifier:    pgtype.Text{},
	})
	require.NoError(t, err)
	_, err = idprepo.New(si.conn).CreateOktaIdentityProviderConnection(ctx, idprepo.CreateOktaIdentityProviderConnectionParams{
		IdentityProviderConnectionID: connectionID,
		OrganizationID:               si.orgID,
		OrgUrl:                       "https://tenant.okta.com",
		IssuerUrl:                    issuer.Issuer,
		RemoteSessionIssuerID:        issuerID,
		RemoteSessionClientID:        clientID,
		ListingMode:                  identityproviderconnections.ListingModeCustomApp,
	})
	require.NoError(t, err)
	markVerified(t, ctx, si.conn, si.orgID, connectionID)
	return connectionID
}

func markVerified(t *testing.T, ctx context.Context, conn *pgxpool.Pool, orgID string, connectionID uuid.UUID) {
	t.Helper()
	_, err := idprepo.New(conn).UpdateIdentityProviderConnectionVerification(ctx, idprepo.UpdateIdentityProviderConnectionVerificationParams{
		Status:         identityproviderconnections.StatusVerified,
		LastVerifiedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true, InfinityModifier: pgtype.Finite},
		LastError:      pgtype.Text{},
		ID:             connectionID,
		OrganizationID: orgID,
	})
	require.NoError(t, err)
}

func recordAgent(t *testing.T, ctx context.Context, si *instance, agentID string) {
	t.Helper()
	_, err := idprepo.New(si.conn).UpdateOktaIdentityProviderConnectionAgent(ctx, idprepo.UpdateOktaIdentityProviderConnectionAgentParams{
		AgentID:                      conv.ToPGTextEmpty(agentID),
		AgentAppID:                   pgtype.Text{},
		IdentityProviderConnectionID: si.connectionID,
		OrganizationID:               si.orgID,
	})
	require.NoError(t, err)
}

func createProject(t *testing.T, ctx context.Context, si *instance, orgID, slug string) uuid.UUID {
	t.Helper()
	id, err := testrepo.New(si.conn).CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{ID: uuid.New(), Name: slug, Slug: slug, OrganizationID: orgID})
	require.NoError(t, err)
	return id
}

// createResourceIssuer is an MCP server's authorization server with
// discovered metadata; capable ones advertise the ID-JAG profile and jwt-bearer.
func createResourceIssuer(t *testing.T, ctx context.Context, si *instance, orgID string, projectID uuid.UUID, capable bool) uuid.UUID {
	t.Helper()
	issuerID := createUndiscoveredIssuer(t, ctx, si, orgID, projectID)
	grants, profiles := []string{"authorization_code"}, []string{}
	if capable {
		grants = append(grants, oauthwire.GrantTypeJWTBearer)
		profiles = append(profiles, oauthwire.GrantProfileIDJAG)
	}
	n, err := si.q.SetIssuerGrantCapabilitiesFixture(ctx, repo.SetIssuerGrantCapabilitiesFixtureParams{
		GrantTypesSupported:                 grants,
		AuthorizationGrantProfilesSupported: profiles,
		ID:                                  issuerID,
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	return issuerID
}

func createUndiscoveredIssuer(t *testing.T, ctx context.Context, si *instance, orgID string, projectID uuid.UUID) uuid.UUID {
	t.Helper()
	return provisiontest.CreateIssuer(t, ctx, si.conn, orgID, uuid.NullUUID{UUID: projectID, Valid: true}, "https://as.example/token")
}

func createServer(t *testing.T, ctx context.Context, si *instance, projectID, issuerID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	id, _ := createServerWithBackend(t, ctx, si, projectID, issuerID, uuid.NullUUID{}, name)
	return id
}

// createServerWithBackend creates a remote-backed server; the backend URL is
// its resource indicator. A user session issuer makes Speakeasy front the login.
func createServerWithBackend(t *testing.T, ctx context.Context, si *instance, projectID, issuerID uuid.UUID, userSessionIssuerID uuid.NullUUID, name string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	slug := strings.ToLower(name) + "-" + uuid.NewString()[:8]
	backendID, err := si.q.CreateRemoteBackendFixture(ctx, repo.CreateRemoteBackendFixtureParams{ProjectID: projectID, Name: conv.ToPGText(name), Slug: conv.ToPGText(slug), Url: "https://" + slug + ".example/mcp"})
	require.NoError(t, err)
	id, err := si.q.CreateEligibleMCPServerFixture(ctx, repo.CreateEligibleMCPServerFixtureParams{
		ProjectID:             projectID,
		Name:                  conv.ToPGText(name),
		Slug:                  conv.ToPGText(slug),
		RemoteMcpServerID:     uuid.NullUUID{UUID: backendID, Valid: true},
		RemoteSessionIssuerID: uuid.NullUUID{UUID: issuerID, Valid: true},
		UserSessionIssuerID:   userSessionIssuerID,
		Visibility:            "private",
	})
	require.NoError(t, err)
	return id, backendID
}

func createUserSessionIssuer(t *testing.T, ctx context.Context, si *instance) uuid.UUID {
	t.Helper()
	id, err := testrepo.New(si.conn).InsertOrganizationTierUserSessionIssuerFixture(ctx, testrepo.InsertOrganizationTierUserSessionIssuerFixtureParams{
		OrganizationID:     conv.ToPGText(si.orgID),
		Slug:               "usi-" + uuid.NewString()[:8],
		AuthnChallengeMode: "interactive",
		SessionDuration:    pgtype.Interval{Microseconds: int64(time.Hour / time.Microsecond), Days: 0, Months: 0, Valid: true},
	})
	require.NoError(t, err)
	return id
}

func createHostedServer(t *testing.T, ctx context.Context, si *instance, orgID string, projectID uuid.UUID) uuid.UUID {
	t.Helper()
	q := testrepo.New(si.conn)
	toolsetID, err := q.CreateToolsetFixture(ctx, testrepo.CreateToolsetFixtureParams{ID: uuid.New(), OrganizationID: orgID, ProjectID: projectID, Name: "hosted", Slug: "hosted-" + uuid.NewString()[:8]})
	require.NoError(t, err)
	id, err := q.CreateRemoteMCPServerFixture(ctx, testrepo.CreateRemoteMCPServerFixtureParams{ID: uuid.New(), ProjectID: projectID, ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true}, Visibility: "private"})
	require.NoError(t, err)
	return id
}

func addClient(t *testing.T, ctx context.Context, si *instance, issuerID uuid.UUID, projectID uuid.NullUUID, clientID string) uuid.UUID {
	t.Helper()
	var orgID pgtype.Text
	if !projectID.Valid {
		orgID = conv.ToPGText(si.orgID)
	}
	id, err := si.q.CreateIssuerClientFixture(ctx, repo.CreateIssuerClientFixtureParams{
		ProjectID:             projectID,
		OrganizationID:        orgID,
		RemoteSessionIssuerID: issuerID,
		ClientID:              clientID,
		Scope:                 []string{"files:read"},
		ResourceIdentifier:    pgtype.Text{},
	})
	require.NoError(t, err)
	return id
}

func list(t *testing.T, ctx context.Context, si *instance, includeAll bool) *gen.ListOktaResourceConnectionsResult {
	t.Helper()
	res, err := si.svc.List(ctx, &gen.ListPayload{SessionToken: nil, IncludeAll: includeAll})
	require.NoError(t, err)
	return res
}

func rowFor(t *testing.T, res *gen.ListOktaResourceConnectionsResult, serverID uuid.UUID) *gen.OktaResourceConnectionServer {
	t.Helper()
	for _, r := range res.Servers {
		if r.McpServerID == serverID.String() {
			return r
		}
	}
	require.FailNow(t, "row missing", serverID.String())
	return nil
}

func confirm(t *testing.T, ctx context.Context, si *instance, audience string, appID *string, ids ...uuid.UUID) (*gen.ConfirmOktaResourceConnectionsResult, error) {
	t.Helper()
	items := make([]*gen.OktaResourceConnectionConfirmation, 0, len(ids))
	for _, id := range ids {
		items = append(items, &gen.OktaResourceConnectionConfirmation{McpServerID: id.String(), Audience: audience, OktaApplicationID: appID})
	}
	res, err := si.svc.Confirm(ctx, &gen.ConfirmPayload{SessionToken: nil, Connections: items})
	if err != nil {
		return nil, fmt.Errorf("confirm: %w", err)
	}
	return res, nil
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()
	require.Error(t, err)
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

func asSupportSession(ctx context.Context, si *instance) context.Context {
	support := *si.authCtx
	support.IsAdmin = true
	support.SupportOrganizationID = si.orgID
	return contextvalues.WithValidatedSupportSession(ctx, &support)
}

type fixture struct {
	projectID uuid.UUID
	issuerID  uuid.UUID
	serverID  uuid.UUID
}

// capableServer is one eligible server whose issuer advertises ID-JAG and has a single client.
func capableServer(t *testing.T, ctx context.Context, si *instance, name string) fixture {
	t.Helper()
	projectID := createProject(t, ctx, si, si.orgID, "proj-"+uuid.NewString()[:8])
	issuerID := createResourceIssuer(t, ctx, si, si.orgID, projectID, true)
	addClient(t, ctx, si, issuerID, uuid.NullUUID{UUID: projectID, Valid: true}, "0oa"+strings.ToLower(name)+"client")
	return fixture{projectID: projectID, issuerID: issuerID, serverID: createServer(t, ctx, si, projectID, issuerID, name)}
}

const audience = "https://auth.example.com"

func TestList_DerivesStatesAndValues(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)

	capable := capableServer(t, ctx, si, "Notion")
	incapable := createServer(t, ctx, si, capable.projectID, createResourceIssuer(t, ctx, si, si.orgID, capable.projectID, false), "Legacy")
	hosted := createHostedServer(t, ctx, si, si.orgID, capable.projectID)
	// Speakeasy fronting the login does not matter: readiness is about the upstream.
	fronted, _ := createServerWithBackend(t, ctx, si, capable.projectID, capable.issuerID, uuid.NullUUID{UUID: createUserSessionIssuer(t, ctx, si), Valid: true}, "Fronted")
	undiscovered := createServer(t, ctx, si, capable.projectID, createUndiscoveredIssuer(t, ctx, si, si.orgID, capable.projectID), "Undiscovered")
	_, deletedBackend := createServerWithBackend(t, ctx, si, capable.projectID, capable.issuerID, uuid.NullUUID{}, "Orphan")
	_, err := si.q.SoftDeleteRemoteBackendFixture(ctx, repo.SoftDeleteRemoteBackendFixtureParams{ID: deletedBackend, ProjectID: capable.projectID})
	require.NoError(t, err)

	// No agent yet: the capable servers need one; the others are not applicable and hidden by default.
	res := list(t, ctx, si, false)
	require.False(t, res.AgentRecorded)
	require.Equal(t, 2, res.PendingCount)
	require.Equal(t, 3, res.TotalCount, "hosted servers, undiscovered issuers, and dead backends are not eligible")
	require.Equal(t, 1, res.UndiscoveredCount)
	require.Len(t, res.Servers, 2)
	require.Nil(t, res.DeepLink)
	row := rowFor(t, res, capable.serverID)
	require.Equal(t, "needs_agent", row.State)
	require.True(t, row.Pending)
	require.Equal(t, "Notion", row.ServerName)
	require.True(t, strings.HasSuffix(row.ResourceIndicator, ".example/mcp"))
	require.Equal(t, "0oanotionclient", conv.PtrValOr(row.ClientID, ""))
	require.Equal(t, "single", row.ClientBinding)
	require.Equal(t, []string{"files:read"}, row.Scopes)
	require.Nil(t, row.DeepLink)
	require.Nil(t, row.Audience)
	require.Equal(t, "needs_agent", rowFor(t, res, fronted).State)

	all := list(t, ctx, si, true)
	require.Len(t, all.Servers, 3)
	require.Equal(t, "not_applicable", rowFor(t, all, incapable).State)
	require.Equal(t, "no_idjag", conv.PtrValOr(rowFor(t, all, incapable).NotApplicableReason, ""))
	require.Nil(t, rowFor(t, all, capable.serverID).NotApplicableReason)
	for _, r := range all.Servers {
		require.NotEqual(t, hosted.String(), r.McpServerID)
		require.NotEqual(t, undiscovered.String(), r.McpServerID)
	}

	recordAgent(t, ctx, si, "wlp1")
	res = list(t, ctx, si, false)
	require.True(t, res.AgentRecorded)
	row = rowFor(t, res, capable.serverID)
	require.Equal(t, "needs_connection", row.State)
	require.Equal(t, "https://tenant-admin.okta.com/admin/workload-principals/ai-agents/wlp1/resource-connections/create", conv.PtrValOr(row.DeepLink, ""))
	require.Equal(t, "https://tenant-admin.okta.com/admin/workload-principals/ai-agents/wlp1/resource-connections/create", conv.PtrValOr(res.DeepLink, ""))
}

func TestList_ClientBindingStates(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	recordAgent(t, ctx, si, "wlp1")

	projectID := createProject(t, ctx, si, si.orgID, "proj")
	missing := createServer(t, ctx, si, projectID, createResourceIssuer(t, ctx, si, si.orgID, projectID, true), "Missing")
	ambiguousIssuer := createResourceIssuer(t, ctx, si, si.orgID, projectID, true)
	addClient(t, ctx, si, ambiguousIssuer, uuid.NullUUID{UUID: projectID, Valid: true}, "0oaone")
	addClient(t, ctx, si, ambiguousIssuer, uuid.NullUUID{}, "0oatwo")
	ambiguous := createServer(t, ctx, si, projectID, ambiguousIssuer, "Ambiguous")

	res := list(t, ctx, si, true)
	require.Equal(t, "missing", rowFor(t, res, missing).ClientBinding)
	require.Nil(t, rowFor(t, res, missing).ClientID)
	require.Equal(t, "ambiguous", rowFor(t, res, ambiguous).ClientBinding)
}

func TestList_UsesAttachedClient(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	projectID := createProject(t, ctx, si, si.orgID, "proj")
	issuerID := createResourceIssuer(t, ctx, si, si.orgID, projectID, true)
	login := createUserSessionIssuer(t, ctx, si)
	serverID, _ := createServerWithBackend(t, ctx, si, projectID, issuerID, uuid.NullUUID{UUID: login, Valid: true}, "Fronted")
	addClient(t, ctx, si, issuerID, uuid.NullUUID{UUID: projectID, Valid: true}, "unattached")
	attached := addClient(t, ctx, si, issuerID, uuid.NullUUID{}, "attached")
	err := remotesessionsrepo.New(si.conn).AttachRemoteSessionClientToUserSessionIssuer(ctx, remotesessionsrepo.AttachRemoteSessionClientToUserSessionIssuerParams{
		RemoteSessionClientID: attached,
		UserSessionIssuerID:   login,
	})
	require.NoError(t, err)
	row := rowFor(t, list(t, ctx, si, true), serverID)
	require.Equal(t, "single", row.ClientBinding)
	require.Equal(t, "attached", conv.PtrValOr(row.ClientID, ""))
	require.Equal(t, []string{"files:read"}, row.Scopes)
}

func TestConfirmAndReset(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	recordAgent(t, ctx, si, "wlp1")
	a := capableServer(t, ctx, si, "Alpha")
	b := capableServer(t, ctx, si, "Beta")
	_, err := si.q.CreateOktaApplicationFixture(ctx, repo.CreateOktaApplicationFixtureParams{OrganizationID: si.orgID, IdentityProviderConnectionID: si.connectionID, OktaAppID: "0oa1234567890abcdef", Label: "Alpha - XAA", Name: "alpha_saml"})
	require.NoError(t, err)

	before, err := audittest.AuditLogCountByAction(ctx, si.conn, audit.ActionOktaResourceConnectionConfirm)
	require.NoError(t, err)

	appID := "0oa1234567890abcdef"
	confirmed, err := confirm(t, ctx, si, "  "+audience+"  ", &appID, a.serverID)
	require.NoError(t, err)
	require.Len(t, confirmed.Servers, 1)
	require.Equal(t, "connected", confirmed.Servers[0].State)
	require.False(t, confirmed.Servers[0].Pending)
	require.Equal(t, audience, conv.PtrValOr(confirmed.Servers[0].Audience, ""), "audience is trimmed and kept as typed")
	require.Equal(t, appID, conv.PtrValOr(confirmed.Servers[0].OktaApplicationID, ""))
	require.Equal(t, "Alpha - XAA", conv.PtrValOr(confirmed.Servers[0].OktaApplicationLabel, ""))
	require.NotNil(t, confirmed.Servers[0].ConfirmedAt)
	firstConfirmedAt := *confirmed.Servers[0].ConfirmedAt

	// Repeating keeps the row and the app instance; duplicates collapse; the audience updates.
	confirmed, err = confirm(t, ctx, si, audience+"/v2", nil, a.serverID, b.serverID, b.serverID)
	require.NoError(t, err)
	require.Len(t, confirmed.Servers, 2, "duplicate ids collapse")
	rowA := rowFor(t, &gen.ListOktaResourceConnectionsResult{Servers: confirmed.Servers, PendingCount: 0, TotalCount: 0, UndiscoveredCount: 0, AgentRecorded: true, ConnectionID: nil, DeepLink: nil}, a.serverID)
	require.Equal(t, firstConfirmedAt, *rowA.ConfirmedAt, "confirmation is idempotent")
	require.Equal(t, audience+"/v2", conv.PtrValOr(rowA.Audience, ""))
	require.Equal(t, appID, conv.PtrValOr(rowA.OktaApplicationID, ""), "an omitted app instance keeps the recorded one")

	after, err := audittest.AuditLogCountByAction(ctx, si.conn, audit.ActionOktaResourceConnectionConfirm)
	require.NoError(t, err)
	require.Equal(t, before+3, after, "one audit row per confirmed server per call")
	record, err := audittest.LatestAuditLogByAction(ctx, si.conn, audit.ActionOktaResourceConnectionConfirm)
	require.NoError(t, err)
	require.Equal(t, si.authCtx.UserID, record.ActorID)

	// Connected rows vanish from the default list; include_all keeps them.
	require.Empty(t, list(t, ctx, si, false).Servers)
	require.Len(t, list(t, ctx, si, true).Servers, 2)

	// Reset withdraws the row and audits.
	reset, err := si.svc.Reset(ctx, &gen.ResetPayload{SessionToken: nil, McpServerID: a.serverID.String()})
	require.NoError(t, err)
	require.Equal(t, "needs_connection", reset.State)
	require.Nil(t, reset.ConfirmedAt)
	require.Nil(t, reset.Audience)
	resets, err := audittest.AuditLogCountByAction(ctx, si.conn, audit.ActionOktaResourceConnectionReset)
	require.NoError(t, err)
	require.EqualValues(t, 1, resets)
	_, err = si.svc.Reset(ctx, &gen.ResetPayload{SessionToken: nil, McpServerID: a.serverID.String()})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)

	// Validation.
	_, err = confirm(t, ctx, si, "http://auth.example.com", nil, a.serverID)
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = confirm(t, ctx, si, audience+"?x=1", nil, a.serverID)
	requireOopsCode(t, err, oops.CodeBadRequest)
	bad := "not-an-app"
	_, err = confirm(t, ctx, si, audience, &bad, a.serverID)
	requireOopsCode(t, err, oops.CodeBadRequest)
	unknown := "0oaunknown0000000000"
	_, err = confirm(t, ctx, si, audience, &unknown, a.serverID)
	requireOopsCode(t, err, oops.CodeBadRequest)
	rows, err := si.q.ListResourceConnections(ctx, repo.ListResourceConnectionsParams{OrganizationID: si.orgID, IdentityProviderConnectionID: si.connectionID})
	require.NoError(t, err)
	require.Len(t, rows, 1, "a refused confirmation rolls back the whole batch")
}

func TestConfirm_RejectsRemovedApplication(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	recordAgent(t, ctx, si, "wlp1")
	server := capableServer(t, ctx, si, "Resource")
	appID := "0oa1234567890abcdef"
	_, err := si.q.CreateOktaApplicationFixture(ctx, repo.CreateOktaApplicationFixtureParams{OrganizationID: si.orgID, IdentityProviderConnectionID: si.connectionID, OktaAppID: appID, Label: "Resource", Name: "resource_saml"})
	require.NoError(t, err)
	_, err = confirm(t, ctx, si, audience, &appID, server.serverID)
	require.NoError(t, err)

	err = oktaapprepo.New(si.conn).RemoveApplications(ctx, oktaapprepo.RemoveApplicationsParams{
		RemovedAt:                    pgtype.Timestamptz{Time: time.Now(), Valid: true},
		OrganizationID:               si.orgID,
		IdentityProviderConnectionID: si.connectionID,
		OktaAppIds:                   []string{appID},
	})
	require.NoError(t, err)
	_, err = confirm(t, ctx, si, audience+"/changed", &appID, server.serverID)
	requireOopsCode(t, err, oops.CodeBadRequest)
	current := rowFor(t, list(t, ctx, si, true), server.serverID)
	require.Equal(t, audience, *current.Audience, "the rejected update must roll back")

	_, err = si.svc.Reset(ctx, &gen.ResetPayload{SessionToken: nil, McpServerID: server.serverID.String()})
	require.NoError(t, err)
	_, err = confirm(t, ctx, si, audience, &appID, server.serverID)
	requireOopsCode(t, err, oops.CodeBadRequest)
	require.Equal(t, "needs_connection", rowFor(t, list(t, ctx, si, true), server.serverID).State)
}

func TestSharedUpstream_OneRowManyServers(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	recordAgent(t, ctx, si, "wlp1")
	a := capableServer(t, ctx, si, "Shared")
	// A second server pointing at the same backend URL and issuer shares the confirmation.
	slug := "shared-twin-" + uuid.NewString()[:8]
	backendURL := rowFor(t, list(t, ctx, si, true), a.serverID).ResourceIndicator
	backendID, err := si.q.CreateRemoteBackendFixture(ctx, repo.CreateRemoteBackendFixtureParams{ProjectID: a.projectID, Name: conv.ToPGText("Twin"), Slug: conv.ToPGText(slug), Url: backendURL})
	require.NoError(t, err)
	twin, err := si.q.CreateEligibleMCPServerFixture(ctx, repo.CreateEligibleMCPServerFixtureParams{ProjectID: a.projectID, Name: conv.ToPGText("Twin"), Slug: conv.ToPGText(slug), RemoteMcpServerID: uuid.NullUUID{UUID: backendID, Valid: true}, RemoteSessionIssuerID: uuid.NullUUID{UUID: a.issuerID, Valid: true}, UserSessionIssuerID: uuid.NullUUID{}, Visibility: "private"})
	require.NoError(t, err)

	confirmed, err := confirm(t, ctx, si, audience, nil, a.serverID)
	require.NoError(t, err)
	require.Equal(t, "connected", confirmed.Servers[0].State)
	res := list(t, ctx, si, true)
	require.Equal(t, "connected", rowFor(t, res, twin).State, "the twin shares the row")
	rows, err := si.q.ListResourceConnections(ctx, repo.ListResourceConnectionsParams{OrganizationID: si.orgID, IdentityProviderConnectionID: si.connectionID})
	require.NoError(t, err)
	require.Len(t, rows, 1)

	// Resetting through one server withdraws the shared row for both.
	_, err = si.svc.Reset(ctx, &gen.ResetPayload{SessionToken: nil, McpServerID: a.serverID.String()})
	require.NoError(t, err)
	res = list(t, ctx, si, true)
	require.Equal(t, "needs_connection", rowFor(t, res, twin).State)
	require.Equal(t, "needs_connection", rowFor(t, res, a.serverID).State)
	_, err = si.svc.Reset(ctx, &gen.ResetPayload{SessionToken: nil, McpServerID: twin.String()})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)

	// Rebinding a server to another issuer moves it to a different upstream.
	_, err = confirm(t, ctx, si, audience, nil, twin)
	require.NoError(t, err)
	other := createResourceIssuer(t, ctx, si, si.orgID, a.projectID, true)
	_, err = si.q.SetMCPServerIssuerFixture(ctx, repo.SetMCPServerIssuerFixtureParams{RemoteSessionIssuerID: uuid.NullUUID{UUID: other, Valid: true}, ID: twin, ProjectID: a.projectID})
	require.NoError(t, err)
	res = list(t, ctx, si, true)
	require.Equal(t, "needs_connection", rowFor(t, res, twin).State)
	require.Equal(t, "connected", rowFor(t, res, a.serverID).State)
	_, err = si.svc.Reset(ctx, &gen.ResetPayload{SessionToken: nil, McpServerID: twin.String()})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)
}

func TestResourceConnections_OnlyServersTheAdminCanRead(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	recordAgent(t, ctx, si, "wlp1")
	visible := capableServer(t, ctx, si, "Notion")
	hidden := capableServer(t, ctx, si, "Linear")

	narrow := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, si.orgID), authz.NewGrant(authz.ScopeMCPRead, visible.serverID.String()))
	res, err := si.svc.List(narrow, &gen.ListPayload{SessionToken: nil, IncludeAll: true})
	require.NoError(t, err)
	require.Len(t, res.Servers, 1)
	require.Equal(t, visible.serverID.String(), res.Servers[0].McpServerID)
	require.Equal(t, 1, res.TotalCount)

	_, err = confirm(t, narrow, si, audience, nil, hidden.serverID)
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = si.svc.Reset(narrow, &gen.ResetPayload{SessionToken: nil, McpServerID: hidden.serverID.String()})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestList_IncludesPlatformGlobalClients(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	recordAgent(t, ctx, si, "wlp1")
	projectID := createProject(t, ctx, si, si.orgID, "proj-"+uuid.NewString()[:8])
	issuerID := createResourceIssuer(t, ctx, si, si.orgID, projectID, true)
	_, err := si.q.CreateIssuerClientFixture(ctx, repo.CreateIssuerClientFixtureParams{
		ProjectID: uuid.NullUUID{}, OrganizationID: pgtype.Text{}, RemoteSessionIssuerID: issuerID,
		ClientID: "0oaglobalclient", Scope: []string{"files:read"}, ResourceIdentifier: pgtype.Text{},
	})
	require.NoError(t, err)
	serverID := createServer(t, ctx, si, projectID, issuerID, "Notion")

	row := rowFor(t, list(t, ctx, si, true), serverID)
	require.Equal(t, "0oaglobalclient", *row.ClientID)
	require.Equal(t, oktaresourceconnections.ClientBindingSingle, row.ClientBinding)
}

func TestList_SkipsRevokedTunnels(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	recordAgent(t, ctx, si, "wlp1")
	f := capableServer(t, ctx, si, "Notion")

	tunneled := func(name, status string) uuid.UUID {
		slug := strings.ToLower(name) + "-" + uuid.NewString()[:8]
		backendID, err := si.q.CreateTunneledBackendFixture(ctx, repo.CreateTunneledBackendFixtureParams{
			ProjectID: f.projectID, Name: name, KeyHash: "hash-" + slug, KeyPrefix: "gram_tun", Status: status,
			ResourceIdentifier: conv.ToPGText("https://" + slug + ".example/mcp"),
		})
		require.NoError(t, err)
		id, err := si.q.CreateTunneledMCPServerFixture(ctx, repo.CreateTunneledMCPServerFixtureParams{
			ProjectID: f.projectID, Name: conv.ToPGText(name), Slug: conv.ToPGText(slug),
			TunneledMcpServerID:   uuid.NullUUID{UUID: backendID, Valid: true},
			RemoteSessionIssuerID: uuid.NullUUID{UUID: f.issuerID, Valid: true},
		})
		require.NoError(t, err)
		return id
	}
	live := tunneled("Live", "active")
	revoked := tunneled("Revoked", "revoked")

	res := list(t, ctx, si, true)
	ids := make([]string, 0, len(res.Servers))
	for _, server := range res.Servers {
		ids = append(ids, server.McpServerID)
	}
	require.Contains(t, ids, live.String())
	require.NotContains(t, ids, revoked.String())

	_, err := confirm(t, ctx, si, audience, nil, revoked)
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestResourceConnections_Guards(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	recordAgent(t, ctx, si, "wlp1")
	f := capableServer(t, ctx, si, "Epsilon")

	// Flag off: confirm refused, reads still work.
	si.flags.SetFlag(feature.FlagOktaConnections, si.orgID, false)
	_, err := confirm(t, ctx, si, audience, nil, f.serverID)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Len(t, list(t, ctx, si, true).Servers, 1)
	si.flags.SetFlag(feature.FlagOktaConnections, si.orgID, true)

	// Support sessions read but never confirm.
	_, err = confirm(t, asSupportSession(ctx, si), si, audience, nil, f.serverID)
	requireOopsCode(t, err, oops.CodeForbidden)
	require.Len(t, list(t, asSupportSession(ctx, si), si, true).Servers, 1)

	// Unknown and hosted ids are not found; a bad id is a bad request.
	_, err = confirm(t, ctx, si, audience, nil, uuid.New())
	requireOopsCode(t, err, oops.CodeNotFound)
	hosted := createHostedServer(t, ctx, si, si.orgID, f.projectID)
	_, err = confirm(t, ctx, si, audience, nil, hosted)
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = si.svc.Confirm(ctx, &gen.ConfirmPayload{SessionToken: nil, Connections: []*gen.OktaResourceConnectionConfirmation{{McpServerID: "nope", Audience: audience, OktaApplicationID: nil}}})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = si.svc.Confirm(ctx, &gen.ConfirmPayload{SessionToken: nil, Connections: []*gen.OktaResourceConnectionConfirmation{nil}})
	requireOopsCode(t, err, oops.CodeBadRequest)

	// Another organization cannot see or touch this organization's servers.
	otherOrg := createOrganization(t, ctx, si.conn)
	other := *si.authCtx
	other.ActiveOrganizationID = otherOrg
	otherCtx := authztest.WithExactGrants(t, contextvalues.SetAuthContext(ctx, &other), authz.NewGrant(authz.ScopeOrgAdmin, otherOrg), authz.NewGrant(authz.ScopeMCPRead, authz.WildcardResource))
	_, err = si.svc.List(otherCtx, &gen.ListPayload{SessionToken: nil, IncludeAll: true})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)
	otherSi := &instance{svc: si.svc, conn: si.conn, q: si.q, orgID: otherOrg, connectionID: uuid.Nil, flags: si.flags, authCtx: &other}
	otherSi.connectionID = createConnection(t, otherCtx, otherSi)
	si.flags.SetFlag(feature.FlagOktaConnections, otherOrg, true)
	require.Empty(t, list(t, otherCtx, otherSi, true).Servers)
	_, err = confirm(t, otherCtx, otherSi, audience, nil, f.serverID)
	requireOopsCode(t, err, oops.CodeNotFound)
	_, err = si.svc.Reset(otherCtx, &gen.ResetPayload{SessionToken: nil, McpServerID: f.serverID.String()})
	requireOopsCode(t, err, oops.CodeNotFound)

	// A disabled server is gone from the list and cannot be confirmed.
	_, err = si.q.SetMCPServerVisibilityFixture(ctx, repo.SetMCPServerVisibilityFixtureParams{Visibility: "disabled", ID: f.serverID, ProjectID: f.projectID})
	require.NoError(t, err)
	require.Empty(t, list(t, ctx, si, true).Servers)
	_, err = confirm(t, ctx, si, audience, nil, f.serverID)
	requireOopsCode(t, err, oops.CodeNotFound)

	// No live connection at all.
	provisiontest.SoftDeleteConnection(t, ctx, si.conn, si.orgID, si.connectionID)
	_, err = si.svc.List(ctx, &gen.ListPayload{SessionToken: nil, IncludeAll: false})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)
}

func TestConfirm_RequiresVerifiedConnection(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	recordAgent(t, ctx, si, "wlp1")
	f := capableServer(t, ctx, si, "Zeta")

	_, err := idprepo.New(si.conn).UpdateIdentityProviderConnectionVerification(ctx, idprepo.UpdateIdentityProviderConnectionVerificationParams{
		Status:         identityproviderconnections.StatusPending,
		LastVerifiedAt: pgtype.Timestamptz{},
		LastError:      pgtype.Text{},
		ID:             si.connectionID,
		OrganizationID: si.orgID,
	})
	require.NoError(t, err)
	require.Len(t, list(t, ctx, si, true).Servers, 1, "reads still work")
	_, err = confirm(t, ctx, si, audience, nil, f.serverID)
	requireOopsCode(t, err, oops.CodeFailedPrecondition)
	_, err = si.svc.Reset(ctx, &gen.ResetPayload{SessionToken: nil, McpServerID: f.serverID.String()})
	requireOopsCode(t, err, oops.CodeFailedPrecondition)
}

func TestDeleteForConnection(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	recordAgent(t, ctx, si, "wlp1")
	f := capableServer(t, ctx, si, "Theta")
	_, err := confirm(t, ctx, si, audience, nil, f.serverID)
	require.NoError(t, err)
	rows, err := si.q.ListResourceConnections(ctx, repo.ListResourceConnectionsParams{OrganizationID: si.orgID, IdentityProviderConnectionID: si.connectionID})
	require.NoError(t, err)
	require.Len(t, rows, 1)

	require.NoError(t, oktaresourceconnections.DeleteForConnection(ctx, si.conn, si.orgID, si.connectionID))
	rows, err = si.q.ListResourceConnections(ctx, repo.ListResourceConnectionsParams{OrganizationID: si.orgID, IdentityProviderConnectionID: si.connectionID})
	require.NoError(t, err)
	require.Empty(t, rows)
}
