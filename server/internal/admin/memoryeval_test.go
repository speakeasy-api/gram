package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/demoseed"
	demoseedrepo "github.com/speakeasy-api/gram/server/internal/demoseed/repo"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/evalrun"
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

func seedMemoryEvalCorpusProject(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	spec := demoseed.LocalSpec()
	_, err := orgrepo.New(db).UpsertOrganizationMetadata(t.Context(), orgrepo.UpsertOrganizationMetadataParams{ID: spec.OrgID, Name: spec.OrgName, Slug: spec.OrgSlug})
	require.NoError(t, err)
	id, ok := demoseed.MemoryEvalProjectID(spec, memoryeval.ProjectMain)
	require.True(t, ok)
	require.NoError(t, demoseedrepo.New(db).MemoryEvalInsertProject(t.Context(), demoseedrepo.MemoryEvalInsertProjectParams{ProjectID: id, Name: "Team memory eval", Slug: "team-memory-eval", OrgID: spec.OrgID, CreatedAt: pgtype.Timestamptz{Time: time.Now(), InfinityModifier: pgtype.Finite, Valid: true}}))
}

func memoryEvalPayloadConfig() *gen.AdminMemoryEvalRunConfig {
	return memoryEvalConfigView(evalrun.DefaultConfig())
}

func TestStartMemoryEvalRun_QueuesAndStartsTheWorkflow(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedMemoryEvalCorpusProject(t, db)
	starter := &recordingMemoryEvalStarter{}
	svc.SetMemoryEvalRunStarter(starter)

	run, err := svc.StartMemoryEvalRun(ctx, &gen.StartMemoryEvalRunPayload{Config: memoryEvalPayloadConfig()})
	require.NoError(t, err)
	require.Equal(t, "queued", run.Status)
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
	seedMemoryEvalCorpusProject(t, db)
	starter := &recordingMemoryEvalStarter{err: errors.New("temporal unavailable")}
	svc.SetMemoryEvalRunStarter(starter)

	_, err := svc.StartMemoryEvalRun(ctx, &gen.StartMemoryEvalRunPayload{Config: memoryEvalPayloadConfig()})
	require.Error(t, err)
	require.Len(t, starter.started, 1)

	row, err := evalrunrepo.New(db).GetRun(ctx, starter.started[0])
	require.NoError(t, err)
	require.Equal(t, "failed", row.Status, "an unstarted run must not block the next one")
}

func TestStartMemoryEvalRun_Unconfigured(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newTestAdminService(t)

	_, err := svc.StartMemoryEvalRun(ctx, &gen.StartMemoryEvalRunPayload{Config: memoryEvalPayloadConfig()})
	require.Error(t, err)
}

func TestListAndGetMemoryEvalRuns(t *testing.T) {
	t.Parallel()
	ctx, svc, db := newTestAdminService(t)
	seedMemoryEvalCorpusProject(t, db)
	svc.SetMemoryEvalRunStarter(&recordingMemoryEvalStarter{})
	q := evalrunrepo.New(db)
	ctx = contextvalues.SetAdminAuthContext(ctx, &contextvalues.AdminAuthContext{
		SessionID: "session-eval", Email: "operator@example.com", OIDCSubject: "oidc-subject-eval", Name: "Test Operator", HD: "example.com",
	})

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

	_, err = svc.GetMemoryEvalRun(ctx, &gen.GetMemoryEvalRunPayload{ID: uuid.NewString()})
	var oopsErr *oops.ShareableError
	require.ErrorAs(t, err, &oopsErr)
	require.Equal(t, oops.CodeNotFound, oopsErr.Code)
}
