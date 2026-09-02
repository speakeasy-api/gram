package hostedmcpbackfill

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
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
	issuerID uuid.NullUUID
}

func (f *fixture) seedToolset(t *testing.T, spec toolsetSpec) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, New(f.pool).SeedToolsetFixture(t.Context(), SeedToolsetFixtureParams{
		ID:                  id,
		OrganizationID:      f.orgID,
		ProjectID:           f.projectID,
		Name:                "Hosted " + spec.mcpSlug,
		Slug:                "ts-" + uuid.NewString()[:8],
		McpSlug:             conv.ToPGText(spec.mcpSlug),
		McpIsPublic:         spec.public,
		McpEnabled:          spec.enabled,
		CustomDomainID:      spec.domainID,
		UserSessionIssuerID: spec.issuerID,
	}))
	return id
}

func (f *fixture) seedDomain(t *testing.T, deletedAt *time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	var ts pgtype.Timestamptz
	if deletedAt != nil {
		ts = pgtype.Timestamptz{Time: *deletedAt, Valid: true}
	}
	require.NoError(t, New(f.pool).SeedCustomDomainFixture(t.Context(), SeedCustomDomainFixtureParams{
		ID: id, OrganizationID: f.orgID, Domain: "mcp-" + uuid.NewString()[:8] + ".example.test", DeletedAt: ts,
	}))
	return id
}

func (f *fixture) seedIssuer(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, New(f.pool).SeedUserSessionIssuerFixture(t.Context(), SeedUserSessionIssuerFixtureParams{
		ID:             id,
		ProjectID:      uuid.NullUUID{UUID: f.projectID, Valid: true},
		OrganizationID: conv.ToPGText(f.orgID),
		Slug:           "usi-" + uuid.NewString()[:8],
	}))
	return id
}

// Mirrors the manually created prod wrappers: foreign id, private, no endpoints.
func (f *fixture) seedForeignWrapper(t *testing.T, toolsetID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, New(f.pool).SeedWrapperFixture(t.Context(), SeedWrapperFixtureParams{
		ID:         id,
		ProjectID:  f.projectID,
		Name:       conv.ToPGText("manual wrapper"),
		Slug:       conv.ToPGText("manual-" + uuid.NewString()[:8]),
		ToolsetID:  uuid.NullUUID{UUID: toolsetID, Valid: true},
		Visibility: "private",
	}))
	return id
}

func (f *fixture) seedEndpoint(t *testing.T, wrapperID uuid.UUID, domainID uuid.NullUUID, slug string, root bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, New(f.pool).SeedEndpointFixture(t.Context(), SeedEndpointFixtureParams{
		ID: id, ProjectID: f.projectID, CustomDomainID: domainID,
		McpServerID: uuid.NullUUID{UUID: wrapperID, Valid: true}, Slug: slug,
		IsDomainRoot: pgtype.Bool{Bool: root, Valid: root},
	}))
	return id
}

func (f *fixture) seedGrant(t *testing.T, principal urn.Principal, scope authz.Scope, effect string, resourceID string) {
	t.Helper()
	selectors, err := json.Marshal(map[string]string{authz.SelectorKeyResourceKind: authz.ResourceKindMCP, authz.SelectorKeyResourceID: resourceID})
	require.NoError(t, err)
	var eff pgtype.Text
	if effect != "" {
		eff = conv.ToPGText(effect)
	}
	require.NoError(t, New(f.pool).SeedGrantFixture(t.Context(), SeedGrantFixtureParams{
		OrganizationID: f.orgID, PrincipalUrn: principal, Scope: string(scope), Effect: eff, Selectors: selectors,
	}))
}

func (f *fixture) run(t *testing.T, opts Options) Report {
	t.Helper()
	report, err := NewRunner(f.pool, opts).Run(t.Context())
	require.NoError(t, err)
	return report
}

func (f *fixture) apply(t *testing.T) Report {
	t.Helper()
	return f.run(t, Options{Apply: true})
}

func (f *fixture) wrapper(t *testing.T, toolsetID uuid.UUID) GetWrapperFixtureRow {
	t.Helper()
	w, err := New(f.pool).GetWrapperFixture(t.Context(), uuid.NullUUID{UUID: toolsetID, Valid: true})
	require.NoError(t, err)
	return w
}

func (f *fixture) endpoints(t *testing.T, wrapperID uuid.UUID) []ListEndpointsFixtureRow {
	t.Helper()
	rows, err := New(f.pool).ListEndpointsFixture(t.Context(), uuid.NullUUID{UUID: wrapperID, Valid: true})
	require.NoError(t, err)
	return rows
}

func (f *fixture) grants(t *testing.T) []ListGrantsFixtureRow {
	t.Helper()
	rows, err := New(f.pool).ListGrantsFixture(t.Context(), f.orgID)
	require.NoError(t, err)
	return rows
}
