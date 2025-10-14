package templatefuncs

import (
	"html/template"
	"strings"
)

// FuncMap returns all template helper functions
func FuncMap() template.FuncMap {
	return template.FuncMap{
		"diff":         Diff,
		"indent":       Indent,
		"asPosixName":  AsPosixName,
		"asHTTPHeader": AsHTTPHeader,
	}
}

// Diff subtracts b from a
func Diff(a, b int) int {
	return a - b
}

// Indent adds spaces to the beginning of each line (except the first)
func Indent(spaces int, text string) string {
	if spaces <= 0 || text == "" {
		return text
	}
	indent := strings.Repeat(" ", spaces)
	lines := strings.Split(text, "\n")
	for i := 1; i < len(lines); i++ {
		if i == len(lines)-1 && lines[i] == "" {
			continue
		}
		lines[i] = indent + lines[i]
	}
	return strings.Join(lines, "\n")
}

// AsPosixName converts a string to UPPERCASE and replaces - with _
func AsPosixName(s string) string {
	return strings.ToUpper(strings.ReplaceAll(s, "-", "_"))
}

// AsHTTPHeader capitalizes each word and replaces _ with -
func AsHTTPHeader(s string) string {
	// Replace underscores with hyphens first
	s = strings.ReplaceAll(s, "_", "-")
	// Split by hyphens, capitalize each part, then rejoin
	parts := strings.Split(s, "-")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
		}
	}
	return strings.Join(parts, "-")
}
