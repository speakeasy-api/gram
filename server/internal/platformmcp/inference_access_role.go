package platformmcp

import (
	"context"
	"crypto/hmac"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const operationConfigureInferenceAccessRole = "configure_inference_access_role"

type ConfigureInferenceAccessRoleInput struct {
	ProjectID       string `json:"project_id" jsonschema:"explicit project ID for admission and receipt scope; grant scope is controlled by all_projects"`
	AllProjects     bool   `json:"all_projects,omitempty" jsonschema:"true targets the all-project inference grant; confirm impact across every project and role holder; false targets only project_id"`
	RoleReference   string `json:"role_reference" jsonschema:"fresh opaque custom role reference from list_access_roles"`
	ExpectedVersion string `json:"expected_version" jsonschema:"version from the fresh role read"`
	Enabled         bool   `json:"enabled" jsonschema:"true adds the selected inference grant; false removes only that exact grant, not other scopes"`
	IdempotencyKey  string `json:"idempotency_key" jsonschema:"stable unique key for this exact mutation and retries"`
	Confirmed       bool   `json:"confirmed" jsonschema:"true only after explicit confirmation of the selected scope, role, grant change and impact on all role holders and affected projects"`
}

type ConfigureInferenceAccessRoleOutput struct {
	Role              AccessRoleMutationSummary `json:"role" jsonschema:"historical committed role snapshot; read roles again before another change"`
	ProjectID         string                    `json:"project_id"`
	Scope             string                    `json:"scope"`
	AllProjects       bool                      `json:"all_projects"`
	ExactGrantPresent bool                      `json:"exact_grant_present" jsonschema:"fresh read of the exact selected inference grant on this role, not effective agent or owner access; other grants may still permit inference"`
	Receipt           RiskMutationToolReceipt   `json:"receipt"`
}

func inferenceAccessRoleGrant(projectID uuid.UUID, allProjects bool) *accessgen.RoleGrant {
	project := projectID.String()
	var selectorProject *string
	if !allProjects {
		selectorProject = &project
	}
	return &accessgen.RoleGrant{Scope: string(authz.ScopeInferenceExecute), Selectors: []*accessgen.Selector{{ResourceKind: "inference", ResourceID: "*", ProjectID: selectorProject, Tool: nil, Disposition: nil, ServerURL: nil, ClientType: nil, ConnectionOwner: nil, UpstreamClient: nil}}}
}

func hasInferenceAccessRoleGrant(role *accessgen.Role, projectID uuid.UUID, allProjects bool) bool {
	for _, grant := range role.Grants {
		if grant.Scope != string(authz.ScopeInferenceExecute) {
			continue
		}
		for _, selector := range grant.Selectors {
			if selector.ResourceKind == "inference" && selector.ResourceID == "*" && ((allProjects && (selector.ProjectID == nil || *selector.ProjectID == "*")) || (!allProjects && selector.ProjectID != nil && *selector.ProjectID == projectID.String())) && selector.Tool == nil && selector.Disposition == nil && selector.ServerURL == nil && selector.ClientType == nil && selector.ConnectionOwner == nil && selector.UpstreamClient == nil {
				return true
			}
		}
	}
	return false
}

func (s *AccessRoleMutationService) ConfigureInference(ctx context.Context, principal Principal, input ConfigureInferenceAccessRoleInput) (ConfigureInferenceAccessRoleOutput, error) {
	if principal.surface() != SurfacePlatformMCP {
		return ConfigureInferenceAccessRoleOutput{}, accessRoleMutationInvalid("Inference access configuration requires the external Platform MCP.")
	}
	if !input.Confirmed {
		return ConfigureInferenceAccessRoleOutput{}, &AccessRoleMutationError{Code: "confirmation_required", Message: "Show the role, selected inference scope (one project or all projects), grant change and impact on all role holders; ask the user to confirm it, then retry with confirmed: true.", Cause: ErrAccessRoleMutationInvalid}
	}
	if !s.valid() {
		return ConfigureInferenceAccessRoleOutput{}, accessRoleMutationUnavailable(nil)
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.RoleReference = strings.TrimSpace(input.RoleReference)
	input.ExpectedVersion = strings.TrimSpace(input.ExpectedVersion)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if principal.UserID == "" || input.RoleReference == "" || !validAccessRoleVersion(input.ExpectedVersion) || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 {
		return ConfigureInferenceAccessRoleOutput{}, accessRoleMutationInvalid("The access role update request is invalid.")
	}
	project, workosOrgID, err := s.admit(ctx, principal, input.ProjectID)
	if err != nil {
		return ConfigureInferenceAccessRoleOutput{}, err
	}
	roleID, err := s.reads.references.decodeForReceiptLookup(input.RoleReference, principal, subjectKindAccessRole, s.reads.now().UTC())
	if err != nil {
		return ConfigureInferenceAccessRoleOutput{}, accessRoleMutationNotFound()
	}
	normalized := struct {
		ProjectID       string `json:"project_id"`
		RoleID          string `json:"role_id"`
		ExpectedVersion string `json:"expected_version"`
		Enabled         bool   `json:"enabled"`
		// Omission preserves hashes for existing project-scoped receipts.
		AllProjects bool `json:"all_projects,omitempty"`
	}{project.ID.String(), roleID, input.ExpectedVersion, input.Enabled, input.AllProjects}
	var addGrants, removeGrants []*accessgen.RoleGrant
	grant := inferenceAccessRoleGrant(project.ID, input.AllProjects)
	if input.Enabled {
		addGrants = []*accessgen.RoleGrant{grant}
	} else {
		removeGrants = []*accessgen.RoleGrant{grant}
	}
	if input.AllProjects {
		// Remove the equivalent wildcard spelling on both enable and disable.
		// UpdateRoleTx removes exact selectors before adding the canonical one,
		// so enabling converges to one grant without touching narrowed grants.
		wildcard := inferenceAccessRoleGrant(project.ID, true)
		allProjects := "*"
		wildcard.Selectors[0].ProjectID = &allProjects
		removeGrants = append(removeGrants, wildcard)
	}
	// Only inference grants change here, so role delivery cannot gain MCP Use
	// access and needs no administrator exposure-approval marker.
	// A replayed receipt must not repeat the role write or reconciliation.
	var delivery access.RoleReconciliation
	receipt, err := s.receipts.execute(ctx, principal, project, operationConfigureInferenceAccessRole, input.IdempotencyKey, normalized, s.charge(principal), func(ctx context.Context, tx pgx.Tx) (AccessRoleMutationReceiptResult, error) {
		if _, err := s.reads.references.Decode(input.RoleReference, principal, subjectKindAccessRole, s.reads.now().UTC()); err != nil {
			return AccessRoleMutationReceiptResult{}, accessRoleMutationNotFound()
		}
		roleUUID, parseErr := uuid.Parse(roleID)
		if parseErr != nil || roleUUID == uuid.Nil {
			return AccessRoleMutationReceiptResult{}, accessRoleMutationNotFound()
		}
		if _, lockErr := accessrepo.New(tx).LockOrganizationRoleByID(ctx, accessrepo.LockOrganizationRoleByIDParams{OrganizationID: principal.OrganizationID, ID: roleUUID}); errors.Is(lockErr, pgx.ErrNoRows) {
			return AccessRoleMutationReceiptResult{}, accessRoleMutationNotFound()
		} else if lockErr != nil {
			return AccessRoleMutationReceiptResult{}, accessRoleMutationUnavailable(lockErr)
		}
		current, readErr := s.backend.GetRoleByIDTx(ctx, tx, principal.OrganizationID, roleID)
		if readErr != nil {
			return AccessRoleMutationReceiptResult{}, classifyAccessRoleBackendError(readErr)
		}
		if current == nil || current.IsSystem {
			return AccessRoleMutationReceiptResult{}, accessRoleMutationNotFound()
		}
		version, versionErr := s.roleVersion(current)
		if versionErr != nil || !hmac.Equal([]byte(version), []byte(input.ExpectedVersion)) {
			return AccessRoleMutationReceiptResult{}, accessRoleMutationConflict("The access role changed after it was read. Read it again and retry with the new version.")
		}
		result, reconciliation, err := s.backend.UpdateRoleTx(ctx, tx, principal.OrganizationID, workosOrgID, access.RoleAuditActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID), DisplayName: nil}, &accessgen.UpdateRolePayload{ApikeyToken: nil, SessionToken: nil, ID: roleID, Name: nil, Description: nil, AddGrants: addGrants, RemoveGrants: removeGrants, MemberIds: nil, AgentIds: nil})
		if err != nil {
			return AccessRoleMutationReceiptResult{}, classifyAccessRoleBackendError(err)
		}
		if result.After == nil {
			return AccessRoleMutationReceiptResult{}, accessRoleMutationNotFound()
		}
		delivery = reconciliation
		return s.receiptResult(result.After, result.Slug, "updated", "complete")
	})
	if err != nil {
		return ConfigureInferenceAccessRoleOutput{}, err
	}
	if !receipt.Replayed {
		s.backend.ReconcileRoleDelivery(ctx, delivery)
	}
	result, err := decodeAccessRoleMutationReceipt(operationConfigureInferenceAccessRole, receipt.ResultPayload)
	if err != nil {
		return ConfigureInferenceAccessRoleOutput{}, err
	}
	summary, err := s.outputSummary(principal, result)
	if err != nil {
		return ConfigureInferenceAccessRoleOutput{}, err
	}
	// Verify current state separately: receipt replay is a historical write, not live access.
	tx, err := s.reads.db.Begin(ctx)
	if err != nil {
		return ConfigureInferenceAccessRoleOutput{}, accessRoleMutationUnavailable(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := s.backend.GetRoleByIDTx(ctx, tx, principal.OrganizationID, roleID)
	if err != nil {
		return ConfigureInferenceAccessRoleOutput{}, classifyAccessRoleBackendError(err)
	}
	if current == nil {
		return ConfigureInferenceAccessRoleOutput{}, accessRoleMutationNotFound()
	}
	return ConfigureInferenceAccessRoleOutput{Role: summary, ProjectID: project.ID.String(), Scope: string(authz.ScopeInferenceExecute), AllProjects: input.AllProjects, ExactGrantPresent: hasInferenceAccessRoleGrant(current, project.ID, input.AllProjects), Receipt: riskMutationToolReceipt(receipt)}, nil
}
