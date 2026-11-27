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
	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
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
// existing skill's presentation or emitting a user-authored audit event. A raw
// hash that already resolves to a version keeps that version, whatever
// supporting files it has.
func CaptureSkillContent(ctx context.Context, db *pgxpool.Pool, projectID uuid.UUID, content string) (*CaptureResult, error) {
	return captureSkill(ctx, db, projectID, content, nil)
}

// CaptureSkillTree materializes an observed skill directory: its SKILL.md and
// supporting files. The captured version is the one whose tree digest matches
// the directory, created with its files in the same transaction when no such
// version exists. When the directory changed without its SKILL.md changing,
// the raw hash alias moves to the captured version so later activations
// attribute to the latest captured tree.
func CaptureSkillTree(ctx context.Context, db *pgxpool.Pool, projectID uuid.UUID, content string, files []skilltree.File) (*CaptureResult, error) {
	if files == nil {
		files = []skilltree.File{}
	}
	return captureSkill(ctx, db, projectID, content, files)
}

// captureSkill captures a SKILL.md and, when files is non-nil, the supporting
// files of its directory. A nil files keeps the manifest-only capture rules.
func captureSkill(ctx context.Context, db *pgxpool.Pool, projectID uuid.UUID, content string, files []skilltree.File) (*CaptureResult, error) {
	parsed, err := parseSkillManifest(content)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidCapture, manifestErrorMessage(err))
	}
	captureTree := files != nil
	var entries []skilltree.Entry
	canonicalSHA256 := parsed.CanonicalSHA256
	if captureTree {
		entries, err = skilltree.Validate(files)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrInvalidCapture, err.Error())
		}
		tree, err := parsed.withSupportingFiles(entries)
		if err != nil {
			return nil, fmt.Errorf("digest captured skill tree: %w", err)
		}
		canonicalSHA256 = tree.CanonicalSHA256
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
	if err := queries.LockSkillObservationReconciliation(ctx, projectID); err != nil {
		return nil, fmt.Errorf("lock skill observation reconciliation: %w", err)
	}
	resolved, err := queries.ResolveSkillObservationVersions(ctx, repo.ResolveSkillObservationVersionsParams{
		ProjectID: projectID, RawSha256s: []string{parsed.RawSHA256},
	})
	if err != nil {
		return nil, fmt.Errorf("resolve captured skill hash: %w", err)
	}

	var skill repo.Skill
	var version repo.SkillVersion
	createdSkill, createdVersion := false, false
	switch len(resolved) {
	case 0:
		if err := queries.LockSkillName(ctx, repo.LockSkillNameParams{ProjectID: projectID, Name: parsed.Name}); err != nil {
			return nil, fmt.Errorf("lock captured skill name: %w", err)
		}
		skill, err = queries.GetSkillByNameForUpdate(ctx, repo.GetSkillByNameForUpdateParams{ProjectID: projectID, Name: parsed.Name})
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
		version, createdVersion, err = findOrCreateCapturedVersion(ctx, queries, projectID, skill.ID, parsed, canonicalSHA256, entries, metadata, validationErrors)
		if err != nil {
			return nil, err
		}
	case 1:
		skill, err = queries.GetSkillForUpdate(ctx, repo.GetSkillForUpdateParams{
			ProjectID: projectID, ID: resolved[0].SkillID,
		})
		if err == nil {
			version, err = queries.GetProjectSkillVersion(ctx, repo.GetProjectSkillVersionParams{
				ProjectID: projectID, SkillVersionID: resolved[0].SkillVersionID,
			})
		}
		if err != nil {
			return nil, fmt.Errorf("load captured skill by hash: %w", err)
		}
		// The alias may point at a version with supporting files, whose
		// canonical hash covers more than SKILL.md, so compare the manifests
		// themselves to catch an alias that belongs to different content.
		if !sameCapturedManifest(version, parsed) {
			return nil, ErrCaptureHashConflict
		}
		if captureTree && version.CanonicalSha256 != canonicalSHA256 {
			// Same SKILL.md bytes, different directory: capture the tree as
			// its own version of the skill the raw hash already belongs to.
			version, createdVersion, err = findOrCreateCapturedVersion(ctx, queries, projectID, skill.ID, parsed, canonicalSHA256, entries, metadata, validationErrors)
			if err != nil {
				return nil, err
			}
			if err := queries.RepointSkillRawHashAlias(ctx, repo.RepointSkillRawHashAliasParams{
				ProjectID: projectID, RawSha256: parsed.RawSHA256, SkillID: skill.ID, SkillVersionID: version.ID,
			}); err != nil {
				return nil, fmt.Errorf("repoint captured skill raw hash alias: %w", err)
			}
		}
	default:
		return nil, ErrCaptureHashConflict
	}
	if createdVersion {
		if err := queries.InsertCapturedSkillVersionOrigin(ctx, repo.InsertCapturedSkillVersionOriginParams{
			ProjectID: projectID, SkillID: skill.ID, SkillVersionID: version.ID,
		}); err != nil {
			return nil, fmt.Errorf("record captured skill version origin: %w", err)
		}
		// The captured version becomes the skill's current version, so the
		// registry summary follows its manifest description — same step as the
		// manual record-version flow.
		skill, err = queries.SyncSkillSummary(ctx, repo.SyncSkillSummaryParams{
			ProjectID: projectID, ID: skill.ID, Summary: conv.PtrToPGText(parsed.Description),
		})
		if err != nil {
			return nil, fmt.Errorf("sync captured skill summary: %w", err)
		}
		if err := ReplayOpenSuggestionOntoBase(ctx, queries, projectID, skill.ID); err != nil {
			return nil, err
		}
	}

	matches, err := queries.StoreSkillRawHashAlias(ctx, repo.StoreSkillRawHashAliasParams{
		ProjectID: projectID, SkillID: skill.ID, SkillVersionID: version.ID,
		RawSha256: parsed.RawSHA256, CanonicalSha256: version.CanonicalSha256,
	})
	if err != nil {
		return nil, fmt.Errorf("store captured skill raw hash alias: %w", err)
	}
	if !matches {
		return nil, ErrCaptureHashConflict
	}
	if _, err := queries.BackfillSkillObservationsForCapturedVersion(ctx, repo.BackfillSkillObservationsForCapturedVersionParams{
		ProjectID: projectID, RawSha256: parsed.RawSHA256, CanonicalSha256: version.CanonicalSha256,
		SkillID: skill.ID, SkillVersionID: version.ID,
	}); err != nil {
		return nil, fmt.Errorf("backfill captured skill observations: %w", err)
	}
	if createdVersion && !createdSkill {
		if err := lockPluginPackageContent(ctx, tx, projectID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit captured skill transaction: %w", err)
	}

	return &CaptureResult{SkillID: skill.ID, SkillVersionID: version.ID, CreatedSkill: createdSkill, CreatedVersion: createdVersion}, nil
}

// findOrCreateCapturedVersion returns the skill's version with canonicalSHA256,
// creating it with the captured manifest and supporting files when it does not
// exist. The bool reports whether the version was created.
func findOrCreateCapturedVersion(
	ctx context.Context,
	queries *repo.Queries,
	projectID, skillID uuid.UUID,
	parsed parsedSkillManifest,
	canonicalSHA256 string,
	entries []skilltree.Entry,
	metadata, validationErrors []byte,
) (repo.SkillVersion, bool, error) {
	version, err := queries.CreateSkillVersion(ctx, repo.CreateSkillVersionParams{
		Content: parsed.RawContent, CanonicalSha256: canonicalSHA256, RawSha256: parsed.RawSHA256,
		Description: conv.PtrToPGText(parsed.Description), Metadata: metadata, SpecValid: parsed.SpecValid,
		ValidationErrors: validationErrors, CreatedByUserID: "system", ProjectID: projectID, SkillID: skillID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// The tree digest covers every file, so an existing version with the
		// same digest already holds these supporting files.
		version, err = queries.GetSkillVersionByHash(ctx, repo.GetSkillVersionByHashParams{
			ProjectID: projectID, SkillID: skillID, CanonicalSha256: canonicalSHA256,
		})
		if err != nil {
			return repo.SkillVersion{}, false, fmt.Errorf("load captured skill version by hash: %w", err)
		}
		return version, false, nil
	}
	if err != nil {
		return repo.SkillVersion{}, false, fmt.Errorf("create captured skill version: %w", err)
	}

	if err := insertSkillVersionFiles(ctx, queries, projectID, skillID, version.ID, entries); err != nil {
		return repo.SkillVersion{}, false, fmt.Errorf("store captured skill files: %w", err)
	}
	return version, true, nil
}

// sameCapturedManifest reports whether a stored version's SKILL.md is
// canonically the captured one.
func sameCapturedManifest(version repo.SkillVersion, parsed parsedSkillManifest) bool {
	if version.CanonicalSha256 == parsed.CanonicalSHA256 {
		return true
	}
	stored, err := parseSkillManifest(version.Content)
	return err == nil && stored.canonicalContent == parsed.canonicalContent
}
