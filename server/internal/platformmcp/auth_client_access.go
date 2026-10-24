package platformmcp

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authclients"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const operationSetMCPRoleAuthClientAccess = "set_mcp_role_auth_client_access"

var roleAuthClientAccessLevels = map[string]string{
	"allowed":     authclients.LevelUse,
	"blocked":     authclients.LevelBlocked,
	"not_granted": "",
}

type SetMCPRoleAuthClientAccessInput struct {
	ProjectID     string `json:"project_id" jsonschema:"explicit project ID that owns the configured MCP"`
	MCPID         string `json:"mcp_id" jsonschema:"configured MCP ID returned by find_mcp"`
	RoleReference string `json:"role_reference" jsonschema:"opaque role reference returned by list_access_roles or get_mcp_access"`
	AuthClientID  string `json:"auth_client_id" jsonschema:"shared credential ID returned for this role by get_mcp_access"`
	Access        string `json:"access" jsonschema:"allowed, blocked, or not_granted"`
	Confirmed     bool   `json:"confirmed" jsonschema:"set true only after the user confirms this exact role, credential, and access; it applies on every server that offers the credential"`
}

type SetMCPRoleAuthClientAccessOutput struct {
	Role       string                  `json:"role"`
	AuthClient MCPRoleAuthClientAccess `json:"auth_client" jsonschema:"the role's effective access to the credential after the change; rules on every credential or credential type can still allow or block it"`
}

// WithAuthClientAudit lets role mutations change which shared credentials a
// role may use. Without it the tool reports unavailable.
func (s *AccessRoleMutationService) WithAuthClientAudit(auditLogger *audit.Logger) *AccessRoleMutationService {
	if s != nil {
		s.audit = auditLogger
	}
	return s
}

// SetRoleAuthClientAccess writes the role's own Use or Blocked rule on one
// shared credential the server offers, through the same save the auth client
// audience editor uses.
func (s *AccessRoleMutationService) SetRoleAuthClientAccess(ctx context.Context, principal Principal, input SetMCPRoleAuthClientAccessInput) (SetMCPRoleAuthClientAccessOutput, error) {
	if !input.Confirmed {
		return SetMCPRoleAuthClientAccessOutput{}, &AccessRoleMutationError{Code: "confirmation_required", Message: "Show the role, the credential, and the access it will have on every server offering that credential; ask the user to confirm it, then retry with confirmed: true.", Cause: ErrAccessRoleMutationInvalid}
	}
	if !s.valid() || s.audit == nil {
		return SetMCPRoleAuthClientAccessOutput{}, accessRoleMutationUnavailable(nil)
	}
	level, ok := roleAuthClientAccessLevels[strings.TrimSpace(input.Access)]
	if !ok {
		return SetMCPRoleAuthClientAccessOutput{}, accessRoleMutationInvalid("access must be allowed, blocked, or not_granted.")
	}
	project, _, err := s.admit(ctx, principal, input.ProjectID)
	if err != nil {
		return SetMCPRoleAuthClientAccessOutput{}, err
	}
	mcpID, err := uuid.Parse(strings.TrimSpace(input.MCPID))
	if err != nil {
		return SetMCPRoleAuthClientAccessOutput{}, accessRoleMutationNotFound()
	}
	clientID, err := uuid.Parse(strings.TrimSpace(input.AuthClientID))
	if err != nil {
		return SetMCPRoleAuthClientAccessOutput{}, accessRoleMutationNotFound()
	}
	roleID, err := s.reads.references.Decode(input.RoleReference, principal, subjectKindAccessRole, s.reads.now())
	if err != nil {
		return SetMCPRoleAuthClientAccessOutput{}, accessRoleMutationNotFound()
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
		return SetMCPRoleAuthClientAccessOutput{}, accessRoleMutationNotFound()
	} else if err != nil {
		return SetMCPRoleAuthClientAccessOutput{}, accessRoleMutationUnavailable(err)
	}
	upstream, err := authclients.ReadServerAuthentication(ctx, s.reads.db, principal.OrganizationID, mcpID)
	if err != nil {
		return SetMCPRoleAuthClientAccessOutput{}, accessRoleMutationUnavailable(err)
	}
	offered := slices.ContainsFunc(upstream.Clients, func(client *types.AuthClient) bool { return client.ID == clientID.String() && !client.Personal })
	if !offered {
		return SetMCPRoleAuthClientAccessOutput{}, accessRoleMutationInvalid("That credential is not a shared credential this server offers. Choose one get_mcp_access returns for the role.")
	}

	roles, err := s.reads.roles.ListRoles(ctx, principal.OrganizationID)
	if err != nil {
		return SetMCPRoleAuthClientAccessOutput{}, accessRoleMutationUnavailable(err)
	}
	index := slices.IndexFunc(roles.Roles, func(role *accessgen.Role) bool { return role.ID == roleID })
	if index < 0 {
		return SetMCPRoleAuthClientAccessOutput{}, accessRoleMutationNotFound()
	}
	rolePrincipal, err := urn.ParsePrincipal(roles.Roles[index].PrincipalUrn)
	if err != nil {
		return SetMCPRoleAuthClientAccessOutput{}, accessRoleMutationUnavailable(err)
	}

	if err := s.budget.AllowConnectionOrOrganization(ctx, principal); err != nil {
		if errors.Is(err, ErrOperationRateLimited) {
			return SetMCPRoleAuthClientAccessOutput{}, &AccessRoleMutationError{Code: "rate_limited", Message: "The access role mutation rate limit was reached.", Cause: err}
		}
		return SetMCPRoleAuthClientAccessOutput{}, accessRoleMutationUnavailable(err)
	}
	if _, err := authclients.SetPrincipalLevel(ctx, s.reads.db, s.audit, principal.OrganizationID, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID), clientID, rolePrincipal, level); err != nil {
		if errors.Is(err, authclients.ErrAuthClientNotFound) || errors.Is(err, authclients.ErrPrincipalInvalid) {
			return SetMCPRoleAuthClientAccessOutput{}, accessRoleMutationNotFound()
		}
		return SetMCPRoleAuthClientAccessOutput{}, accessRoleMutationUnavailable(err)
	}

	// Report the effective result, which broader rules can still decide.
	updated, err := s.reads.roles.GetRoleByID(ctx, principal.OrganizationID, roleID)
	if err != nil {
		return SetMCPRoleAuthClientAccessOutput{}, fmt.Errorf("reload role after auth client change: %w", err)
	}
	grants, _ := roleAuthzGrants(updated)
	access, err := roleSharedAuthClientAccess(grants, upstream, project.ID.String())
	if err != nil {
		return SetMCPRoleAuthClientAccessOutput{}, fmt.Errorf("evaluate auth client access after change: %w", err)
	}
	index = slices.IndexFunc(access, func(entry MCPRoleAuthClientAccess) bool { return entry.AuthClientID == clientID.String() })
	if index < 0 {
		return SetMCPRoleAuthClientAccessOutput{}, accessRoleMutationNotFound()
	}
	return SetMCPRoleAuthClientAccessOutput{Role: updated.Name, AuthClient: access[index]}, nil
}
