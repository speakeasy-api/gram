package suggest

import (
	"errors"
	"fmt"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/skills/skilldiff"
)

// ResolvedChange is a generated change once it has been located in the skill:
// the file it edits, the diff a reviewer acts on, and the rationale and
// evidence belonging to that change alone.
type ResolvedChange struct {
	// Path is the supporting file the change edits, or empty for SKILL.md.
	Path      string
	Diff      string
	Rationale string
	Evidence  []int
}

// ResolveChanges applies each generated change to the file it names, in order,
// and records the edit each one makes. Every change is diffed against the
// content it was applied to rather than against the finished file, so a change
// carries only its own edit even when a later change touches nearby lines.
//
// A change whose text cannot be located exactly once is an error rather than a
// silent drop: the model is asked to correct it, because guessing which
// occurrence was meant is how an edit lands in the wrong place. So is a change
// to a file that does not exist, is not text, or is read-only.
func ResolveChanges(base skilldiff.Tree, changes []GeneratedChange) (skilldiff.Tree, []ResolvedChange, error) {
	tree := base
	resolved := make([]ResolvedChange, 0, len(changes))

	for i, change := range changes {
		path := change.Path
		if skilldiff.IsManifestPath(path) {
			path = ""
		}
		name := skilldiff.FilePath(path)
		if isReadOnly(path) {
			return skilldiff.Tree{}, nil, fmt.Errorf("change %d: %q is read-only; scripts can be read for context but not edited", i+1, name)
		}
		content, err := tree.Text(path)
		if errors.Is(err, skilldiff.ErrConflict) {
			return skilldiff.Tree{}, nil, fmt.Errorf("change %d: %q is not an editable text file of this skill; use SKILL.md or a path from supporting_files", i+1, name)
		}
		if err != nil {
			return skilldiff.Tree{}, nil, fmt.Errorf("change %d: read %q: %w", i+1, name, err)
		}

		switch strings.Count(content, change.Find) {
		case 1:
		case 0:
			return skilldiff.Tree{}, nil, fmt.Errorf("change %d: the text in \"find\" does not appear in %s; copy it verbatim from that file", i+1, name)
		default:
			return skilldiff.Tree{}, nil, fmt.Errorf("change %d: the text in \"find\" appears more than once in %s; extend it until it is unique", i+1, name)
		}

		updated := strings.Replace(content, change.Find, change.Replace, 1)
		if updated == content {
			continue
		}

		diff, err := skilldiff.UnifiedFile(name, content, updated)
		if err != nil {
			return skilldiff.Tree{}, nil, fmt.Errorf("render proposed change %d: %w", i+1, err)
		}
		tree, err = tree.WithText(path, updated)
		if err != nil {
			return skilldiff.Tree{}, nil, fmt.Errorf("apply proposed change %d: %w", i+1, err)
		}
		resolved = append(resolved, ResolvedChange{
			Path:      path,
			Diff:      diff,
			Rationale: change.Rationale,
			Evidence:  change.Evidence,
		})
	}

	return tree, resolved, nil
}

// isReadOnly reports whether generated suggestions may not edit the file at
// path. Scripts run on the agent's machine, so a suggested change to one is
// code a reviewer would have to audit rather than guidance they can read; the
// model sees them as context only.
func isReadOnly(path string) bool {
	return strings.HasPrefix(path, "scripts/")
}
