package mcpskills

import (
	"strings"

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
// also the entry's identity: skill://<name>/SKILL.md.
func SkillURI(name string) string {
	return skillURIPrefix + name + "/" + skillFileName
}

// IsSkillURI reports whether a URI uses the skill:// scheme, whether or not
// it names a skill this server could serve. A surface that declares the
// Skills extension answers every such URI it does not serve as an unknown
// skill rather than as an unknown resource.
func IsSkillURI(uri string) bool {
	return strings.HasPrefix(uri, skillURIPrefix)
}

// ParseSkillURI recovers the skill name from a URI of the exact form
// skill://<name>/SKILL.md. It reports false for any other shape, including
// nested skill paths and supporting files, and for a name that does not
// satisfy the Agent Skills naming rules, so a caller can treat false as "not
// a skill this server could serve" without consulting storage.
func ParseSkillURI(uri string) (string, bool) {
	rest, ok := strings.CutPrefix(uri, skillURIPrefix)
	if !ok {
		return "", false
	}
	name, ok := strings.CutSuffix(rest, "/"+skillFileName)
	if !ok || !skills.ValidSpecName(name) {
		return "", false
	}
	return name, true
}
