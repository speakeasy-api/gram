package skills

import (
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
)

// Resource kinds mirror the optional directories the Agent Skills
// specification reserves inside a skill directory. Anything else a manifest
// points at is reported as skillResourceKindOther.
const (
	skillResourceKindScript    = "script"
	skillResourceKindReference = "reference"
	skillResourceKindAsset     = "asset"
	skillResourceKindOther     = "other"
)

// maxSkillResourceReferences bounds how many supporting files a single
// manifest can report, so a pathological body cannot inflate API responses.
const maxSkillResourceReferences = 200

// skillResourceReference is a supporting file that a SKILL.md body points at,
// expressed as a path relative to the skill directory root.
//
// Gram stores a skill as a single SKILL.md, so these files are declared but
// never ingested or distributed. Reporting them lets callers see where a
// manifest depends on content the platform does not hold.
type skillResourceReference struct {
	Path string
	Kind string
}

var (
	// Markdown inline link and image destinations: the `dest` in `](dest)`,
	// in either the angle-bracket or bare form. A bare destination ends at
	// whitespace, so an optional link title is never captured.
	markdownLinkDestinationPattern = regexp.MustCompile(`\]\(\s*(?:<([^<>\n]*)>|([^\s()]+))`)

	// Markdown link reference definitions: `[label]: dest`.
	markdownLinkDefinitionPattern = regexp.MustCompile(`(?m)^[ \t]{0,3}\[[^\]\n]+\]:[ \t]*(?:<([^<>\n]*)>|(\S+))`)

	// Paths under a spec-reserved directory, wherever they appear: prose,
	// inline code spans, or fenced code blocks such as `python scripts/run.py`.
	reservedDirectoryPathPattern = regexp.MustCompile(`(?:^|[^A-Za-z0-9_./~+-])((?:scripts|references|assets)/[A-Za-z0-9_./~+%-]+)`)

	// A leading URI scheme, which marks a destination as external rather than
	// a file inside the skill directory.
	uriSchemePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.\-]*:`)
)

// parseSkillResourceReferences extracts the supporting files a SKILL.md body
// declares. Destinations are resolved relative to the skill root and returned
// sorted by path with duplicates collapsed.
func parseSkillResourceReferences(body string) []skillResourceReference {
	references := make([]skillResourceReference, 0)
	seen := make(map[string]struct{})

	add := func(candidate string) {
		if len(references) >= maxSkillResourceReferences {
			return
		}
		resourcePath, ok := normalizeSkillResourcePath(candidate)
		if !ok {
			return
		}
		if _, duplicate := seen[resourcePath]; duplicate {
			return
		}
		seen[resourcePath] = struct{}{}
		references = append(references, skillResourceReference{
			Path: resourcePath,
			Kind: skillResourceKind(resourcePath),
		})
	}

	// Markdown destinations are authoritative, so they are collected first and
	// then blanked out. The reserved-directory scan that follows only sees
	// prose and code, and cannot re-report part of a destination it already
	// read as a whole.
	residual := []byte(body)
	spans := make([][]int, 0)
	for _, pattern := range []*regexp.Regexp{markdownLinkDestinationPattern, markdownLinkDefinitionPattern} {
		for _, match := range pattern.FindAllSubmatchIndex(residual, -1) {
			add(submatchString(residual, match, 1) + submatchString(residual, match, 2))
			spans = append(spans, match)
		}
	}
	for _, span := range spans {
		for i := span[0]; i < span[1]; i++ {
			if residual[i] != '\n' {
				residual[i] = ' '
			}
		}
	}

	for _, match := range reservedDirectoryPathPattern.FindAllSubmatch(residual, -1) {
		add(string(match[1]))
	}

	slices.SortFunc(references, func(a, b skillResourceReference) int {
		return strings.Compare(a.Path, b.Path)
	})
	return references
}

func submatchString(source []byte, match []int, group int) string {
	start, end := match[2*group], match[2*group+1]
	if start < 0 {
		return ""
	}
	return string(source[start:end])
}

// normalizeSkillResourcePath reduces a raw markdown destination to a path
// relative to the skill root, reporting false when the destination does not
// name a file inside the skill directory.
func normalizeSkillResourcePath(candidate string) (string, bool) {
	candidate = strings.TrimSpace(candidate)
	candidate = strings.TrimRight(candidate, `.,;:!?)]}>"'`)
	if candidate == "" {
		return "", false
	}

	// External destinations, in-page anchors, and paths anchored outside the
	// skill directory are not supporting files.
	if uriSchemePattern.MatchString(candidate) || strings.HasPrefix(candidate, "#") || strings.HasPrefix(candidate, "/") || strings.HasPrefix(candidate, "~") {
		return "", false
	}

	candidate, _, _ = strings.Cut(candidate, "#")
	candidate, _, _ = strings.Cut(candidate, "?")
	candidate = unescapeMarkdownPunctuation(candidate)
	if decoded, err := url.PathUnescape(candidate); err == nil {
		candidate = decoded
	}
	// Spaces survive because an angle-bracket destination may name a file that
	// contains them; control characters never legitimately appear in a path.
	if candidate == "" || strings.ContainsAny(candidate, "\t\n\r") {
		return "", false
	}

	cleaned := path.Clean(candidate)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "/") {
		return "", false
	}

	// Only report destinations that plausibly name a file: anything under a
	// spec-reserved directory, or a path whose final segment has an extension.
	if reservedSkillResourceRoot(cleaned) == "" && !strings.Contains(path.Base(cleaned), ".") {
		return "", false
	}

	return cleaned, true
}

// unescapeMarkdownPunctuation resolves the backslash escapes Markdown allows
// before ASCII punctuation, so an escaped space or underscore in a link
// destination resolves to the character the author meant.
func unescapeMarkdownPunctuation(candidate string) string {
	if !strings.Contains(candidate, `\`) {
		return candidate
	}

	var unescaped strings.Builder
	unescaped.Grow(len(candidate))
	for i := 0; i < len(candidate); i++ {
		if candidate[i] == '\\' && i+1 < len(candidate) && isASCIIPunctuation(candidate[i+1]) {
			i++
		}
		unescaped.WriteByte(candidate[i])
	}
	return unescaped.String()
}

func isASCIIPunctuation(character byte) bool {
	switch {
	case character >= '!' && character <= '/':
		return true
	case character >= ':' && character <= '@':
		return true
	case character >= '[' && character <= '`':
		return true
	case character >= '{' && character <= '~':
		return true
	default:
		return false
	}
}

func skillResourceKind(resourcePath string) string {
	switch reservedSkillResourceRoot(resourcePath) {
	case "scripts":
		return skillResourceKindScript
	case "references":
		return skillResourceKindReference
	case "assets":
		return skillResourceKindAsset
	default:
		return skillResourceKindOther
	}
}

// reservedSkillResourceRoot reports the spec-reserved directory a path sits
// under, or the empty string when it sits elsewhere in the skill directory.
func reservedSkillResourceRoot(resourcePath string) string {
	root, rest, nested := strings.Cut(resourcePath, "/")
	if !nested || rest == "" {
		return ""
	}
	switch root {
	case "scripts", "references", "assets":
		return root
	default:
		return ""
	}
}
