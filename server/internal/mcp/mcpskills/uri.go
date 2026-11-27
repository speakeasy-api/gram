package mcpskills

import (
	"net/url"
	"strings"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/skills"
	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

const (
	// skillURIPrefix is the scheme and authority separator of every served
	// skill URI. SEP-2640 recommends the skill:// scheme; Speakeasy puts the
	// project slug in the authority and the skill name in the path.
	skillURIPrefix = "skill://"

	// skillFileName is the manifest every served skill has. Its URI is the
	// entry's identity and the first of its resources.
	skillFileName = skilltree.ManifestPath
)

// SkillURI returns the resource URI of a served skill's SKILL.md, which is
// also the entry's identity: skill://<project-slug>/<name>/SKILL.md. The
// project slug keeps two projects' skills of the same name apart, and SEP-2640
// only requires the final path segment to equal the frontmatter name.
func SkillURI(projectSlug, name string) string {
	return skillURIPrefix + projectSlug + "/" + name + "/" + skillFileName
}

// SkillFileURI returns the resource URI of one supporting file of a served
// skill: skill://<project-slug>/<name>/<path>, with each path segment
// percent-encoded where the URI syntax requires it. Typical paths such as
// references/REFERENCE.md are written verbatim.
func SkillFileURI(projectSlug, name, filePath string) string {
	segments := strings.Split(filePath, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return skillURIPrefix + projectSlug + "/" + name + "/" + strings.Join(segments, "/")
}

// IsSkillURI reports whether a URI uses the skill:// scheme, whether or not
// it names a skill this server could serve. A surface that declares the
// Skills extension answers every such URI it does not serve as an unknown
// skill rather than as an unknown resource.
func IsSkillURI(uri string) bool {
	return strings.HasPrefix(uri, skillURIPrefix)
}

// ParseSkillURI recovers the project slug and skill name from a URI of the
// exact form skill://<project-slug>/<name>/SKILL.md, the URI that identifies a
// skill entry. It reports false for any other shape, including a bare
// skill://<name>/SKILL.md and supporting-file URIs, and for a slug or name
// that does not satisfy its naming rules, so a caller can treat false as "not
// a skill this server could serve" without consulting storage.
func ParseSkillURI(uri string) (projectSlug, name string, ok bool) {
	projectSlug, name, filePath, ok := ParseSkillResourceURI(uri)
	if !ok || filePath != "" {
		return "", "", false
	}
	return projectSlug, name, true
}

// ParseSkillResourceURI recovers the project slug, skill name and file path
// from the URI of any file of a served skill. filePath is empty for the
// skill's SKILL.md and otherwise the supporting file's path relative to the
// skill root. It reports false for a URI that is not exactly the one
// SkillURI or SkillFileURI would build, including one whose path breaks the
// supporting-file path rules, so every file has exactly one URI.
func ParseSkillResourceURI(uri string) (projectSlug, name, filePath string, ok bool) {
	rest, ok := strings.CutPrefix(uri, skillURIPrefix)
	if !ok {
		return "", "", "", false
	}
	projectSlug, rest, ok = strings.Cut(rest, "/")
	if !ok || !constants.SlugPatternRE.MatchString(projectSlug) {
		return "", "", "", false
	}
	name, rest, ok = strings.Cut(rest, "/")
	if !ok || !skills.ValidSpecName(name) {
		return "", "", "", false
	}
	if rest == skillFileName {
		return projectSlug, name, "", true
	}

	segments := strings.Split(rest, "/")
	for i, segment := range segments {
		decoded, err := url.PathUnescape(segment)
		if err != nil {
			return "", "", "", false
		}
		segments[i] = decoded
	}
	filePath = strings.Join(segments, "/")
	if skilltree.ValidatePath(filePath) != nil || SkillFileURI(projectSlug, name, filePath) != uri {
		return "", "", "", false
	}
	return projectSlug, name, filePath, true
}
