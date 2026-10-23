package catalogpromotion

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/catalogpromotion/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/dns"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/mcpregistry"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/telemetry"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

var promotionInfra *testenv.Environment

func TestMain(m *testing.M) {
	env, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true})
	if err != nil {
		log.Fatal(err)
	}
	promotionInfra = env
	code := m.Run()
	if err := cleanup(); err != nil {
		log.Fatal(err)
	}
	os.Exit(code)
}

type fakePromotionFeatures struct{ enabled bool }

func (f fakePromotionFeatures) SnapshotStrict(context.Context, string) (productfeatures.ProductFeaturesSnapshot, error) {
	return productfeatures.ProductFeaturesSnapshot{LogsEnabled: f.enabled}, nil
}

type fakePromotionHealth struct {
	now      time.Time
	failures int64
	err      error
}

func (f *fakePromotionHealth) Outcomes(context.Context, telemetry.MCPServerTelemetryTarget, time.Time, time.Time) (*telemetry.MCPServerOutcomes, error) {
	return &telemetry.MCPServerOutcomes{Success: 20, Failed: f.failures}, f.err
}
func (f *fakePromotionHealth) Series(context.Context, telemetry.MCPServerTelemetryTarget, time.Time, time.Time, time.Duration) ([]telemetry.MCPServerSeriesPoint, error) {
	return []telemetry.MCPServerSeriesPoint{{BucketStart: f.now.Add(-24 * time.Hour).Truncate(24 * time.Hour), Total: 10}, {BucketStart: f.now.Truncate(24 * time.Hour), Total: 10}}, f.err
}

type promotionFixture struct {
	service *Service
	db      *pgxpool.Pool
	target  Target
	request Request
	health  *fakePromotionHealth
}

func newPromotionFixture(t *testing.T) promotionFixture {
	t.Helper()
	db, err := promotionInfra.CloneTestDatabase(t, "catalog_promotion")
	require.NoError(t, err)
	orgID := "org_synthetic_" + uuid.NewString()
	require.NoError(t, orgrepo.New(db).CreateOrganizationMetadata(t.Context(), orgrepo.CreateOrganizationMetadataParams{ID: orgID, Name: "Synthetic", Slug: "synthetic-" + uuid.NewString()}))
	project, err := projectrepo.New(db).CreateProject(t.Context(), projectrepo.CreateProjectParams{Name: "Synthetic", Slug: "synthetic", OrganizationID: orgID})
	require.NoError(t, err)
	now := time.Now().UTC()
	source, err := repo.New(db).SeedPromotionSourceFixture(t.Context(), repo.SeedPromotionSourceFixtureParams{ProjectID: project.ID, Url: "https://catalog.example.test/mcp", CreatedAt: conv.ToPGTimestamptz(now.Add(-7 * 24 * time.Hour))})
	require.NoError(t, err)
	validator, err := mcpregistry.LoadValidator()
	require.NoError(t, err)
	health := &fakePromotionHealth{now: now}
	policy := guardian.NewDefaultPolicy(testenv.NewTracerProvider(t), guardian.WithResolver(dns.NewMockResolver(dns.MockResolverConfig{LookupIPFunc: func(context.Context, string, string) ([]net.IP, error) { return []net.IP{net.ParseIP("8.8.8.8")}, nil }})))
	svc := New(db, mcpregistry.New(db, validator), nil, health, nil, policy)
	svc.features = fakePromotionFeatures{enabled: true}
	svc.now = func() time.Time { return now }
	target := Target{OrganizationID: orgID, ProjectID: project.ID, MCPServerID: source}
	candidate, err := svc.GetCandidate(t.Context(), target)
	require.NoError(t, err)
	require.True(t, candidate.Eligible)
	request := Request{Target: target, SourceRevision: candidate.SourceRevision, PublicMetadataConfirmed: true, RetryKey: "synthetic-retry",
		Public: PublicMetadata{Name: "io.example/catalog", Title: "Synthetic MCP", Description: "Public synthetic service", URL: "https://catalog.example.test/mcp", DocumentationURL: "https://docs.example.test/setup", DocsSlug: "synthetic-mcp", AuthMode: "none"}}
	return promotionFixture{service: svc, db: db, target: target, request: request, health: health}
}

func (f promotionFixture) promote(t *testing.T) (Candidate, error) {
	t.Helper()
	tx := testenv.BeginTx(t, t.Context(), f.db)
	defer o11y.NoLogDefer(func() error { return tx.Rollback(context.WithoutCancel(t.Context())) })
	result, err := f.service.PromoteTx(t.Context(), tx, f.request, "user:synthetic-staff")
	if err != nil {
		return Candidate{}, err
	}
	if err := tx.Commit(t.Context()); err != nil {
		return Candidate{}, fmt.Errorf("commit test promotion: %w", err)
	}
	return result, nil
}
