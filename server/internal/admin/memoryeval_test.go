package admin

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/evalrun"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/evalrun/evalruntest"
	evalrunrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/evalrun/repo"
)

type recordingMemoryEvalStarter struct {
	started []uuid.UUID
	err     error
}

func (s *recordingMemoryEvalStarter) StartMemoryEvalRun(_ context.Context, runID uuid.UUID) error {
	s.started = append(s.started, runID)
	return s.err
}

// withMemoryEvalOperator signs ctx in as a staff member, whose email a start
// records as requested_by.
func withMemoryEvalOperator(ctx context.Context) context.Context {
	return contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID: "session-eval", Email: "operator@example.com", OIDCSubject: "oidc-subject-eval", Name: "Test Operator", HD: "example.com",
	})
}

func memoryEvalPayloadConfig() *gen.AdminMemoryEvalRunConfig {
	return memoryEvalConfigView(evalrun.DefaultConfig())
}

func TestStartMemoryEvalRun_QueuesAndStartsTheWorkflow(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = withMemoryEvalOperator(ctx)
	evalruntest.SeedCorpus(t, db)
	starter := &recordingMemoryEvalStarter{}
	svc.SetMemoryEvalRunStarter(starter)

	run, err := svc.StartMemoryEvalRun(ctx, &gen.StartMemoryEvalRunPayload{Config: memoryEvalPayloadConfig()})
	require.NoError(t, err)
	require.Equal(t, "queued", run.Status)
	row, err := evalrunrepo.New(db).GetRun(ctx, uuid.MustParse(run.ID))
	require.NoError(t, err)
	require.Equal(t, "operator@example.com", row.RequestedBy)
	require.Equal(t, []uuid.UUID{uuid.MustParse(run.ID)}, starter.started)
	require.Equal(t, evalrun.DefaultConfig().AnswerModels, run.Config.AnswerModels)

	// A second start while the first is active conflicts.
	_, err = svc.StartMemoryEvalRun(ctx, &gen.StartMemoryEvalRunPayload{Config: memoryEvalPayloadConfig()})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeConflict, oopsErr.Code)

	opts, err := svc.GetMemoryEvalOptions(ctx, &gen.GetMemoryEvalOptionsPayload{})
	require.NoError(t, err)
	require.Equal(t, 1, opts.RunsToday)
	require.Equal(t, evalrun.DailyRunLimit, opts.DailyRunLimit)
	require.NotEmpty(t, opts.AllowedModels)
}

func TestStartMemoryEvalRun_FailsTheRowWhenTheWorkflowCannotStart(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	ctx = withMemoryEvalOperator(ctx)
	evalruntest.SeedCorpus(t, db)
	starter := &recordingMemoryEvalStarter{err: errors.New("temporal unavailable")}
	svc.SetMemoryEvalRunStarter(starter)

	_, err := svc.StartMemoryEvalRun(ctx, &gen.StartMemoryEvalRunPayload{Config: memoryEvalPayloadConfig()})
	require.Error(t, err)
	require.Len(t, starter.started, 1)

	row, err := evalrunrepo.New(db).GetRun(ctx, starter.started[0])
	require.NoError(t, err)
	require.Equal(t, "failed", row.Status, "an unstarted run must not block the next one")

	opts, err := svc.GetMemoryEvalOptions(ctx, &gen.GetMemoryEvalOptionsPayload{})
	require.NoError(t, err)
	require.Equal(t, 0, opts.RunsToday, "a run no workflow claimed spent nothing, so it must not use up the daily limit")
}

// Each Queue outcome and precondition maps to its own error code.
func TestStartMemoryEvalRun_ErrorCodes(t *testing.T) {
	t.Parallel()

	tooManyRuns := evalrun.DefaultConfig()
	tooManyRuns.Runs = evalrun.MaxRuns + 1

	cases := []struct {
		name string
		// unseeded leaves the eval corpus out.
		unseeded bool
		// noStarter runs the server without Temporal.
		noStarter bool
		// anonymous leaves out the staff email a start records.
		anonymous bool
		config    evalrun.Config
		// before runs on the seeded database ahead of the start.
		before func(t *testing.T, ctx context.Context, db *pgxpool.Pool)
		want   oops.Code
	}{
		{name: "server without Temporal", noStarter: true, want: oops.CodeUnavailable},
		{name: "invalid config", config: tooManyRuns, want: oops.CodeInvalid},
		{name: "no staff email", anonymous: true, want: oops.CodeUnauthorized},
		{name: "corpus not seeded", unseeded: true, want: oops.CodeNotFound},
		{name: "another run active", before: queueActiveRun, want: oops.CodeConflict},
		{name: "daily limit reached", before: spendDailyLimit, want: oops.CodeRateLimitExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, svc, db := newTestAdminService(t)
			if !tc.anonymous {
				ctx = withMemoryEvalOperator(ctx)
			}
			if !tc.unseeded {
				evalruntest.SeedCorpus(t, db)
			}
			starter := &recordingMemoryEvalStarter{}
			if !tc.noStarter {
				svc.SetMemoryEvalRunStarter(starter)
			}
			if tc.before != nil {
				tc.before(t, ctx, db)
			}
			cfg := evalrun.DefaultConfig()
			if tc.config.Runs != 0 {
				cfg = tc.config
			}

			_, err := svc.StartMemoryEvalRun(ctx, &gen.StartMemoryEvalRunPayload{Config: memoryEvalConfigView(cfg)})
			requireOopsCode(t, err, tc.want)
			if !tc.noStarter {
				require.Empty(t, starter.started, "a refused start launches no workflow")
			}
		})
	}
}

// queueActiveRun leaves one run queued, so the next start conflicts.
func queueActiveRun(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	_, err := evalrun.Queue(ctx, db, evalrun.DefaultConfig(), "earlier@example.com")
	require.NoError(t, err)
}

// spendDailyLimit uses up the daily limit with runs a workflow claimed and
// that then failed, which count because they spent.
func spendDailyLimit(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	q := evalrunrepo.New(db)
	for range evalrun.DailyRunLimit {
		run, err := evalrun.Queue(ctx, db, evalrun.DefaultConfig(), "earlier@example.com")
		require.NoError(t, err)
		_, err = q.MarkRunRunning(ctx, run.ID)
		require.NoError(t, err)
		_, err = q.FailRun(ctx, evalrunrepo.FailRunParams{Error: pgtype.Text{String: "test", Valid: true}, CostUsd: pgtype.Numeric{}, ID: run.ID})
		require.NoError(t, err)
	}
}

// Malformed IDs and unknown cursors are bad requests; a well-formed unknown
// run ID is not found.
func TestMemoryEvalRunReads_ErrorCodes(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)
	unknown := uuid.NewString()
	malformed := "not-a-uuid"

	_, err := svc.ListMemoryEvalRuns(ctx, &gen.ListMemoryEvalRunsPayload{Limit: 10, Cursor: &malformed})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = svc.ListMemoryEvalRuns(ctx, &gen.ListMemoryEvalRunsPayload{Limit: 10, Cursor: &unknown})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = svc.GetMemoryEvalRun(ctx, &gen.GetMemoryEvalRunPayload{ID: malformed})
	requireOopsCode(t, err, oops.CodeBadRequest)
	_, err = svc.GetMemoryEvalRun(ctx, &gen.GetMemoryEvalRunPayload{ID: unknown})
	requireOopsCode(t, err, oops.CodeNotFound)
}

func TestListAndGetMemoryEvalRuns(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	evalruntest.SeedCorpus(t, db)
	svc.SetMemoryEvalRunStarter(&recordingMemoryEvalStarter{})
	q := evalrunrepo.New(db)
	ctx = withMemoryEvalOperator(ctx)

	var ids []string
	for range 3 {
		run, err := svc.StartMemoryEvalRun(ctx, &gen.StartMemoryEvalRunPayload{Config: memoryEvalPayloadConfig()})
		require.NoError(t, err)
		id := uuid.MustParse(run.ID)
		_, err = q.MarkRunRunning(ctx, id)
		require.NoError(t, err)
		var cost pgtype.Numeric
		require.NoError(t, cost.Scan("1.2500"))
		_, err = q.CompleteRun(ctx, evalrunrepo.CompleteRunParams{Report: []byte(`{"judge_model":"j","scores":[]}`), CostUsd: cost, ID: id})
		require.NoError(t, err)
		ids = append(ids, run.ID)
	}

	page, err := svc.ListMemoryEvalRuns(ctx, &gen.ListMemoryEvalRunsPayload{Limit: 2})
	require.NoError(t, err)
	require.Len(t, page.Runs, 2)
	require.Equal(t, ids[2], page.Runs[0].ID, "newest first")
	require.Nil(t, page.Runs[0].ReportJSON, "list leaves reports out")
	require.NotNil(t, page.NextCursor)

	rest, err := svc.ListMemoryEvalRuns(ctx, &gen.ListMemoryEvalRunsPayload{Limit: 2, Cursor: page.NextCursor})
	require.NoError(t, err)
	require.Len(t, rest.Runs, 1)
	require.Equal(t, ids[0], rest.Runs[0].ID)
	require.Nil(t, rest.NextCursor)

	got, err := svc.GetMemoryEvalRun(ctx, &gen.GetMemoryEvalRunPayload{ID: ids[0]})
	require.NoError(t, err)
	require.Equal(t, "succeeded", got.Status)
	require.NotNil(t, got.ReportJSON)
	require.JSONEq(t, `{"judge_model":"j","scores":[]}`, *got.ReportJSON)
	require.NotNil(t, got.CostUsd)
	require.InDelta(t, 1.25, *got.CostUsd, 1e-9)
	require.Equal(t, "operator@example.com", got.RequestedBy)
}
