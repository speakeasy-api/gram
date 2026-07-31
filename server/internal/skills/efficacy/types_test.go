package efficacy

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func TestNewEvaluationProjectsStoredRow(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 7, 20, 11, 30, 0, 0, time.UTC)
	reservedOn := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	row := repo.SkillEfficacyEvaluation{
		ID:              uuid.New(),
		OrganizationID:  "org",
		ProjectID:       uuid.New(),
		Surface:         SurfaceAssistant,
		SessionID:       "session",
		ChatID:          uuid.New(),
		SkillID:         uuid.New(),
		SkillVersionID:  uuid.New(),
		CanonicalSha256: "sha",
		ObservedAt:      conv.ToPGTimestamptz(observedAt),
		State:           StateReserved,
		ReservedOn:      pgtype.Date{Time: reservedOn, InfinityModifier: pgtype.Finite, Valid: true},
		Attempts:        2,
		LastError:       conv.ToPGText("boom"),
		ScoredAt:        pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		CreatedAt:       conv.ToPGTimestamptz(observedAt),
		UpdatedAt:       conv.ToPGTimestamptz(observedAt),
	}

	evaluation := NewEvaluation(row)

	require.Equal(t, row.ID, evaluation.ID)
	require.Equal(t, StateReserved, evaluation.State)
	require.Equal(t, int32(2), evaluation.Attempts)
	require.Equal(t, observedAt, evaluation.ObservedAt.UTC())
	require.Equal(t, reservedOn, evaluation.ReservedOn.UTC())
}

func TestNewEvaluationLeavesUnreservedDayZero(t *testing.T) {
	t.Parallel()

	row := repo.SkillEfficacyEvaluation{
		ID:              uuid.New(),
		OrganizationID:  "org",
		ProjectID:       uuid.New(),
		Surface:         SurfaceDev,
		SessionID:       "session",
		ChatID:          uuid.New(),
		SkillID:         uuid.New(),
		SkillVersionID:  uuid.New(),
		CanonicalSha256: "sha",
		ObservedAt:      conv.ToPGTimestamptz(time.Date(2026, 7, 20, 11, 30, 0, 0, time.UTC)),
		State:           StatePending,
		ReservedOn:      pgtype.Date{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		Attempts:        0,
		LastError:       pgtype.Text{String: "", Valid: false},
		ScoredAt:        pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		CreatedAt:       pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		UpdatedAt:       pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
	}

	require.True(t, NewEvaluation(row).ReservedOn.IsZero())
}
