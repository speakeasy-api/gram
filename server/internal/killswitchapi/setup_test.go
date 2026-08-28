//nolint:glint // Integration fixtures intentionally use isolated raw SQL.
package killswitchapi

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/killswitches"
	"github.com/speakeasy-api/gram/server/internal/killswitches/mcptoolexecution"
	"github.com/speakeasy-api/gram/server/internal/testenv"
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

type allowAdmin struct{}

func (allowAdmin) RequireUserOrganizationScope(context.Context, string, string, authz.Scope) error {
	return nil
}

func newIntegrationService(t *testing.T) (*Service, *pgxpool.Pool, string, string, []uuid.UUID) {
	t.Helper()
	db, err := infra.CloneTestDatabase(t, "killswitchapi_"+uuid.NewString()[:8])
	require.NoError(t, err)
	orgID := "org_" + uuid.NewString()
	userID := "user_" + uuid.NewString()
	_, err = db.Exec(t.Context(), `INSERT INTO organization_metadata (id, name, slug) VALUES ($1, 'Test Organization', $1)`, orgID)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO users (id, email, display_name) VALUES ($1, $1 || '@example.test', 'Test User')`, userID)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO organization_user_relationships (organization_id, user_id) VALUES ($1, $2)`, orgID, userID)
	require.NoError(t, err)
	var projectID uuid.UUID
	require.NoError(t, db.QueryRow(t.Context(), `INSERT INTO projects (name, slug, organization_id) VALUES ('project', $1, $2) RETURNING id`, "p-"+uuid.NewString()[:12], orgID).Scan(&projectID))
	servers := make([]uuid.UUID, 2)
	for i := range servers {
		slug := "ts-" + uuid.NewString()[:12]
		var toolsetID uuid.UUID
		require.NoError(t, db.QueryRow(t.Context(), `INSERT INTO toolsets (organization_id, project_id, name, slug) VALUES ($1, $2, $3, $3) RETURNING id`, orgID, projectID, slug).Scan(&toolsetID))
		require.NoError(t, db.QueryRow(t.Context(), `INSERT INTO mcp_servers (project_id, name, toolset_id, visibility) VALUES ($1, $2, $3, 'private') RETURNING id`, projectID, "Server", toolsetID).Scan(&servers[i]))
	}
	registry, err := mcptoolexecution.NewRegistry(db)
	require.NoError(t, err)
	lifecycle, err := killswitches.NewLifecycleService(db, registry, killswitches.NewCustomerLifecycleValidator(), killswitches.NewAuditBeforeCommitHook(audit.NewLogger()))
	require.NoError(t, err)
	facade, err := killswitches.NewFacade(lifecycle)
	require.NoError(t, err)
	authorized, err := killswitches.NewAuthorizedService(facade, allowAdmin{})
	require.NoError(t, err)
	return &Service{db: db, authorized: authorized, user: mcptoolexecution.NewAuthenticatedUserPrincipalAdapter(db), server: mcptoolexecution.NewMCPServerResourceAdapter(db)}, db, orgID, userID, servers
}

func insertForeignServer(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()
	orgID := "org_" + uuid.NewString()
	_, err := db.Exec(t.Context(), `INSERT INTO organization_metadata (id, name, slug) VALUES ($1, 'Other Organization', $1)`, orgID)
	require.NoError(t, err)
	var projectID uuid.UUID
	require.NoError(t, db.QueryRow(t.Context(), `INSERT INTO projects (name, slug, organization_id) VALUES ('other', $1, $2) RETURNING id`, "p-"+uuid.NewString()[:12], orgID).Scan(&projectID))
	slug := "ts-" + uuid.NewString()[:12]
	var toolsetID uuid.UUID
	require.NoError(t, db.QueryRow(t.Context(), `INSERT INTO toolsets (organization_id, project_id, name, slug) VALUES ($1, $2, $3, $3) RETURNING id`, orgID, projectID, slug).Scan(&toolsetID))
	var serverID uuid.UUID
	require.NoError(t, db.QueryRow(t.Context(), `INSERT INTO mcp_servers (project_id, name, toolset_id, visibility) VALUES ($1, 'Foreign Server', $2, 'private') RETURNING id`, projectID, toolsetID).Scan(&serverID))
	return serverID
}
