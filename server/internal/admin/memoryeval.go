package admin

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gen "github.com/speakeasy-api/gram/server/gen/admin"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/evalrun"
	"github.com/speakeasy-api/gram/server/internal/platformmcp/memoryeval/evalrun/repo"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
)

// MemoryEvalRunStarter starts the workflow that executes a queued team
// memory eval run. Implemented by background.TemporalMemoryEvalRunStarter.
type MemoryEvalRunStarter interface {
	StartMemoryEvalRun(ctx context.Context, runID uuid.UUID) error
}

// SetMemoryEvalRunStarter enables startMemoryEvalRun. Nil leaves it
// unavailable; listing and reading runs work either way.
func (s *Service) SetMemoryEvalRunStarter(starter MemoryEvalRunStarter) {
	s.memoryEvalStarter = starter
}

func (s *Service) GetMemoryEvalOptions(ctx context.Context, _ *gen.GetMemoryEvalOptionsPayload) (*gen.AdminMemoryEvalOptions, error) {
	recent, err := evalrun.RunsInWindow(ctx, s.db)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "count memory eval runs").LogError(ctx, s.logger)
	}
	return &gen.AdminMemoryEvalOptions{
		Defaults:        memoryEvalConfigView(evalrun.DefaultConfig()),
		AllowedModels:   openrouter.AllowedModels(),
		DailyRunLimit:   evalrun.DailyRunLimit,
		RunsLast24h:     int(recent),
		MaxAnswerModels: evalrun.MaxAnswerModels,
		MaxRuns:         evalrun.MaxRuns,
	}, nil
}

func (s *Service) ListMemoryEvalRuns(ctx context.Context, payload *gen.ListMemoryEvalRunsPayload) (*gen.ListMemoryEvalRunsResult, error) {
	q := repo.New(s.db)
	params := repo.ListRunsParams{
		BeforeCreatedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		BeforeID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		RowLimit:        int32(payload.Limit + 1), //nolint:gosec // the design bounds limit to 100
	}
	if payload.Cursor != nil {
		cursorID, err := uuid.Parse(*payload.Cursor)
		if err != nil {
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor")
		}
		cursor, err := q.GetRun(ctx, cursorID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, oops.E(oops.CodeBadRequest, err, "invalid cursor")
		case err != nil:
			return nil, oops.E(oops.CodeUnexpected, err, "load cursor run").LogError(ctx, s.logger)
		}
		params.BeforeCreatedAt = cursor.CreatedAt
		params.BeforeID = uuid.NullUUID{UUID: cursor.ID, Valid: true}
	}
	rows, err := q.ListRuns(ctx, params)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "list memory eval runs").LogError(ctx, s.logger)
	}
	var next *string
	if len(rows) > payload.Limit {
		rows = rows[:payload.Limit]
		next = conv.PtrEmpty(rows[len(rows)-1].ID.String())
	}
	runs := make([]*gen.AdminMemoryEvalRun, 0, len(rows))
	for _, row := range rows {
		runs = append(runs, memoryEvalRunView(listedRun(row)))
	}
	return &gen.ListMemoryEvalRunsResult{Runs: runs, NextCursor: next}, nil
}

func (s *Service) GetMemoryEvalRun(ctx context.Context, payload *gen.GetMemoryEvalRunPayload) (*gen.AdminMemoryEvalRun, error) {
	id, err := uuid.Parse(payload.ID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, err, "invalid run id")
	}
	row, err := repo.New(s.db).GetRun(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, err, "memory eval run not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get memory eval run").LogError(ctx, s.logger)
	}
	return memoryEvalRunView(row), nil
}

func (s *Service) StartMemoryEvalRun(ctx context.Context, payload *gen.StartMemoryEvalRunPayload) (*gen.AdminMemoryEvalRun, error) {
	if s.memoryEvalStarter == nil {
		return nil, oops.E(oops.CodeUnavailable, nil, "memory eval runs are not available on this server").LogWarn(ctx, s.logger)
	}
	cfg := memoryEvalConfigFromPayload(payload.Config)
	if err := cfg.Validate(); err != nil {
		return nil, oops.E(oops.CodeInvalid, err, "%s", err.Error())
	}
	// requested_by is the only record of who started a run that spends
	// model credit, so a start without a staff email is refused.
	_, _, operatorEmail := adminActor(ctx)
	if operatorEmail == nil || *operatorEmail == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	requestedBy := *operatorEmail

	run, err := evalrun.Queue(ctx, s.db, cfg, requestedBy)
	switch {
	case errors.Is(err, evalrun.ErrNoCorpus):
		return nil, oops.E(oops.CodeNotFound, err, "the memory eval corpus is not seeded; reseed the demo organization")
	case errors.Is(err, evalrun.ErrRunActive):
		return nil, oops.E(oops.CodeConflict, err, "another memory eval run is queued or running")
	case errors.Is(err, evalrun.ErrDailyLimit):
		return nil, oops.E(oops.CodeRateLimitExceeded, err, "%d memory eval runs already started in the last %s", evalrun.DailyRunLimit, evalrun.RunWindow)
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "queue memory eval run").LogError(ctx, s.logger)
	}

	logger := s.logger.With(attr.SlogMemoryEvalRunID(run.ID.String()), attr.SlogAuthUserEmail(requestedBy))
	if err := s.memoryEvalStarter.StartMemoryEvalRun(ctx, run.ID); err != nil {
		if failErr := evalrun.FailToStart(context.WithoutCancel(ctx), s.db, run, "the run could not be started; try again"); failErr != nil {
			logger.ErrorContext(ctx, "fail unstarted memory eval run", attr.SlogError(failErr))
		}
		return nil, oops.E(oops.CodeUnexpected, err, "start memory eval run").LogError(ctx, logger)
	}
	logger.InfoContext(ctx, "started memory eval run")
	return memoryEvalRunView(run), nil
}

func memoryEvalConfigView(cfg evalrun.Config) *gen.AdminMemoryEvalRunConfig {
	return &gen.AdminMemoryEvalRunConfig{
		AnswerModels: cfg.AnswerModels,
		JudgeModel:   cfg.JudgeModel,
		SummaryModel: cfg.SummaryModel,
		DigestModel:  cfg.DigestModel,
		Runs:         cfg.Runs,
	}
}

func memoryEvalConfigFromPayload(cfg *gen.AdminMemoryEvalRunConfig) evalrun.Config {
	if cfg == nil {
		return evalrun.Config{AnswerModels: nil, JudgeModel: "", SummaryModel: "", DigestModel: "", Runs: 0}
	}
	return evalrun.Config{
		AnswerModels: cfg.AnswerModels,
		JudgeModel:   cfg.JudgeModel,
		SummaryModel: cfg.SummaryModel,
		DigestModel:  cfg.DigestModel,
		Runs:         cfg.Runs,
	}
}

// listedRun is a listed run as a run row. The list query leaves out the
// report to keep pages small, so the view shows none.
func listedRun(row repo.ListRunsRow) repo.MemoryEvalRun {
	var run repo.MemoryEvalRun
	run.ID = row.ID
	run.Status = row.Status
	run.Config = row.Config
	run.Error = row.Error
	run.CostUsd = row.CostUsd
	run.RequestedBy = row.RequestedBy
	run.StartedAt = row.StartedAt
	run.FinishedAt = row.FinishedAt
	run.CreatedAt = row.CreatedAt
	return run
}

// memoryEvalRunView maps a row, with its report when the row carries one.
func memoryEvalRunView(row repo.MemoryEvalRun) *gen.AdminMemoryEvalRun {
	var cfg evalrun.Config
	// Queue validated and encoded the config; a decode failure shows an empty
	// config rather than hiding the run.
	_ = json.Unmarshal(row.Config, &cfg)

	view := &gen.AdminMemoryEvalRun{
		ID:          row.ID.String(),
		Status:      row.Status,
		Config:      memoryEvalConfigView(cfg),
		CostUsd:     nil,
		Error:       nil,
		RequestedBy: row.RequestedBy,
		ReportJSON:  nil,
		StartedAt:   nil,
		FinishedAt:  nil,
		CreatedAt:   row.CreatedAt.Time.Format(time.RFC3339),
	}
	if row.CostUsd.Valid {
		if f, err := row.CostUsd.Float64Value(); err == nil && f.Valid {
			view.CostUsd = &f.Float64
		}
	}
	if row.Error.Valid {
		view.Error = &row.Error.String
	}
	if len(row.Report) > 0 {
		view.ReportJSON = conv.PtrEmpty(string(row.Report))
	}
	if row.StartedAt.Valid {
		view.StartedAt = conv.PtrEmpty(row.StartedAt.Time.Format(time.RFC3339))
	}
	if row.FinishedAt.Valid {
		view.FinishedAt = conv.PtrEmpty(row.FinishedAt.Time.Format(time.RFC3339))
	}
	return view
}
