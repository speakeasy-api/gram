package activities

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/temporal"

	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/suggest"
)

type SkillSuggestionIdentity struct {
	ProjectID uuid.UUID `json:"project_id"`
	SkillID   uuid.UUID `json:"skill_id"`
	Force     bool      `json:"force"`
}

type AnalyzeSkillSuggestionParams struct {
	SkillSuggestionIdentity
	Now time.Time `json:"now"`
}

type SkillSuggestionProject struct {
	ProjectID uuid.UUID `json:"project_id"`
}

type ListSkillSuggestionProjectsParams struct {
	AfterProjectID uuid.UUID `json:"after_project_id"`
	ActiveSince    time.Time `json:"active_since"`
	PageLimit      int32     `json:"page_limit"`
}

type ListRecentlyActiveSuggestionSkillsParams struct {
	ProjectID        uuid.UUID `json:"project_id"`
	ActiveSince      time.Time `json:"active_since"`
	CursorLastSeenAt time.Time `json:"cursor_last_seen_at"`
	CursorID         uuid.UUID `json:"cursor_id"`
	PageLimit        int32     `json:"page_limit"`
}

type ActiveSuggestionSkill struct {
	ID         uuid.UUID `json:"id"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

type SkillSuggestionAnalyzer struct {
	db       *pgxpool.Pool
	engine   *suggest.Engine
	signaler suggest.Signaler
}

func NewSkillSuggestionAnalyzer(db *pgxpool.Pool, engine *suggest.Engine, signaler suggest.Signaler) *SkillSuggestionAnalyzer {
	return &SkillSuggestionAnalyzer{db: db, engine: engine, signaler: signaler}
}

func (a *SkillSuggestionAnalyzer) AnalyzeSkillSuggestion(ctx context.Context, params AnalyzeSkillSuggestionParams) (*suggest.Result, error) {
	input := suggest.RunInput{
		ProjectID: params.ProjectID,
		SkillID:   params.SkillID,
		Now:       params.Now,
	}
	run := a.engine.Run
	if params.Force {
		run = a.engine.RunForced
	}
	result, err := run(ctx, input)
	if errors.Is(err, suggest.ErrModelFailure) {
		return nil, temporal.NewNonRetryableApplicationError("skill suggestion model failure", "skill_suggestion_model_failure", err)
	}
	if err != nil {
		return nil, fmt.Errorf("analyze skill suggestion: %w", err)
	}
	return &result, nil
}

func (a *SkillSuggestionAnalyzer) ListSkillSuggestionProjects(ctx context.Context, params ListSkillSuggestionProjectsParams) ([]SkillSuggestionProject, error) {
	rows, err := repo.New(a.db).ListSkillSuggestionProjects(ctx, repo.ListSkillSuggestionProjectsParams{
		AfterProjectID: params.AfterProjectID,
		ActiveSince:    pgtype.Timestamptz{Time: params.ActiveSince, InfinityModifier: pgtype.Finite, Valid: true},
		PageLimit:      params.PageLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list skill suggestion projects: %w", err)
	}
	projects := make([]SkillSuggestionProject, len(rows))
	for i, projectID := range rows {
		projects[i] = SkillSuggestionProject{ProjectID: projectID}
	}
	return projects, nil
}

func (a *SkillSuggestionAnalyzer) ListRecentlyActiveSuggestionSkills(ctx context.Context, params ListRecentlyActiveSuggestionSkillsParams) ([]ActiveSuggestionSkill, error) {
	rows, err := repo.New(a.db).ListRecentlyActiveSkills(ctx, repo.ListRecentlyActiveSkillsParams{
		ProjectID:        params.ProjectID,
		ActiveSince:      pgtype.Timestamptz{Time: params.ActiveSince, InfinityModifier: pgtype.Finite, Valid: true},
		CursorLastSeenAt: pgtype.Timestamptz{Time: params.CursorLastSeenAt, InfinityModifier: pgtype.Finite, Valid: !params.CursorLastSeenAt.IsZero()},
		CursorID:         uuid.NullUUID{UUID: params.CursorID, Valid: params.CursorID != uuid.Nil},
		PageLimit:        params.PageLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list recently active suggestion skills: %w", err)
	}
	skills := make([]ActiveSuggestionSkill, len(rows))
	for i, row := range rows {
		skills[i] = ActiveSuggestionSkill{ID: row.ID, LastSeenAt: row.LastSeenAt.Time}
	}
	return skills, nil
}

func (a *SkillSuggestionAnalyzer) SignalSkillSuggestions(ctx context.Context, identities []SkillSuggestionIdentity) error {
	var result error
	for _, identity := range identities {
		if err := a.signaler.Signal(ctx, identity.ProjectID, identity.SkillID); err != nil {
			result = errors.Join(result, fmt.Errorf("signal skill suggestion %s: %w", identity.SkillID, err))
		}
	}
	return result
}
