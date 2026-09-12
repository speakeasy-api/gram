package aitargets

import "strings"

// MatchConfigDir reports whether a directory path satisfies a config-dir
// signature. A `*` matches within one path segment and never crosses a `/`.
//
// The device agent does the real matching; this is the reference for it.
// Callers pass an expanded, cleaned path.
func MatchConfigDir(signature, path string) bool {
	sigSegments := strings.Split(signature, "/")
	pathSegments := strings.Split(path, "/")
	if len(sigSegments) != len(pathSegments) {
		return false
	}
	for i, segment := range sigSegments {
		if !matchSegment(segment, pathSegments[i]) {
			return false
		}
	}
	return true
}

// matchSegment globs within one segment. Not path.Match, which treats `\` as
// an escape everywhere but Windows — a signature may not contain one at all.
func matchSegment(pattern, segment string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == segment
	}

	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(segment, parts[0]) {
		return false
	}
	rest := segment[len(parts[0]):]
	last := parts[len(parts)-1]
	if !strings.HasSuffix(rest, last) {
		return false
	}
	rest = rest[:len(rest)-len(last)]

	// Interior literals must appear in order, each consuming what it matched.
	for _, part := range parts[1 : len(parts)-1] {
		if part == "" {
			continue
		}
		index := strings.Index(rest, part)
		if index < 0 {
			return false
		}
		rest = rest[index+len(part):]
	}
	return true
}
