package legacypolicyscope

import (
	"context"
	"encoding/json"
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

// TestRunnerApplyFailsWhenRowsStayLocked pins the SKIP LOCKED hazard: a row
// another session holds is passed over silently, so an apply that reported
// success would leave the table half folded for the follow-up column drop.
func TestRunnerApplyFailsWhenRowsStayLocked(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	pool, err := testInfra.CloneTestDatabase(t, "legacyscope_locked")
	require.NoError(t, err)

	ids := seed(t, ctx, pool,
		seedPolicy{action: "block", sources: []string{"gitleaks"},
			messageTypes: []string{"tool_request"}, scopeInclude: "", scopeExempt: ""},
		seedPolicy{action: "flag", sources: []string{"gitleaks"},
			messageTypes: []string{"tool_request"}, scopeInclude: "", scopeExempt: ""},
	)

	//nolint:glint // notestingrawsql: the test must hold the row's transaction open while the runner scans
	holder, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = holder.Rollback(ctx) }()
	_, err = testrepo.New(holder).LockRiskPolicyFixture(ctx, ids[0])
	require.NoError(t, err)

	runner, err := NewRunner(pool, slog.New(slog.DiscardHandler), Options{
		BatchSize: 10, LockTimeout: 0, StatementTimeout: 0,
		ApplyAttempts: 2, RetryDelay: -1,
	})
	require.NoError(t, err)

	summary, err := runner.Run(ctx, ModeApply)
	require.ErrorIs(t, err, ErrIncompleteApply)
	require.Equal(t, int64(1), summary.Remaining)
	require.Equal(t, int64(2), summary.Attempts)

	// The unlocked row is still folded; only the held one is left behind.
	lockedRow, _ := readPolicy(t, ctx, pool, ids[0])
	require.Equal(t, []string{"tool_request"}, lockedRow.MessageTypes)
	freeRow, _ := readPolicy(t, ctx, pool, ids[1])
	require.Empty(t, freeRow.MessageTypes)

	// Once the holder releases, a rerun completes and validates.
	require.NoError(t, holder.Rollback(ctx))
	_, err = runner.Run(ctx, ModeApply)
	require.NoError(t, err)
	_, err = runner.Run(ctx, ModeValidate)
	require.NoError(t, err)
}

func TestRunnerApplyPreservesUnknownAnalyzerConfigKeys(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	pool, err := testInfra.CloneTestDatabase(t, "legacyscope_unknown_keys")
	require.NoError(t, err)

	ids := seed(t, ctx, pool, seedPolicy{
		action: "block", sources: []string{"gitleaks"},
		messageTypes: []string{"tool_request"}, scopeInclude: "", scopeExempt: "",
	})
	require.NoError(t, testrepo.New(pool).SetRiskPolicyAnalyzerConfigFixture(ctx, testrepo.SetRiskPolicyAnalyzerConfigFixtureParams{
		ID:             ids[0],
		AnalyzerConfig: []byte(`{"presidio":{"score_threshold":0.4},"future_scanner":{"mode":"strict"}}`),
	}))

	_, err = newRunner(t, pool).Run(ctx, ModeApply)
	require.NoError(t, err)

	row, scopes := readPolicy(t, ctx, pool, ids[0])
	require.Len(t, scopes, 1)
	var config map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(row.AnalyzerConfig, &config))
	require.JSONEq(t, `{"mode":"strict"}`, string(config["future_scanner"]),
		"the fold must not drop analyzer_config members it does not model")
	require.JSONEq(t, `{"score_threshold":0.4}`, string(config["presidio"]))
}

// A whitespace-only legacy column narrows nothing but still matches the
// candidate predicate, so the fold has to clear it rather than compose it into
// CEL the engine would reject.
func TestRunnerApplyClearsWhitespaceOnlyLegacyScope(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	pool, err := testInfra.CloneTestDatabase(t, "legacyscope_whitespace")
	require.NoError(t, err)

	ids := seed(t, ctx, pool, seedPolicy{
		action: "block", sources: []string{"gitleaks"},
		messageTypes: nil, scopeInclude: "   ", scopeExempt: "",
	})

	summary, err := newRunner(t, pool).Run(ctx, ModeApply)
	require.NoError(t, err)
	require.Equal(t, int64(1), summary.Noop)
	require.Equal(t, int64(0), summary.Remaining)

	row, scopes := readPolicy(t, ctx, pool, ids[0])
	require.False(t, row.ScopeInclude.Valid)
	require.Empty(t, scopes, "nothing to compose, so no category scope is invented")
	require.Equal(t, int64(1), row.Version, "a behaviour-identical fold keeps findings addressable")
}
