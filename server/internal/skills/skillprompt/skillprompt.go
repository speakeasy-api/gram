// Package skillprompt selects which of a skill's supporting files an LLM judge
// reads alongside SKILL.md.
//
// SKILL.md is always sent whole and first by the caller. Supporting files are
// added after it in priority order, references/ first and scripts/ last, while
// they fit a byte budget. A file that does not fit is skipped and smaller files
// after it can still be added. Files are included whole or not at all: a
// truncated reference reads as complete guidance and would mislead the judge.
// Binary files and files that do not fit are listed by path so the judge knows
// they exist without assuming what they say.
package skillprompt

import (
	"slices"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/skills/skilldiff"
	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

// OmitReason says why a supporting file is listed without its content.
type OmitReason string

const (
	// OmitBinary marks a file that is not UTF-8 text.
	OmitBinary OmitReason = "binary"

	// OmitBudget marks a text file that did not fit the byte budget.
	OmitBudget OmitReason = "over_budget"
)

// File is a supporting file whose content the judge reads.
type File struct {
	// Path is relative to the skill root, for example references/api.md.
	Path string `json:"path"`

	// Content is the whole file.
	Content string `json:"content"`
}

// OmittedFile is a supporting file the judge is told about but not shown.
type OmittedFile struct {
	// Path is relative to the skill root.
	Path string `json:"path"`

	// Reason says why the content is missing.
	Reason OmitReason `json:"reason"`
}

// Selection is the outcome of fitting supporting files into a budget.
type Selection struct {
	// Included are the files sent with content, in priority order.
	Included []File

	// Omitted are the files listed by path only, in the same order.
	Omitted []OmittedFile
}

// Select fits files into budgetBytes of content. Files are visited in
// priority order and each one that still fits is included, so one large
// reference does not crowd out the small ones after it.
func Select(files []skilltree.File, budgetBytes int) Selection {
	ordered := slices.Clone(files)
	slices.SortFunc(ordered, func(a, b skilltree.File) int {
		if byRank := rank(a.Path) - rank(b.Path); byRank != 0 {
			return byRank
		}
		return strings.Compare(a.Path, b.Path)
	})

	selection := Selection{Included: nil, Omitted: nil}
	remaining := budgetBytes
	for _, file := range ordered {
		switch {
		case !skilldiff.IsText(file.Content):
			selection.Omitted = append(selection.Omitted, OmittedFile{Path: file.Path, Reason: OmitBinary})
		case len(file.Content) > remaining:
			selection.Omitted = append(selection.Omitted, OmittedFile{Path: file.Path, Reason: OmitBudget})
		default:
			remaining -= len(file.Content)
			selection.Included = append(selection.Included, File{Path: file.Path, Content: string(file.Content)})
		}
	}

	return selection
}

// rank orders supporting files by how likely they are to carry instructions:
// references hold guidance split out of SKILL.md, scripts are code the skill
// runs, and everything else sits between.
func rank(path string) int {
	switch {
	case strings.HasPrefix(path, "references/"):
		return 0
	case strings.HasPrefix(path, "scripts/"):
		return 2
	default:
		return 1
	}
}
