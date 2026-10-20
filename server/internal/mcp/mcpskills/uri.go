package mcpskills

import (
	"strings"

	"github.com/speakeasy-api/gram/server/internal/constants"
	"github.com/speakeasy-api/gram/server/internal/skills"
)

const (
	// skillURIPrefix is the scheme and authority separator of every served
	// skill URI. SEP-2640 recommends the skill:// scheme and places the skill
	// name in the authority component.
	skillURIPrefix = "skill://"

	// skillFileName is the one file a served skill consists of. Speakeasy
	// serves single-file skills, so it is both the entry's URI and its only
	// resource.
	skillFileName = "SKILL.md"
)

// SkillURI returns the resource URI of a served skill's SKILL.md, which is
// also the entry's identity: skill://<project-slug>/<name>/SKILL.md. The
// project slug keeps two projects' skills of the same name apart, and SEP-2640
// only requires the final path segment to equal the frontmatter name.
func SkillURI(projectSlug, name string) string {
	return skillURIPrefix + projectSlug + "/" + name + "/" + skillFileName
}

// ParseSkillURI recovers the project slug and skill name from a URI of the
// exact form skill://<project-slug>/<name>/SKILL.md. It reports false for any
// other shape, including a bare skill://<name>/SKILL.md, deeper paths and
// supporting files, and for a slug or name that does not satisfy its naming
// rules, so a caller can treat false as "not a skill this server could serve"
// without consulting storage.
func ParseSkillURI(uri string) (projectSlug, name string, ok bool) {
	rest, ok := strings.CutPrefix(uri, skillURIPrefix)
	if !ok {
		return "", "", false
	}
	path, ok := strings.CutSuffix(rest, "/"+skillFileName)
	if !ok {
		return "", "", false
	}
	projectSlug, name, ok = strings.Cut(path, "/")
	if !ok || !constants.SlugPatternRE.MatchString(projectSlug) || !skills.ValidSpecName(name) {
		return "", "", false
	}
	return projectSlug, name, true
}
