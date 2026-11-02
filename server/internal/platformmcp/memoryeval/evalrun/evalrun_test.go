package evalrun_test

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/bench"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/evalrun"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/evalrun/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

func TestDefaultConfigIsValid(t *testing.T) {
	t.Parallel()
	require.NoError(t, evalrun.DefaultConfig().Validate())
}

func TestConfigValidateEnforcesSpendCaps(t *testing.T) {
	t.Parallel()

	tooManyRuns := evalrun.DefaultConfig()
	tooManyRuns.Runs = evalrun.MaxRuns + 1
	require.Error(t, tooManyRuns.Validate())

	tooManyModels := evalrun.DefaultConfig()
	for len(tooManyModels.AnswerModels) <= evalrun.MaxAnswerModels {
		tooManyModels.AnswerModels = append(tooManyModels.AnswerModels, tooManyModels.AnswerModels[0])
	}
	require.Error(t, tooManyModels.Validate())

	badDigest := evalrun.DefaultConfig()
	badDigest.DigestModel = "not-a-provider/not-a-model"
	require.Error(t, badDigest.Validate())
}

func TestTotalCostAddsEveryStage(t *testing.T) {
	t.Parallel()

	report := &bench.Report{
		JudgeCostUSD: 1,
		Scores:       []bench.ArmScore{{CostUSD: 2}, {CostUSD: 3}},
		Preparation: &bench.Preparation{
			SummarySpend: bench.ModelSpend{CostUSD: 4},
			DigestSpend:  bench.ModelSpend{CostUSD: 5},
		},
	}
	require.InDelta(t, 15.0, evalrun.TotalCost(report), 1e-9)
}

func newRun(t *testing.T, q *repo.Queries, orgID string) repo.MemoryEvalRun {
	t.Helper()
	cfg, err := json.Marshal(evalrun.DefaultConfig())
	require.NoError(t, err)
	run, err := q.CreateRun(t.Context(), repo.CreateRunParams{OrganizationID: orgID, Config: cfg, RequestedBy: "staff-user"})
	require.NoError(t, err)
	require.Equal(t, "queued", run.Status)
	return run
}

// noCalls panics, failing the test, if the runner spends anything.
type noCalls struct {
	openrouter.CompletionClient
}

// A run against an organization without a seeded corpus fails on its row,
// without spending, and a second attempt leaves the resolved row alone.
func TestRunnerFailsUnseededOrganizationOnTheRow(t *testing.T) {
	t.Parallel()

	db, err := testInfra(t).CloneTestDatabase(t, "evalrun")
	require.NoError(t, err)
	q := repo.New(db)
	runner := evalrun.NewRunner(testenv.NewLogger(t), db, noCalls{})

	run := newRun(t, q, "org-without-corpus")
	require.NoError(t, runner.Run(t.Context(), run.ID))

	got, err := q.GetRun(t.Context(), run.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)
	require.Contains(t, got.Error.String, "no seeded memory eval corpus")
	require.True(t, got.StartedAt.Valid)
	require.True(t, got.FinishedAt.Valid)

	// A duplicate attempt is a no-op: the run is no longer queued.
	require.NoError(t, runner.Run(t.Context(), run.ID))
	again, err := q.GetRun(t.Context(), run.ID)
	require.NoError(t, err)
	require.Equal(t, got.FinishedAt, again.FinishedAt)
}

// MarkInterrupted resolves a stuck run but never overwrites a finished one.
func TestRunnerMarkInterrupted(t *testing.T) {
	t.Parallel()

	db, err := testInfra(t).CloneTestDatabase(t, "evalrun")
	require.NoError(t, err)
	q := repo.New(db)
	runner := evalrun.NewRunner(testenv.NewLogger(t), db, noCalls{})

	stuck := newRun(t, q, "org-a")
	_, err = q.MarkRunRunning(t.Context(), stuck.ID)
	require.NoError(t, err)
	require.NoError(t, runner.MarkInterrupted(t.Context(), stuck.ID))
	got, err := q.GetRun(t.Context(), stuck.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", got.Status)

	done := newRun(t, q, "org-a")
	_, err = q.MarkRunRunning(t.Context(), done.ID)
	require.NoError(t, err)
	n, err := q.CompleteRun(t.Context(), repo.CompleteRunParams{Report: []byte(`{}`), CostUsd: got.CostUsd, ID: done.ID})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	require.NoError(t, runner.MarkInterrupted(t.Context(), done.ID))
	finished, err := q.GetRun(t.Context(), done.ID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", finished.Status)

	// Unknown runs are a no-op too.
	require.NoError(t, runner.MarkInterrupted(t.Context(), uuid.New()))
}
