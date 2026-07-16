package skills_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/workos"
)

var infra *testenv.Environment

func TestMain(m *testing.M) {
	res, cleanup, err := testenv.Launch(context.Background(), testenv.LaunchOptions{Postgres: true, Redis: true, ClickHouse: true})
	if err != nil {
		log.Fatalf("Failed to launch test infrastructure: %v", err)
	}

	infra = res
	code := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Failed to cleanup test infrastructure: %v", err)
	}
	os.Exit(code)
}

type testInstance struct {
	service        *skills.Service
	conn           *pgxpool.Pool
	repo           *repo.Queries
	features       *productfeatures.Client
	sessionManager *sessions.Manager
	authContext    *contextvalues.AuthContext
	projectID      uuid.UUID
}

func newTestService(t *testing.T) (context.Context, *testInstance) {
	t.Helper()

	ctx := t.Context()
	logger := testenv.NewLogger(t)
	tracerProvider := testenv.NewTracerProvider(t)
	conn, err := infra.CloneTestDatabase(t, "testdb")
	require.NoError(t, err)

	redisClient, err := infra.NewRedisClient(t, 0)
	require.NoError(t, err)
	billingClient := billing.NewStubClient(logger, tracerProvider)
	sessionManager := testenv.NewTestManager(t, logger, tracerProvider, conn, redisClient, cache.Suffix("gram-local"), billingClient)
	ctx = testenv.InitAuthContext(t, ctx, conn, sessionManager)

	authContext, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)
	require.NotNil(t, authContext)

	organizationID := "skills-org-" + uuid.NewString()
	_, err = orgrepo.New(conn).UpsertOrganizationMetadata(ctx, orgrepo.UpsertOrganizationMetadataParams{
		ID:          organizationID,
		Name:        organizationID,
		Slug:        organizationID,
		WorkosID:    pgtype.Text{},
		Whitelisted: pgtype.Bool{},
	})
	require.NoError(t, err)
	projectSlug := "skills-" + uuid.NewString()[:8]
	project, err := projectrepo.New(conn).CreateProject(ctx, projectrepo.CreateProjectParams{
		Name:           projectSlug,
		Slug:           projectSlug,
		OrganizationID: organizationID,
	})
	require.NoError(t, err)
	authContext.ActiveOrganizationID = organizationID
	authContext.ProjectID = &project.ID
	authContext.ProjectSlug = &project.Slug
	ctx = contextvalues.SetAuthContext(ctx, authContext)

	chConn, err := infra.NewClickhouseClient(t)
	require.NoError(t, err)
	authzEngine := authz.NewEngine(logger, conn, chConn, authztest.RBACAlwaysEnabled, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	features := productfeatures.NewClient(logger, tracerProvider, conn, redisClient)
	service := skills.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, features, audit.NewLogger())

	ti := &testInstance{
		service:        service,
		conn:           conn,
		repo:           repo.New(conn),
		features:       features,
		sessionManager: sessionManager,
		authContext:    authContext,
		projectID:      *authContext.ProjectID,
	}
	enableSkills(t, ctx, ti)
	ctx = authztest.WithExactGrants(t, ctx, authz.NewGrant(authz.ScopeSkillWrite, ti.projectID.String()))

	return ctx, ti
}

func enableSkills(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()

	_, err := featurerepo.New(ti.conn).EnableFeature(ctx, featurerepo.EnableFeatureParams{
		OrganizationID: ti.authContext.ActiveOrganizationID,
		FeatureName:    string(productfeatures.FeatureSkills),
	})
	require.NoError(t, err)
	ti.features.UpdateFeatureCache(ctx, ti.authContext.ActiveOrganizationID, productfeatures.FeatureSkills, true)
}

func disableSkills(t *testing.T, ctx context.Context, ti *testInstance) {
	t.Helper()

	_, err := featurerepo.New(ti.conn).DeleteFeature(ctx, featurerepo.DeleteFeatureParams{
		OrganizationID: ti.authContext.ActiveOrganizationID,
		FeatureName:    string(productfeatures.FeatureSkills),
	})
	require.NoError(t, err)
	ti.features.UpdateFeatureCache(ctx, ti.authContext.ActiveOrganizationID, productfeatures.FeatureSkills, false)
}

func createProjectContext(t *testing.T, ctx context.Context, ti *testInstance, grants ...authz.Scope) (context.Context, uuid.UUID) {
	t.Helper()

	slug := fmt.Sprintf("skills-%s", uuid.NewString()[:8])
	project, err := projectrepo.New(ti.conn).CreateProject(ctx, projectrepo.CreateProjectParams{
		Name:           slug,
		Slug:           slug,
		OrganizationID: ti.authContext.ActiveOrganizationID,
	})
	require.NoError(t, err)

	authContext := *ti.authContext
	authContext.ProjectID = &project.ID
	authContext.ProjectSlug = &project.Slug
	projectCtx := contextvalues.SetAuthContext(ctx, &authContext)
	exactGrants := make([]authz.Grant, len(grants))
	for i, scope := range grants {
		exactGrants[i] = authz.NewGrant(scope, project.ID.String())
	}

	return authztest.WithExactGrants(t, projectCtx, exactGrants...), project.ID
}

func skillManifest(name, description, body string) string {
	return fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n\n%s\n", name, description, body)
}

func createSkill(t *testing.T, ctx context.Context, ti *testInstance, name, description string) *gen.RecordSkillResult {
	t.Helper()

	result, err := ti.service.Create(ctx, &gen.CreatePayload{
		Content:          skillManifest(name, description, "# "+name),
		SessionToken:     nil,
		ApikeyToken:      nil,
		ProjectSlugInput: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}
