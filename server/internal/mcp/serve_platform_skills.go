package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpmetrics"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpskills"
	"github.com/speakeasy-api/gram/server/internal/mcp/mcpversions"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	"github.com/speakeasy-api/gram/server/internal/skills"
)

// platformSkillCatalog is the set of reviewed skills the platform toolset
// serves over the Skills extension (SEP-2640): the skills embedded in the
// server and shipped inside every Platform MCP package. The catalog is the
// same for every caller and never changes while the server runs, so it is
// built once at construction and read without locking.
type platformSkillCatalog struct {
	// entries holds every served skill, sorted by name, in the order
	// skills/list returns them.
	entries []mcpskills.Entry

	// byName indexes entries by skill name, the final segment of a skill
	// URI, for skills/get and resources/read.
	byName map[string]mcpskills.Entry
}

// newPlatformSkillCatalog renders the embedded reviewed skills as served
// entries. Each entry carries the exact embedded bytes, their SHA-256 as the
// digest, and the SKILL.md frontmatter parsed verbatim; it belongs to no
// stored skill, version, or plugin. The embedded set is validated against the
// Agent Skills specification at load time, so a failure here means the server
// binary ships an invalid skill and must not start serving the catalog.
func newPlatformSkillCatalog() (*platformSkillCatalog, error) {
	sources, err := plugins.PlatformMCPSkills()
	if err != nil {
		return nil, fmt.Errorf("load embedded platform skills: %w", err)
	}

	catalog := &platformSkillCatalog{
		entries: make([]mcpskills.Entry, 0, len(sources)),
		byName:  make(map[string]mcpskills.Entry, len(sources)),
	}
	for name, content := range sources {
		frontmatter, err := skills.SkillFrontmatter(string(content))
		if err != nil {
			return nil, fmt.Errorf("parse embedded platform skill %q frontmatter: %w", name, err)
		}

		digest := sha256.Sum256(content)
		entry := mcpskills.Entry{
			SkillID:     uuid.Nil,
			VersionID:   uuid.Nil,
			PluginID:    uuid.Nil,
			Name:        name,
			Content:     string(content),
			RawSHA256:   hex.EncodeToString(digest[:]),
			Frontmatter: frontmatter,
			Pinned:      false,
		}
		if err := entry.CheckFrontmatterName(); err != nil {
			return nil, fmt.Errorf("embedded platform skill %q: %w", name, err)
		}
		catalog.entries = append(catalog.entries, entry)
		catalog.byName[name] = entry
	}
	slices.SortFunc(catalog.entries, func(a, b mcpskills.Entry) int {
		return strings.Compare(a.Name, b.Name)
	})

	return catalog, nil
}

// get returns the entry a skill URI names, or false when the URI is not the
// SKILL.md of a served skill.
func (c *platformSkillCatalog) get(uri string) (entry mcpskills.Entry, ok bool) {
	name, ok := mcpskills.ParseSkillURI(uri)
	if !ok {
		return entry, false
	}
	entry, ok = c.byName[name]
	return entry, ok
}

// platformSkillsEnabled reports whether the platform toolset serves the
// Skills extension to the request's organization. The surface answers only
// assistant-token callers, so the organization is the one the token was
// issued for; a request with no auth context, as a dispatch without
// authorization produces, is served nothing.
func (s *Service) platformSkillsEnabled(ctx context.Context, authCtx *contextvalues.AuthContext) bool {
	if authCtx == nil {
		return false
	}
	return s.skillsOverMCPEnabledForOrganization(ctx, authCtx.ActiveOrganizationID)
}

// platformExtensions lists the extension capability labels the platform
// toolset declares to this request, which is also the set whose methods
// dispatch admits.
func (s *Service) platformExtensions(ctx context.Context, authCtx *contextvalues.AuthContext) []string {
	if !s.platformSkillsEnabled(ctx, authCtx) {
		return nil
	}
	return []string{mcpversions.ExtensionSkills}
}

// listPlatformSkills answers skills/list with the whole embedded catalog in
// one page. The catalog is static, but the listing stays private to the
// caller: whether this surface serves skills at all depends on the caller's
// organization.
func (s *Service) listPlatformSkills(ctx context.Context, req *rawRequest) (json.RawMessage, error) {
	if _, err := parseSkillsListParams(ctx, s.logger, req); err != nil {
		return nil, err
	}

	return marshalSkillsList(ctx, s.logger, req.ID, s.platformSkills.entries, serverInfoPlatformToolset)
}

// getPlatformSkill answers skills/get with the catalog entry for one URI, or
// the invalid-params error the extension requires for a URI that names no
// served skill.
func (s *Service) getPlatformSkill(ctx context.Context, req *rawRequest) (json.RawMessage, error) {
	params, err := parseSkillsGetParams(ctx, s.logger, req)
	if err != nil {
		return nil, err
	}

	entry, ok := s.platformSkills.get(params.URI)
	if !ok {
		return nil, skillNotFound(ctx, s.logger, mcpskills.ErrSkillNotFound, params.URI)
	}

	return marshalSkillsGet(ctx, s.logger, req.ID, entry, serverInfoPlatformToolset)
}

// readPlatformSkill answers resources/read for an embedded skill's SKILL.md
// with the exact embedded bytes. Embedded skills are reviewed Speakeasy
// content rather than a customer's managed skill, so a read is counted on the
// skill read metric but recorded as no activation: there is no stored version
// to observe. uri is the skill URI the request named, already known to have
// the shape of a served skill.
func (s *Service) readPlatformSkill(ctx context.Context, req *rawRequest, uri string) (json.RawMessage, error) {
	entry, ok := s.platformSkills.get(uri)
	if !ok {
		return nil, skillNotFound(ctx, s.logger, mcpskills.ErrSkillNotFound, uri)
	}

	bs, err := marshalSkillRead(ctx, s.logger, req.ID, entry, serverInfoPlatformToolset)
	if err != nil {
		return nil, err
	}
	s.metrics.RecordSkillRead(ctx, mcpmetrics.SurfacePlatform, mcpmetrics.SkillOriginEmbedded)
	return bs, nil
}

// listPlatformResources answers resources/list with no resources. The
// platform toolset declares the resources capability only to carry skill
// reads, and skills are listed through skills/list rather than as resources.
func (s *Service) listPlatformResources(ctx context.Context, req *rawRequest) (json.RawMessage, error) {
	bs, err := json.Marshal(result[resourcesListResult]{
		ID:             req.ID,
		Result:         resourcesListResult{Resources: make([]*resourceListEntry, 0)},
		serverIdentity: serverInfoPlatformToolset,
		cacheHints:     cacheHintsCallerVarying,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "failed to serialize resources/list response").LogError(ctx, s.logger)
	}
	return bs, nil
}
