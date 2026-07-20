package skills

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

var ErrInvalidCapture = errors.New("invalid captured skill manifest")
var ErrCaptureHashConflict = errors.New("captured skill raw hash conflicts with an existing alias")

type CaptureResult struct {
	SkillID        uuid.UUID
	SkillVersionID uuid.UUID
	CreatedSkill   bool
	CreatedVersion bool
}

// CaptureSkillContent materializes an observed manifest without changing an
// existing skill's presentation or emitting a user-authored audit event.
func CaptureSkillContent(ctx context.Context, db *pgxpool.Pool, projectID uuid.UUID, content string) (*CaptureResult, error) {
	parsed, err := parseSkillManifest(content)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidCapture, manifestErrorMessage(err))
	}
	metadata, err := json.Marshal(parsed.Metadata)
	if err != nil {
		return nil, fmt.Errorf("encode captured skill metadata: %w", err)
	}
	validationErrors, err := json.Marshal(parsed.ValidationErrors)
	if err != nil {
		return nil, fmt.Errorf("encode captured skill validation errors: %w", err)
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin captured skill transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	queries := repo.New(tx)
	if err := queries.LockSkillName(ctx, repo.LockSkillNameParams{ProjectID: projectID, Name: parsed.Name}); err != nil {
		return nil, fmt.Errorf("lock captured skill name: %w", err)
	}

	skill, err := queries.GetSkillByNameForUpdate(ctx, repo.GetSkillByNameForUpdateParams{ProjectID: projectID, Name: parsed.Name})
	createdSkill := false
	if errors.Is(err, pgx.ErrNoRows) {
		skill, err = queries.CreateCapturedSkill(ctx, repo.CreateCapturedSkillParams{
			ProjectID: projectID, Name: parsed.Name, DisplayName: parsed.DisplayName, Summary: conv.PtrToPGText(parsed.Description),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			skill, err = queries.GetSkillByNameForUpdate(ctx, repo.GetSkillByNameForUpdateParams{ProjectID: projectID, Name: parsed.Name})
		} else if err == nil {
			createdSkill = true
		}
	}
	if err != nil {
		return nil, fmt.Errorf("resolve captured skill: %w", err)
	}

	version, err := queries.CreateSkillVersion(ctx, repo.CreateSkillVersionParams{
		Content: parsed.RawContent, CanonicalSha256: parsed.CanonicalSHA256, RawSha256: parsed.RawSHA256,
		Description: conv.PtrToPGText(parsed.Description), Metadata: metadata, SpecValid: parsed.SpecValid,
		ValidationErrors: validationErrors, CreatedByUserID: "system", ProjectID: projectID, SkillID: skill.ID,
	})
	createdVersion := err == nil
	if errors.Is(err, pgx.ErrNoRows) {
		version, err = queries.GetSkillVersionByHash(ctx, repo.GetSkillVersionByHashParams{
			ProjectID: projectID, SkillID: skill.ID, CanonicalSha256: parsed.CanonicalSHA256,
		})
	}
	if err != nil {
		return nil, fmt.Errorf("create captured skill version: %w", err)
	}
	if createdVersion {
		if err := queries.InsertCapturedSkillVersionOrigin(ctx, repo.InsertCapturedSkillVersionOriginParams{
			ProjectID: projectID, SkillID: skill.ID, SkillVersionID: version.ID,
		}); err != nil {
			return nil, fmt.Errorf("record captured skill version origin: %w", err)
		}
	}

	matches, err := queries.StoreSkillRawHashAlias(ctx, repo.StoreSkillRawHashAliasParams{
		ProjectID: projectID, SkillID: skill.ID, SkillVersionID: version.ID,
		RawSha256: parsed.RawSHA256, CanonicalSha256: parsed.CanonicalSHA256,
	})
	if err != nil {
		return nil, fmt.Errorf("store captured skill raw hash alias: %w", err)
	}
	if !matches {
		return nil, ErrCaptureHashConflict
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit captured skill transaction: %w", err)
	}

	return &CaptureResult{SkillID: skill.ID, SkillVersionID: version.ID, CreatedSkill: createdSkill, CreatedVersion: createdVersion}, nil
}
