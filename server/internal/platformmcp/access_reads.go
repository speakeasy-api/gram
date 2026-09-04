package platformmcp

import (
	"context"
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
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

const (
	AccessReadsConnectionLimitName   = "platform-mcp-access-reads-connection"
	AccessReadsOrganizationLimitName = "platform-mcp-access-reads-organization"

	AccessReadQueriesPerConnectionPerMinute   = 30
	AccessReadQueriesPerOrganizationPerMinute = 300

	maxAccessMembers     = 50
	maxAccessTools       = 100
	minAccessQueryLength = 3
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
	BlockedToolRules        int      `json:"blocked_tool_rules"`
	BlockedDispositionRules []string `json:"blocked_disposition_rules"`
}

type AccessRole struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	MemberCount SubjectCount      `json:"member_count"`
	MCPAccess   MCPConnectSummary `json:"mcp_access"`
	Reference   string            `json:"reference"`
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
}

type ListAccessMembersOutput struct {
	Members      []AccessMember `json:"members"`
	TotalMatches SubjectCount   `json:"total_matches"`
	Suppressed   bool           `json:"suppressed"`
	Truncated    bool           `json:"truncated"`
	ExpiresAt    string         `json:"expires_at,omitempty"`
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

type MCPRoleCoverage struct {
	Name                string       `json:"name"`
	Type                string       `json:"type"`
	MemberCount         SubjectCount `json:"member_count"`
	Reference           string       `json:"reference"`
	CanEnterServer      bool         `json:"can_enter_server"`
	KnownToolAccess     string       `json:"known_tool_access"`
	AllowedKnownTools   []string     `json:"allowed_known_tools"`
	DispositionRules    []string     `json:"disposition_rules"`
	BlockedDispositions []string     `json:"blocked_dispositions"`
	UnevaluatedGrants   bool         `json:"unevaluated_grants"`
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
}

func NewAccessReadService(logger *slog.Logger, db *pgxpool.Pool, budget OperationBudget, keyMaterial string) *AccessReadService {
	if logger == nil {
		return &AccessReadService{logger: nil, db: nil, roles: nil, budget: OperationBudget{Connection: nil, Organization: nil}, references: nil, now: nil}
	}
	references, err := newSubjectReferenceCodec(keyMaterial)
	if err != nil {
		logger.ErrorContext(context.Background(), "build Platform MCP access reference codec", attr.SlogError(err))
	}
	return &AccessReadService{
		logger:     logger,
		db:         db,
		roles:      access.NewRoleManager(logger, db, nil, nil),
		budget:     budget,
		references: references,
		now:        time.Now,
	}
}

func (s *AccessReadService) valid() bool {
	return s != nil && s.db != nil && s.roles != nil && s.budget.valid() && s.references != nil && s.now != nil
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
		output.Roles = append(output.Roles, AccessRole{
			Name:        role.Name,
			Type:        accessRoleType(role),
			MemberCount: NewSubjectCount(int64(role.MemberCount)),
			MCPAccess:   summarizeMCPConnect(role.Grants),
			Reference:   reference,
		})
	}
	return output, nil
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

	members, err := s.roles.ListMembers(ctx, principal.OrganizationID)
	if err != nil {
		return ListAccessMembersOutput{}, fmt.Errorf("list platform mcp access members: %w", err)
	}
	matching := make([]*accessgen.AccessMember, 0, len(members.Members))
	for _, member := range members.Members {
		if roleID != "" && !slices.Contains(member.RoleIds, roleID) {
			continue
		}
		if query != "" && !matchesAccessMember(member, query, roleNames) {
			continue
		}
		matching = append(matching, member)
	}

	count := len(matching)
	output := ListAccessMembersOutput{
		Members:      []AccessMember{},
		TotalMatches: NewSubjectCount(int64(count)),
		Suppressed:   count > 0 && count < SubjectSuppressionThreshold,
		Truncated:    false,
		ExpiresAt:    "",
	}
	// Organization-privacy rules do not allow a row-bearing response to reveal
	// a filtered cohort smaller than five. Return only the suppressed count.
	if output.Suppressed || count == 0 {
		return output, nil
	}

	limit := maxAccessMembers
	if input.Limit > 0 {
		limit = min(input.Limit, maxAccessMembers)
	}
	if len(matching) > limit {
		matching = matching[:limit]
		output.Truncated = true
	}
	now := s.now().UTC()
	output.ExpiresAt = now.Add(SubjectReferenceTTL).Format(time.RFC3339)
	for _, member := range matching {
		reference, err := s.references.Encode(principal, subjectKindAccessMember, member.ID, now)
		if err != nil {
			return ListAccessMembersOutput{}, fmt.Errorf("issue access member reference: %w", err)
		}
		names := make([]string, 0, len(member.RoleIds))
		for _, id := range member.RoleIds {
			if name := roleNames[id]; name != "" {
				names = append(names, name)
			}
		}
		slices.Sort(names)
		output.Members = append(output.Members, AccessMember{
			MaskedIdentity: maskAccessMember(member),
			Roles:          slices.Compact(names),
			Reference:      reference,
		})
	}
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
		output.Roles = append(output.Roles, MCPRoleCoverage{
			Name:                role.Name,
			Type:                accessRoleType(role),
			MemberCount:         NewSubjectCount(int64(role.MemberCount)),
			Reference:           reference,
			CanEnterServer:      canEnter,
			KnownToolAccess:     knownToolAccess(len(allowedTools), len(tools), catalog, truncated),
			AllowedKnownTools:   allowedTools,
			DispositionRules:    dispositions,
			BlockedDispositions: blockedDispositions,
			UnevaluatedGrants:   unevaluated,
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
	summary := MCPConnectSummary{AllServers: false, ProjectRules: 0, ServerRules: 0, ToolRules: 0, DispositionRules: []string{}, BlockedServers: false, BlockedToolRules: 0, BlockedDispositionRules: []string{}}
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
			if selector.ProjectID != nil && !blocked {
				summary.ProjectRules++
			}
			if selector.ResourceID != authz.WildcardResource && !blocked {
				summary.ServerRules++
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
	if row.Visibility == "public" {
		return "public_bypass"
	}
	if row.UnproxiedMcpServerID.Valid {
		return "not_served"
	}
	return "rbac"
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

func matchesAccessMember(member *accessgen.AccessMember, query string, roleNames map[string]string) bool {
	if strings.Contains(normalizeAccessQuery(member.Name), query) || strings.Contains(normalizeAccessQuery(member.Email), query) {
		return true
	}
	for _, roleID := range member.RoleIds {
		if strings.Contains(normalizeAccessQuery(roleNames[roleID]), query) {
			return true
		}
	}
	return false
}

func maskAccessMember(member *accessgen.AccessMember) string {
	if member == nil {
		return ""
	}
	if member.Email != "" {
		return maskSubject(member.Email)
	}
	return maskSubject(member.Name)
}
