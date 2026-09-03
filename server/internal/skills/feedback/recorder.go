package feedback

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	domainskills "github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/suggest"
)

const signalTimeout = time.Second

type RecordInput struct {
	ProjectID      uuid.UUID
	SkillID        uuid.NullUUID
	SkillVersionID uuid.NullUUID
	SkillName      string
	Source         domainskills.FeedbackSource
	Outcome        domainskills.FeedbackOutcome
	Note           string
	SessionID      string
	UserID         string
	UserEmail      string
}

type Recorder struct {
	db       *pgxpool.Pool
	logger   *slog.Logger
	signaler suggest.Signaler
}

func NewRecorder(db *pgxpool.Pool, logger *slog.Logger, signaler suggest.Signaler) *Recorder {
	return &Recorder{db: db, logger: logger, signaler: signaler}
}

func (r *Recorder) Record(ctx context.Context, input RecordInput) (repo.SkillFeedback, error) {
	input.SkillName = strings.TrimSpace(input.SkillName)
	switch {
	case !domainskills.ValidSpecName(input.SkillName):
		return repo.SkillFeedback{}, errors.New("skill must be a canonical 1-64 character skill name")
	case !input.Source.Valid():
		return repo.SkillFeedback{}, errors.New("invalid feedback source")
	case !input.Outcome.Valid():
		return repo.SkillFeedback{}, errors.New("invalid feedback outcome")
	case utf8.RuneCountInString(input.Note) > domainskills.MaxFeedbackNoteRunes:
		return repo.SkillFeedback{}, fmt.Errorf("feedback note must be at most %d Unicode characters", domainskills.MaxFeedbackNoteRunes)
	case input.SkillVersionID.Valid && !input.SkillID.Valid:
		return repo.SkillFeedback{}, errors.New("skill version requires an exact skill")
	}

	queries := repo.New(r.db)
	if !input.SkillID.Valid && !input.SkillVersionID.Valid {
		skill, err := queries.GetActiveSkillByName(ctx, repo.GetActiveSkillByNameParams{
			ProjectID: input.ProjectID,
			Name:      input.SkillName,
		})
		switch {
		case err == nil:
			input.SkillID = uuid.NullUUID{UUID: skill.ID, Valid: true}
			input.SkillName = skill.Name
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return repo.SkillFeedback{}, fmt.Errorf("resolve active skill for feedback: %w", err)
		}
	}

	stored, err := queries.CreateSkillFeedback(ctx, repo.CreateSkillFeedbackParams{
		ID:             uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ProjectID:      input.ProjectID,
		SkillID:        input.SkillID,
		SkillVersionID: input.SkillVersionID,
		SkillName:      input.SkillName,
		Source:         string(input.Source),
		Outcome:        string(input.Outcome),
		Note:           conv.ToPGTextEmpty(input.Note),
		SessionID:      conv.ToPGTextEmpty(input.SessionID),
		UserID:         conv.ToPGTextEmpty(input.UserID),
		UserEmail:      conv.ToPGTextEmpty(input.UserEmail),
	})
	if err != nil {
		return repo.SkillFeedback{}, fmt.Errorf("create skill feedback: %w", err)
	}

	if r.signaler != nil && stored.SkillID.Valid {
		signalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), signalTimeout)
		defer cancel()
		if err := r.signaler.Signal(signalCtx, input.ProjectID, stored.SkillID.UUID); err != nil {
			r.logger.ErrorContext(signalCtx, "signal skill feedback analysis",
				attr.SlogError(err),
				attr.SlogProjectID(input.ProjectID.String()),
				attr.SlogResourceID(stored.SkillID.UUID.String()),
			)
		}
	}

	return stored, nil
}
