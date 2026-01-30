package plog

import (
	"path/filepath"
	"strconv"
	"strings"
)

// Source represents a source code location.
type Source struct {
	File     string
	Line     int
	Function string
}

// parseSource parses a source location from a JSON value.
// It handles both string format ("file:line") and object format ({"file":"...", "line":42}).
func parseSource(v any) *Source {
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case string:
		return parseSourceString(val)
	case map[string]any:
		return parseSourceObject(val)
	default:
		return nil
	}
}

// parseSourceString parses "file:line" or "file:line:function" format.
func parseSourceString(s string) *Source {
	if s == "" {
		return nil
	}

	src := &Source{
		File:     "",
		Line:     0,
		Function: "",
	}

	// Try to parse file:line or file:line:function
	// Handle Windows paths (e.g., C:\path\file.go:42)
	lastColon := strings.LastIndex(s, ":")
	if lastColon == -1 {
		src.File = s
		return src
	}

	// Check if the part after the last colon is a number (line) or function name
	after := s[lastColon+1:]
	if line, err := strconv.Atoi(after); err == nil {
		src.File = s[:lastColon]
		src.Line = line
	} else {
		// Might be file:line:function
		secondLastColon := strings.LastIndex(s[:lastColon], ":")
		if secondLastColon != -1 {
			if line, err := strconv.Atoi(s[secondLastColon+1 : lastColon]); err == nil {
				src.File = s[:secondLastColon]
				src.Line = line
				src.Function = after
			} else {
				// Can't parse, treat entire string as file
				src.File = s
			}
		} else {
			// No second colon, treat as file
			src.File = s
		}
	}

	return src
}

// parseSourceObject parses {"file":"...", "line":42, "function":"..."} format.
func parseSourceObject(m map[string]any) *Source {
	src := &Source{
		File:     "",
		Line:     0,
		Function: "",
	}

	if file, ok := m["file"].(string); ok {
		src.File = file
	}
	if line, ok := m["line"].(float64); ok {
		src.Line = int(line)
	}
	if fn, ok := m["function"].(string); ok {
		src.Function = fn
	}

	if src.File == "" && src.Line == 0 && src.Function == "" {
		return nil
	}
	return src
}

// RelativePath returns the source file path relative to the working directory.
func (s *Source) RelativePath(workingDir string) string {
	if s == nil || s.File == "" {
		return ""
	}
	if workingDir == "" {
		return s.File
	}
	if rel, err := filepath.Rel(workingDir, s.File); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return s.File
}

// String returns the source location as a string.
func (s *Source) String() string {
	if s == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(s.File)
	if s.Line > 0 {
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(s.Line))
	}
	return b.String()
}
