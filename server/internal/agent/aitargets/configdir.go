package aitargets

import (
	gopath "path"
	"strings"
)

// MatchConfigDir reports whether a directory path satisfies a config-dir
// signature. A `*` matches within one path segment and never crosses a `/`.
//
// The device agent does the real matching; this is the reference for it.
// Callers pass an expanded, cleaned path.
func MatchConfigDir(signature, path string) bool {
	sigSegments := strings.Split(cleanSignature(signature), "/")
	pathSegments := strings.Split(slashPath(path), "/")
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

// cleanSignature puts a signature into the shape a cleaned path already has.
// Nothing trims a config dir on the way in — a signature is stored exactly as
// the organization wrote it — so `~/.codex/` arrives with its trailing `/`.
// Split raw, that trailing separator becomes an empty last segment, which no
// directory can ever produce, so the signature would silently never match
// anything. Cleaning fixes redundant and trailing separators alike, and leaves
// `*` alone because it means nothing to path cleaning.
func cleanSignature(signature string) string {
	if signature == "" {
		// Clean would turn this into ".", which would then match the relative
		// current directory. An empty signature matches nothing instead.
		return ""
	}
	return gopath.Clean(signature)
}

// slashPath canonicalizes a path onto `/`, the separator signatures are always
// authored with. A Windows agent hands over a native path, whose separator is
// `\`: left as it is, the whole path stays one segment and a `*` swallows
// directory boundaries, which is the one thing single-segment globbing exists
// to prevent.
//
// The rewrite is confined to paths that are rooted the Windows way, because
// `\` is a legal character in a Unix filename. Rewriting every backslash would
// split a directory literally named `a\b` in two, so the wildcard signature
// that should match it stops matching, and the unrelated path `a/b` starts
// comparing equal to it.
//
// The converted path is then cleaned, for the same reason cleanSignature
// cleans: a UNC path converts to a doubled leading separator (`\\host\share`
// becomes `//host/share`), which splits into one more segment than the
// identically cleaned signature, so the counts never line up and no UNC
// directory could match. Both sides go through the same cleaning.
func slashPath(path string) string {
	if !hasWindowsRoot(path) {
		return path
	}
	return gopath.Clean(strings.ReplaceAll(path, `\`, "/"))
}

// hasWindowsRoot reports whether a path carries a Windows root: a drive letter
// (`C:\Users\...`) or a UNC share (`\\host\share\...`). The root is what tells
// the platforms apart, and it is always present here — a config dir reaches
// matching expanded, so it is absolute — whereas a Unix path may contain a
// backslash but can never start with one of these.
func hasWindowsRoot(path string) bool {
	if strings.HasPrefix(path, `\\`) {
		return true
	}
	if len(path) < 3 || path[1] != ':' {
		return false
	}
	drive := path[0]
	isDriveLetter := (drive >= 'a' && drive <= 'z') || (drive >= 'A' && drive <= 'Z')
	return isDriveLetter && (path[2] == '\\' || path[2] == '/')
}

// matchSegment globs within one segment. Not path.Match, which treats `\` as
// an escape everywhere but Windows — a signature may not contain one at all.
func matchSegment(pattern, segment string) bool {
	if strings.Contains(pattern, `\`) {
		return false
	}
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
