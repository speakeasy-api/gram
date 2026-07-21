package skills

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

const reconcileErrorInvalidName = "invalid_name"

type ReconcileSkillObservationsResult struct {
	Processed int
	HasMore   bool
}

type pendingSkillObservation struct {
	row         repo.SkillObservation
	name        string
	displayName string
}

// ReconcileSkillObservations folds one project-scoped batch into the skill
// registry. Observation completion and sighting updates commit atomically, so
// retries cannot count the same activation twice.
func ReconcileSkillObservations(ctx context.Context, db *pgxpool.Pool, projectID uuid.UUID, batchSize int32) (*ReconcileSkillObservationsResult, error) {
	if batchSize <= 0 {
		return nil, fmt.Errorf("reconcile skill observations: batch size must be positive")
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin skill observation reconciliation: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := repo.New(tx)

	rows, err := queries.ClaimPendingSkillObservations(ctx, repo.ClaimPendingSkillObservationsParams{
		ProjectID: projectID,
		BatchSize: batchSize,
	})
	if err != nil {
		return nil, fmt.Errorf("claim skill observations: %w", err)
	}
	pending := make([]pendingSkillObservation, 0, len(rows))
	invalidIDs := make([]uuid.UUID, 0)
	for _, row := range rows {
		displayName, name, normalizeErr := normalizeObservedSkillName(row.SkillName, row.Source.String, row.SourceLevel.String, row.Provider)
		if normalizeErr != nil {
			invalidIDs = append(invalidIDs, row.ID)
			continue
		}
		pending = append(pending, pendingSkillObservation{row: row, name: name, displayName: displayName})
	}
	processed := 0
	if len(invalidIDs) > 0 {
		count, err := queries.FailSkillObservationReconciliations(ctx, repo.FailSkillObservationReconciliationsParams{
			ProjectID: projectID, ObservationIds: invalidIDs, ErrorCode: conv.ToPGText(reconcileErrorInvalidName),
		})
		if err != nil {
			return nil, fmt.Errorf("mark invalid skill observations: %w", err)
		}
		processed += int(count)
	}
	// Advisory name locks must be acquired in a stable order when two workers
	// happen to claim the same project's rows concurrently.
	sort.SliceStable(pending, func(i, j int) bool {
		if pending[i].name != pending[j].name {
			return pending[i].name < pending[j].name
		}
		return pending[i].row.ID.String() < pending[j].row.ID.String()
	})

	for index := 0; index < len(pending); {
		observation := pending[index]
		end := index + 1
		observationIDs := []uuid.UUID{observation.row.ID}
		for end < len(pending) && pending[end].name == observation.name {
			observationIDs = append(observationIDs, pending[end].row.ID)
			end++
		}

		if err := queries.LockSkillName(ctx, repo.LockSkillNameParams{ProjectID: projectID, Name: observation.name}); err != nil {
			return nil, fmt.Errorf("lock observed skill name: %w", err)
		}
		skill, err := queries.GetSkillByNameForUpdate(ctx, repo.GetSkillByNameForUpdateParams{ProjectID: projectID, Name: observation.name})
		if errors.Is(err, pgx.ErrNoRows) {
			skill, err = queries.CreateObservedSkill(ctx, repo.CreateObservedSkillParams{
				ProjectID: projectID, Name: observation.name, DisplayName: observation.displayName,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				skill, err = queries.GetSkillByNameForUpdate(ctx, repo.GetSkillByNameForUpdateParams{ProjectID: projectID, Name: observation.name})
			}
		}
		if err != nil {
			return nil, fmt.Errorf("resolve observed skill: %w", err)
		}

		count, err := queries.CompleteSkillObservations(ctx, repo.CompleteSkillObservationsParams{
			ProjectID: projectID, ObservationIds: observationIDs, SkillID: skill.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("complete skill observation: %w", err)
		}
		if count == 0 {
			index = end
			continue
		}
		processed += int(count)
		index = end
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit skill observation reconciliation: %w", err)
	}
	return &ReconcileSkillObservationsResult{Processed: processed, HasMore: len(rows) == int(batchSize)}, nil
}

func normalizeObservedSkillName(observedName, source, sourceLevel, provider string) (string, string, error) {
	displayName := strings.TrimSpace(observedName)
	if strings.EqualFold(strings.TrimSpace(sourceLevel), "plugin") ||
		strings.EqualFold(strings.TrimSpace(source), "marketplace") ||
		strings.EqualFold(strings.TrimSpace(source), "plugin") ||
		strings.EqualFold(strings.TrimSpace(provider), "marketplace") ||
		strings.EqualFold(strings.TrimSpace(provider), "plugin") {
		if separator := strings.LastIndexByte(displayName, ':'); separator >= 0 {
			displayName = strings.TrimSpace(displayName[separator+1:])
		}
	}
	name, err := normalizeSkillName(displayName)
	return displayName, name, err
}
