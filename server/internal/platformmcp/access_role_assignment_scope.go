package platformmcp

import (
	"context"
	"crypto/hmac"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
)

func (s *AccessRoleAssignmentService) validateAssignmentRole(ctx context.Context, tx pgx.Tx, principal Principal, project ResolvedProject, roleID string, input AssignMCPAccessRoleInput) error {
	mcpID, err := uuid.Parse(input.MCPID)
	if err != nil || mcpID == uuid.Nil || !validAccessRoleVersion(input.ExpectedRoleVersion) {
		return accessRoleMutationInvalid("Fresh assignment requires the selected MCP and its role version. Read access again and confirm the complete role scope.")
	}
	row, err := platformrepo.New(tx).GetPlatformMCPInventoryItem(ctx, platformrepo.GetPlatformMCPInventoryItemParams{
		OrganizationID: principal.OrganizationID, ConnectionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ConnectionGeneration: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserID: inventoryText(principal.UserID), ActingSurface: inventoryText(string(principal.surface())), McpServerID: mcpID, ProjectID: project.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return accessRoleMutationNotFound()
	}
	if err != nil {
		return accessRoleMutationUnavailable(err)
	}
	if accessAuthorizationMode(row) != "rbac" {
		return accessRoleMutationInvalid("The selected MCP must use role-based access.")
	}
	roleUUID, err := uuid.Parse(roleID)
	if err != nil {
		return accessRoleMutationNotFound()
	}
	if _, err := accessrepo.New(tx).LockOrganizationRoleByID(ctx, accessrepo.LockOrganizationRoleByIDParams{OrganizationID: principal.OrganizationID, ID: roleUUID}); errors.Is(err, pgx.ErrNoRows) {
		return accessRoleMutationNotFound()
	} else if err != nil {
		return accessRoleMutationUnavailable(err)
	}
	role, err := s.roles.backend.GetRoleByIDTx(ctx, tx, principal.OrganizationID, roleID)
	if err != nil {
		return classifyAccessRoleBackendError(err)
	}
	if role == nil || role.IsSystem {
		return accessRoleMutationNotFound()
	}
	version, err := s.roles.roleVersion(role)
	if err != nil {
		return accessRoleMutationUnavailable(err)
	}
	if !hmac.Equal([]byte(version), []byte(input.ExpectedRoleVersion)) {
		return accessRoleMutationConflict("The role changed after confirmation. Read its complete access again and reconfirm.")
	}
	if !accessRoleAssignmentEligible(role, project.ID.String(), row.McpServerID.String()) {
		return accessRoleMutationInvalid("The role includes broader or unrecognized access. Select a dedicated role limited to this MCP; do not narrow a shared role automatically.")
	}
	return nil
}

type AccessRoleAssignmentRule struct {
	AllTools    bool   `json:"all_tools"`
	Tool        string `json:"tool,omitempty"`
	Disposition string `json:"disposition,omitempty"`
}

func accessRoleAssignmentRules(role *accessgen.Role, projectID, mcpID string) []AccessRoleAssignmentRule {
	if !accessRoleAssignmentEligible(role, projectID, mcpID) {
		return nil
	}
	rules := make([]AccessRoleAssignmentRule, 0)
	for _, grant := range role.Grants {
		for _, selector := range grant.Selectors {
			rule := AccessRoleAssignmentRule{AllTools: false, Tool: "", Disposition: ""}
			if selector.Tool != nil {
				rule.Tool = *selector.Tool
				if rule.Tool == authz.WildcardResource {
					rule.Tool = ""
				}
			}
			if selector.Disposition != nil {
				rule.Disposition = *selector.Disposition
			}
			rule.AllTools = rule.Tool == "" && rule.Disposition == ""
			rules = append(rules, rule)
		}
	}
	return rules
}

// Only exact, enumerable MCP grants are safe to confirm through this workflow.
// A partial MCP summary must never hide additional non-MCP or wildcard access.
func accessRoleAssignmentEligible(role *accessgen.Role, projectID, mcpID string) bool {
	if role == nil || role.IsSystem || len(role.Grants) == 0 || projectID == "" || mcpID == "" {
		return false
	}
	for _, grant := range role.Grants {
		if grant == nil || grant.Scope != string(authz.ScopeMCPConnect) || len(grant.Selectors) == 0 {
			return false
		}
		for _, selector := range grant.Selectors {
			if selector == nil || selector.ResourceKind != authz.ResourceKindMCP || selector.ResourceID != mcpID ||
				selector.ProjectID == nil || *selector.ProjectID != projectID || (selector.ServerURL != nil && *selector.ServerURL != "") {
				return false
			}
			if selector.Disposition != nil && *selector.Disposition != "" && !validAccessRoleDisposition(*selector.Disposition) {
				return false
			}
		}
	}
	return true
}
