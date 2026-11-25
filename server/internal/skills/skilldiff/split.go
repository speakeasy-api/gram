package skilldiff

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// ErrMalformed reports a multi-file diff that Split cannot divide into files.
var ErrMalformed = errors.New("malformed skill diff")

// FileDiff is the part of a multi-file diff that edits one file.
type FileDiff struct {
	// Path is the edited file relative to the skill root, taken from the
	// b/<path> header.
	Path string

	// Diff is a standalone single-file diff, headers included, that Apply
	// replays onto the file at Path.
	Diff string
}

// hunkHeader matches "@@ -l[,s] +l[,s] @@" and captures the two line counts.
var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,(\d+))? \+\d+(?:,(\d+))? @@`)

// Join concatenates per-file diffs into one multi-file diff, the inverse of
// Split.
func Join(files []FileDiff) string {
	var joined strings.Builder
	for _, file := range files {
		joined.WriteString(file.Diff)
	}
	return joined.String()
}

// Split divides a multi-file unified diff into one diff per file, in the order
// they appear. Hunk line counts decide where a hunk ends, so a removed line
// that itself starts with "--" is never mistaken for the next file header. A
// diff whose old and new names differ is rejected: skill suggestions edit
// files in place and never rename them. An empty diff yields no files.
func Split(diff string) ([]FileDiff, error) {
	if strings.TrimSpace(diff) == "" {
		return nil, nil
	}

	splitter := diffSplitter{
		files:     nil,
		current:   strings.Builder{},
		fromPath:  "",
		path:      "",
		oldLeft:   0,
		newLeft:   0,
		inHeaders: false,
	}
	for _, line := range strings.SplitAfter(diff, "\n") {
		if line == "" {
			continue
		}
		if err := splitter.line(line); err != nil {
			return nil, err
		}
	}
	if splitter.oldLeft > 0 || splitter.newLeft > 0 {
		return nil, fmt.Errorf("%w: diff ends inside a hunk", ErrMalformed)
	}
	if err := splitter.flush(); err != nil {
		return nil, err
	}

	return splitter.files, nil
}

// diffSplitter is the state Split carries between lines.
type diffSplitter struct {
	// files holds the completed per-file diffs.
	files []FileDiff

	// current accumulates the file diff being read.
	current strings.Builder

	// fromPath is the name in the current file's --- header.
	fromPath string

	// path is the name in the current file's +++ header.
	path string

	// oldLeft counts the preimage lines the current hunk still has to show.
	oldLeft int

	// newLeft counts the postimage lines the current hunk still has to show.
	newLeft int

	// inHeaders is set between a --- header and its +++ header.
	inHeaders bool
}

func (s *diffSplitter) line(line string) error {
	if s.oldLeft > 0 || s.newLeft > 0 {
		// A line beyond its side's declared count would drive that counter
		// negative and let the hunk end early without notice.
		switch line[0] {
		case ' ':
			if s.oldLeft == 0 || s.newLeft == 0 {
				return fmt.Errorf("%w: hunk has more lines than its counts", ErrMalformed)
			}
			s.oldLeft--
			s.newLeft--
		case '-':
			if s.oldLeft == 0 {
				return fmt.Errorf("%w: hunk has more removed lines than its counts", ErrMalformed)
			}
			s.oldLeft--
		case '+':
			if s.newLeft == 0 {
				return fmt.Errorf("%w: hunk has more added lines than its counts", ErrMalformed)
			}
			s.newLeft--
		case '\\':
		default:
			return fmt.Errorf("%w: hunk ends before its line counts", ErrMalformed)
		}
		s.current.WriteString(line)
		return nil
	}

	switch {
	case strings.HasPrefix(line, "--- "):
		if err := s.flush(); err != nil {
			return err
		}
		s.fromPath = headerPath(line, "--- ", fromPrefix)
		s.inHeaders = true
	case strings.HasPrefix(line, "+++ ") && s.inHeaders:
		s.path = headerPath(line, "+++ ", toPrefix)
		if s.fromPath != s.path {
			return fmt.Errorf("%w: diff renames %q to %q", ErrMalformed, s.fromPath, s.path)
		}
		s.inHeaders = false
	case strings.HasPrefix(line, "@@"):
		if s.path == "" || s.inHeaders {
			return fmt.Errorf("%w: hunk before file headers", ErrMalformed)
		}
		match := hunkHeader.FindStringSubmatch(line)
		if match == nil {
			return fmt.Errorf("%w: invalid hunk header %q", ErrMalformed, strings.TrimSpace(line))
		}
		s.oldLeft, s.newLeft = hunkCount(match[1]), hunkCount(match[2])
	case s.current.Len() == 0:
		return fmt.Errorf("%w: content before the first file header", ErrMalformed)
	}
	s.current.WriteString(line)
	return nil
}

func (s *diffSplitter) flush() error {
	if s.current.Len() == 0 {
		return nil
	}
	if s.path == "" {
		return fmt.Errorf("%w: file diff has no +++ header", ErrMalformed)
	}
	s.files = append(s.files, FileDiff{Path: s.path, Diff: s.current.String()})
	s.current.Reset()
	s.path, s.fromPath = "", ""
	return nil
}

// headerPath extracts the file name from a ---/+++ header, dropping the
// optional tab-separated timestamp and the a/ or b/ prefix.
func headerPath(line, marker, prefix string) string {
	name := strings.TrimSuffix(strings.TrimPrefix(line, marker), "\n")
	name, _, _ = strings.Cut(name, "\t")
	return strings.TrimPrefix(name, prefix)
}

// hunkCount reads an optional hunk line count, which defaults to one. The
// pattern only captures digits, so a parse failure means an absurd count and
// is read as zero, which the line accounting then reports as malformed.
func hunkCount(raw string) int {
	if raw == "" {
		return 1
	}
	count, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return count
}

// IsText reports whether content can be edited as text: valid UTF-8 with no NUL
// bytes. Suggestions never edit, and judges never read, a file that fails it.
func IsText(content []byte) bool {
	return utf8.Valid(content) && !bytes.ContainsRune(content, 0)
}
