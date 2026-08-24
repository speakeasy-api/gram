//nolint:exhaustruct // Generated repository parameter types intentionally use documented zero-value optional fields.
package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

const (
	PluginsConnectionLimitName   = "platform-mcp-plugins-connection"
	PluginsOrganizationLimitName = "platform-mcp-plugins-organization"
)

const (
	// PluginQueriesPerConnectionPerMinute and
	// PluginQueriesPerOrganizationPerMinute bound plugin inventory reads. An
	// administrator answering "which plugins exist and what is in them" walks a
	// page and then opens the plugins that looked interesting, so the allowance
	// matches the diagnostics reads rather than a mutation's.
	PluginQueriesPerConnectionPerMinute   = 30
	PluginQueriesPerOrganizationPerMinute = 300
)

// maxPluginPageSize bounds one page of plugin inventory.
const maxPluginPageSize = 50

// maxPluginMembers bounds the membership lists on one plugin. A plugin is a
// curated bundle an administrator assembled; a bundle larger than this is
// reported truncated rather than paged, because the question the tool answers
// ("what is in this plugin") is already answered by the first hundred entries.
const maxPluginMembers = 100

var (
	// ErrPluginProjectNotFound is a project this principal may not read, or one
	// that does not exist. The two are deliberately one answer: distinguishing
	// them tells a caller which project ids exist in other organizations.
	ErrPluginProjectNotFound = errors.New("platform mcp plugin project not found")
	// ErrPluginNotFound is a named plugin that matches nothing in the project.
	ErrPluginNotFound = errors.New("platform mcp plugin not found")
	// ErrPluginAmbiguous is a name matching more than one plugin. It is never
	// resolved by picking one, and never by falling back to the default plugin.
	ErrPluginAmbiguous = errors.New("platform mcp plugin target ambiguous")
	// ErrPluginCursorInvalid is a page cursor that was not issued for this
	// principal, project, and listing.
	ErrPluginCursorInvalid = errors.New("invalid platform MCP plugin cursor")
)

// PluginAudience is who receives a plugin, projected as a shape rather than as
// principals. A plugin assignment holds a principal URN that embeds a user id,
// which this surface must not carry.
type PluginAudience struct {
	// AllMembers is true when the plugin is assigned to every organization
	// member through the wildcard principal.
	AllMembers bool `json:"all_members"`

	// Roles is how many role principals the plugin is assigned to.
	Roles int64 `json:"roles"`

	// Users is how many individual principals the plugin is assigned to.
	Users int64 `json:"users"`
}

// Plugin publication states.
const (
	// PluginPublicationPublished means the project's package repository holds a
	// published package for this plugin.
	PluginPublicationPublished = "published"
	// PluginPublicationUnpublished means the project has a package repository
	// but nothing published for this plugin yet.
	PluginPublicationUnpublished = "unpublished"
	// PluginPublicationNoRepository means the project has no package repository
	// connected, so no plugin in it can be published at all.
	PluginPublicationNoRepository = "no_repository"
)

// Plugin is the inventory projection of one plugin: what it is, how much it
// carries, who receives it, and whether it has been published.
type Plugin struct {
	// ID is the plugin id, and is what a distribution target should name to be
	// unambiguous.
	ID string `json:"id"`

	// Name is the administrator-facing plugin name.
	Name string `json:"name"`

	// Slug is the plugin's project-unique slug.
	Slug string `json:"slug"`

	// Description is the plugin's description, absent when it has none.
	Description string `json:"description,omitempty"`

	// IsDefault marks the project's fallback plugin.
	IsDefault bool `json:"is_default"`

	// ServerCount is how many MCP servers the plugin carries.
	ServerCount int64 `json:"server_count"`

	// SkillCount is how many skills the plugin carries.
	SkillCount int64 `json:"skill_count"`

	// Audience is who receives the plugin.
	Audience PluginAudience `json:"audience"`

	// Publication is the plugin's package publication state.
	Publication string `json:"publication"`
}

// PluginServer is one MCP server a plugin carries. It names the server; it
// does not hand out an endpoint URL.
type PluginServer struct {
	// DisplayName is the name the generated package gives this server.
	DisplayName string `json:"display_name"`

	// Backend is what the entry is backed by: "toolset" or "mcp_server".
	Backend string `json:"backend"`

	// MCPSlug is the server's MCP slug, empty when the backing server has no
	// usable endpoint.
	MCPSlug string `json:"mcp_slug,omitempty"`

	// Policy is "required" or "optional" for the installing client.
	Policy string `json:"policy"`

	// Enabled is false when the backing server is disabled, in which case the
	// plugin carries an entry that currently serves nothing.
	Enabled bool `json:"enabled"`
}

// PluginSkill is one skill a plugin carries.
type PluginSkill struct {
	// Name is the skill name.
	Name string `json:"name"`

	// PinnedVersionID is the version the distribution is pinned to, empty when
	// it follows the skill's latest valid version.
	PinnedVersionID string `json:"pinned_version_id,omitempty"`

	// FollowsLatest is true when no version is pinned, so authoring a new
	// version changes what this plugin ships.
	FollowsLatest bool `json:"follows_latest"`
}

type ListPluginsInput struct {
	ProjectID string `json:"project_id" jsonschema:"AICP project ID whose plugins to list"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum number of plugins to return; server clamps this to 50"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"opaque cursor from a previous list_plugins result"`
}

type ListPluginsOutput struct {
	// ProjectID echoes the project the plugins belong to.
	ProjectID string `json:"project_id"`

	// Plugins is one page of the project's plugins.
	Plugins []Plugin `json:"plugins"`

	// NextCursor is the cursor for the next page, empty when this page is the
	// last one.
	NextCursor string `json:"next_cursor,omitempty"`
}

type GetPluginInput struct {
	ProjectID string `json:"project_id" jsonschema:"AICP project ID that owns the plugin"`
	Plugin    string `json:"plugin" jsonschema:"exact plugin ID, slug, or name as returned by list_plugins"`
}

type GetPluginOutput struct {
	// ProjectID echoes the project the plugin belongs to.
	ProjectID string `json:"project_id"`

	// Plugin is the resolved plugin's inventory projection.
	Plugin Plugin `json:"plugin"`

	// Servers is the MCP servers the plugin carries.
	Servers []PluginServer `json:"servers"`

	// Skills is the skills the plugin carries.
	Skills []PluginSkill `json:"skills"`

	// Truncated is true when the plugin carries more members than one result
	// projects, so the lists above are a prefix rather than the whole bundle.
	Truncated bool `json:"truncated"`
}

// PluginsService answers what plugins a project has and what is inside one,
// and resolves the exact plugin a distribution names.
type PluginsService struct {
	db      *pgxpool.Pool
	budget  OperationBudget
	cursors *pluginCursorCodec
}

func NewPluginsService(db *pgxpool.Pool, budget OperationBudget, cursorKeyMaterial string) *PluginsService {
	codec, err := newPluginCursorCodec(cursorKeyMaterial)
	if err != nil {
		codec = nil
	}
	return &PluginsService{db: db, budget: budget, cursors: codec}
}

func (s *PluginsService) valid() bool {
	return s != nil && s.db != nil && s.budget.valid() && s.cursors != nil
}

func (s *PluginsService) ListPlugins(ctx context.Context, principal Principal, input ListPluginsInput) (ListPluginsOutput, error) {
	if !s.valid() {
		return ListPluginsOutput{}, ErrUnavailable
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return ListPluginsOutput{}, err
	}
	q := platformrepo.New(s.db)
	project, err := s.resolveProject(ctx, q, principal, input.ProjectID)
	if err != nil {
		return ListPluginsOutput{}, err
	}
	after, err := s.cursors.Decode(input.Cursor, principal, project.ID)
	if err != nil {
		return ListPluginsOutput{}, err
	}
	// An omitted or non-positive limit asks for the default page rather than
	// one row, which is what clamping alone would produce.
	limit := maxPluginPageSize
	if input.Limit > 0 {
		limit = min(input.Limit, maxPluginPageSize)
	}
	rows, err := q.ListPlatformMCPPluginInventory(ctx, platformrepo.ListPlatformMCPPluginInventoryParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
		UseAfter:       after != uuid.Nil,
		AfterID:        after,
		// One extra row decides whether another page exists without a second
		// query; it is trimmed before projection.
		ResultLimit: int32(limit + 1), // #nosec G115 -- bounded above by maxPluginPageSize.
	})
	if err != nil {
		return ListPluginsOutput{}, fmt.Errorf("list platform mcp plugins: %w", err)
	}
	rows, more := boundedRows(rows, limit)

	output := ListPluginsOutput{ProjectID: project.ID.String(), Plugins: make([]Plugin, 0, len(rows))}
	for _, row := range rows {
		output.Plugins = append(output.Plugins, pluginFromInventoryRow(row))
	}
	if more && len(rows) > 0 {
		cursor, err := s.cursors.Encode(pluginCursor{
			OrganizationID: principal.OrganizationID,
			Binding:        principalCursorBinding(principal),
			ProjectID:      project.ID.String(),
			AfterPluginID:  rows[len(rows)-1].ID.String(),
		})
		if err != nil {
			return ListPluginsOutput{}, err
		}
		output.NextCursor = cursor
	}
	return output, nil
}

func (s *PluginsService) GetPlugin(ctx context.Context, principal Principal, input GetPluginInput) (GetPluginOutput, error) {
	if !s.valid() {
		return GetPluginOutput{}, ErrUnavailable
	}
	if strings.TrimSpace(input.Plugin) == "" {
		return GetPluginOutput{}, ErrPluginNotFound
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return GetPluginOutput{}, err
	}
	q := platformrepo.New(s.db)
	project, err := s.resolveProject(ctx, q, principal, input.ProjectID)
	if err != nil {
		return GetPluginOutput{}, err
	}
	target, err := s.resolve(ctx, q, principal, project.ID, input.Plugin)
	if err != nil {
		return GetPluginOutput{}, err
	}
	row, err := q.GetPlatformMCPPluginInventoryItem(ctx, platformrepo.GetPlatformMCPPluginInventoryItemParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
		PluginID:       target.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return GetPluginOutput{}, ErrPluginNotFound
	}
	if err != nil {
		return GetPluginOutput{}, fmt.Errorf("get platform mcp plugin: %w", err)
	}
	servers, err := q.ListPlatformMCPPluginServers(ctx, platformrepo.ListPlatformMCPPluginServersParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
		PluginID:       target.ID,
		ResultLimit:    maxPluginMembers + 1,
	})
	if err != nil {
		return GetPluginOutput{}, fmt.Errorf("list platform mcp plugin servers: %w", err)
	}
	skills, err := q.ListPlatformMCPPluginSkills(ctx, platformrepo.ListPlatformMCPPluginSkillsParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      project.ID,
		PluginID:       uuid.NullUUID{UUID: target.ID, Valid: true},
		ResultLimit:    maxPluginMembers + 1,
	})
	if err != nil {
		return GetPluginOutput{}, fmt.Errorf("list platform mcp plugin skills: %w", err)
	}
	servers, serversTruncated := boundedRows(servers, maxPluginMembers)
	skills, skillsTruncated := boundedRows(skills, maxPluginMembers)

	output := GetPluginOutput{
		ProjectID: project.ID.String(),
		// The two inventory rows are the same projection selected two ways, so the
		// conversion keeps one mapping rather than a second copy of it.
		Plugin:    pluginFromInventoryRow(platformrepo.ListPlatformMCPPluginInventoryRow(row)),
		Servers:   make([]PluginServer, 0, len(servers)),
		Skills:    make([]PluginSkill, 0, len(skills)),
		Truncated: serversTruncated || skillsTruncated,
	}
	for _, server := range servers {
		backend := "mcp_server"
		if server.ToolsetBacked {
			backend = "toolset"
		}
		output.Servers = append(output.Servers, PluginServer{
			DisplayName: server.DisplayName,
			Backend:     backend,
			MCPSlug:     server.McpSlug,
			Policy:      server.Policy,
			Enabled:     server.Enabled,
		})
	}
	for _, skill := range skills {
		pinned := ""
		if skill.PinnedVersionID.Valid {
			pinned = skill.PinnedVersionID.UUID.String()
		}
		output.Skills = append(output.Skills, PluginSkill{
			Name:            skill.SkillName,
			PinnedVersionID: pinned,
			FollowsLatest:   !skill.PinnedVersionID.Valid,
		})
	}
	return output, nil
}

// PluginRef is one resolved plugin. It is echoed back by every distribution so
// a caller sees which plugin its name resolved to rather than inferring it.
type PluginRef struct {
	// ID is the resolved plugin id.
	ID uuid.UUID

	// Name is the resolved plugin name.
	Name string

	// Slug is the resolved plugin slug.
	Slug string

	// IsDefault marks the project's fallback plugin, so a caller can tell that
	// a name happened to resolve to it rather than being sent there implicitly.
	IsDefault bool
}

// ResolvePlugin matches one plugin in the project exactly. A name that matches
// nothing is not_found and a name that matches more than one is ambiguous;
// neither falls back to the default plugin.
func (s *PluginsService) ResolvePlugin(ctx context.Context, principal Principal, projectID uuid.UUID, wanted string) (PluginRef, error) {
	if s == nil || s.db == nil {
		return PluginRef{}, ErrUnavailable
	}
	return s.resolve(ctx, platformrepo.New(s.db), principal, projectID, wanted)
}

func (s *PluginsService) resolve(ctx context.Context, q *platformrepo.Queries, principal Principal, projectID uuid.UUID, wanted string) (PluginRef, error) {
	name := strings.TrimSpace(wanted)
	if name == "" {
		return PluginRef{}, ErrPluginNotFound
	}
	// Matching happens in SQL over the project's whole plugin set. Matching in
	// Go over a bounded page would refuse a plugin that exists once a project
	// outgrew that page, and would resolve an ambiguous name to whichever of
	// its matches the page happened to carry.
	matches, err := q.ResolvePlatformMCPPluginTarget(ctx, platformrepo.ResolvePlatformMCPPluginTargetParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      projectID,
		Target:         name,
	})
	if err != nil {
		return PluginRef{}, fmt.Errorf("resolve platform mcp plugin target: %w", err)
	}
	switch len(matches) {
	case 0:
		return PluginRef{}, ErrPluginNotFound
	case 1:
		return PluginRef{
			ID:        matches[0].ID,
			Name:      matches[0].Name,
			Slug:      matches[0].Slug,
			IsDefault: matches[0].IsDefault,
		}, nil
	default:
		return PluginRef{}, ErrPluginAmbiguous
	}
}

// resolveProject resolves the project a plugin read is scoped to. A project in
// another organization is reported exactly as a missing one.
func (s *PluginsService) resolveProject(ctx context.Context, q *platformrepo.Queries, principal Principal, projectID string) (ResolvedProject, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(projectID))
	if err != nil {
		return ResolvedProject{}, ErrPluginProjectNotFound
	}
	row, err := q.ResolvePlatformMCPProjectByID(ctx, platformrepo.ResolvePlatformMCPProjectByIDParams{
		OrganizationID: principal.OrganizationID,
		ProjectID:      parsed,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ResolvedProject{}, ErrPluginProjectNotFound
	}
	if err != nil {
		return ResolvedProject{}, fmt.Errorf("resolve platform mcp plugin project: %w", err)
	}
	return ResolvedProject{ID: row.ID, Name: row.Name, Slug: row.Slug}, nil
}

func pluginFromInventoryRow(row platformrepo.ListPlatformMCPPluginInventoryRow) Plugin {
	publication := PluginPublicationNoRepository
	switch {
	case row.Published:
		publication = PluginPublicationPublished
	case row.RepositoryConnected:
		publication = PluginPublicationUnpublished
	}
	return Plugin{
		ID:          row.ID.String(),
		Name:        row.Name,
		Slug:        row.Slug,
		Description: row.Description.String,
		IsDefault:   row.IsDefault,
		ServerCount: row.ServerCount,
		SkillCount:  row.SkillCount,
		Audience: PluginAudience{
			AllMembers: row.WildcardAssignmentCount > 0,
			Roles:      row.RoleAssignmentCount,
			Users:      row.UserAssignmentCount,
		},
		Publication: publication,
	}
}
