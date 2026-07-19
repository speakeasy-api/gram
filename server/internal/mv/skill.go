package mv

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func BuildSkillView(skill repo.Skill, latestVersionID uuid.UUID, versionCount int64) *types.Skill {
	var latestVersionIDValue *string
	if latestVersionID != uuid.Nil {
		latestVersionIDValue = conv.PtrEmpty(latestVersionID.String())
	}
	return &types.Skill{
		ID:              skill.ID.String(),
		ProjectID:       skill.ProjectID.String(),
		Name:            skill.Name,
		DisplayName:     skill.DisplayName,
		Summary:         conv.FromPGText[string](skill.Summary),
		SourceKind:      skill.SourceKind,
		Classification:  skill.Classification,
		LatestVersionID: latestVersionIDValue,
		VersionCount:    versionCount,
		FirstSeenAt:     conv.PtrEmpty(conv.FromPGTimestamptz(skill.FirstSeenAt)),
		LastSeenAt:      conv.PtrEmpty(conv.FromPGTimestamptz(skill.LastSeenAt)),
		SeenCount:       skill.SeenCount,
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

type SkillVersionSightingStats struct {
	FirstSeenAt pgtype.Timestamptz
	LastSeenAt  pgtype.Timestamptz
	SeenCount   int64
}

func BuildSkillVersionView(version repo.SkillVersion, frontmatter map[string]any, sightings SkillVersionSightingStats) (*types.SkillVersion, error) {
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
		FirstSeenAt:      conv.PtrEmpty(conv.FromPGTimestamptz(sightings.FirstSeenAt)),
		LastSeenAt:       conv.PtrEmpty(conv.FromPGTimestamptz(sightings.LastSeenAt)),
		SeenCount:        sightings.SeenCount,
	}, nil
}

func BuildSkillVersionListView(rows []repo.ListSkillVersionsRow, frontmatter func(content string) map[string]any) ([]*types.SkillVersion, error) {
	result := make([]*types.SkillVersion, len(rows))
	for i, row := range rows {
		view, err := BuildSkillVersionView(row.SkillVersion, frontmatter(row.SkillVersion.Content), SkillVersionSightingStats{
			FirstSeenAt: row.FirstSeenAt,
			LastSeenAt:  row.LastSeenAt,
			SeenCount:   row.SeenCount,
		})
		if err != nil {
			return nil, fmt.Errorf("build skill version %s: %w", row.SkillVersion.ID, err)
		}
		result[i] = view
	}

	return result, nil
}

func BuildSkillDistributionView(distribution repo.SkillDistribution, skillName string, skillDisplayName string, pluginName string, resolvedVersionID uuid.UUID) *types.SkillDistribution {
	return &types.SkillDistribution{
		ID:                distribution.ID.String(),
		ProjectID:         distribution.ProjectID.String(),
		SkillID:           distribution.SkillID.String(),
		SkillName:         skillName,
		SkillDisplayName:  skillDisplayName,
		PluginID:          distribution.PluginID.UUID.String(),
		PluginName:        pluginName,
		PinnedVersionID:   conv.FromNullableUUID(distribution.PinnedVersionID),
		ResolvedVersionID: resolvedVersionID.String(),
		Channel:           distribution.Channel,
		CreatedByUserID:   distribution.CreatedByUserID,
		CreatedAt:         conv.FromPGTimestamptz(distribution.CreatedAt),
		UpdatedAt:         conv.FromPGTimestamptz(distribution.UpdatedAt),
	}
}

func BuildSkillDistributionListView(rows []repo.ListActiveSkillDistributionsRow) []*types.SkillDistribution {
	result := make([]*types.SkillDistribution, len(rows))
	for i, row := range rows {
		result[i] = BuildSkillDistributionView(row.SkillDistribution, row.SkillName, row.SkillDisplayName, row.PluginName, row.ResolvedVersionID)
	}

	return result
}
