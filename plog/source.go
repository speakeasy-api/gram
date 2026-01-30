package plog

import (
	"go/build"
	"os"
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

// goModCache returns the Go module cache directory.
// It checks GOMODCACHE first, then falls back to GOPATH/pkg/mod.
func goModCache() string {
	if modCache := os.Getenv("GOMODCACHE"); modCache != "" {
		return modCache
	}
	gopath := build.Default.GOPATH
	if gopath == "" {
		return ""
	}
	// GOPATH can contain multiple paths; use the first one
	if idx := strings.Index(gopath, string(filepath.ListSeparator)); idx != -1 {
		gopath = gopath[:idx]
	}
	return filepath.Join(gopath, "pkg", "mod")
}

// formatModulePath checks if the path is in the Go module cache and returns
// a formatted version like "go.temporal.io/sdk@v1.39.0:log/with_logger.go".
// Returns empty string if not a module cache path.
func formatModulePath(path, modCache string) string {
	if modCache == "" || !strings.HasPrefix(path, modCache) {
		return ""
	}

	// Extract the part after the module cache directory
	modPart := strings.TrimPrefix(path, modCache)
	modPart = strings.TrimPrefix(modPart, string(filepath.Separator))

	// Find the @ that marks the version
	atIdx := strings.Index(modPart, "@")
	if atIdx == -1 {
		return ""
	}

	// Find the first / after the @ (separates version from path in module)
	slashAfterVersion := strings.Index(modPart[atIdx:], string(filepath.Separator))
	if slashAfterVersion == -1 {
		// No path after version, just return module@version
		return modPart
	}

	// Split into module@version and relative path
	moduleVersion := modPart[:atIdx+slashAfterVersion]
	relativePath := modPart[atIdx+slashAfterVersion+1:]

	return moduleVersion + ":" + relativePath
}

// RelativePath returns the source file path relative to the working directory.
// For paths in the Go module cache, it formats them as "module@version:path/in/module".
func (s *Source) RelativePath(workingDir string) string {
	if s == nil || s.File == "" {
		return ""
	}

	// Check if this is a Go module cache path and format it elegantly
	if modPath := formatModulePath(s.File, goModCache()); modPath != "" {
		return modPath
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
