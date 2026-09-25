package killswitchapi

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/visibility"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	infra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("clean up test infrastructure: %v", err)
	}
	os.Exit(code)
}

func newIntegrationService(t *testing.T) (*Service, *pgxpool.Pool, string, string, []uuid.UUID) {
	t.Helper()
	service, db, orgID, userID, servers, _ := newIntegrationServiceWithAdmin(t, true)
	return service, db, orgID, userID, servers
}

func newIntegrationServiceWithAdmin(t *testing.T, grantAdmin bool) (*Service, *pgxpool.Pool, string, string, []uuid.UUID, *authz.Engine) {
	t.Helper()
	db, err := infra.CloneTestDatabase(t, "killswitchapi_"+uuid.NewString()[:8])
	require.NoError(t, err)
	orgID := "org_" + uuid.NewString()
	userID := "user_" + uuid.NewString()
	require.NoError(t, orgrepo.New(db).CreateOrganizationMetadata(t.Context(), orgrepo.CreateOrganizationMetadataParams{ID: orgID, Name: "Test Organization", Slug: orgID}))
	seedOrganizationMember(t, db, orgID, userID, "Test User")
	if grantAdmin {
		selectors, selectorErr := authz.NewSelector(authz.ScopeOrgAdmin, orgID).MarshalJSON()
		require.NoError(t, selectorErr)
		_, grantErr := accessrepo.New(db).UpsertPrincipalGrant(t.Context(), accessrepo.UpsertPrincipalGrantParams{
			OrganizationID: orgID, PrincipalUrn: urn.NewPrincipal(urn.PrincipalTypeUser, userID), Scope: string(authz.ScopeOrgAdmin), Selectors: selectors,
		})
		require.NoError(t, grantErr)
	}
	_, servers := seedProjectServers(t, db, orgID, "project", "Server", "Server")
	registry, err := mcptoolexecution.NewRegistry(db)
	require.NoError(t, err)
	lifecycle, err := killswitches.NewLifecycleService(db, registry, mcptoolexecution.NewCustomerLifecycleValidator(), killswitches.NewAuditBeforeCommitHook(audit.NewLogger()))
	require.NoError(t, err)
	facade, err := killswitches.NewFacade(lifecycle)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(testenv.NewLogger(t), db, func(context.Context, string) (bool, error) { return false, nil }, workos.NewStubClient())
	authorized, err := killswitches.NewAuthorizedService(facade, authzEngine)
	require.NoError(t, err)
	user, ok := registry.PrincipalAdapter(mcptoolexecution.PrincipalKindUser)
	require.True(t, ok)
	server, ok := registry.ResourceAdapter(mcptoolexecution.ResourceKindMCPServer)
	require.True(t, ok)
	return &Service{db: db, authorized: authorized, user: user, server: server}, db, orgID, userID, servers, authzEngine
}

func insertForeignServer(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()
	orgID := "org_" + uuid.NewString()
	require.NoError(t, orgrepo.New(db).CreateOrganizationMetadata(t.Context(), orgrepo.CreateOrganizationMetadataParams{ID: orgID, Name: "Other Organization", Slug: orgID}))
	_, servers := seedProjectServers(t, db, orgID, "other", "Foreign Server")
	return servers[0]
}

// seedOrganizationMember inserts a user and their membership in orgID.
func seedOrganizationMember(t *testing.T, db *pgxpool.Pool, orgID string, userID string, displayName string) {
	t.Helper()
	queries := testrepo.New(db)
	require.NoError(t, queries.InsertUserFixture(t.Context(), testrepo.InsertUserFixtureParams{ID: userID, Email: userID + "@example.test", DisplayName: displayName}))
	require.NoError(t, queries.CreateOrganizationUserRelationshipFixture(t.Context(), testrepo.CreateOrganizationUserRelationshipFixtureParams{
		OrganizationID: orgID, UserID: pgtype.Text{String: userID, Valid: true},
	}))
}

// seedProjectServers inserts a project in orgID and one private MCP server,
// backed by its own toolset, per server name.
func seedProjectServers(t *testing.T, db *pgxpool.Pool, orgID string, projectName string, serverNames ...string) (uuid.UUID, []uuid.UUID) {
	t.Helper()
	queries := testrepo.New(db)
	projectID, err := queries.CreateProjectFixture(t.Context(), testrepo.CreateProjectFixtureParams{
		ID: uuid.Must(uuid.NewV7()), Name: projectName, Slug: "p-" + uuid.NewString()[:12], OrganizationID: orgID,
	})
	require.NoError(t, err)
	servers := make([]uuid.UUID, len(serverNames))
	for i, name := range serverNames {
		slug := "ts-" + uuid.NewString()[:12]
		toolsetID, err := queries.CreateToolsetFixture(t.Context(), testrepo.CreateToolsetFixtureParams{
			ID: uuid.Must(uuid.NewV7()), OrganizationID: orgID, ProjectID: projectID, Name: slug, Slug: slug,
		})
		require.NoError(t, err)
		server, err := mcpserversrepo.New(db).CreateMCPServer(t.Context(), mcpserversrepo.CreateMCPServerParams{
			ID: uuid.Must(uuid.NewV7()), ProjectID: projectID, Name: pgtype.Text{String: name, Valid: true},
			ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true}, Visibility: visibility.Private,
		})
		require.NoError(t, err)
		servers[i] = server.ID
	}
	return projectID, servers
}
