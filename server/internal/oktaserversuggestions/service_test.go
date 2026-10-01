package oktaserversuggestions_test

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/okta_server_suggestions"
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
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	registryrepo "github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
	"github.com/speakeasy-api/gram/server/internal/oktaserversuggestions"
	"github.com/speakeasy-api/gram/server/internal/oktaserversuggestions/repo"
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
	svc          *oktaserversuggestions.Service
	conn         *pgxpool.Pool
	q            *repo.Queries
	orgID        string
	connectionID uuid.UUID
	flags        *feature.InMemory
	authCtx      *contextvalues.AuthContext
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

func createOrganization(t *testing.T, ctx context.Context, conn *pgxpool.Pool) string {
	t.Helper()
	orgID := "org_" + uuid.NewString()
	_, err := orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          orgID,
		Name:        "Okta Server Suggestions Test Org",
		Slug:        "suggest-test-" + uuid.NewString(),
		WorkosID:    conv.ToPGText(orgID),
		Whitelisted: pgtype.Bool{Bool: false, Valid: false},
	})
	require.NoError(t, err)
	return orgID
}

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
	validator, err := mcpregistry.LoadValidator()
	require.NoError(t, err)
	svc := oktaserversuggestions.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, audit.NewLogger(), flags, validator)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgAdmin, orgID))

	si := &instance{svc: svc, conn: conn, q: repo.New(conn), orgID: orgID, connectionID: uuid.Nil, flags: flags, authCtx: authCtx}
	si.connectionID = createConnection(t, ctx, si, orgID)
	return ctx, si
}

func createConnection(t *testing.T, ctx context.Context, si *instance, orgID string) uuid.UUID {
	t.Helper()
	connectionID := provisiontest.CreateConnection(t, ctx, si.conn, orgID, identityproviderconnections.ProviderOkta)
	issuerID := provisiontest.CreateIssuer(t, ctx, si.conn, orgID, uuid.NullUUID{}, "https://tenant.okta.com/oauth2/v1/token")
	issuer, err := si.q.GetIssuerFixture(ctx, issuerID)
	require.NoError(t, err)
	clientID, err := si.q.CreateIssuerClientFixture(ctx, repo.CreateIssuerClientFixtureParams{
		ProjectID:             uuid.NullUUID{},
		OrganizationID:        conv.ToPGText(orgID),
		RemoteSessionIssuerID: issuerID,
		ClientID:              "0oaconnectionclient000",
		Scope:                 []string{},
		ResourceIdentifier:    pgtype.Text{},
	})
	require.NoError(t, err)
	_, err = idprepo.New(si.conn).CreateOktaIdentityProviderConnection(ctx, idprepo.CreateOktaIdentityProviderConnectionParams{
		IdentityProviderConnectionID: connectionID,
		OrganizationID:               orgID,
		OrgUrl:                       "https://tenant.okta.com",
		IssuerUrl:                    issuer.Issuer,
		RemoteSessionIssuerID:        issuerID,
		RemoteSessionClientID:        clientID,
		ListingMode:                  identityproviderconnections.ListingModeCustomApp,
	})
	require.NoError(t, err)
	setStatus(t, ctx, si, orgID, connectionID, identityproviderconnections.StatusVerified)
	return connectionID
}

func setStatus(t *testing.T, ctx context.Context, si *instance, orgID string, connectionID uuid.UUID, status string) {
	t.Helper()
	_, err := idprepo.New(si.conn).UpdateIdentityProviderConnectionVerification(ctx, idprepo.UpdateIdentityProviderConnectionVerificationParams{
		Status:         status,
		LastVerifiedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true, InfinityModifier: pgtype.Finite},
		LastError:      pgtype.Text{},
		ID:             connectionID,
		OrganizationID: orgID,
	})
	require.NoError(t, err)
}

func entryRecord(name string, remotes []string, okta string) string {
	quoted := make([]string, 0, len(remotes))
	for _, u := range remotes {
		quoted = append(quoted, `{"type":"streamable-http","url":"`+u+`","headers":[{"name":"Authorization","description":"Bearer token","isRequired":true,"isSecret":true}]}`)
	}
	record := `{"server":{"name":"` + name + `","title":"` + strings.TrimPrefix(name, "example.test/") + `","description":"Demo","version":"1.0.0","icons":[{"src":"https://icons.example.test/icon.png"}],"remotes":[` + strings.Join(quoted, ",") + `]}`
	if okta != "" {
		record += `,"_meta":{"com.speakeasy.ai/catalog":{"documentationUrl":"https://docs.example.test","supportsDcr":true},"com.speakeasy.ai/okta":` + okta + `}`
	}
	return record + "}"
}

func createEntry(t *testing.T, ctx context.Context, si *instance, record string, published bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, registryrepo.New(si.conn).InsertRegistryEntryFixture(ctx, registryrepo.InsertRegistryEntryFixtureParams{ID: id, Data: json.RawMessage(record), Published: published}))
	return id
}

func createApp(t *testing.T, ctx context.Context, si *instance, appID, label, name, mode string, assigned bool) {
	t.Helper()
	_, err := si.q.CreateOktaApplicationFixture(ctx, repo.CreateOktaApplicationFixtureParams{
		OrganizationID: si.orgID, IdentityProviderConnectionID: si.connectionID,
		OktaAppID: appID, Label: label, Name: name, SignOnMode: mode, Status: "ACTIVE",
	})
	require.NoError(t, err)
	if assigned {
		require.NoError(t, si.q.CreateOktaAssignmentFixture(ctx, repo.CreateOktaAssignmentFixtureParams{
			OrganizationID: si.orgID, IdentityProviderConnectionID: si.connectionID,
			OktaAppID: appID, PrincipalKind: "user", OktaPrincipalID: "00u" + appID, AssignmentScope: "USER",
		}))
	}
}

// installServer fronts a remote backend with a live MCP server in a new project.
func installServer(t *testing.T, ctx context.Context, si *instance, orgID, url string) uuid.UUID {
	t.Helper()
	slug := "p-" + uuid.NewString()[:8]
	projectID, err := testrepo.New(si.conn).CreateProjectFixture(ctx, testrepo.CreateProjectFixtureParams{ID: uuid.New(), Name: slug, Slug: slug, OrganizationID: orgID})
	require.NoError(t, err)
	backendID, err := si.q.CreateRemoteBackendFixture(ctx, repo.CreateRemoteBackendFixtureParams{ProjectID: projectID, Name: conv.ToPGText("Server"), Slug: conv.ToPGText(slug), Url: url})
	require.NoError(t, err)
	serverID, err := si.q.CreateMCPServerFixture(ctx, repo.CreateMCPServerFixtureParams{ProjectID: projectID, Name: conv.ToPGText("Server"), Slug: conv.ToPGText(slug), RemoteMcpServerID: uuid.NullUUID{UUID: backendID, Valid: true}})
	require.NoError(t, err)
	return serverID
}

func list(t *testing.T, ctx context.Context, si *instance, includeAll bool) *gen.ListOktaServerSuggestionsResult {
	t.Helper()
	result, err := si.svc.List(ctx, &gen.ListPayload{SessionToken: nil, IncludeAll: includeAll})
	require.NoError(t, err)
	return result
}

func TestListMatchesAssignedActiveApplications(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	linear := createEntry(t, ctx, si, entryRecord("example.test/linear", []string{"https://mcp.linear.example/mcp", "https://mcp.linear.example/sse"}, `{"oinNames":["linear","integrator-4080826_linear_1"],"xaaSignOnModes":["SAML_2_0"],"xaaIssuer":"https://auth.linear.example"}`), true)
	createEntry(t, ctx, si, entryRecord("example.test/notion", []string{"https://mcp.notion.example/mcp"}, `{"oinNames":["notion"]}`), true)
	createEntry(t, ctx, si, entryRecord("example.test/draft", []string{"https://mcp.draft.example/mcp"}, `{"oinNames":["slack"]}`), false)
	createEntry(t, ctx, si, entryRecord("example.test/plain", []string{"https://mcp.plain.example/mcp"}, ""), true)
	createEntry(t, ctx, si, entryRecord("example.test/noremote", nil, `{"oinNames":["github"]}`), true)

	createApp(t, ctx, si, "0oa1", "Linear (SAML)", "linear", "SAML_2_0", true)
	createApp(t, ctx, si, "0oa2", "Linear (SWA)", "integrator-4080826_linear_1", "BROWSER_PLUGIN", true)
	createApp(t, ctx, si, "0oa3", "Notion", "notion", "OPENID_CONNECT", false)
	createApp(t, ctx, si, "0oa4", "Slack", "slack", "SAML_2_0", true)
	createApp(t, ctx, si, "0oa5", "GitHub", "github", "SAML_2_0", true)

	result := list(t, ctx, si, false)
	require.Equal(t, 1, result.OpenCount)
	require.Equal(t, 1, result.TotalCount)
	require.Equal(t, si.connectionID.String(), conv.PtrValOrEmpty(result.ConnectionID, ""))
	require.Len(t, result.Suggestions, 1)
	sg := result.Suggestions[0]
	require.Equal(t, linear.String(), sg.RegistryEntryID)
	require.Equal(t, "example.test/linear", sg.ServerName)
	require.Equal(t, "linear", conv.PtrValOrEmpty(sg.Title, ""))
	require.Equal(t, "https://docs.example.test", conv.PtrValOrEmpty(sg.DocumentationURL, ""))
	require.Equal(t, "https://icons.example.test/icon.png", conv.PtrValOrEmpty(sg.IconURL, ""))
	require.True(t, sg.SupportsDcr)
	require.Equal(t, "https://auth.linear.example", conv.PtrValOrEmpty(sg.XaaIssuer, ""))
	require.Equal(t, oktaserversuggestions.StateOpen, sg.State)
	require.Len(t, sg.Remotes, 2)
	require.Equal(t, "https://mcp.linear.example/mcp", sg.Remotes[0].URL)
	require.Len(t, sg.Remotes[0].Headers, 1)
	require.Equal(t, "Authorization", sg.Remotes[0].Headers[0].Name)
	require.True(t, sg.Remotes[0].Headers[0].IsRequired)
	require.True(t, sg.Remotes[0].Headers[0].IsSecret)
	require.Len(t, sg.OktaApplications, 2)
	require.Equal(t, "Linear (SAML)", sg.OktaApplications[0].Label)
	require.True(t, sg.OktaApplications[0].XaaSupported)
	require.Equal(t, 1, sg.OktaApplications[0].UserAssignments)
	require.Equal(t, "Linear (SWA)", sg.OktaApplications[1].Label)
	require.False(t, sg.OktaApplications[1].XaaSupported)

	// Notion becomes a suggestion once assigned; default modes accept OIDC.
	require.NoError(t, si.q.CreateOktaAssignmentFixture(ctx, repo.CreateOktaAssignmentFixtureParams{
		OrganizationID: si.orgID, IdentityProviderConnectionID: si.connectionID,
		OktaAppID: "0oa3", PrincipalKind: "group", OktaPrincipalID: "00g3", AssignmentScope: "",
	}))
	result = list(t, ctx, si, false)
	require.Len(t, result.Suggestions, 2)
	require.Equal(t, "example.test/notion", result.Suggestions[1].ServerName)
	require.True(t, result.Suggestions[1].OktaApplications[0].XaaSupported)
	require.Equal(t, 1, result.Suggestions[1].OktaApplications[0].GroupAssignments)

	// An entry with no remote to install is never suggested.
	require.NotContains(t, []string{result.Suggestions[0].ServerName, result.Suggestions[1].ServerName}, "example.test/noremote")

	// Removing every assignment drops it again.
	n, err := si.q.RemoveOktaAssignmentsFixture(ctx, repo.RemoveOktaAssignmentsFixtureParams{OrganizationID: si.orgID, IdentityProviderConnectionID: si.connectionID, OktaAppID: "0oa3"})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	require.Len(t, list(t, ctx, si, false).Suggestions, 1)
}

func TestDismissAndRestore(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	linear := createEntry(t, ctx, si, entryRecord("example.test/linear", []string{"https://mcp.linear.example/mcp"}, `{"oinNames":["linear"]}`), true)
	createEntry(t, ctx, si, entryRecord("example.test/notion", []string{"https://mcp.notion.example/mcp"}, `{"oinNames":["notion"]}`), true)
	createApp(t, ctx, si, "0oa1", "Linear", "linear", "SAML_2_0", true)
	createApp(t, ctx, si, "0oa2", "Notion", "notion", "OPENID_CONNECT", true)
	require.Equal(t, 2, list(t, ctx, si, false).OpenCount)

	view, err := si.svc.Dismiss(ctx, &gen.DismissPayload{SessionToken: nil, RegistryEntryID: linear.String()})
	require.NoError(t, err)
	require.Equal(t, oktaserversuggestions.StateDismissed, view.State)
	require.NotNil(t, view.DismissedAt)
	result := list(t, ctx, si, true)
	require.Equal(t, 1, result.OpenCount)
	require.Equal(t, 2, result.TotalCount)
	require.Equal(t, "example.test/notion", result.Suggestions[0].ServerName)
	require.Equal(t, oktaserversuggestions.StateDismissed, result.Suggestions[1].State)
	require.Len(t, list(t, ctx, si, false).Suggestions, 1)

	// Dismissing twice keeps the first timestamp.
	first := *view.DismissedAt
	view, err = si.svc.Dismiss(ctx, &gen.DismissPayload{SessionToken: nil, RegistryEntryID: linear.String()})
	require.NoError(t, err)
	require.Equal(t, first, *view.DismissedAt)

	view, err = si.svc.Restore(ctx, &gen.RestorePayload{SessionToken: nil, RegistryEntryID: linear.String()})
	require.NoError(t, err)
	require.Equal(t, oktaserversuggestions.StateOpen, view.State)
	require.Nil(t, view.DismissedAt)
	require.Equal(t, 2, list(t, ctx, si, false).OpenCount)
	// Restoring an open suggestion is a no-op.
	_, err = si.svc.Restore(ctx, &gen.RestorePayload{SessionToken: nil, RegistryEntryID: linear.String()})
	require.NoError(t, err)

	// Every change is audited with the state transition.
	rows, err := si.q.ListAuditRowsFixture(ctx, si.orgID)
	require.NoError(t, err)
	require.Len(t, rows, 4)
	require.Equal(t, string(audit.ActionOktaServerSuggestionRestore), rows[0].Action)
	require.Equal(t, string(audit.ActionOktaServerSuggestionDismiss), rows[3].Action)
	require.Equal(t, linear.String(), rows[3].SubjectID)
	require.Equal(t, "okta_server_suggestion", rows[3].SubjectType)
	require.Equal(t, "example.test/linear", rows[3].SubjectDisplayName.String)
	require.Equal(t, si.authCtx.UserID, rows[3].ActorID)

	// Only entries currently suggested to the organization can be acted on.
	for _, id := range []string{
		uuid.NewString(),
		createEntry(t, ctx, si, entryRecord("example.test/draft", nil, `{"oinNames":["draft"]}`), false).String(),
		createEntry(t, ctx, si, entryRecord("example.test/unmatched", nil, `{"oinNames":["github"]}`), true).String(),
		createEntry(t, ctx, si, entryRecord("example.test/plain", nil, ""), true).String(),
	} {
		_, err = si.svc.Dismiss(ctx, &gen.DismissPayload{SessionToken: nil, RegistryEntryID: id})
		requireOopsCode(t, err, oops.CodeNotFound)
		_, err = si.svc.Restore(ctx, &gen.RestorePayload{SessionToken: nil, RegistryEntryID: id})
		requireOopsCode(t, err, oops.CodeNotFound)
	}
	_, err = si.svc.Dismiss(ctx, &gen.DismissPayload{SessionToken: nil, RegistryEntryID: "nope"})
	requireOopsCode(t, err, oops.CodeBadRequest)
}

func TestInstalledDetection(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	linear := createEntry(t, ctx, si, entryRecord("example.test/linear", []string{"https://mcp.linear.example/mcp"}, `{"oinNames":["linear"]}`), true)
	notion := createEntry(t, ctx, si, entryRecord("example.test/notion", []string{"https://mcp.notion.example/mcp"}, `{"oinNames":["notion"]}`), true)
	createApp(t, ctx, si, "0oa1", "Linear", "linear", "SAML_2_0", true)
	createApp(t, ctx, si, "0oa2", "Notion", "notion", "OPENID_CONNECT", true)

	// A trailing slash on the admin-entered URL still counts, and installed
	// outranks dismissed while keeping the dismissal timestamp.
	serverID := installServer(t, ctx, si, si.orgID, "https://mcp.notion.example/mcp/")
	_, err := si.svc.Dismiss(ctx, &gen.DismissPayload{SessionToken: nil, RegistryEntryID: notion.String()})
	require.NoError(t, err)
	result := list(t, ctx, si, true)
	require.Equal(t, 1, result.OpenCount)
	require.Equal(t, linear.String(), result.Suggestions[0].RegistryEntryID)
	require.Equal(t, oktaserversuggestions.StateInstalled, result.Suggestions[1].State)
	require.Equal(t, []string{"https://mcp.notion.example/mcp"}, result.Suggestions[1].InstalledUrls)
	require.NotNil(t, result.Suggestions[1].DismissedAt)
	require.Len(t, list(t, ctx, si, false).Suggestions, 1)

	// A soft-deleted MCP server no longer counts: the backend alone is not a
	// running server.
	n, err := si.q.SoftDeleteMCPServerFixture(ctx, serverID)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	require.Equal(t, oktaserversuggestions.StateDismissed, list(t, ctx, si, true).Suggestions[1].State)
	_, err = si.svc.Restore(ctx, &gen.RestorePayload{SessionToken: nil, RegistryEntryID: notion.String()})
	require.NoError(t, err)
	installServer(t, ctx, si, si.orgID, "https://mcp.linear.example/mcp")
	result = list(t, ctx, si, false)
	require.Len(t, result.Suggestions, 1)
	require.Equal(t, notion.String(), result.Suggestions[0].RegistryEntryID)

	// Another organization's server never leaks in.
	other := createOrganization(t, ctx, si.conn)
	installServer(t, ctx, si, other, "https://mcp.notion.example/mcp")
	require.Equal(t, oktaserversuggestions.StateOpen, list(t, ctx, si, true).Suggestions[0].State)
}

func TestListSkipsInvalidRecords(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	createEntry(t, ctx, si, `{"server":{"name":"example.test/broken","description":"Broken","version":"1"},"_meta":{"com.speakeasy.ai/okta":{"oinNames":["linear"],"xaaIssuer":"ftp://nope"}}}`, true)
	createEntry(t, ctx, si, `{"server":{"name":"example.test/shape","description":"Broken","version":"1"},"_meta":{"com.speakeasy.ai/okta":{"oinNames":"linear"}}}`, true)
	createApp(t, ctx, si, "0oa1", "Linear", "linear", "SAML_2_0", true)
	result := list(t, ctx, si, true)
	require.Empty(t, result.Suggestions)
	require.Equal(t, 0, result.TotalCount)
}

func TestListIconScheme(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	withIcon := func(name, oin, src string) {
		record := strings.Replace(entryRecord(name, []string{"https://mcp." + oin + ".example/mcp"}, `{"oinNames":["`+oin+`"]}`), "https://icons.example.test/icon.png", src, 1)
		createEntry(t, ctx, si, record, true)
		createApp(t, ctx, si, "0oa-"+oin, oin, oin, "SAML_2_0", true)
	}
	withIcon("example.test/upper", "upper", "HTTPS://icons.example.test/icon.png")
	withIcon("example.test/plain", "plain", "http://icons.example.test/icon.png")

	icons := map[string]string{}
	for _, sg := range list(t, ctx, si, false).Suggestions {
		icons[sg.ServerName] = conv.PtrValOrEmpty(sg.IconURL, "")
	}
	require.Equal(t, map[string]string{
		"example.test/upper": "HTTPS://icons.example.test/icon.png",
		"example.test/plain": "",
	}, icons)
}

func TestPreconditionsAndAuthorization(t *testing.T) {
	t.Parallel()
	ctx, si := newTestService(t)
	linear := createEntry(t, ctx, si, entryRecord("example.test/linear", []string{"https://mcp.linear.example/mcp"}, `{"oinNames":["linear"]}`), true)
	createApp(t, ctx, si, "0oa1", "Linear", "linear", "SAML_2_0", true)

	si.flags.SetFlag(feature.FlagOktaConnections, si.orgID, false)
	_, err := si.svc.List(ctx, &gen.ListPayload{SessionToken: nil, IncludeAll: false})
	requireOopsCode(t, err, oops.CodeForbidden)
	si.flags.SetFlag(feature.FlagOktaConnections, si.orgID, true)

	// A pending or revoked connection serves nothing and writes nothing.
	for _, status := range []string{identityproviderconnections.StatusPending, identityproviderconnections.StatusRevoked} {
		setStatus(t, ctx, si, si.orgID, si.connectionID, status)
		_, err = si.svc.List(ctx, &gen.ListPayload{SessionToken: nil, IncludeAll: false})
		requireOopsCode(t, err, oops.CodeFailedPrecondition)
		_, err = si.svc.Dismiss(ctx, &gen.DismissPayload{SessionToken: nil, RegistryEntryID: linear.String()})
		requireOopsCode(t, err, oops.CodeFailedPrecondition)
	}
	dismissals, err := si.q.ListDismissals(ctx, si.orgID)
	require.NoError(t, err)
	require.Empty(t, dismissals)
	setStatus(t, ctx, si, si.orgID, si.connectionID, identityproviderconnections.StatusDegraded)
	require.Len(t, list(t, ctx, si, true).Suggestions, 1)

	readOnly := authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeOrgRead, si.orgID))
	_, err = si.svc.List(readOnly, &gen.ListPayload{SessionToken: nil, IncludeAll: false})
	require.Error(t, err)

	// Support sessions read but never dismiss. Legacy API keys are resolved by
	// the authorization engine from the key row, so they are not simulated here.
	support := asSupportSession(ctx, si)
	require.Len(t, list(t, support, si, true).Suggestions, 1)
	_, err = si.svc.Dismiss(support, &gen.DismissPayload{SessionToken: nil, RegistryEntryID: linear.String()})
	requireOopsCode(t, err, oops.CodeForbidden)
}
