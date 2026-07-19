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
const reconcileErrorUnresolvedHash = "unresolved_hash"
const reconcileErrorAmbiguousHash = "ambiguous_hash"

type ReconcileSkillObservationsResult struct {
	Processed int
	HasMore   bool
}

type pendingSkillObservation struct {
	row         repo.SkillObservation
	name        string
	displayName string
	skillID     uuid.UUID
	versionID   uuid.NullUUID
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
	if err := queries.LockSkillObservationReconciliation(ctx, projectID); err != nil {
		return nil, fmt.Errorf("lock skill observation reconciliation: %w", err)
	}

	rows, err := queries.ClaimPendingSkillObservations(ctx, repo.ClaimPendingSkillObservationsParams{
		ProjectID: projectID,
		BatchSize: batchSize,
	})
	if err != nil {
		return nil, fmt.Errorf("claim skill observations: %w", err)
	}
	pending := make([]pendingSkillObservation, 0, len(rows))
	failedIDs := make(map[string][]uuid.UUID)
	rawHashes := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.RawSha256.Valid && (!row.SkillID.Valid || !row.SkillVersionID.Valid) {
			rawHashes = append(rawHashes, row.RawSha256.String)
		}
	}
	resolvedVersions := make(map[string][]repo.ResolveSkillObservationVersionsRow, len(rawHashes))
	if len(rawHashes) > 0 {
		versions, resolveErr := queries.ResolveSkillObservationVersions(ctx, repo.ResolveSkillObservationVersionsParams{
			ProjectID: projectID, RawSha256s: rawHashes,
		})
		if resolveErr != nil {
			return nil, fmt.Errorf("resolve observed skill hashes: %w", resolveErr)
		}
		for _, version := range versions {
			resolvedVersions[version.RawSha256] = append(resolvedVersions[version.RawSha256], version)
		}
	}
	for _, row := range rows {
		if row.SkillID.Valid && row.SkillVersionID.Valid {
			pending = append(pending, pendingSkillObservation{
				row: row, name: "", displayName: "", skillID: row.SkillID.UUID, versionID: row.SkillVersionID,
			})
			continue
		}
		if row.RawSha256.Valid {
			versions := resolvedVersions[row.RawSha256.String]
			if len(versions) == 1 {
				pending = append(pending, pendingSkillObservation{
					row: row, name: "", displayName: "", skillID: versions[0].SkillID,
					versionID: conv.ToNullUUID(versions[0].SkillVersionID),
				})
				continue
			}
			errorCode := reconcileErrorUnresolvedHash
			if len(versions) > 1 {
				errorCode = reconcileErrorAmbiguousHash
			}
			failedIDs[errorCode] = append(failedIDs[errorCode], row.ID)
			continue
		}
		displayName, name, normalizeErr := normalizeObservedSkillName(row.SkillName, row.SourceLevel.String)
		if normalizeErr != nil {
			failedIDs[reconcileErrorInvalidName] = append(failedIDs[reconcileErrorInvalidName], row.ID)
			continue
		}
		pending = append(pending, pendingSkillObservation{
			row: row, name: name, displayName: displayName, skillID: uuid.Nil,
			versionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		})
	}
	processed := 0
	for errorCode, observationIDs := range failedIDs {
		count, err := queries.FailSkillObservationReconciliations(ctx, repo.FailSkillObservationReconciliationsParams{
			ProjectID: projectID, ObservationIds: observationIDs, ErrorCode: conv.ToPGText(errorCode),
		})
		if err != nil {
			return nil, fmt.Errorf("mark unresolved skill observations: %w", err)
		}
		processed += int(count)
	}
	// Advisory name locks must be acquired in a stable order when two workers
	// happen to claim the same project's rows concurrently.
	sort.SliceStable(pending, func(i, j int) bool {
		if pending[i].skillID != pending[j].skillID {
			return pending[i].skillID.String() < pending[j].skillID.String()
		}
		if pending[i].versionID.UUID != pending[j].versionID.UUID {
			return pending[i].versionID.UUID.String() < pending[j].versionID.UUID.String()
		}
		if pending[i].name != pending[j].name {
			return pending[i].name < pending[j].name
		}
		return pending[i].row.ID.String() < pending[j].row.ID.String()
	})

	for index := 0; index < len(pending); {
		observation := pending[index]
		end := index + 1
		observationIDs := []uuid.UUID{observation.row.ID}
		for end < len(pending) && pending[end].skillID == observation.skillID && pending[end].versionID == observation.versionID && pending[end].name == observation.name {
			observationIDs = append(observationIDs, pending[end].row.ID)
			end++
		}

		skillID := observation.skillID
		if skillID == uuid.Nil {
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
			skillID = skill.ID
		}

		count, err := queries.CompleteSkillObservations(ctx, repo.CompleteSkillObservationsParams{
			ProjectID: projectID, ObservationIds: observationIDs, SkillID: skillID, SkillVersionID: observation.versionID,
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

func normalizeObservedSkillName(observedName, sourceLevel string) (string, string, error) {
	displayName := strings.TrimSpace(observedName)
	if strings.EqualFold(strings.TrimSpace(sourceLevel), "plugin") {
		if separator := strings.LastIndexByte(displayName, ':'); separator >= 0 {
			displayName = strings.TrimSpace(displayName[separator+1:])
		}
	}
	name, err := normalizeSkillName(displayName)
	return displayName, name, err
}
