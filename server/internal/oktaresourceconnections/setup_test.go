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
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections"
	"github.com/speakeasy-api/gram/server/internal/oktaresourceconnections/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
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
	issuerID := provisiontest.CreateIssuer(t, ctx, si.conn, orgID, uuid.NullUUID{UUID: projectID, Valid: true}, "https://as.example/token")
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
	return provisiontest.CreateIssuer(t, ctx, si.conn, orgID, uuid.NullUUID{UUID: projectID, Valid: true}, "")
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
