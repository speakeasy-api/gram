package suggest

import (
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/skills/skilldiff"
)

// resolvedChange is a generated change once it has been located in the manifest:
// the diff a reviewer acts on, with the rationale and evidence belonging to that
// change alone.
type resolvedChange struct {
	Diff      string
	Rationale string
	Evidence  []int
}

// resolveChanges applies each generated change to the manifest in order and
// records the edit each one makes. Every change is diffed against the content
// it was applied to rather than against the finished manifest, so a change
// carries only its own edit even when a later change touches nearby lines.
//
// A change whose text cannot be located exactly once is an error rather than a
// silent drop: the model is asked to correct it, because guessing which
// occurrence was meant is how an edit lands in the wrong place.
func resolveChanges(baseContent string, changes []GeneratedChange) (string, []resolvedChange, error) {
	content := baseContent
	resolved := make([]resolvedChange, 0, len(changes))

	for i, change := range changes {
		switch strings.Count(content, change.Find) {
		case 1:
		case 0:
			return "", nil, fmt.Errorf("change %d: the text in \"find\" does not appear in the skill; copy it verbatim from the current SKILL.md", i+1)
		default:
			return "", nil, fmt.Errorf("change %d: the text in \"find\" appears more than once in the skill; extend it until it is unique", i+1)
		}

		updated := strings.Replace(content, change.Find, change.Replace, 1)
		if updated == content {
			continue
		}

		diff, err := skilldiff.Unified(content, updated)
		if err != nil {
			return "", nil, fmt.Errorf("render proposed change %d: %w", i+1, err)
		}
		content = updated
		resolved = append(resolved, resolvedChange{
			Diff:      diff,
			Rationale: change.Rationale,
			Evidence:  change.Evidence,
		})
	}

	return content, resolved, nil
}
