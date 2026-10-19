package mcpskills

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// digestPrefix is the algorithm label SEP-2640 fixes for resource digests:
// "sha256:" followed by 64 lowercase hexadecimal characters.
const digestPrefix = "sha256:"

// Skill is the SEP-2640 skill entry returned by skills/list and skills/get.
type Skill struct {
	// URI is the resource URI of the skill's SKILL.md, readable through
	// resources/read.
	URI string `json:"uri"`

	// Frontmatter is the SKILL.md YAML frontmatter rendered verbatim as a JSON
	// object. It stays a map because the extension passes every field the
	// author wrote through unchanged; name and description are always present
	// because the Agent Skills specification requires them.
	Frontmatter map[string]any `json:"frontmatter"`

	// Resources enumerates every file of the skill. Speakeasy serves
	// single-file skills, so it holds exactly the SKILL.md entry.
	Resources []SkillResource `json:"resources"`
}

// SkillResource is one file of a skill with the digest and size of its
// content, as SEP-2640 defines it.
type SkillResource struct {
	// URI is the resource URI of the file.
	URI string `json:"uri"`

	// Digest is the SHA-256 of the file's raw bytes as "sha256:<hex>".
	Digest string `json:"digest"`

	// Size is the byte length of the raw content the digest covers.
	Size int `json:"size"`
}

// Entry is one served skill resolved for a caller: the version Speakeasy
// serves and everything a surface needs to answer the extension's methods
// and record a read.
type Entry struct {
	// SkillID is the skill the entry serves.
	SkillID uuid.UUID

	// VersionID is the resolved skill version whose content is served.
	VersionID uuid.UUID

	// PluginID is the plugin whose distribution won the resolution.
	PluginID uuid.UUID

	// Name is the skill's normalized name, the final path segment of the URI
	// and the frontmatter name.
	Name string

	// Content is the SKILL.md as stored, the exact bytes resources/read
	// returns and the digest covers.
	Content string

	// RawSHA256 is the lowercase hex SHA-256 of Content as recorded on the
	// version.
	RawSHA256 string

	// Frontmatter is the parsed YAML frontmatter of Content.
	Frontmatter map[string]any

	// Pinned reports whether the winning distribution pins this version
	// rather than tracking the skill's latest valid version.
	Pinned bool
}

// URI returns the entry's resource URI, skill://<name>/SKILL.md.
func (e Entry) URI() string {
	return SkillURI(e.Name)
}

// CheckFrontmatterName reports an error when the entry's frontmatter name is
// not exactly its served name. SEP-2640 requires the final segment of the
// entry's URI to equal frontmatter.name, and a conforming host refuses to
// load an entry where they differ, so such an entry must not be served.
func (e Entry) CheckFrontmatterName() error {
	name, ok := e.Frontmatter["name"].(string)
	if !ok {
		return fmt.Errorf("frontmatter name is %T, not a string", e.Frontmatter["name"])
	}
	if name != e.Name {
		return fmt.Errorf("frontmatter name %q does not equal served name %q", name, e.Name)
	}
	return nil
}

// Digest returns the SEP-2640 digest of the served content, "sha256:<hex>".
func (e Entry) Digest() string {
	return Digest(e.RawSHA256)
}

// Digest renders a raw SHA-256 hex string in the SEP-2640 "sha256:<hex>" form
// agents see when they load a skill.
func Digest(rawSHA256 string) string {
	return digestPrefix + strings.ToLower(rawSHA256)
}

// Size returns the byte length of the served content.
func (e Entry) Size() int {
	return len(e.Content)
}

// Wire renders the entry in the shape skills/list and skills/get return.
func (e Entry) Wire() Skill {
	uri := e.URI()
	return Skill{
		URI:         uri,
		Frontmatter: e.Frontmatter,
		Resources: []SkillResource{{
			URI:    uri,
			Digest: e.Digest(),
			Size:   e.Size(),
		}},
	}
}
