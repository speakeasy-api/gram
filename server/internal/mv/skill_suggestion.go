package mv

import (
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/skilldiff"
)

// SkillEditSuggestionEvidence carries the values a suggestion derives from its
// linked feedback and base version rather than storing on the row itself.
type SkillEditSuggestionEvidence struct {
	SkillName            string
	SkillDisplayName     string
	BaseContent          string
	FeedbackCount        int64
	FeedbackSessionCount int64
}

func BuildSkillEditSuggestionView(suggestion repo.SkillEditSuggestion, evidence SkillEditSuggestionEvidence) *types.SkillEditSuggestion {
	// A suggestion whose diff no longer applies is reported rather than hidden:
	// the reviewer needs to know the proposal went stale.
	proposedContent, err := skilldiff.Apply(evidence.BaseContent, suggestion.ProposedDiff)
	if err != nil {
		proposedContent = ""
	}

	return &types.SkillEditSuggestion{
		ID:                   suggestion.ID.String(),
		SkillID:              suggestion.SkillID.String(),
		SkillName:            evidence.SkillName,
		SkillDisplayName:     evidence.SkillDisplayName,
		BaseVersionID:        suggestion.BaseVersionID.String(),
		ProposedDiff:         suggestion.ProposedDiff,
		ProposedContent:      proposedContent,
		AppliesCleanly:       err == nil,
		Rationale:            suggestion.Rationale,
		Status:               suggestion.Status,
		FeedbackCount:        evidence.FeedbackCount,
		FeedbackSessionCount: evidence.FeedbackSessionCount,
		ScoredSessionCount:   suggestion.ScoredSessionCount,
		ApprovedByUserID:     conv.FromPGText[string](suggestion.ApprovedByUserID),
		ApprovedAt:           conv.PtrEmpty(conv.FromPGTimestamptz(suggestion.ApprovedAt)),
		CreatedAt:            conv.FromPGTimestamptz(suggestion.CreatedAt),
		UpdatedAt:            conv.FromPGTimestamptz(suggestion.UpdatedAt),
	}
}

func BuildSkillEditSuggestionListView(rows []repo.ListOpenSkillEditSuggestionsRow) []*types.SkillEditSuggestion {
	result := make([]*types.SkillEditSuggestion, len(rows))
	for i, row := range rows {
		result[i] = BuildSkillEditSuggestionView(row.SkillEditSuggestion, SkillEditSuggestionEvidence{
			SkillName:            row.SkillName,
			SkillDisplayName:     row.SkillDisplayName,
			BaseContent:          row.BaseContent,
			FeedbackCount:        row.FeedbackCount,
			FeedbackSessionCount: row.FeedbackSessionCount,
		})
	}

	return result
}
