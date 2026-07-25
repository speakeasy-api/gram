package mv

import (
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
)

func BuildSkillEditSuggestionView(suggestion repo.SkillEditSuggestion, skillName, skillDisplayName string) *types.SkillEditSuggestion {
	return &types.SkillEditSuggestion{
		ID:                 suggestion.ID.String(),
		SkillID:            suggestion.SkillID.String(),
		SkillName:          skillName,
		SkillDisplayName:   skillDisplayName,
		BaseVersionID:      suggestion.BaseVersionID.String(),
		ProposedContent:    suggestion.ProposedContent,
		Rationale:          suggestion.Rationale,
		Status:             suggestion.Status,
		FeedbackCount:      suggestion.FeedbackCount,
		ScoredSessionCount: suggestion.ScoredSessionCount,
		ResultingVersionID: conv.FromNullableUUID(suggestion.ResultingVersionID),
		ApprovedByUserID:   conv.FromPGText[string](suggestion.ApprovedByUserID),
		ApprovedAt:         conv.PtrEmpty(conv.FromPGTimestamptz(suggestion.ApprovedAt)),
		CreatedAt:          conv.FromPGTimestamptz(suggestion.CreatedAt),
		UpdatedAt:          conv.FromPGTimestamptz(suggestion.UpdatedAt),
	}
}

func BuildSkillEditSuggestionListView(rows []repo.ListOpenSkillEditSuggestionsRow) []*types.SkillEditSuggestion {
	result := make([]*types.SkillEditSuggestion, len(rows))
	for i, row := range rows {
		result[i] = BuildSkillEditSuggestionView(row.SkillEditSuggestion, row.SkillName, row.SkillDisplayName)
	}
	return result
}
