package platformmcp

import (
	"context"
	"strings"

	"github.com/google/uuid"
	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/authz"
)

const operationConfigureMemoryAccessRole = "configure_memory_access_role"

type ConfigureMemoryAccessRoleInput struct {
	ProjectID       string `json:"project_id" jsonschema:"explicit project ID whose team memory the grant covers"`
	Scope           string `json:"scope" jsonschema:"memory:read to search the project's team memory, or memory:write to also save memos; memory:write includes memory:read"`
	RoleReference   string `json:"role_reference" jsonschema:"fresh opaque custom role reference from list_access_roles"`
	ExpectedVersion string `json:"expected_version" jsonschema:"version from the fresh role read"`
	Enabled         bool   `json:"enabled" jsonschema:"true adds the selected memory grant for this project; false removes only that exact grant, not other scopes or projects"`
	IdempotencyKey  string `json:"idempotency_key" jsonschema:"stable unique key for this exact mutation and retries"`
	Confirmed       bool   `json:"confirmed" jsonschema:"true only after explicit confirmation of the project, scope, role, grant change and impact on all role holders"`
}

type ConfigureMemoryAccessRoleOutput struct {
	Role              AccessRoleMutationSummary `json:"role" jsonschema:"historical committed role snapshot; read roles again before another change"`
	ProjectID         string                    `json:"project_id"`
	Scope             string                    `json:"scope"`
	ExactGrantPresent bool                      `json:"exact_grant_present" jsonschema:"fresh read of the exact selected memory grant on this role, not effective access; other grants on the role may still allow team memory"`
	Receipt           RiskMutationToolReceipt   `json:"receipt"`
}

// memoryAccessRoleScope admits only the two team memory scopes.
func memoryAccessRoleScope(value string) (authz.Scope, bool) {
	switch scope := authz.Scope(strings.TrimSpace(value)); scope {
	case authz.ScopeMemoryRead, authz.ScopeMemoryWrite:
		return scope, true
	default:
		return "", false
	}
}

// memoryAccessRoleGrant is one project's memory grant. Team memory is
// project-scoped, so there is no all-projects form here.
func memoryAccessRoleGrant(scope authz.Scope, projectID uuid.UUID) *accessgen.RoleGrant {
	return &accessgen.RoleGrant{Scope: string(scope), Selectors: []*accessgen.Selector{{ResourceKind: authz.ResourceKindProject, ResourceID: projectID.String(), ProjectID: nil, Tool: nil, Disposition: nil, ServerURL: nil, ClientType: nil, ConnectionOwner: nil, UpstreamClient: nil}}}
}

func hasMemoryAccessRoleGrant(role *accessgen.Role, scope authz.Scope, projectID uuid.UUID) bool {
	for _, grant := range role.Grants {
		if grant.Scope != string(scope) {
			continue
		}
		for _, selector := range grant.Selectors {
			if selector.ResourceKind == authz.ResourceKindProject && selector.ResourceID == projectID.String() && (selector.ProjectID == nil || *selector.ProjectID == projectID.String()) && selector.Tool == nil && selector.Disposition == nil && selector.ServerURL == nil && selector.ClientType == nil && selector.ConnectionOwner == nil && selector.UpstreamClient == nil {
				return true
			}
		}
	}
	return false
}

// ConfigureMemory adds or removes one project's memory:read or memory:write
// grant on a custom role, with the same organization-admin, confirmation,
// version and idempotency safeguards as the inference role tool.
func (s *AccessRoleMutationService) ConfigureMemory(ctx context.Context, principal Principal, input ConfigureMemoryAccessRoleInput) (ConfigureMemoryAccessRoleOutput, error) {
	if principal.surface() != SurfacePlatformMCP {
		return ConfigureMemoryAccessRoleOutput{}, accessRoleMutationInvalid("Team memory access configuration requires the external Platform MCP.")
	}
	if !input.Confirmed {
		return ConfigureMemoryAccessRoleOutput{}, &AccessRoleMutationError{Code: "confirmation_required", Message: "Show the role, project, memory scope, grant change and that every holder of the role gains or loses it; ask the user to confirm it, then retry with confirmed: true.", Cause: ErrAccessRoleMutationInvalid}
	}
	if !s.valid() {
		return ConfigureMemoryAccessRoleOutput{}, accessRoleMutationUnavailable(nil)
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.RoleReference = strings.TrimSpace(input.RoleReference)
	input.ExpectedVersion = strings.TrimSpace(input.ExpectedVersion)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	scope, ok := memoryAccessRoleScope(input.Scope)
	if !ok || principal.UserID == "" || input.RoleReference == "" || !validAccessRoleVersion(input.ExpectedVersion) || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 {
		return ConfigureMemoryAccessRoleOutput{}, accessRoleMutationInvalid("The access role update request is invalid.")
	}
	project, workosOrgID, err := s.admit(ctx, principal, input.ProjectID)
	if err != nil {
		return ConfigureMemoryAccessRoleOutput{}, err
	}
	roleID, err := s.reads.references.decodeForReceiptLookup(input.RoleReference, principal, subjectKindAccessRole, s.reads.now().UTC())
	if err != nil {
		return ConfigureMemoryAccessRoleOutput{}, accessRoleMutationNotFound()
	}
	normalized := struct {
		ProjectID       string `json:"project_id"`
		Scope           string `json:"scope"`
		RoleID          string `json:"role_id"`
		ExpectedVersion string `json:"expected_version"`
		Enabled         bool   `json:"enabled"`
	}{project.ID.String(), string(scope), roleID, input.ExpectedVersion, input.Enabled}
	var addGrants, removeGrants []*accessgen.RoleGrant
	if input.Enabled {
		addGrants = []*accessgen.RoleGrant{memoryAccessRoleGrant(scope, project.ID)}
	} else {
		removeGrants = []*accessgen.RoleGrant{memoryAccessRoleGrant(scope, project.ID)}
	}
	// Only memory grants change here, so role delivery cannot gain MCP Use
	// access and needs no administrator exposure-approval marker.
	summary, receipt, current, err := s.updateRoleGrants(ctx, principal, project, workosOrgID, operationConfigureMemoryAccessRole, roleID, input.RoleReference, input.ExpectedVersion, input.IdempotencyKey, normalized, addGrants, removeGrants)
	if err != nil {
		return ConfigureMemoryAccessRoleOutput{}, err
	}
	return ConfigureMemoryAccessRoleOutput{Role: summary, ProjectID: project.ID.String(), Scope: string(scope), ExactGrantPresent: hasMemoryAccessRoleGrant(current, scope, project.ID), Receipt: riskMutationToolReceipt(receipt)}, nil
}
