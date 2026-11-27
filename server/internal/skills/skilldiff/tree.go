package skilldiff

import (
	"fmt"
	"slices"

	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

// Tree is the content a suggestion edits: one skill version's SKILL.md and
// its supporting files.
type Tree struct {
	// Manifest is the SKILL.md content.
	Manifest string

	// Files are the supporting files. Binary files are carried so a version
	// recorded from the tree keeps them, but no change ever edits one.
	Files []skilltree.File
}

// IsManifestPath reports whether a change path names SKILL.md. Stored changes
// written before supporting files existed carry no path, which means SKILL.md.
func IsManifestPath(path string) bool {
	return path == "" || path == skilltree.ManifestPath
}

// FilePath names the file a change path edits, resolving the empty path of a
// SKILL.md change to SKILL.md.
func FilePath(path string) string {
	if path == "" {
		return skilltree.ManifestPath
	}
	return path
}

// Text returns the text of the file at path. A file the tree does not have, or
// one that is not text, is a conflict: a change aimed at it can no longer
// apply.
func (t Tree) Text(path string) (string, error) {
	if IsManifestPath(path) {
		return t.Manifest, nil
	}
	index := t.index(path)
	if index < 0 {
		return "", fmt.Errorf("skill file %q does not exist: %w", path, ErrConflict)
	}
	if !IsText(t.Files[index].Content) {
		return "", fmt.Errorf("skill file %q is not text: %w", path, ErrConflict)
	}
	return string(t.Files[index].Content), nil
}

// WithText returns a copy of the tree with the file at path replaced by
// content. The file must already exist as text; Text reports why it cannot.
func (t Tree) WithText(path, content string) (Tree, error) {
	if _, err := t.Text(path); err != nil {
		return Tree{}, err
	}
	if IsManifestPath(path) {
		return Tree{Manifest: content, Files: t.Files}, nil
	}

	files := slices.Clone(t.Files)
	index := t.index(path)
	files[index] = skilltree.File{Path: path, Content: []byte(content), ContentType: files[index].ContentType}
	return Tree{Manifest: t.Manifest, Files: files}, nil
}

// ApplyChange replays a single-file diff onto the file at path and returns the
// resulting tree, leaving t untouched.
func (t Tree) ApplyChange(path, diff string) (Tree, error) {
	base, err := t.Text(path)
	if err != nil {
		return Tree{}, err
	}
	applied, err := Apply(base, diff)
	if err != nil {
		return Tree{}, err
	}
	return t.WithText(path, applied)
}

func (t Tree) index(path string) int {
	return slices.IndexFunc(t.Files, func(file skilltree.File) bool { return file.Path == path })
}
