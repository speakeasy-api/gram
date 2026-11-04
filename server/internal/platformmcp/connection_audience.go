package platformmcp

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/oops"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const operationSetMCPConnectionAudience = "set_mcp_connection_audience"

// MCPConnectionAudience is who may connect to one server: the connect rules
// naming it, and how many members they reach once blocks and overrides are
// applied. Members are counted, never named.
type MCPConnectionAudience struct {
	Version  string                      `json:"version" jsonschema:"pass as expected_version to set_mcp_connection_audience"`
	Everyone string                      `json:"everyone" jsonschema:"this server's own connect rule for every organization member: allowed, blocked, or not_granted. A rule covering every server can still let everyone connect; list_access_roles reports those"`
	Roles    []MCPConnectionAudienceRole `json:"roles" jsonschema:"roles with their own connect rule naming this server"`
	// NamedMembers counts members given their own connect rule on this server.
	NamedMembers            SubjectCount `json:"named_members" jsonschema:"members with their own connect rule naming this server; identities are withheld"`
	MembersCanConnect       SubjectCount `json:"members_can_connect" jsonschema:"members whose rules let them connect, including those connecting over a block"`
	MembersConnectOverBlock SubjectCount `json:"members_connect_over_block" jsonschema:"members who connect because a rule naming them on this server outranks a block they inherit from a role or everyone"`
	MembersBlocked          SubjectCount `json:"members_blocked" jsonschema:"members a block stops from connecting"`
}

// MCPConnectionAudienceRole is one role's own connect rule on one server.
type MCPConnectionAudienceRole struct {
	Name         string `json:"name"`
	Reference    string `json:"reference" jsonschema:"opaque role reference for set_mcp_connection_audience"`
	Access       string `json:"access" jsonschema:"the role's whole-server connect rule here: allowed, blocked, or not_granted"`
	ToolsLimited bool   `json:"tools_limited" jsonschema:"true when the role also has a connect rule here limited to some tools or tool annotations; change those in the dashboard"`
}

// connectRuleFor reports one principal's whole-server connect rule among the
// rules naming a server, and whether a narrower connect rule also names it.
// A whole-server block wins over a grant held at the same time, as it does at
// runtime.
func connectRuleFor(rules []*accessgen.SetResourceAudienceEntry, principalURN string) (string, bool) {
	rule, limited := access.ConnectRuleNotGranted, false
	for _, entry := range rules {
		if entry.PrincipalUrn != principalURN || (entry.Level != "use" && entry.Level != "blocked") {
			continue
		}
		if len(entry.Tools) > 0 || len(entry.Dispositions) > 0 {
			limited = true
			continue
		}
		switch {
		case entry.Level == "blocked":
			rule = access.ConnectRuleBlocked
		case rule != access.ConnectRuleBlocked:
			rule = access.ConnectRuleAllowed
		}
	}
	return rule, limited
}

// connectionAudience renders a server's connection audience for one caller,
// issuing role references bound to them.
func (s *AccessReadService) connectionAudience(principal Principal, audience access.ResourceConnectionAudience, roles []*accessgen.Role, now time.Time) (MCPConnectionAudience, error) {
	everyone, _ := connectRuleFor(audience.Rules, authz.AllUsersPrincipal().String())
	output := MCPConnectionAudience{
		Version:                 audience.Version,
		Everyone:                everyone,
		Roles:                   make([]MCPConnectionAudienceRole, 0),
		NamedMembers:            NewSubjectCount(0),
		MembersCanConnect:       NewSubjectCount(0),
		MembersConnectOverBlock: NewSubjectCount(0),
		MembersBlocked:          NewSubjectCount(0),
	}

	for _, role := range roles {
		rule, limited := connectRuleFor(audience.Rules, role.PrincipalUrn)
		if rule == access.ConnectRuleNotGranted && !limited {
			continue
		}
		reference, err := s.references.Encode(principal, subjectKindAccessRole, role.ID, now)
		if err != nil {
			return MCPConnectionAudience{}, errors.Join(errors.New("issue connection audience role reference"), err)
		}
		output.Roles = append(output.Roles, MCPConnectionAudienceRole{Name: role.Name, Reference: reference, Access: rule, ToolsLimited: limited})
	}
	slices.SortFunc(output.Roles, func(a, b MCPConnectionAudienceRole) int { return strings.Compare(a.Name, b.Name) })

	named := make(map[string]struct{})
	for _, entry := range audience.Rules {
		if (entry.Level == "use" || entry.Level == "blocked") && strings.HasPrefix(entry.PrincipalUrn, string(urn.PrincipalTypeUser)+":") && entry.PrincipalUrn != authz.AllUsersPrincipal().String() {
			named[entry.PrincipalUrn] = struct{}{}
		}
	}
	output.NamedMembers = NewSubjectCount(int64(len(named)))

	if audience.Audience != nil {
		var canConnect, overBlock, blocked int64
		for _, member := range audience.Audience.Members {
			switch member.Decision {
			case "allowed":
				canConnect++
			case "allowed_over_block":
				canConnect++
				overBlock++
			case "blocked":
				blocked++
			}
		}
		output.MembersCanConnect = NewSubjectCount(canConnect)
		output.MembersConnectOverBlock = NewSubjectCount(overBlock)
		output.MembersBlocked = NewSubjectCount(blocked)
	}
	return output, nil
}

// readConnectionAudience reads and renders one server's connection audience.
func (s *AccessReadService) readConnectionAudience(ctx context.Context, principal Principal, mcpID string, roles []*accessgen.Role, now time.Time) (MCPConnectionAudience, error) {
	audience, err := s.roles.ReadResourceConnectionAudience(ctx, principal.OrganizationID, mcpID)
	if err != nil {
		return MCPConnectionAudience{}, errors.Join(errors.New("read MCP connection audience"), err)
	}
	return s.connectionAudience(principal, audience, roles, now)
}

type SetMCPConnectionAudienceInput struct {
	ProjectID       string `json:"project_id" jsonschema:"explicit project ID that owns the configured MCP"`
	MCPID           string `json:"mcp_id" jsonschema:"configured MCP ID returned by find_mcp"`
	Audience        string `json:"audience" jsonschema:"who the rule is for: everyone, role, or member"`
	RoleReference   string `json:"role_reference,omitempty" jsonschema:"for audience role: opaque role reference returned by list_access_roles or get_mcp_access"`
	MemberReference string `json:"member_reference,omitempty" jsonschema:"for audience member: opaque member reference returned by list_access_members"`
	Access          string `json:"access" jsonschema:"allowed, blocked, or not_granted"`
	ExpectedVersion string `json:"expected_version" jsonschema:"connection_audience.version from the immediately preceding get_mcp_access response"`
	Confirmed       bool   `json:"confirmed" jsonschema:"set true only after the user confirms this exact audience, server, and access"`
}

type SetMCPConnectionAudienceOutput struct {
	ConnectionAudience MCPConnectionAudience `json:"connection_audience" jsonschema:"the server's connection audience after the change"`
	ExpiresAt          string                `json:"expires_at"`
}

// SetConnectionAudience sets one audience's whole-server connect rule on one
// server through the same save, guards and audit entry as the dashboard's
// server access editor, and leaves every other rule naming the server alone.
func (s *AccessRoleMutationService) SetConnectionAudience(ctx context.Context, principal Principal, input SetMCPConnectionAudienceInput) (SetMCPConnectionAudienceOutput, error) {
	if !input.Confirmed {
		return SetMCPConnectionAudienceOutput{}, &AccessRoleMutationError{Code: "confirmation_required", Message: "Show the server, who the rule is for, and the access it will have, including that a block on everyone or a role also stops administrators who hold it unless they are named on the server; ask the user to confirm, then retry with confirmed: true.", Cause: ErrAccessRoleMutationInvalid}
	}
	if !s.valid() {
		return SetMCPConnectionAudienceOutput{}, accessRoleMutationUnavailable(nil)
	}
	rule := strings.TrimSpace(input.Access)
	if rule != access.ConnectRuleAllowed && rule != access.ConnectRuleBlocked && rule != access.ConnectRuleNotGranted {
		return SetMCPConnectionAudienceOutput{}, accessRoleMutationInvalid("access must be allowed, blocked, or not_granted.")
	}
	expectedVersion := strings.TrimSpace(input.ExpectedVersion)
	if expectedVersion == "" {
		return SetMCPConnectionAudienceOutput{}, accessRoleMutationInvalid("expected_version is required. Read get_mcp_access and pass its connection_audience.version.")
	}
	project, _, err := s.admit(ctx, principal, input.ProjectID)
	if err != nil {
		return SetMCPConnectionAudienceOutput{}, err
	}
	mcpID, err := uuid.Parse(strings.TrimSpace(input.MCPID))
	if err != nil {
		return SetMCPConnectionAudienceOutput{}, accessRoleMutationNotFound()
	}
	now := s.reads.now()

	roles, err := s.reads.roles.ListRoles(ctx, principal.OrganizationID)
	if err != nil {
		return SetMCPConnectionAudienceOutput{}, accessRoleMutationUnavailable(err)
	}
	var target urn.Principal
	switch strings.TrimSpace(input.Audience) {
	case "everyone":
		target = authz.AllUsersPrincipal()
	case "role":
		roleID, err := s.reads.references.Decode(input.RoleReference, principal, subjectKindAccessRole, now)
		if err != nil {
			return SetMCPConnectionAudienceOutput{}, accessRoleMutationNotFound()
		}
		index := slices.IndexFunc(roles.Roles, func(role *accessgen.Role) bool { return role.ID == roleID })
		if index < 0 {
			return SetMCPConnectionAudienceOutput{}, accessRoleMutationNotFound()
		}
		target, err = urn.ParsePrincipal(roles.Roles[index].PrincipalUrn)
		if err != nil {
			return SetMCPConnectionAudienceOutput{}, accessRoleMutationUnavailable(err)
		}
	case "member":
		memberID, err := s.reads.references.Decode(input.MemberReference, principal, subjectKindAccessMember, now)
		if err != nil {
			return SetMCPConnectionAudienceOutput{}, accessRoleMutationNotFound()
		}
		target = urn.NewPrincipal(urn.PrincipalTypeUser, memberID)
	default:
		return SetMCPConnectionAudienceOutput{}, accessRoleMutationInvalid("audience must be everyone, role, or member.")
	}

	if _, err := platformrepo.New(s.reads.db).GetPlatformMCPInventoryItem(ctx, platformrepo.GetPlatformMCPInventoryItemParams{
		OrganizationID:       principal.OrganizationID,
		ConnectionID:         uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		ConnectionGeneration: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserID:               inventoryText(principal.UserID),
		ActingSurface:        inventoryText(string(principal.surface())),
		McpServerID:          mcpID,
		ProjectID:            project.ID,
	}); errors.Is(err, pgx.ErrNoRows) {
		return SetMCPConnectionAudienceOutput{}, accessRoleMutationNotFound()
	} else if err != nil {
		return SetMCPConnectionAudienceOutput{}, accessRoleMutationUnavailable(err)
	}

	if err := s.budget.AllowConnectionOrOrganization(ctx, principal); err != nil {
		if errors.Is(err, ErrOperationRateLimited) {
			return SetMCPConnectionAudienceOutput{}, &AccessRoleMutationError{Code: "rate_limited", Message: "The access role mutation rate limit was reached.", Cause: err}
		}
		return SetMCPConnectionAudienceOutput{}, accessRoleMutationUnavailable(err)
	}
	actor := access.RoleAuditActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID), DisplayName: nil}
	audience, err := s.backend.SetResourceConnectRule(ctx, principal.OrganizationID, actor, mcpID.String(), target, rule, expectedVersion)
	if err != nil {
		return SetMCPConnectionAudienceOutput{}, connectionAudienceMutationError(err)
	}

	output, err := s.reads.connectionAudience(principal, audience, roles.Roles, now)
	if err != nil {
		return SetMCPConnectionAudienceOutput{}, &AccessRoleMutationError{Code: unavailableCode, Message: "The rule was saved, but the server's current connection audience could not be read. Read get_mcp_access before retrying.", Cause: errors.Join(ErrAccessRoleMutationUnavailable, err)}
	}
	return SetMCPConnectionAudienceOutput{ConnectionAudience: output, ExpiresAt: now.Add(SubjectReferenceTTL).UTC().Format(time.RFC3339)}, nil
}

// connectionAudienceMutationError maps the access service's refusals onto the
// mutation refusal codes the other access tools use.
func connectionAudienceMutationError(err error) error {
	var shareable *oops.ShareableError
	if !errors.As(err, &shareable) {
		return accessRoleMutationUnavailable(err)
	}
	switch shareable.Code {
	case oops.CodeFailedPrecondition, oops.CodeConflict:
		return accessRoleMutationConflict("This server's access changed since it was read. Read get_mcp_access again and retry with the new connection_audience.version.")
	case oops.CodeNotFound:
		return accessRoleMutationNotFound()
	case oops.CodeInvalid, oops.CodeBadRequest:
		return accessRoleMutationInvalid(shareable.Error())
	default:
		return accessRoleMutationUnavailable(err)
	}
}
