package hostedmcpbackfill

import (
	"context"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
)

var testInfra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatalf("launch test infrastructure: %v", err)
	}
	testInfra = res
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatalf("cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

type fixture struct {
	pool      *pgxpool.Pool
	orgID     string
	projectID uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool, err := testInfra.CloneTestDatabase(t, "hostedmcpbackfill")
	require.NoError(t, err)
	return newFixtureIn(t, pool)
}

func newFixtureIn(t *testing.T, pool *pgxpool.Pool) *fixture {
	t.Helper()
	f := &fixture{pool: pool, orgID: "org_" + uuid.NewString(), projectID: uuid.New()}
	q := New(pool)
	require.NoError(t, q.SeedOrganizationFixture(t.Context(), SeedOrganizationFixtureParams{ID: f.orgID, Name: "org", Slug: "org-" + uuid.NewString()[:8]}))
	require.NoError(t, q.SeedProjectFixture(t.Context(), SeedProjectFixtureParams{ID: f.projectID, Name: "project", Slug: "p-" + uuid.NewString()[:8], OrganizationID: f.orgID}))
	return f
}

type toolsetSpec struct {
	mcpSlug  string
	public   bool
	enabled  bool
	domainID uuid.NullUUID
}

func (f *fixture) seedToolset(t *testing.T, spec toolsetSpec) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, New(f.pool).SeedToolsetFixture(t.Context(), SeedToolsetFixtureParams{
		ID:             id,
		OrganizationID: f.orgID,
		ProjectID:      f.projectID,
		Name:           "Hosted " + spec.mcpSlug,
		Slug:           "ts-" + uuid.NewString()[:8],
		McpSlug:        conv.ToPGText(spec.mcpSlug),
		McpIsPublic:    spec.public,
		McpEnabled:     spec.enabled,
		CustomDomainID: spec.domainID,
	}))
	return id
}

func (f *fixture) seedDomain(t *testing.T, deleted bool) uuid.NullUUID {
	t.Helper()
	id := uuid.New()
	var deletedAt pgtype.Timestamptz
	if deleted {
		deletedAt = pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true}
	}
	require.NoError(t, New(f.pool).SeedCustomDomainFixture(t.Context(), SeedCustomDomainFixtureParams{
		ID: id, OrganizationID: f.orgID, Domain: "mcp-" + uuid.NewString()[:8] + ".example.test", DeletedAt: deletedAt,
	}))
	return uuid.NullUUID{UUID: id, Valid: true}
}

func (f *fixture) seedServer(t *testing.T, id, toolsetID uuid.UUID, name, slug, visibility string) {
	t.Helper()
	require.NoError(t, New(f.pool).SeedServerFixture(t.Context(), SeedServerFixtureParams{
		ID: id, ProjectID: f.projectID, Name: conv.ToPGText(name), Slug: conv.ToPGText(slug),
		ToolsetID: uuid.NullUUID{UUID: toolsetID, Valid: true}, Visibility: visibility,
	}))
}

func (f *fixture) seedEndpoint(t *testing.T, serverID uuid.UUID, domainID uuid.NullUUID, slug string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, New(f.pool).SeedEndpointFixture(t.Context(), SeedEndpointFixtureParams{
		ID: id, ProjectID: f.projectID, CustomDomainID: domainID, McpServerID: uuid.NullUUID{UUID: serverID, Valid: true}, Slug: slug,
	}))
	return id
}

func (f *fixture) run(t *testing.T, opts Options) Report {
	t.Helper()
	report, err := NewRunner(f.pool, opts).Run(t.Context())
	require.NoError(t, err)
	return report
}

type dbState struct {
	servers   []McpServer
	endpoints []McpEndpoint
	audits    []ListAuditActorsFixtureRow
}

func (f *fixture) state(t *testing.T) dbState {
	t.Helper()
	q := New(f.pool)
	servers, err := q.ListServersFixture(t.Context(), f.projectID)
	require.NoError(t, err)
	endpoints, err := q.ListEndpointsFixture(t.Context(), f.projectID)
	require.NoError(t, err)
	audits, err := q.ListAuditActorsFixture(t.Context(), f.orgID)
	require.NoError(t, err)
	return dbState{servers: servers, endpoints: endpoints, audits: audits}
}

func (s dbState) server(t *testing.T, id uuid.UUID) McpServer {
	t.Helper()
	for _, server := range s.servers {
		if server.ID == id {
			return server
		}
	}
	require.FailNow(t, "server not found", id.String())
	return McpServer{}
}

func (s dbState) liveEndpoints(serverID uuid.UUID) []McpEndpoint {
	var out []McpEndpoint
	for _, endpoint := range s.endpoints {
		if endpoint.McpServerID == (uuid.NullUUID{UUID: serverID, Valid: true}) && !endpoint.Deleted {
			out = append(out, endpoint)
		}
	}
	return out
}

func rowFor(t *testing.T, report Report, toolsetID uuid.UUID) RowReport {
	t.Helper()
	for _, row := range report.Rows {
		if row.ToolsetID == toolsetID {
			return row
		}
	}
	require.FailNow(t, "row not found", toolsetID.String())
	return RowReport{}
}
