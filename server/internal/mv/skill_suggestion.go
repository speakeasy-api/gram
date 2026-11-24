package mv

import (
	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/skills/repo"
	"github.com/speakeasy-api/gram/server/internal/skills/skilldiff"
	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

// SkillEditSuggestionEvidence carries the values a suggestion derives from its
// proposed changes and base version rather than storing on the row itself.
type SkillEditSuggestionEvidence struct {
	SkillName        string
	SkillDisplayName string
	BaseContent      string
	// BaseFiles are the base version's supporting files. Changes to a
	// supporting file are checked against them; leave it nil only when no
	// change names a supporting file.
	BaseFiles            []skilltree.File
	Changes              []repo.ListSkillEditSuggestionChangesRow
	FeedbackCount        int64
	FeedbackSessionCount int64
}

func BuildSkillEditSuggestionView(suggestion repo.SkillEditSuggestion, evidence SkillEditSuggestionEvidence) *types.SkillEditSuggestion {
	// Each change is applied to what the ones before it produced in the same
	// file, so the whole proposal is the skill a reviewer gets by taking every
	// change. A change that no longer applies is reported rather than hidden:
	// the reviewer needs to know that part of the proposal went stale.
	proposed := skilldiff.Tree{Manifest: evidence.BaseContent, Files: evidence.BaseFiles}
	appliesCleanly := true
	changes := make([]*types.SkillEditSuggestionChange, 0, len(evidence.Changes))
	for _, change := range evidence.Changes {
		applied, err := proposed.ApplyChange(change.Path.String, change.ProposedDiff)
		if err == nil {
			proposed = applied
		} else {
			appliesCleanly = false
		}
		changes = append(changes, &types.SkillEditSuggestionChange{
			ID:                   change.ID.String(),
			SuggestionID:         change.SuggestionID.String(),
			ProposedDiff:         change.ProposedDiff,
			Rationale:            change.Rationale,
			AppliesCleanly:       err == nil,
			FeedbackCount:        change.FeedbackCount,
			FeedbackSessionCount: change.FeedbackSessionCount,
			CreatedAt:            conv.FromPGTimestamptz(change.CreatedAt),
			Path:                 skilldiff.FilePath(change.Path.String),
		})
	}
	if len(changes) == 0 {
		appliesCleanly = false
	}

	return &types.SkillEditSuggestion{
		ID:                   suggestion.ID.String(),
		SkillID:              suggestion.SkillID.String(),
		SkillName:            evidence.SkillName,
		SkillDisplayName:     evidence.SkillDisplayName,
		BaseVersionID:        suggestion.BaseVersionID.String(),
		Changes:              changes,
		ProposedContent:      proposed.Manifest,
		AppliesCleanly:       appliesCleanly,
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

// BuildSkillEditSuggestionListView renders a page of suggestions. baseFiles
// holds each suggestion's base supporting files keyed by suggestion ID, for
// the suggestions that change a supporting file.
func BuildSkillEditSuggestionListView(rows []repo.ListOpenSkillEditSuggestionsRow, changes []repo.ListSkillEditSuggestionChangesRow, baseFiles map[uuid.UUID][]skilltree.File) []*types.SkillEditSuggestion {
	changesBySuggestion := make(map[uuid.UUID][]repo.ListSkillEditSuggestionChangesRow, len(rows))
	for _, change := range changes {
		changesBySuggestion[change.SuggestionID] = append(changesBySuggestion[change.SuggestionID], change)
	}

	result := make([]*types.SkillEditSuggestion, len(rows))
	for i, row := range rows {
		result[i] = BuildSkillEditSuggestionView(row.SkillEditSuggestion, SkillEditSuggestionEvidence{
			SkillName:            row.SkillName,
			SkillDisplayName:     row.SkillDisplayName,
			BaseContent:          row.BaseContent,
			BaseFiles:            baseFiles[row.SkillEditSuggestion.ID],
			Changes:              changesBySuggestion[row.SkillEditSuggestion.ID],
			FeedbackCount:        row.FeedbackCount,
			FeedbackSessionCount: row.FeedbackSessionCount,
		})
	}

	return result
}
