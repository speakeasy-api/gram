package skills_test

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	assistantsrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/auth/sessions"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/authztest"
	"github.com/speakeasy-api/gram/server/internal/billing"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/productfeatures"
	featurerepo "github.com/speakeasy-api/gram/server/internal/productfeatures/repo"
	projectrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/skilldiff"
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
	signaler       *captureSuggestionSignaler
}

type suggestionSignal struct {
	projectID uuid.UUID
	skillID   uuid.UUID
}

type captureSuggestionSignaler struct {
	mu      sync.Mutex
	signals []suggestionSignal
	err     error
}

func (s *captureSuggestionSignaler) SignalManual(_ context.Context, projectID, skillID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.signals = append(s.signals, suggestionSignal{projectID: projectID, skillID: skillID})
	return s.err
}

func (s *captureSuggestionSignaler) recorded() []suggestionSignal {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]suggestionSignal(nil), s.signals...)
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
	ctx = authztest.InitAuthContext(t, ctx, conn, sessionManager)

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

	authzEngine := authz.NewEngine(logger, conn, authztest.ChallengeLoggingAlwaysDisabled, workos.NewStubClient())
	features := productfeatures.NewClient(logger, tracerProvider, conn, redisClient)
	signaler := &captureSuggestionSignaler{signals: nil, err: nil}
	siteURL, err := url.Parse("https://app.getgram.test")
	require.NoError(t, err)
	service := skills.NewService(logger, tracerProvider, conn, sessionManager, authzEngine, features, audit.NewLogger(), signaler, siteURL)

	ti := &testInstance{
		service:        service,
		conn:           conn,
		repo:           repo.New(conn),
		features:       features,
		sessionManager: sessionManager,
		authContext:    authContext,
		projectID:      *authContext.ProjectID,
		signaler:       signaler,
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

func diffTo(t *testing.T, base, proposed string) string {
	t.Helper()

	diff, err := skilldiff.Unified(base, proposed)
	require.NoError(t, err)
	require.NotEmpty(t, diff)
	return diff
}

func applyDiff(t *testing.T, base, diff string) string {
	t.Helper()

	applied, err := skilldiff.Apply(base, diff)
	require.NoError(t, err)
	return applied
}

func suggestionFeedbackCount(t *testing.T, ctx context.Context, ti *testInstance, suggestionID uuid.UUID) int64 {
	t.Helper()

	count, err := ti.repo.CountSkillEditSuggestionFeedback(ctx, repo.CountSkillEditSuggestionFeedbackParams{
		ProjectID:    ti.projectID,
		SuggestionID: suggestionID,
	})
	require.NoError(t, err)
	return count
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

func createPlugin(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, name string) pluginsrepo.Plugin {
	t.Helper()

	plugin, err := pluginsrepo.New(ti.conn).CreatePlugin(ctx, pluginsrepo.CreatePluginParams{
		OrganizationID: ti.authContext.ActiveOrganizationID,
		ProjectID:      projectID,
		Name:           name,
		Slug:           name,
		Description:    pgtype.Text{},
	})
	require.NoError(t, err)
	return plugin
}

func deletePlugin(t *testing.T, ctx context.Context, ti *testInstance, plugin pluginsrepo.Plugin) {
	t.Helper()

	err := pluginsrepo.New(ti.conn).DeletePlugin(ctx, pluginsrepo.DeletePluginParams{
		ID:             plugin.ID,
		OrganizationID: plugin.OrganizationID,
		ProjectID:      plugin.ProjectID,
	})
	require.NoError(t, err)
}

func createAssistant(t *testing.T, ctx context.Context, ti *testInstance, projectID uuid.UUID, name string) assistantsrepo.CreateAssistantRow {
	t.Helper()
	assistant, err := assistantsrepo.New(ti.conn).CreateAssistant(ctx, assistantsrepo.CreateAssistantParams{
		ProjectID: projectID, OrganizationID: ti.authContext.ActiveOrganizationID,
		CreatedByUserID: conv.ToPGText(ti.authContext.UserID), Name: name, Model: "test-model",
		Instructions: "", WarmTtlSeconds: 60, MaxConcurrency: 1, Status: "active",
	})
	require.NoError(t, err)
	return assistant
}

func deleteAssistant(t *testing.T, ctx context.Context, ti *testInstance, projectID, assistantID uuid.UUID) {
	t.Helper()
	require.NoError(t, assistantsrepo.New(ti.conn).DeleteAssistant(ctx, assistantsrepo.DeleteAssistantParams{ProjectID: projectID, AssistantID: assistantID}))
}

func requireOopsCode(t *testing.T, err error, code oops.Code) {
	t.Helper()

	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, code, oopsErr.Code)
}

// seedSuggestionParams mirrors the fields a test cares about when standing up a
// suggestion, including the single proposed change most tests need.
type seedSuggestionParams struct {
	ProposedDiff       string
	Rationale          string
	ScoredSessionCount int64
	BaseVersionID      uuid.UUID
	ProjectID          uuid.UUID
	SkillID            uuid.UUID
}

// seedSuggestion writes a suggestion carrying one proposed change, which is the
// shape the analysis engine produces. It keeps the repository error so callers
// can assert on it the same way they do for a direct query.
func seedSuggestion(t *testing.T, ctx context.Context, ti *testInstance, params seedSuggestionParams) (repo.SkillEditSuggestion, error) {
	t.Helper()

	suggestion, err := ti.repo.CreateSkillEditSuggestion(ctx, repo.CreateSkillEditSuggestionParams{
		Rationale:          params.Rationale,
		ScoredSessionCount: params.ScoredSessionCount,
		BaseVersionID:      params.BaseVersionID,
		ProjectID:          params.ProjectID,
		SkillID:            params.SkillID,
	})
	if err != nil {
		return suggestion, fmt.Errorf("create skill suggestion: %w", err)
	}
	_, err = ti.repo.CreateSkillEditSuggestionChange(ctx, repo.CreateSkillEditSuggestionChangeParams{
		ProposedDiff: params.ProposedDiff,
		Rationale:    params.Rationale,
		Position:     0,
		ProjectID:    params.ProjectID,
		SuggestionID: suggestion.ID,
	})
	require.NoError(t, err)

	return suggestion, nil
}

// suggestionChanges returns the proposed changes attached to a suggestion, in
// the order a reviewer sees them.
func suggestionChanges(t *testing.T, ctx context.Context, ti *testInstance, suggestionID uuid.UUID) []repo.ListSkillEditSuggestionChangesRow {
	t.Helper()

	changes, err := ti.repo.ListSkillEditSuggestionChanges(ctx, repo.ListSkillEditSuggestionChangesParams{
		ProjectID:     ti.projectID,
		SuggestionIds: []uuid.UUID{suggestionID},
	})
	require.NoError(t, err)
	return changes
}

// suggestionContent applies every proposed change to base, which is the
// manifest a reviewer sees when taking the whole suggestion.
func suggestionContent(t *testing.T, ctx context.Context, ti *testInstance, suggestionID uuid.UUID, base string) string {
	t.Helper()

	content := base
	for _, change := range suggestionChanges(t, ctx, ti, suggestionID) {
		content = applyDiff(t, content, change.ProposedDiff)
	}
	return content
}
