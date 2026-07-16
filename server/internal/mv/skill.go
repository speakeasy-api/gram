package mv

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func BuildSkillView(skill repo.Skill, latestVersionID uuid.UUID, versionCount int64) *types.Skill {
	return &types.Skill{
		ID:              skill.ID.String(),
		ProjectID:       skill.ProjectID.String(),
		Name:            skill.Name,
		DisplayName:     skill.DisplayName,
		Summary:         conv.FromPGText[string](skill.Summary),
		SourceKind:      skill.SourceKind,
		Classification:  skill.Classification,
		LatestVersionID: latestVersionID.String(),
		VersionCount:    versionCount,
		CreatedAt:       conv.FromPGTimestamptz(skill.CreatedAt),
		UpdatedAt:       conv.FromPGTimestamptz(skill.UpdatedAt),
	}
}

func BuildSkillListView(rows []repo.ListSkillsRow) []*types.Skill {
	result := make([]*types.Skill, len(rows))
	for i, row := range rows {
		result[i] = BuildSkillView(row.Skill, row.LatestVersionID, row.VersionCount)
	}

	return result
}

func BuildSkillVersionView(version repo.SkillVersion, frontmatter map[string]any) (*types.SkillVersion, error) {
	metadata := make(map[string]any)
	metadataDecoder := json.NewDecoder(bytes.NewReader(version.Metadata))
	metadataDecoder.UseNumber()
	if err := metadataDecoder.Decode(&metadata); err != nil {
		return nil, fmt.Errorf("decode skill version metadata: %w", err)
	}
	if metadata == nil {
		metadata = make(map[string]any)
	}

	if frontmatter == nil {
		frontmatter = make(map[string]any)
	}

	validationErrors := make([]*types.SkillValidationError, 0)
	//nolint:musttag // Goa's generated type uses exported field names without JSON tags.
	if err := json.Unmarshal(version.ValidationErrors, &validationErrors); err != nil {
		return nil, fmt.Errorf("decode skill version validation errors: %w", err)
	}
	if validationErrors == nil {
		validationErrors = make([]*types.SkillValidationError, 0)
	}

	return &types.SkillVersion{
		ID:               version.ID.String(),
		SkillID:          version.SkillID.String(),
		Content:          version.Content,
		CanonicalSha256:  version.CanonicalSha256,
		RawSha256:        version.RawSha256,
		Description:      conv.FromPGText[string](version.Description),
		Metadata:         metadata,
		Frontmatter:      frontmatter,
		SpecValid:        version.SpecValid,
		ValidationErrors: validationErrors,
		CreatedAt:        conv.FromPGTimestamptz(version.CreatedAt),
		CreatedByUserID:  version.CreatedByUserID,
	}, nil
}

func BuildSkillVersionListView(rows []repo.SkillVersion, frontmatter func(content string) map[string]any) ([]*types.SkillVersion, error) {
	result := make([]*types.SkillVersion, len(rows))
	for i, row := range rows {
		view, err := BuildSkillVersionView(row, frontmatter(row.Content))
		if err != nil {
			return nil, fmt.Errorf("build skill version %s: %w", row.ID, err)
		}
		result[i] = view
	}

	return result, nil
}
