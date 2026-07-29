// Package skilldiff represents a proposed SKILL.md edit as a unified diff so a
// suggestion outlives the exact version it was generated against.
package skilldiff

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/gitleaks/go-gitdiff/gitdiff"
	"github.com/pmezard/go-difflib/difflib"
)

// ErrConflict reports that a diff no longer lines up with the content it is
// replayed onto, which retires the suggestion carrying it.
var ErrConflict = errors.New("skill diff conflicts with the current content")

const (
	fromFile     = "a/SKILL.md"
	toFile       = "b/SKILL.md"
	contextLines = 3
)

// Unified renders the edit that turns base into proposed. Both sides are
// normalized to end in a newline, and an unchanged edit renders as the empty
// diff, which Apply replays as the identity.
func Unified(base, proposed string) (string, error) {
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        splitLines(base),
		FromFile: fromFile,
		FromDate: "",
		B:        splitLines(proposed),
		ToFile:   toFile,
		ToDate:   "",
		Eol:      "",
		Context:  contextLines,
	})
	if err != nil {
		return "", fmt.Errorf("render skill diff: %w", err)
	}

	return diff, nil
}

// Hunks splits diff into one standalone diff per hunk, in the order they appear,
// so a reviewer can accept part of a suggestion without taking the rest. Each
// result carries the file headers and replays through Apply on its own; Apply
// relocates hunks by their context, so the pieces do not have to be applied in
// order. An empty diff yields no hunks.
func Hunks(diff string) []string {
	if strings.TrimSpace(diff) == "" {
		return nil
	}

	var header strings.Builder
	var hunks []string
	var current *strings.Builder
	for _, line := range strings.SplitAfter(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "@@"):
			if current != nil {
				hunks = append(hunks, current.String())
			}
			current = &strings.Builder{}
			current.WriteString(header.String())
			current.WriteString(line)
		case current != nil:
			current.WriteString(line)
		default:
			header.WriteString(line)
		}
	}
	if current != nil {
		hunks = append(hunks, current.String())
	}

	return hunks
}

// Apply replays diff onto base. Hunks are relocated to wherever their recorded
// context still appears, so an edit elsewhere in the skill shifts a suggestion
// rather than invalidating it. A hunk whose context is gone is a conflict, not
// a silent partial edit.
func Apply(base, diff string) (string, error) {
	if strings.TrimSpace(diff) == "" {
		return base, nil
	}

	// The parser reports malformed input by yielding no files rather than an
	// error, so an unusable diff surfaces below as a conflict.
	files, err := gitdiff.Parse(strings.NewReader(diff))
	if err != nil {
		return "", fmt.Errorf("parse skill diff: %w: %w", ErrConflict, err)
	}

	source := strings.Join(splitLines(base), "")
	target := splitLines(source)

	var applied bytes.Buffer
	var count int
	for file := range files {
		count++
		if count > 1 {
			return "", fmt.Errorf("skill diff covers more than one file: %w", ErrConflict)
		}
		for i, fragment := range file.TextFragments {
			if err := relocate(fragment, target); err != nil {
				return "", fmt.Errorf("locate skill diff hunk %d: %w", i+1, err)
			}
		}
		if err := gitdiff.Apply(&applied, strings.NewReader(source), file); err != nil {
			return "", fmt.Errorf("apply skill diff: %w: %w", ErrConflict, err)
		}
	}
	if count == 0 {
		return "", fmt.Errorf("skill diff contains no file fragments: %w", ErrConflict)
	}

	return applied.String(), nil
}

// relocate repoints a hunk at the occurrence of its preimage nearest to the
// position it was recorded at, which absorbs line shifts introduced above it.
func relocate(fragment *gitdiff.TextFragment, target []string) error {
	if fragment.OldLines == 0 {
		return nil
	}

	preimage := make([]string, 0, fragment.OldLines)
	for _, line := range fragment.Lines {
		if line.Old() {
			preimage = append(preimage, line.Line)
		}
	}

	recorded := int(fragment.OldPosition - 1)
	found := -1
	for start := 0; start+len(preimage) <= len(target); start++ {
		if !matchesAt(target, start, preimage) {
			continue
		}
		if found == -1 || distance(start, recorded) < distance(found, recorded) {
			found = start
		}
	}
	if found == -1 {
		return ErrConflict
	}

	fragment.OldPosition = int64(found + 1)

	return nil
}

func matchesAt(target []string, start int, preimage []string) bool {
	for i, line := range preimage {
		if target[start+i] != line {
			return false
		}
	}

	return true
}

func distance(a, b int) int {
	if a < b {
		return b - a
	}

	return a - b
}

// splitLines keeps the newline on every line it returns and normalizes content
// that does not end in one. The difflib helper of the same name appends a
// phantom trailing line, which would then be diffed as real content.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}

	lines := strings.SplitAfter(content, "\n")
	if last := len(lines) - 1; lines[last] == "" {
		lines = lines[:last]
	} else {
		lines[last] += "\n"
	}

	return lines
}
