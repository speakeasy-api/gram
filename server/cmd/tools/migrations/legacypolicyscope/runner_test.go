package legacypolicyscope

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/conv"
	projectsRepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
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

type seedPolicy struct {
	action       string
	sources      []string
	messageTypes []string
	scopeInclude string
	scopeExempt  string
}

func seed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, policies ...seedPolicy) []uuid.UUID {
	t.Helper()

	orgID := "org-" + uuid.NewString()
	slug := fmt.Sprintf("t%s", uuid.NewString()[:8])
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true, InfinityModifier: pgtype.Finite}

	require.NoError(t, testrepo.New(pool).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{
		ID: orgID, Name: slug, Slug: slug, GramAccountType: "free",
		WorkosID:    pgtype.Text{String: "", Valid: false},
		Whitelisted: true, FreeTrialStartedAt: now, FreeTrialEndsAt: now,
		DisabledAt: pgtype.Timestamptz{Time: time.Time{}, Valid: false, InfinityModifier: pgtype.Finite},
		CreatedAt:  now,
	}))

	project, err := projectsRepo.New(pool).CreateProject(ctx, projectsRepo.CreateProjectParams{
		Name: slug, Slug: slug, OrganizationID: orgID,
	})
	require.NoError(t, err)

	queries := testrepo.New(pool)
	ids := make([]uuid.UUID, 0, len(policies))
	for i, p := range policies {
		id, err := queries.SeedLegacyScopeRiskPolicyFixture(ctx, testrepo.SeedLegacyScopeRiskPolicyFixtureParams{
			ProjectID: project.ID, OrganizationID: orgID,
			Name:    fmt.Sprintf("policy-%d", i),
			Sources: p.sources, Action: p.action, MessageTypes: p.messageTypes,
			ScopeInclude: conv.ToPGTextEmpty(p.scopeInclude),
			ScopeExempt:  conv.ToPGTextEmpty(p.scopeExempt),
		})
		require.NoError(t, err)
		ids = append(ids, id)
	}
	return ids
}

func readPolicy(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id uuid.UUID) (testrepo.ReadRiskPolicyScopeFixtureRow, []ra.DetectionScopeConfig) {
	t.Helper()
	row, err := testrepo.New(pool).ReadRiskPolicyScopeFixture(ctx, id)
	require.NoError(t, err)
	return row, ra.DetectionScopesFromConfig(row.AnalyzerConfig)
}

func newRunner(t *testing.T, pool *pgxpool.Pool) *Runner {
	t.Helper()
	runner, err := NewRunner(pool, slog.New(slog.DiscardHandler), Options{
		BatchSize: 10, LockTimeout: 0, StatementTimeout: 0,
	})
	require.NoError(t, err)
	return runner
}

func TestRunnerDryRunWritesNothing(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	pool, err := testInfra.CloneTestDatabase(t, "legacyscope_dryrun")
	require.NoError(t, err)

	ids := seed(t, ctx, pool, seedPolicy{
		action: "block", sources: []string{"gitleaks"},
		messageTypes: []string{"tool_request"}, scopeInclude: "", scopeExempt: "",
	})

	summary, err := newRunner(t, pool).Run(ctx, ModeDryRun)
	require.NoError(t, err)
	require.Equal(t, int64(1), summary.Scanned)
	require.Equal(t, int64(1), summary.Preserved)
	require.Equal(t, int64(0), summary.Updated)
	require.Equal(t, int64(1), summary.Remaining)

	row, scopes := readPolicy(t, ctx, pool, ids[0])
	require.Empty(t, scopes)
	require.Equal(t, []string{"tool_request"}, row.MessageTypes)
}

func TestRunnerApplyFoldsByAction(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	pool, err := testInfra.CloneTestDatabase(t, "legacyscope_apply")
	require.NoError(t, err)

	ids := seed(t, ctx, pool,
		seedPolicy{action: "block", sources: []string{"gitleaks"},
			messageTypes: []string{"tool_request", "tool_response"}, scopeInclude: "", scopeExempt: ""},
		seedPolicy{action: "flag", sources: []string{"gitleaks"},
			messageTypes: []string{"tool_request"}, scopeInclude: "", scopeExempt: ""},
	)

	summary, err := newRunner(t, pool).Run(ctx, ModeApply)
	require.NoError(t, err)
	require.Equal(t, int64(2), summary.Scanned)
	require.Equal(t, int64(1), summary.Preserved)
	require.Equal(t, int64(1), summary.Cleared)
	require.Equal(t, int64(2), summary.Updated)
	require.Equal(t, int64(0), summary.Remaining)

	// Enforcing policy keeps its narrowing, now expressed per category.
	blockRow, blockScopes := readPolicy(t, ctx, pool, ids[0])
	require.Empty(t, blockRow.MessageTypes)
	require.False(t, blockRow.ScopeInclude.Valid)
	require.False(t, blockRow.ScopeExempt.Valid)
	require.Len(t, blockScopes, 1)
	require.Equal(t, "secrets", blockScopes[0].Category)
	require.Equal(t, `kind in ["tool_request","tool_response"]`, blockScopes[0].ScopeInclude)
	require.Equal(t, `kind == "assistant_message"`, blockScopes[0].ScopeExempt)

	// Flagging policy is widened to whatever the registry recommends.
	flagRow, flagScopes := readPolicy(t, ctx, pool, ids[1])
	require.Empty(t, flagRow.MessageTypes)
	require.False(t, flagRow.ScopeInclude.Valid)
	require.False(t, flagRow.ScopeExempt.Valid)
	require.Empty(t, flagScopes)

	// Findings carry the policy version they were produced under. A preserved
	// fold scans identically, so its findings stay addressable; a cleared fold
	// changes what is scanned and has to bump.
	require.Equal(t, int64(1), blockRow.Version)
	require.Equal(t, int64(2), flagRow.Version)
}

func TestRunnerApplyIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	pool, err := testInfra.CloneTestDatabase(t, "legacyscope_idempotent")
	require.NoError(t, err)

	ids := seed(t, ctx, pool, seedPolicy{
		action: "block", sources: []string{"gitleaks"},
		messageTypes: []string{"tool_request"}, scopeInclude: "", scopeExempt: "",
	})

	runner := newRunner(t, pool)
	_, err = runner.Run(ctx, ModeApply)
	require.NoError(t, err)
	firstRow, firstScopes := readPolicy(t, ctx, pool, ids[0])

	second, err := runner.Run(ctx, ModeApply)
	require.NoError(t, err)
	require.Equal(t, int64(0), second.Scanned, "folded rows drop out of the candidate set")

	afterRow, afterScopes := readPolicy(t, ctx, pool, ids[0])
	require.Equal(t, firstScopes, afterScopes)
	require.Equal(t, firstRow.Version, afterRow.Version)

	_, err = runner.Run(ctx, ModeValidate)
	require.NoError(t, err)
}

func TestRunnerValidateFailsWhileLegacyScopesRemain(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	pool, err := testInfra.CloneTestDatabase(t, "legacyscope_validate")
	require.NoError(t, err)

	seed(t, ctx, pool, seedPolicy{
		action: "flag", sources: []string{"gitleaks"},
		messageTypes: nil, scopeInclude: `kind == "user_message"`, scopeExempt: "",
	})

	runner := newRunner(t, pool)
	_, err = runner.Run(ctx, ModeValidate)
	require.ErrorIs(t, err, ErrValidationFailed)

	_, err = runner.Run(ctx, ModeApply)
	require.NoError(t, err)

	_, err = runner.Run(ctx, ModeValidate)
	require.NoError(t, err)
}
