package mcpskills

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/skills"
)

// EmbeddedProjectSlug is the project slug in the URI of every embedded skill:
// skill://speakeasy/<name>/SKILL.md. Embedded skills belong to no project, and
// the Platform MCP endpoints serve only embedded skills, so the fixed slug
// never collides with a customer project's skills there.
const EmbeddedProjectSlug = "speakeasy"

// EmbeddedCatalog is the set of reviewed skills the Platform MCP serves over
// the Skills extension (SEP-2640): the skills embedded in the server and
// shipped inside every Platform MCP package. Both platform endpoints, the
// assistant toolset and the external Platform MCP, serve this one catalog.
// It is the same for every caller and never changes while the server runs,
// so it is built once at construction and read without locking.
type EmbeddedCatalog struct {
	// entries holds every served skill, sorted by name, in the order
	// skills/list returns them.
	entries []Entry

	// byName indexes entries by skill name, the final segment of a skill
	// URI, for skills/get and resources/read.
	byName map[string]Entry
}

// NewEmbeddedCatalog renders the embedded reviewed skills as served entries.
// Each entry carries the exact embedded bytes, their SHA-256 as the digest,
// and the SKILL.md frontmatter parsed verbatim; it belongs to no stored
// skill, version, or project, and its URI carries EmbeddedProjectSlug. The embedded set is validated against the Agent
// Skills specification at load time, so a failure here means the server
// binary ships an invalid skill and must not start serving the catalog.
func NewEmbeddedCatalog() (*EmbeddedCatalog, error) {
	sources, err := plugins.PlatformMCPSkills()
	if err != nil {
		return nil, fmt.Errorf("load embedded platform skills: %w", err)
	}

	catalog := &EmbeddedCatalog{
		entries: make([]Entry, 0, len(sources)),
		byName:  make(map[string]Entry, len(sources)),
	}
	for name, source := range sources {
		content := source.Manifest
		frontmatter, err := skills.SkillFrontmatter(string(content))
		if err != nil {
			return nil, fmt.Errorf("parse embedded platform skill %q frontmatter: %w", name, err)
		}

		files := make([]File, 0, len(source.Files))
		for _, file := range source.Files {
			files = append(files, File{
				Path:        file.Path,
				SHA256:      file.SHA256,
				Size:        file.Size,
				ContentType: file.ContentType,
				Content:     file.Content,
			})
		}

		digest := sha256.Sum256(content)
		entry := Entry{
			SkillID:     uuid.Nil,
			ProjectID:   uuid.Nil,
			ProjectSlug: EmbeddedProjectSlug,
			VersionID:   uuid.Nil,
			Name:        name,
			Content:     string(content),
			RawSHA256:   hex.EncodeToString(digest[:]),
			Frontmatter: frontmatter,
			Files:       files,
		}
		if err := entry.CheckFrontmatterName(); err != nil {
			return nil, fmt.Errorf("embedded platform skill %q: %w", name, err)
		}
		catalog.entries = append(catalog.entries, entry)
		catalog.byName[name] = entry
	}
	slices.SortFunc(catalog.entries, func(a, b Entry) int {
		return strings.Compare(a.Name, b.Name)
	})

	return catalog, nil
}

// Entries returns every served skill, sorted by name. The slice is shared;
// callers must not modify it.
func (c *EmbeddedCatalog) Entries() []Entry {
	return c.entries
}

// Get returns the entry a skill URI names, or false when the URI is not the
// SKILL.md of a served skill.
func (c *EmbeddedCatalog) Get(uri string) (entry Entry, ok bool) {
	projectSlug, name, ok := ParseSkillURI(uri)
	if !ok || projectSlug != EmbeddedProjectSlug {
		return entry, false
	}
	entry, ok = c.byName[name]
	return entry, ok
}

// Read returns the content of the file a skill URI names, SKILL.md or a
// supporting file, with the entry it belongs to, or false when the URI names
// no file of a served skill.
func (c *EmbeddedCatalog) Read(uri string) (entry Entry, resource Resource, ok bool) {
	projectSlug, name, filePath, ok := ParseSkillResourceURI(uri)
	if !ok || projectSlug != EmbeddedProjectSlug {
		return entry, resource, false
	}
	served, ok := c.byName[name]
	if !ok {
		return entry, resource, false
	}
	if filePath == "" {
		return served, served.ManifestResource(), true
	}
	file, ok := served.file(filePath)
	if !ok {
		return entry, resource, false
	}
	return served, served.fileResource(file, file.Content), true
}
