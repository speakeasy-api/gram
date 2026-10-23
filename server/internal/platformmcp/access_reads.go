package platformmcp

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/mv"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

const (
	AccessReadsConnectionLimitName   = "platform-mcp-access-reads-connection"
	AccessReadsOrganizationLimitName = "platform-mcp-access-reads-organization"

	AccessReadQueriesPerConnectionPerMinute   = 30
	AccessReadQueriesPerOrganizationPerMinute = 300

	maxAccessMembers          = 50
	maxAccessTools            = 100
	maxMemberMCPAccessServers = 1000
	minAccessQueryLength      = 3
)

const (
	subjectKindAccessRole   = "access_role"
	subjectKindAccessMember = "access_member"
)

var (
	ErrAccessReferenceNotFound = errors.New("platform mcp access reference not found")
	ErrAccessMCPNotFound       = errors.New("platform mcp access target not found")
	ErrAccessQueryRequired     = errors.New("platform mcp access member filter required")
)

type MCPConnectSummary struct {
	AllServers              bool     `json:"all_servers"`
	ProjectRules            int      `json:"project_rules"`
	ServerRules             int      `json:"server_rules"`
	ToolRules               int      `json:"tool_rules"`
	DispositionRules        []string `json:"disposition_rules"`
	BlockedServers          bool     `json:"blocked_servers"`
	BlockedProjectRules     int      `json:"blocked_project_rules"`
	BlockedServerRules      int      `json:"blocked_server_rules"`
	BlockedToolRules        int      `json:"blocked_tool_rules"`
	BlockedDispositionRules []string `json:"blocked_disposition_rules"`
}

type AccessRole struct {
	Name             string                      `json:"name"`
	Type             string                      `json:"type"`
	MemberCount      SubjectCount                `json:"member_count"`
	MCPAccess        MCPConnectSummary           `json:"mcp_access"`
	DirectorySources []AccessRoleDirectorySource `json:"directory_sources" jsonschema:"directory groups and attribute values that give this role to everyone they match; empty when nothing in the directory is mapped to the role"`
	Reference        string                      `json:"reference"`
	Version          string                      `json:"version"`
}

// AccessRoleDirectorySource is one directory group or attribute value mapped
// to a role. Under directory sync it explains where the role's members come
// from; it is changed on the identity provider page of the dashboard.
type AccessRoleDirectorySource struct {
	Kind           string       `json:"kind" jsonschema:"group or attribute"`
	Group          string       `json:"group,omitempty" jsonschema:"directory group name, for a group source"`
	AttributeKey   string       `json:"attribute_key,omitempty" jsonschema:"directory attribute, for an attribute source"`
	AttributeValue string       `json:"attribute_value,omitempty" jsonschema:"directory attribute value, for an attribute source"`
	Removed        bool         `json:"removed,omitempty" jsonschema:"true when the mapped group was deleted from the directory; it matches nobody and has no name"`
	MatchedUsers   SubjectCount `json:"matched_users" jsonschema:"directory users the source matches today, signed in or not; small counts are withheld"`
}

type ListAccessRolesOutput struct {
	Roles     []AccessRole `json:"roles"`
	ExpiresAt string       `json:"expires_at"`
}

type ListAccessMembersInput struct {
	Query         string `json:"query,omitempty" jsonschema:"identity text to search for; required unless role_reference is supplied"`
	RoleReference string `json:"role_reference,omitempty" jsonschema:"opaque role reference returned by list_access_roles"`
	Limit         int    `json:"limit,omitempty" jsonschema:"maximum number of matching members to return; server clamps this to 50"`
}

type AccessMember struct {
	MaskedIdentity string   `json:"masked_identity"`
	Roles          []string `json:"roles"`
	Reference      string   `json:"reference"`
	Version        string   `json:"version"`
}

type ListAccessMembersOutput struct {
	Members      []AccessMember `json:"members"`
	TotalMatches SubjectCount   `json:"total_matches"`
	Suppressed   bool           `json:"suppressed"`
	Truncated    bool           `json:"truncated"`

	// ExactEmailMatch reports that the cohort was smaller than five and the
	// single member returned is the one whose complete email equals the query.
	ExactEmailMatch bool   `json:"exact_email_match"`
	ExpiresAt       string `json:"expires_at,omitempty"`
}

type GetMemberMCPAccessInput struct {
	MemberReference string `json:"member_reference" jsonschema:"opaque member reference returned by list_access_members"`
}

type MemberMCPAccessRow struct {
	ProjectID         string `json:"project_id"`
	ProjectName       string `json:"project_name"`
	MCPID             string `json:"mcp_id"`
	MCPName           string `json:"mcp_name"`
	Backend           string `json:"backend"`
	Visibility        string `json:"visibility"`
	AuthorizationMode string `json:"authorization_mode"`
	ConnectionAccess  string `json:"connection_access" jsonschema:"effective connection result: public, disabled, not_served, all_tools, some_tools, or none"`
	CanConnect        bool   `json:"can_connect"`
	ToolAccess        string `json:"tool_access" jsonschema:"effective tool coverage: all, some, or none"`
	CanView           bool   `json:"can_view"`
	CanManage         bool   `json:"can_manage"`
}

type GetMemberMCPAccessOutput struct {
	Servers   []MemberMCPAccessRow `json:"servers"`
	Truncated bool                 `json:"truncated"`
}

type MCPAccessTool struct {
	Name        string `json:"name"`
	Disposition string `json:"disposition,omitempty"`
}

type MCPAccessTarget struct {
	ID                   string          `json:"id"`
	Name                 string          `json:"name"`
	Backend              string          `json:"backend"`
	Visibility           string          `json:"visibility"`
	AuthorizationMode    string          `json:"authorization_mode"`
	AuthorizationSurface string          `json:"authorization_surface"`
	AccessSummary        string          `json:"access_summary"`
	ToolCatalog          string          `json:"tool_catalog"`
	Tools                []MCPAccessTool `json:"tools"`
	ToolsTruncated       bool            `json:"tools_truncated"`
}

type MCPMatchingPlugin struct {
	PluginID string `json:"plugin_id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
}

type MCPRoleCoverage struct {
	MatchingPlugins     []MCPMatchingPlugin        `json:"matching_plugins,omitzero" jsonschema:"live plugins assigned to this exact role containing this MCP; not authorization or installation evidence"`
	Name                string                     `json:"name"`
	Type                string                     `json:"type"`
	MemberCount         SubjectCount               `json:"member_count"`
	Reference           string                     `json:"reference"`
	Version             string                     `json:"version"`
	CanEnterServer      bool                       `json:"can_enter_server"`
	KnownToolAccess     string                     `json:"known_tool_access"`
	AllowedKnownTools   []string                   `json:"allowed_known_tools"`
	DispositionRules    []string                   `json:"disposition_rules"`
	BlockedDispositions []string                   `json:"blocked_dispositions"`
	UnevaluatedGrants   bool                       `json:"unevaluated_grants"`
	AssignmentRules     []AccessRoleAssignmentRule `json:"assignment_rules,omitempty" jsonschema:"complete role rules for eligible assignments; all_tools includes future tools; tool and disposition together are conjunctive"`
	AssignmentEligible  bool                       `json:"assignment_eligible" jsonschema:"true only when the entire custom role is confined to this project and MCP; still requires explicit confirmation of all returned access rules"`
}

type GetMCPAccessInput struct {
	ProjectID string `json:"project_id" jsonschema:"project ID that owns the configured MCP"`
	MCPID     string `json:"mcp_id" jsonschema:"configured MCP ID returned by find_mcp"`
}

type GetMCPAccessOutput struct {
	ProjectID string            `json:"project_id"`
	MCP       MCPAccessTarget   `json:"mcp"`
	Roles     []MCPRoleCoverage `json:"roles"`
	ExpiresAt string            `json:"expires_at"`
}

// AccessReadService projects the access service's local role/member authority
// into privacy-safe Platform MCP reads. It never contacts WorkOS or a configured
// MCP and never accepts raw role, member, principal, or grant identifiers.
type AccessReadService struct {
	logger     *slog.Logger
	db         *pgxpool.Pool
	roles      *access.RoleManager
	budget     OperationBudget
	references *subjectReferenceCodec
	now        func() time.Time
	versionKey []byte
	// directoryReview backs the directory review tools; nil leaves them
	// reporting unavailable.
	directoryReview DirectoryReviewBackend
}

func NewAccessReadService(logger *slog.Logger, db *pgxpool.Pool, budget OperationBudget, keyMaterial string) *AccessReadService {
	if logger == nil {
		return &AccessReadService{logger: nil, db: nil, roles: nil, budget: OperationBudget{Connection: nil, Organization: nil}, references: nil, now: nil, versionKey: nil, directoryReview: nil}
	}
	references, err := newSubjectReferenceCodec(keyMaterial)
	if err != nil {
		logger.ErrorContext(context.Background(), "build Platform MCP access reference codec", attr.SlogError(err))
	}
	versionKey := sha256.Sum256([]byte("platform-mcp-access-role-version:" + keyMaterial))
	return &AccessReadService{
		logger:     logger,
		db:         db,
		roles:      access.NewRoleManager(logger, db, nil, nil, plugins.PublicationRequests{Enabled: false}, nil),
		budget:     budget,
		references: references,
		now:        time.Now,
		versionKey: versionKey[:],

		directoryReview: nil,
	}
}

func (s *AccessReadService) valid() bool {
	return s != nil && s.db != nil && s.roles != nil && s.budget.valid() && s.references != nil && s.now != nil && len(s.versionKey) == sha256.Size
}

func (s *AccessReadService) ListRoles(ctx context.Context, principal Principal) (ListAccessRolesOutput, error) {
	if !s.valid() {
		return ListAccessRolesOutput{}, ErrUnavailable
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return ListAccessRolesOutput{}, err
	}
	roles, err := s.roles.ListRoles(ctx, principal.OrganizationID)
	if err != nil {
		return ListAccessRolesOutput{}, fmt.Errorf("list platform mcp access roles: %w", err)
	}
	sourceRows, err := platformrepo.New(s.db).ListPlatformMCPRoleDirectorySources(ctx, principal.OrganizationID)
	if err != nil {
		return ListAccessRolesOutput{}, fmt.Errorf("list platform mcp role directory sources: %w", err)
	}
	sourcesByRole := make(map[string][]AccessRoleDirectorySource, len(sourceRows))
	for _, row := range sourceRows {
		sourcesByRole[row.RoleUrn] = append(sourcesByRole[row.RoleUrn], accessRoleDirectorySource(row))
	}

	now := s.now().UTC()
	output := ListAccessRolesOutput{
		Roles:     make([]AccessRole, 0, len(roles.Roles)),
		ExpiresAt: now.Add(SubjectReferenceTTL).Format(time.RFC3339),
	}
	for _, role := range roles.Roles {
		reference, err := s.references.Encode(principal, subjectKindAccessRole, role.ID, now)
		if err != nil {
			return ListAccessRolesOutput{}, fmt.Errorf("issue access role reference: %w", err)
		}
		version, err := accessRoleVersion(s.versionKey, role)
		if err != nil {
			return ListAccessRolesOutput{}, fmt.Errorf("version access role: %w", err)
		}
		output.Roles = append(output.Roles, AccessRole{
			Name:        role.Name,
			Type:        accessRoleType(role),
			MemberCount: NewSubjectCount(int64(role.MemberCount)),
			MCPAccess:   summarizeMCPConnect(role.Grants),
			// Never nil, so a role with no mappings says so as an empty list.
			DirectorySources: append([]AccessRoleDirectorySource{}, sourcesByRole[role.PrincipalUrn]...),
			Reference:        reference,
			Version:          version,
		})
	}
	return output, nil
}

func accessRoleDirectorySource(row platformrepo.ListPlatformMCPRoleDirectorySourcesRow) AccessRoleDirectorySource {
	source := AccessRoleDirectorySource{
		Kind:           row.SourceKind,
		Group:          "",
		AttributeKey:   "",
		AttributeValue: "",
		Removed:        false,
		MatchedUsers:   NewSubjectCount(row.MemberCount),
	}
	if row.SourceKind == "group" {
		// The mapping outlives a group deleted from the directory; it then
		// matches nobody and has no name to show.
		source.Group = conv.FromPGTextOrEmpty[string](row.DirectoryGroupName)
		source.Removed = !row.DirectoryGroupName.Valid
		return source
	}
	source.AttributeKey = conv.FromPGTextOrEmpty[string](row.AttributeKey)
	source.AttributeValue = conv.FromPGTextOrEmpty[string](row.AttributeValue)
	return source
}

func (s *AccessReadService) ListMembers(ctx context.Context, principal Principal, input ListAccessMembersInput) (ListAccessMembersOutput, error) {
	if !s.valid() {
		return ListAccessMembersOutput{}, ErrUnavailable
	}
	query := normalizeAccessQuery(input.Query)
	if input.RoleReference == "" && len([]rune(query)) < minAccessQueryLength {
		return ListAccessMembersOutput{}, ErrAccessQueryRequired
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return ListAccessMembersOutput{}, err
	}
	roles, err := s.roles.ListRoles(ctx, principal.OrganizationID)
	if err != nil {
		return ListAccessMembersOutput{}, fmt.Errorf("list roles for platform mcp members: %w", err)
	}
	roleNames := make(map[string]string, len(roles.Roles))
	for _, role := range roles.Roles {
		roleNames[role.ID] = role.Name
	}

	roleID := ""
	if input.RoleReference != "" {
		roleID, err = s.references.Decode(input.RoleReference, principal, subjectKindAccessRole, s.now())
		if err != nil {
			return ListAccessMembersOutput{}, ErrAccessReferenceNotFound
		}
		if _, ok := roleNames[roleID]; !ok {
			return ListAccessMembersOutput{}, ErrAccessReferenceNotFound
		}
	}

	limit := maxAccessMembers
	if input.Limit > 0 {
		limit = min(input.Limit, maxAccessMembers)
	}
	// Filtering and limiting share the dashboard's member query; privacy and
	// masking remain at the MCP boundary.
	page, err := s.roles.ListMembersFiltered(ctx, principal.OrganizationID, access.MemberListFilter{
		Query: query, RoleID: roleID, Limit: int32(limit),
	})
	if err != nil {
		return ListAccessMembersOutput{}, fmt.Errorf("list platform mcp access members: %w", err)
	}
	members, total := page.Members, page.TotalMatches
	output := ListAccessMembersOutput{
		Members:         []AccessMember{},
		TotalMatches:    NewSubjectCount(total),
		Suppressed:      total > 0 && total < SubjectSuppressionThreshold,
		Truncated:       total > int64(len(members)),
		ExactEmailMatch: false,
		ExpiresAt:       "",
	}
	if total == 0 {
		return output, nil
	}
	// Organization-privacy rules do not allow a row-bearing response to reveal
	// a filtered cohort smaller than five. The one exception is a query that
	// is a member's complete email: the administrator already knows who they
	// are looking for, so returning only that member reveals nothing beyond
	// membership, which the dashboard's member list shows them anyway. Emails
	// that differ only in case name several members; those stay suppressed so
	// the caller is never handed a reference to the wrong person.
	if output.Suppressed {
		if page.ExactEmailMatches != 1 {
			return output, nil
		}
		members = members[:1]
		output.TotalMatches = NewSubjectCount(1)
		output.Suppressed = false
		output.Truncated = false
		output.ExactEmailMatch = true
	}

	now := s.now().UTC()
	output.ExpiresAt = now.Add(SubjectReferenceTTL).Format(time.RFC3339)
	for _, member := range members {
		names := make([]string, 0, len(member.RoleIds)+len(member.DirectoryRoleIds))
		for _, id := range slices.Concat(member.RoleIds, member.DirectoryRoleIds) {
			if name := roleNames[id]; name != "" {
				names = append(names, name)
			}
		}
		slices.Sort(names)
		reference, err := s.references.Encode(principal, subjectKindAccessMember, member.ID, now)
		if err != nil {
			return ListAccessMembersOutput{}, fmt.Errorf("issue access member reference: %w", err)
		}
		version, err := accessMemberRoleVersion(s.versionKey, member.ID, member.RoleIds)
		if err != nil {
			return ListAccessMembersOutput{}, fmt.Errorf("version access member roles: %w", err)
		}
		output.Members = append(output.Members, AccessMember{
			MaskedIdentity: maskSubject(conv.Default(member.Email, member.Name)),
			Roles:          slices.Compact(names),
			Reference:      reference,
			Version:        version,
		})
	}
	return output, nil
}

func (s *AccessReadService) ListMyMCPAccess(ctx context.Context, principal Principal) (GetMemberMCPAccessOutput, error) {
	return s.getMemberMCPAccess(ctx, principal, principal.UserID)
}

func (s *AccessReadService) GetMemberMCPAccess(ctx context.Context, principal Principal, input GetMemberMCPAccessInput) (GetMemberMCPAccessOutput, error) {
	if !s.valid() {
		return GetMemberMCPAccessOutput{}, ErrUnavailable
	}
	memberID, err := s.references.Decode(input.MemberReference, principal, subjectKindAccessMember, s.now())
	if err != nil {
		return GetMemberMCPAccessOutput{}, ErrAccessReferenceNotFound
	}
	return s.getMemberMCPAccess(ctx, principal, memberID)
}

func (s *AccessReadService) getMemberMCPAccess(ctx context.Context, principal Principal, memberID string) (GetMemberMCPAccessOutput, error) {
	if !s.valid() {
		return GetMemberMCPAccessOutput{}, ErrUnavailable
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return GetMemberMCPAccessOutput{}, err
	}
	isMember, err := organizationsrepo.New(s.db).HasActiveOrganizationUser(ctx, organizationsrepo.HasActiveOrganizationUserParams{
		UserID: memberID, OrganizationID: principal.OrganizationID,
	})
	if err != nil {
		return GetMemberMCPAccessOutput{}, fmt.Errorf("check platform mcp access member: %w", err)
	}
	if !isMember {
		return GetMemberMCPAccessOutput{}, ErrAccessReferenceNotFound
	}
	principals, err := authz.ResolveUserPrincipals(ctx, s.db, principal.OrganizationID, memberID)
	if err != nil {
		return GetMemberMCPAccessOutput{}, fmt.Errorf("resolve platform mcp access member principals: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, s.db, principal.OrganizationID, principals)
	if err != nil {
		return GetMemberMCPAccessOutput{}, fmt.Errorf("load platform mcp access member grants: %w", err)
	}
	//nolint:exhaustruct // Optional inventory filters intentionally stay unset.
	rows, err := platformrepo.New(s.db).ListPlatformMCPInventory(ctx, platformrepo.ListPlatformMCPInventoryParams{
		OrganizationID: principal.OrganizationID, SkipAuthorizationFilter: true, LimitValue: maxMemberMCPAccessServers + 1,
	})
	if err != nil {
		return GetMemberMCPAccessOutput{}, fmt.Errorf("list platform mcp access inventory: %w", err)
	}
	truncated := len(rows) > maxMemberMCPAccessServers
	if truncated {
		rows = rows[:maxMemberMCPAccessServers]
	}
	output := GetMemberMCPAccessOutput{Servers: make([]MemberMCPAccessRow, 0, len(rows)), Truncated: truncated}
	for _, row := range rows {
		name := conv.Default(row.McpName.String, row.McpSlug.String)
		if name == "" {
			name = row.McpServerID.String()
		}
		effective, err := access.EvaluateEffectiveMCPAccess(grants, row.McpServerID.String(), row.ProjectID.String())
		if err != nil {
			return GetMemberMCPAccessOutput{}, fmt.Errorf("evaluate member access to MCP %q: %w", name, err)
		}
		//nolint:exhaustruct // Access mode helpers only consume endpoint-kind fields.
		item := platformrepo.GetPlatformMCPInventoryItemRow{
			Visibility: row.Visibility, RemoteMcpServerID: row.RemoteMcpServerID, TunneledMcpServerID: row.TunneledMcpServerID,
			ToolsetID: row.ToolsetID, UnproxiedMcpServerID: row.UnproxiedMcpServerID,
		}
		mode := accessAuthorizationMode(item)
		canConnect, toolAccess, connectionAccess := effective.CanConnect, effective.ToolAccess, effective.ToolAccess+"_tools"
		switch mode {
		case "public_bypass":
			canConnect, toolAccess, connectionAccess = true, "all", "public"
		case "disabled":
			canConnect, toolAccess, connectionAccess = false, "none", "disabled"
		case "not_served":
			canConnect, toolAccess, connectionAccess = false, "none", "not_served"
		case "rbac":
			if !canConnect {
				toolAccess, connectionAccess = "none", "none"
			}
		}
		output.Servers = append(output.Servers, MemberMCPAccessRow{
			ProjectID: row.ProjectID.String(), ProjectName: row.ProjectName, MCPID: row.McpServerID.String(), MCPName: name,
			Backend: accessBackend(item), Visibility: row.Visibility, AuthorizationMode: mode, ConnectionAccess: connectionAccess,
			CanConnect: canConnect, ToolAccess: toolAccess, CanView: effective.CanView, CanManage: effective.CanManage,
		})
	}
	sort.Slice(output.Servers, func(i, j int) bool {
		left, right := output.Servers[i], output.Servers[j]
		if left.ProjectName != right.ProjectName {
			return left.ProjectName < right.ProjectName
		}
		if left.MCPName != right.MCPName {
			return left.MCPName < right.MCPName
		}
		return left.MCPID < right.MCPID
	})
	return output, nil
}

func (s *AccessReadService) GetMCPAccess(ctx context.Context, principal Principal, input GetMCPAccessInput) (GetMCPAccessOutput, error) {
	if !s.valid() {
		return GetMCPAccessOutput{}, ErrUnavailable
	}
	if err := s.budget.Allow(ctx, principal); err != nil {
		return GetMCPAccessOutput{}, err
	}
	projectID, err := uuid.Parse(input.ProjectID)
	if err != nil {
		return GetMCPAccessOutput{}, ErrAccessMCPNotFound
	}
	mcpID, err := uuid.Parse(input.MCPID)
	if err != nil {
		return GetMCPAccessOutput{}, ErrAccessMCPNotFound
	}
	row, err := platformrepo.New(s.db).GetPlatformMCPInventoryItem(ctx, platformrepo.GetPlatformMCPInventoryItemParams{
		OrganizationID:       principal.OrganizationID,
		ConnectionID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ConnectionGeneration: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserID:               inventoryText(principal.UserID),
		ActingSurface:        inventoryText(string(principal.surface())),
		McpServerID:          mcpID,
		ProjectID:            projectID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return GetMCPAccessOutput{}, ErrAccessMCPNotFound
	}
	if err != nil {
		return GetMCPAccessOutput{}, fmt.Errorf("get platform mcp access target: %w", err)
	}

	tools, catalog, truncated, err := s.accessTools(ctx, row)
	if err != nil {
		return GetMCPAccessOutput{}, err
	}
	name := row.McpName.String
	if name == "" {
		name = row.McpSlug.String
	}
	if name == "" {
		name = row.McpServerID.String()
	}
	target := MCPAccessTarget{
		ID:                   row.McpServerID.String(),
		Name:                 name,
		Backend:              accessBackend(row),
		Visibility:           row.Visibility,
		AuthorizationMode:    accessAuthorizationMode(row),
		AuthorizationSurface: "configured_endpoint",
		AccessSummary:        accessSummary(row),
		ToolCatalog:          catalog,
		Tools:                tools,
		ToolsTruncated:       truncated,
	}

	if target.AuthorizationMode != "rbac" {
		return GetMCPAccessOutput{
			ProjectID: projectID.String(),
			MCP:       target,
			Roles:     []MCPRoleCoverage{},
			ExpiresAt: "",
		}, nil
	}

	roles, err := s.roles.ListRoles(ctx, principal.OrganizationID)
	if err != nil {
		return GetMCPAccessOutput{}, fmt.Errorf("list roles for platform mcp access: %w", err)
	}
	principals := make([]string, 0, len(roles.Roles))
	for _, role := range roles.Roles {
		principals = append(principals, role.PrincipalUrn)
	}
	matches, err := plugins.RolePluginsForResource(ctx, s.db, authz.NewEngine(s.logger, s.db, nil, nil), principal.OrganizationID, projectID, mcpID, principals)
	if err != nil {
		return GetMCPAccessOutput{}, fmt.Errorf("read MCP role plugins: %w", err)
	}
	byRole := make(map[string][]MCPMatchingPlugin)
	if matches != nil {
		for _, role := range roles.Roles {
			byRole[role.PrincipalUrn] = []MCPMatchingPlugin{}
		}
	}
	for _, match := range matches {
		byRole[match.PrincipalUrn] = append(byRole[match.PrincipalUrn], MCPMatchingPlugin{PluginID: match.PluginID.String(), Name: match.Name, Slug: match.Slug})
	}
	now := s.now().UTC()
	output := GetMCPAccessOutput{
		ProjectID: projectID.String(),
		MCP:       target,
		Roles:     make([]MCPRoleCoverage, 0, len(roles.Roles)),
		ExpiresAt: now.Add(SubjectReferenceTTL).Format(time.RFC3339),
	}
	for _, role := range roles.Roles {
		grants, unevaluated := roleAuthzGrants(role)
		serverCheck := authz.MCPCheck(authz.ScopeMCPConnect, row.McpServerID.String(), projectID.String())
		canEnter, err := authz.GrantsAuthorize(grants, serverCheck)
		if err != nil {
			return GetMCPAccessOutput{}, fmt.Errorf("evaluate server access for role %q: %w", role.Name, err)
		}
		allowedTools := make([]string, 0, len(tools))
		for _, tool := range tools {
			allowed, err := authz.GrantsAuthorize(grants, authz.MCPToolCallCheck(row.McpServerID.String(), authz.MCPToolCallDimensions{
				Tool: tool.Name, Disposition: tool.Disposition, ProjectID: projectID.String(),
			}))
			if err != nil {
				return GetMCPAccessOutput{}, fmt.Errorf("evaluate tool access for role %q: %w", role.Name, err)
			}
			if allowed {
				allowedTools = append(allowedTools, tool.Name)
			}
		}
		dispositions, blockedDispositions := matchingDispositionRules(role.Grants, row.McpServerID.String(), projectID.String())
		if !canEnter && len(allowedTools) == 0 && len(dispositions) == 0 && len(blockedDispositions) == 0 && !unevaluated {
			continue
		}
		reference, err := s.references.Encode(principal, subjectKindAccessRole, role.ID, now)
		if err != nil {
			return GetMCPAccessOutput{}, fmt.Errorf("issue MCP access role reference: %w", err)
		}
		version, err := accessRoleVersion(s.versionKey, role)
		if err != nil {
			return GetMCPAccessOutput{}, fmt.Errorf("version MCP access role: %w", err)
		}
		output.Roles = append(output.Roles, MCPRoleCoverage{
			MatchingPlugins:     byRole[role.PrincipalUrn],
			Name:                role.Name,
			Type:                accessRoleType(role),
			MemberCount:         NewSubjectCount(int64(role.MemberCount)),
			Reference:           reference,
			Version:             version,
			CanEnterServer:      canEnter,
			KnownToolAccess:     knownToolAccess(len(allowedTools), len(tools), catalog, truncated),
			AllowedKnownTools:   allowedTools,
			DispositionRules:    dispositions,
			BlockedDispositions: blockedDispositions,
			UnevaluatedGrants:   unevaluated,
			AssignmentRules:     accessRoleAssignmentRules(role, projectID.String(), row.McpServerID.String()),
			AssignmentEligible:  accessRoleAssignmentEligible(role, projectID.String(), row.McpServerID.String()),
		})
	}
	return output, nil
}

func (s *AccessReadService) accessTools(ctx context.Context, row platformrepo.GetPlatformMCPInventoryItemRow) ([]MCPAccessTool, string, bool, error) {
	var tools []MCPAccessTool
	catalog := "dynamic"
	switch {
	case row.ToolsetID.Valid:
		toolset, err := toolsetsrepo.New(s.db).GetToolsetByIDAndProject(ctx, toolsetsrepo.GetToolsetByIDAndProjectParams{ID: row.ToolsetID.UUID, ProjectID: row.ProjectID})
		if err != nil {
			return nil, "", false, fmt.Errorf("get hosted MCP toolset: %w", err)
		}
		entries, err := mv.DescribeToolsetEntries(ctx, s.logger, s.db, mv.ProjectID(row.ProjectID), []toolsetsrepo.Toolset{toolset})
		if err != nil {
			return nil, "", false, fmt.Errorf("describe hosted MCP tools: %w", err)
		}
		catalog = "authoritative"
		if len(entries) > 0 {
			for _, tool := range entries[0].Tools {
				if tool == nil || tool.Name == "" {
					continue
				}
				tools = append(tools, MCPAccessTool{Name: tool.Name, Disposition: conv.DispositionFromAnnotations(tool.Annotations)})
			}
		}
	case row.RemoteMcpServerID.Valid || row.TunneledMcpServerID.Valid:
		rows, err := mcpserversrepo.New(s.db).ListMCPServerToolMetadata(ctx, mcpserversrepo.ListMCPServerToolMetadataParams{
			McpServerID: row.McpServerID, ProjectID: row.ProjectID, IncludeDeleted: false,
		})
		if err != nil {
			return nil, "", false, fmt.Errorf("list configured MCP tool metadata: %w", err)
		}
		if len(rows) > 0 {
			catalog = "stored_metadata"
		}
		for _, tool := range rows {
			tools = append(tools, MCPAccessTool{
				Name: tool.ToolName,
				Disposition: conv.DispositionFromAnnotations(conv.AnnotationsFromColumns(
					tool.ReadOnlyHint, tool.DestructiveHint, tool.IdempotentHint, tool.OpenWorldHint,
				)),
			})
		}
	case row.UnproxiedMcpServerID.Valid:
		catalog = "unavailable"
	}
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].Name == tools[j].Name {
			return tools[i].Disposition < tools[j].Disposition
		}
		return tools[i].Name < tools[j].Name
	})
	tools = omitAmbiguousAccessTools(tools)
	truncated := len(tools) > maxAccessTools
	if truncated {
		tools = tools[:maxAccessTools]
	}
	return tools, catalog, truncated, nil
}

func accessRoleType(role *accessgen.Role) string {
	if role.IsSystem {
		return "system"
	}
	return "custom"
}

func summarizeMCPConnect(grants []*accessgen.RoleGrant) MCPConnectSummary {
	summary := MCPConnectSummary{AllServers: false, ProjectRules: 0, ServerRules: 0, ToolRules: 0, DispositionRules: []string{}, BlockedServers: false, BlockedProjectRules: 0, BlockedServerRules: 0, BlockedToolRules: 0, BlockedDispositionRules: []string{}}
	dispositions := map[string]struct{}{}
	blockedDispositions := map[string]struct{}{}
	for _, grant := range grants {
		if grant == nil || (!scopeMayAllowConnect(grant.Scope) && !scopeMayBlockConnect(grant.Scope)) {
			continue
		}
		blocked := scopeMayBlockConnect(grant.Scope)
		if grant.Scope == string(authz.ScopeRoot) || grant.Selectors == nil {
			if blocked {
				summary.BlockedServers = true
			} else {
				summary.AllServers = true
			}
			continue
		}
		for _, selector := range grant.Selectors {
			if selector == nil {
				continue
			}
			if selector.ProjectID != nil {
				if blocked {
					summary.BlockedProjectRules++
				} else {
					summary.ProjectRules++
				}
			}
			if selector.ResourceID != authz.WildcardResource {
				if blocked {
					summary.BlockedServerRules++
				} else {
					summary.ServerRules++
				}
			}
			if selector.Tool != nil {
				if blocked {
					summary.BlockedToolRules++
				} else {
					summary.ToolRules++
				}
			}
			if selector.Disposition != nil && *selector.Disposition != "" {
				if blocked {
					blockedDispositions[*selector.Disposition] = struct{}{}
				} else {
					dispositions[*selector.Disposition] = struct{}{}
				}
			}
			if selector.ResourceID == authz.WildcardResource && selector.ProjectID == nil && selector.Tool == nil && selector.Disposition == nil {
				if blocked {
					summary.BlockedServers = true
				} else {
					summary.AllServers = true
				}
			}
		}
	}
	for disposition := range dispositions {
		summary.DispositionRules = append(summary.DispositionRules, disposition)
	}
	for disposition := range blockedDispositions {
		summary.BlockedDispositionRules = append(summary.BlockedDispositionRules, disposition)
	}
	slices.Sort(summary.DispositionRules)
	slices.Sort(summary.BlockedDispositionRules)
	return summary
}

func roleAuthzGrants(role *accessgen.Role) ([]authz.Grant, bool) {
	grants := make([]authz.Grant, 0)
	if role == nil {
		return grants, false
	}
	unevaluated := false
	for _, grant := range role.Grants {
		if grant == nil {
			continue
		}
		scope := authz.Scope(grant.Scope)
		if grant.Selectors == nil {
			grants = append(grants, authz.Grant{PrincipalUrn: role.PrincipalUrn, Scope: scope, Selector: authz.NewSelector(scope, authz.WildcardResource)})
			continue
		}
		for _, selector := range grant.Selectors {
			if selector == nil {
				continue
			}
			converted := authz.Selector{
				authz.SelectorKeyResourceKind: selector.ResourceKind,
				authz.SelectorKeyResourceID:   selector.ResourceID,
			}
			if selector.ProjectID != nil {
				converted[authz.SelectorKeyProjectID] = *selector.ProjectID
			}
			if selector.Tool != nil {
				converted[authz.SelectorKeyTool] = *selector.Tool
			}
			if selector.Disposition != nil {
				converted[authz.SelectorKeyDisposition] = *selector.Disposition
			}
			if err := authz.ValidateSelector(scope, converted); err != nil {
				unevaluated = true
				continue
			}
			grants = append(grants, authz.Grant{PrincipalUrn: role.PrincipalUrn, Scope: scope, Selector: converted})
		}
	}
	return grants, unevaluated
}

func matchingDispositionRules(grants []*accessgen.RoleGrant, mcpID, projectID string) ([]string, []string) {
	found := map[string]struct{}{}
	blocked := map[string]struct{}{}
	for _, grant := range grants {
		if grant == nil || (!scopeMayAllowConnect(grant.Scope) && !scopeMayBlockConnect(grant.Scope)) {
			continue
		}
		for _, selector := range grant.Selectors {
			if selector == nil || selector.Disposition == nil || *selector.Disposition == "" {
				continue
			}
			if selector.ResourceID != authz.WildcardResource && selector.ResourceID != mcpID {
				continue
			}
			if selector.ProjectID != nil && *selector.ProjectID != authz.WildcardResource && *selector.ProjectID != projectID {
				continue
			}
			if scopeMayBlockConnect(grant.Scope) {
				blocked[*selector.Disposition] = struct{}{}
			} else {
				found[*selector.Disposition] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(found))
	for value := range found {
		out = append(out, value)
	}
	blockedOut := make([]string, 0, len(blocked))
	for value := range blocked {
		blockedOut = append(blockedOut, value)
	}
	slices.Sort(out)
	slices.Sort(blockedOut)
	return out, blockedOut
}

func scopeMayAllowConnect(scope string) bool {
	switch authz.Scope(scope) {
	case authz.ScopeRoot, authz.ScopeMCPConnect, authz.ScopeMCPRead, authz.ScopeMCPWrite:
		return true
	default:
		return false
	}
}

func scopeMayBlockConnect(scope string) bool {
	switch authz.Scope(scope) {
	case authz.ScopeMCPBlockedConnect, authz.ScopeMCPBlockedRead, authz.ScopeMCPBlockedWrite:
		return true
	default:
		return false
	}
}

func omitAmbiguousAccessTools(tools []MCPAccessTool) []MCPAccessTool {
	result := make([]MCPAccessTool, 0, len(tools))
	for start := 0; start < len(tools); {
		end := start + 1
		for end < len(tools) && tools[end].Name == tools[start].Name {
			end++
		}
		if end-start == 1 {
			result = append(result, tools[start])
		}
		start = end
	}
	return result
}

func accessBackend(row platformrepo.GetPlatformMCPInventoryItemRow) string {
	switch {
	case row.RemoteMcpServerID.Valid:
		return "remote"
	case row.TunneledMcpServerID.Valid:
		return "tunneled"
	case row.ToolsetID.Valid:
		return "hosted"
	case row.UnproxiedMcpServerID.Valid:
		return "unproxied"
	default:
		return "legacy"
	}
}

func accessAuthorizationMode(row platformrepo.GetPlatformMCPInventoryItemRow) string {
	if row.Visibility == "disabled" {
		return "disabled"
	}
	if row.UnproxiedMcpServerID.Valid {
		return "not_served"
	}
	switch row.Visibility {
	case "public":
		return "public_bypass"
	case "private":
		return "rbac"
	default:
		return "not_served"
	}
}

func accessSummary(row platformrepo.GetPlatformMCPInventoryItemRow) string {
	switch accessAuthorizationMode(row) {
	case "public_bypass":
		return "everyone"
	case "disabled", "not_served":
		return "nobody"
	default:
		return "by_role"
	}
}

func knownToolAccess(allowed, total int, catalog string, truncated bool) string {
	if total == 0 {
		if catalog == "dynamic" || catalog == "unavailable" {
			return "not_enumerable"
		}
		return "none"
	}
	switch allowed {
	case 0:
		return "none"
	case total:
		if truncated {
			return "some_known_tools"
		}
		return "all"
	default:
		return "some"
	}
}

func normalizeAccessQuery(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}
