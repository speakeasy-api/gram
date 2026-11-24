package skills

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/mv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

// supportingFiles is what a write asked for: an explicit set of supporting
// files, or none, in which case the version keeps the files of its base.
type supportingFiles struct {
	// explicit reports whether the caller sent a file list. An empty explicit
	// list removes every supporting file.
	explicit bool

	// entries are the validated files of an explicit list.
	entries []skilltree.Entry
}

// validateSupportingFiles checks the files of a write request. A nil list
// means the caller did not send files.
func validateSupportingFiles(inputs []*types.SkillFileInput) (supportingFiles, error) {
	if inputs == nil {
		return supportingFiles{explicit: false, entries: nil}, nil
	}

	files := make([]skilltree.File, 0, len(inputs))
	for _, input := range inputs {
		if input == nil {
			return supportingFiles{}, oops.E(oops.CodeBadRequest, nil, "skill file entries must not be null")
		}
		files = append(files, skilltree.File{
			Path:        input.Path,
			Content:     input.Content,
			ContentType: conv.PtrValOr(input.ContentType, ""),
		})
	}
	return validateSkillFiles(files)
}

func validateSkillFiles(files []skilltree.File) (supportingFiles, error) {
	entries, err := skilltree.Validate(files)
	if err != nil {
		return supportingFiles{}, oops.E(oops.CodeBadRequest, err, "%s", err.Error())
	}
	return supportingFiles{explicit: true, entries: entries}, nil
}

// resolve returns the supporting files of the version being written: the
// explicit list, or a copy of the files of baseVersionID. An invalid
// baseVersionID means there is no base, so the version has no files.
func (f supportingFiles) resolve(ctx context.Context, queries *repo.Queries, projectID, skillID uuid.UUID, baseVersionID uuid.NullUUID) ([]skilltree.Entry, error) {
	if f.explicit || !baseVersionID.Valid {
		return f.entries, nil
	}

	rows, err := queries.ListSkillVersionFiles(ctx, repo.ListSkillVersionFilesParams{
		ProjectID:      projectID,
		SkillID:        skillID,
		SkillVersionID: baseVersionID.UUID,
	})
	if err != nil {
		return nil, fmt.Errorf("list base skill version files: %w", err)
	}
	entries := make([]skilltree.Entry, len(rows))
	for i, row := range rows {
		entries[i] = skilltree.Entry{
			Path:        row.Path,
			Content:     row.Content,
			SHA256:      row.Sha256,
			Size:        row.SizeBytes,
			ContentType: row.ContentType,
		}
	}
	return entries, nil
}

// insertSkillVersionFiles stores the supporting files of a newly created
// version.
func insertSkillVersionFiles(ctx context.Context, queries *repo.Queries, projectID, skillID, versionID uuid.UUID, entries []skilltree.Entry) error {
	for _, entry := range entries {
		if err := queries.CreateSkillVersionFile(ctx, repo.CreateSkillVersionFileParams{
			Path:           entry.Path,
			Content:        entry.Content,
			Sha256:         entry.SHA256,
			SizeBytes:      entry.Size,
			ContentType:    entry.ContentType,
			ProjectID:      projectID,
			SkillID:        skillID,
			SkillVersionID: versionID,
		}); err != nil {
			return fmt.Errorf("create skill version file %q: %w", entry.Path, err)
		}
	}
	return nil
}

// skillFileViews renders validated entries as version file metadata.
func skillFileViews(entries []skilltree.Entry) []*types.SkillFile {
	views := make([]*types.SkillFile, len(entries))
	for i, entry := range entries {
		views[i] = &types.SkillFile{
			Path:        entry.Path,
			Sha256:      entry.SHA256,
			SizeBytes:   entry.Size,
			ContentType: entry.ContentType,
		}
	}
	return views
}

// listSkillFileViews loads the supporting-file metadata of one version.
func listSkillFileViews(ctx context.Context, queries *repo.Queries, projectID, skillID, versionID uuid.UUID) ([]*types.SkillFile, error) {
	rows, err := queries.ListSkillVersionFileMetadata(ctx, repo.ListSkillVersionFileMetadataParams{
		ProjectID:      projectID,
		SkillID:        skillID,
		SkillVersionID: versionID,
	})
	if err != nil {
		return nil, fmt.Errorf("list skill version files: %w", err)
	}
	return mv.BuildSkillFileListView(rows), nil
}

func (s *Service) GetVersionFile(ctx context.Context, payload *gen.GetVersionFilePayload) (*types.SkillVersionFileContent, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || authCtx.ProjectID == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	logger := s.logger.With(
		attr.SlogOrganizationID(authCtx.ActiveOrganizationID),
		attr.SlogProjectID(authCtx.ProjectID.String()),
	)
	if err := s.requireSkillReadAccess(ctx, authCtx, payload.SkillID); err != nil {
		return nil, err
	}

	skillID, err := uuid.Parse(payload.SkillID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill id")
	}
	versionID, err := uuid.Parse(payload.VersionID)
	if err != nil {
		return nil, oops.E(oops.CodeBadRequest, nil, "invalid skill version id")
	}

	return getSkillVersionFile(ctx, logger, repo.New(s.db), *authCtx.ProjectID, skillID, versionID, payload.Path)
}

func getSkillVersionFile(ctx context.Context, logger *slog.Logger, queries *repo.Queries, projectID, skillID, versionID uuid.UUID, path string) (*types.SkillVersionFileContent, error) {
	if _, err := queries.GetSkill(ctx, repo.GetSkillParams{ProjectID: projectID, ID: skillID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oops.E(oops.CodeNotFound, nil, "skill not found")
		}
		return nil, oops.E(oops.CodeUnexpected, err, "get skill for version file").LogError(ctx, logger)
	}

	file, err := queries.GetSkillVersionFile(ctx, repo.GetSkillVersionFileParams{
		ProjectID:      projectID,
		SkillID:        skillID,
		SkillVersionID: versionID,
		Path:           path,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, oops.E(oops.CodeNotFound, nil, "skill version file not found")
	case err != nil:
		return nil, oops.E(oops.CodeUnexpected, err, "get skill version file").LogError(ctx, logger)
	}

	return &types.SkillVersionFileContent{
		Path:        file.Path,
		ContentType: file.ContentType,
		Sha256:      file.Sha256,
		SizeBytes:   file.SizeBytes,
		Content:     file.Content,
	}, nil
}
