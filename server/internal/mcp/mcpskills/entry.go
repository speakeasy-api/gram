package mcpskills

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/skills/skilltree"
)

// digestPrefix is the algorithm label SEP-2640 fixes for resource digests:
// "sha256:" followed by 64 lowercase hexadecimal characters.
const digestPrefix = "sha256:"

// manifestMimeType is the media type resources/read reports for a served
// SKILL.md, which is Markdown with YAML frontmatter.
const manifestMimeType = "text/markdown"

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

	// Resources enumerates every file of the skill: SKILL.md first, then each
	// supporting file sorted by path. Every one is readable through
	// resources/read.
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

	// ProjectID is the project that owns the skill. A gateway serves skills
	// from every project in its organization, so it can differ from the
	// endpoint's.
	ProjectID uuid.UUID

	// ProjectSlug is the owning project's slug, the URI segment that keeps
	// same-named skills from different projects apart.
	ProjectSlug string

	// VersionID is the resolved skill version whose content is served.
	VersionID uuid.UUID

	// Name is the skill's normalized name, the final path segment of the URI
	// and the frontmatter name.
	Name string

	// Content is the SKILL.md as stored, the exact bytes resources/read
	// returns and the digest covers.
	Content string

	// RawSHA256 is the lowercase hex SHA-256 of Content: as recorded on the
	// version for a stored skill, or computed from the embedded bytes for an
	// entry of the EmbeddedCatalog.
	RawSHA256 string

	// Frontmatter is the parsed YAML frontmatter of Content.
	Frontmatter map[string]any

	// Files are the version's supporting files, sorted by path.
	Files []File
}

// File is one supporting file of a served skill version.
type File struct {
	// Path is the slash-separated path relative to the skill root, for
	// example references/REFERENCE.md.
	Path string

	// SHA256 is the lowercase hex SHA-256 of the file's bytes.
	SHA256 string

	// Size is the byte length of the file.
	Size int64

	// ContentType is the media type stored with the file.
	ContentType string

	// Content is the file's bytes. An EmbeddedCatalog entry carries it; a
	// resolved entry leaves it nil and Resolver.Read loads it on demand.
	Content []byte
}

// Resource is the content of one file of a served skill, as resources/read
// returns it.
type Resource struct {
	// URI is the file's resource URI.
	URI string

	// Path is the supporting file's path, or empty for SKILL.md.
	Path string

	// MimeType is the media type the read reports: text/markdown for
	// SKILL.md, the stored content type for a supporting file.
	MimeType string

	// Content is the file's exact bytes, the ones its digest covers.
	Content []byte

	// Text reports whether Content is UTF-8 text the read returns as text
	// rather than as a base64 blob.
	Text bool
}

// IsManifest reports whether the resource is the skill's SKILL.md.
func (r Resource) IsManifest() bool {
	return r.Path == ""
}

// URI returns the entry's resource URI,
// skill://<project-slug>/<name>/SKILL.md.
func (e Entry) URI() string {
	return SkillURI(e.ProjectSlug, e.Name)
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

// Wire renders the entry in the shape skills/list and skills/get return:
// one resource for SKILL.md, then one per supporting file.
func (e Entry) Wire() Skill {
	uri := e.URI()
	resources := make([]SkillResource, 0, len(e.Files)+1)
	resources = append(resources, SkillResource{
		URI:    uri,
		Digest: e.Digest(),
		Size:   e.Size(),
	})
	for _, file := range e.Files {
		resources = append(resources, SkillResource{
			URI:    SkillFileURI(e.ProjectSlug, e.Name, file.Path),
			Digest: Digest(file.SHA256),
			Size:   int(file.Size),
		})
	}
	return Skill{
		URI:         uri,
		Frontmatter: e.Frontmatter,
		Resources:   resources,
	}
}

// ManifestResource returns the entry's SKILL.md as a resource.
func (e Entry) ManifestResource() Resource {
	return Resource{URI: e.URI(), Path: "", MimeType: manifestMimeType, Content: []byte(e.Content), Text: true}
}

// fileResource returns one supporting file of the entry, with content, as a
// resource.
func (e Entry) fileResource(file File, content []byte) Resource {
	return Resource{
		URI:      SkillFileURI(e.ProjectSlug, e.Name, file.Path),
		Path:     file.Path,
		MimeType: file.ContentType,
		Content:  content,
		Text:     skilltree.IsText(file.ContentType, content),
	}
}

// file returns the supporting file at filePath, if the entry has one.
func (e Entry) file(filePath string) (file File, ok bool) {
	for _, candidate := range e.Files {
		if candidate.Path == filePath {
			return candidate, true
		}
	}
	return file, false
}
