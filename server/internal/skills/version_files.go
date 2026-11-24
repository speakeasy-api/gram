package skills

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/skilldiff"
	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

// LoadSkillVersionFiles returns a skill version's supporting files with their
// content, sorted by path.
func LoadSkillVersionFiles(ctx context.Context, queries *repo.Queries, projectID, skillID, versionID uuid.UUID) ([]skilltree.File, error) {
	rows, err := queries.ListSkillVersionFiles(ctx, repo.ListSkillVersionFilesParams{
		ProjectID:      projectID,
		SkillID:        skillID,
		SkillVersionID: versionID,
	})
	if err != nil {
		return nil, fmt.Errorf("list skill version files: %w", err)
	}

	files := make([]skilltree.File, len(rows))
	for i, row := range rows {
		files[i] = skilltree.File{Path: row.Path, Content: row.Content, ContentType: row.ContentType}
	}
	return files, nil
}

// LoadSuggestionTree returns the content a suggestion against versionID edits:
// manifest, which the caller already holds, plus the version's supporting
// files.
func LoadSuggestionTree(ctx context.Context, queries *repo.Queries, projectID, skillID, versionID uuid.UUID, manifest string) (skilldiff.Tree, error) {
	files, err := LoadSkillVersionFiles(ctx, queries, projectID, skillID, versionID)
	if err != nil {
		return skilldiff.Tree{}, err
	}
	return skilldiff.Tree{Manifest: manifest, Files: files}, nil
}

// entryFiles converts validated entries back to files.
func entryFiles(entries []skilltree.Entry) []skilltree.File {
	files := make([]skilltree.File, len(entries))
	for i, entry := range entries {
		files[i] = skilltree.File{Path: entry.Path, Content: entry.Content, ContentType: entry.ContentType}
	}
	return files
}
