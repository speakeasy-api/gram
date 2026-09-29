//nolint:exhaustruct // MCP manifests and refusal results use optional zero values.
package platformmcp

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Parity decision: extend context for inspection and add one org-admin outcome.
// Existing access-role tools edit server grants, not saved provisioning intent.
// Project assistants cannot authorize organization-wide changes or inspect other
// projects, so neither this mutation nor its context detail is admitted to them.
// Configure owns the transaction, audit, CAS and durable reconciliation hint.
// Its version is a single-use write token: retries conflict rather than replay.
// Do not advertise replay receipts or invent an independent MCP write path.
// Evidence: TestRoleProvisioningSharedConfigureIntegration covers saves, conflicts
// and live status; roleprovisioning's TestConfigureAuditsSavedIntent and
// TestConfigureAuditAndHintFailuresRollBackTogether cover atomic audit behavior.
type roleProvisioningBackend interface {
	Status(context.Context, string) (roleprovisioning.Status, error)
	Configure(context.Context, roleprovisioning.ConfigureInput) (int64, error)
}

func provisioningBackend(reader Reader) roleProvisioningBackend {
	if r, ok := reader.(*PostgresReader); ok && r.db != nil {
		return roleprovisioning.NewSettings(r.db, audit.NewLogger())
	}
	return nil
}

const provisioningLimit = 100

type RoleProvisioningRole struct {
	Configured        bool   `json:"configured"`
	RoleURN           string `json:"role_urn"`
	Name              string `json:"name"`
	Enabled           bool   `json:"enabled"`
	ProjectID         string `json:"project_id,omitempty"`
	AppliedProjectID  string `json:"applied_project_id,omitempty"`
	PluginID          string `json:"plugin_id,omitempty"`
	OriginAudience    string `json:"origin_audience"`
	PublicationStatus string `json:"publication_status" jsonschema:"not_provisioned, not_connected, not_published, or published_before; published_before is historical evidence, not current-content publication freshness"`
	PendingReason     string `json:"pending_reason,omitempty"`
}

type RoleProvisioningProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type RoleProvisioningContext struct {
	Enabled   bool                      `json:"enabled"`
	Version   int64                     `json:"version"`
	ProjectID string                    `json:"project_id,omitempty"`
	Roles     []RoleProvisioningRole    `json:"roles"`
	Projects  []RoleProvisioningProject `json:"projects"`
	Truncated bool                      `json:"truncated" jsonschema:"when true, use dashboard settings to review and confirm the complete configuration"`
}

func provisioningContext(status roleprovisioning.Status) *RoleProvisioningContext {
	out := &RoleProvisioningContext{Enabled: status.Enabled, Version: status.Version, ProjectID: provisioningID(status.ProjectID), Roles: []RoleProvisioningRole{}, Projects: []RoleProvisioningProject{}, Truncated: len(status.Roles) > provisioningLimit || len(status.Projects) > provisioningLimit}
	for _, role := range status.Roles[:min(len(status.Roles), provisioningLimit)] {
		// Only bounded, known remediation codes leave this surface; persisted error
		// text can contain provider details and is not a safe agent-facing summary.
		pending := role.PendingReason
		switch pending {
		case "", "choose_project", "reconciliation_pending", "audience_approval_required", "private_gateway_audience", "admission_unavailable":
		default:
			pending = "reconciliation_pending"
		}
		out.Roles = append(out.Roles, RoleProvisioningRole{Configured: role.Configured, RoleURN: role.RoleURN, Name: role.Name, Enabled: role.Enabled, ProjectID: provisioningID(role.ProjectID), AppliedProjectID: provisioningID(role.AppliedProjectID), PluginID: provisioningID(role.PluginID), OriginAudience: role.OriginAudience, PublicationStatus: role.PublicationStatus, PendingReason: pending})
	}
	for _, project := range status.Projects[:min(len(status.Projects), provisioningLimit)] {
		out.Projects = append(out.Projects, RoleProvisioningProject{ID: project.ID.String(), Name: project.Name})
	}
	return out
}

func provisioningID(id uuid.NullUUID) string {
	if !id.Valid {
		return ""
	}
	return id.UUID.String()
}

func canReadProvisioning(ctx context.Context, principal Principal) bool {
	if principal.surface() != SurfacePlatformMCP {
		return false
	}
	grants, ok := authz.GrantsFromContext(ctx)
	return ok && grantsAuthorizeAnyScope(grants, principal.OrganizationID, []authz.Scope{authz.ScopeOrgAdmin})
}

type ConfigureRoleProvisioningSelection struct {
	RoleURN   string `json:"role_urn" jsonschema:"exact live IdP role URN from get_platform_context"`
	Enabled   bool   `json:"enabled" jsonschema:"whether this role participates; disabling retains plugins and audience"`
	ProjectID string `json:"project_id,omitempty" jsonschema:"explicit destination UUID; omitted preserves sticky mapping; zero UUID leaves pending"`
}

type ConfigureRoleProvisioningInput struct {
	OrganizationID  string                               `json:"organization_id" jsonschema:"exact organization ID from get_platform_context; must match this session"`
	ProjectID       string                               `json:"project_id" jsonschema:"explicit default destination UUID from get_platform_context; zero UUID deliberately leaves pending"`
	ExpectedVersion int64                                `json:"expected_version" jsonschema:"single-use version from a fresh get_platform_context; stale retries conflict and must not be automatically resubmitted"`
	Enabled         bool                                 `json:"enabled" jsonschema:"enable role-plugin automation; disable retains existing plugins and audience and does not stop IdP sync"`
	Roles           []ConfigureRoleProvisioningSelection `json:"roles" jsonschema:"up to 100 explicit role selections; omitted roles preserve existing exclusions; first configuration defaults remaining live IdP roles on"`
	Confirmed       bool                                 `json:"confirmed" jsonschema:"true only after user confirms exact organization, default destination, role selections, and any pending destinations"`
}

type ConfigureRoleProvisioningOutput struct {
	Version    int64                    `json:"version"`
	Status     *RoleProvisioningContext `json:"status,omitempty"`
	NextAction string                   `json:"next_action"`
}

func registerRoleProvisioningTool(reg *Registrar, backend roleProvisioningBackend) {
	addTool(reg, &mcp.Tool{Name: "configure_role_provisioning", Title: "Configure Role Plugin Provisioning", Description: "Save confirmed organization-wide role-plugin provisioning settings. First inspect get_platform_context. Requires an exact organization, explicit default project (zero UUID means pending), role selections and expected_version. Omitted roles preserve exclusions; first save defaults remaining live IdP roles on. Saved intent is not completed provisioning, audience assignment or publication. Disabling retains plugins and audience; IdP sync continues. Version is single-use: an exact retry conflicts after commit, not a replay receipt. On uncertain results re-read context, never automatically retry with a new version.", Annotations: &mcp.ToolAnnotations{IdempotentHint: false, DestructiveHint: new(true)}}, ToolMeta{Authorization: ExternalAuthorizationOrgAdmin, Audiences: externalOnly, ProjectScope: ProjectScopeNone}, func(ctx context.Context, _ *mcp.CallToolRequest, in ConfigureRoleProvisioningInput) (*mcp.CallToolResult, ConfigureRoleProvisioningOutput, error) {
		principal, err := principalFromToolContext(ctx)
		if err != nil {
			return nil, ConfigureRoleProvisioningOutput{}, err
		}
		out, err := configureRoleProvisioning(ctx, principal, backend, in)
		return nil, out, err
	})
}

func configureRoleProvisioning(ctx context.Context, principal Principal, backend roleProvisioningBackend, in ConfigureRoleProvisioningInput) (ConfigureRoleProvisioningOutput, error) {
	refuse := func(code, message string) (ConfigureRoleProvisioningOutput, error) {
		return ConfigureRoleProvisioningOutput{}, &ToolRefusalError{Code: code, Payload: message}
	}
	if !canReadProvisioning(ctx, principal) {
		return refuse("forbidden", "Organization administrator permission is required on the external Platform MCP.")
	}
	if backend == nil {
		return refuse(unavailableCode, "Role provisioning is unavailable.")
	}
	if in.OrganizationID != principal.OrganizationID || !in.Confirmed || in.ExpectedVersion < 0 || len(in.Roles) > provisioningLimit {
		return refuse("invalid_configuration", "Confirm the exact session organization, destination, role selections and current version.")
	}
	project, err := uuid.Parse(in.ProjectID)
	if err != nil {
		return refuse("invalid_configuration", "Select an explicit destination project UUID; zero UUID means pending.")
	}
	roles := make([]roleprovisioning.Selection, 0, len(in.Roles))
	for _, selection := range in.Roles {
		var destination *uuid.UUID
		if selection.ProjectID != "" {
			id, err := uuid.Parse(selection.ProjectID)
			if err != nil {
				return refuse("invalid_configuration", "Role destination must be a project UUID.")
			}
			destination = &id
		}
		roles = append(roles, roleprovisioning.Selection{RoleURN: selection.RoleURN, Enabled: selection.Enabled, ProjectID: destination})
	}
	// Fresh read validates intent against live state; Configure rechecks version,
	// role identity and project ownership under its own transaction lock.
	status, err := backend.Status(ctx, principal.OrganizationID)
	if err != nil {
		return refuse(unavailableCode, "Unable to read current provisioning settings.")
	}
	if len(status.Roles) > provisioningLimit || len(status.Projects) > provisioningLimit {
		return refuse("configuration_too_large", "Use dashboard settings to inspect and confirm this complete configuration; platform context is truncated.")
	}
	if status.Version != in.ExpectedVersion {
		return refuse("version_conflict", "Re-read get_platform_context and ask for confirmation again; the previous request may already have committed.")
	}
	version, err := backend.Configure(ctx, roleprovisioning.ConfigureInput{Actor: roleprovisioning.Actor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID), PublicationUserID: principal.UserID}, OrganizationID: principal.OrganizationID, ExpectedVersion: in.ExpectedVersion, Enabled: in.Enabled, ProjectID: &project, Roles: roles})
	if errors.Is(err, roleprovisioning.ErrConflict) {
		return refuse("version_conflict", "Settings changed; re-read context and confirm again. Do not automatically retry.")
	}
	if errors.Is(err, roleprovisioning.ErrInvalid) {
		return refuse("invalid_configuration", "Select live IdP roles and destination projects in this organization.")
	}
	if err != nil {
		return refuse(unavailableCode, "Unable to confirm save; re-read context before retrying.")
	}
	out := ConfigureRoleProvisioningOutput{Version: version, NextAction: "Inspect live status; saved intent does not prove provisioning or publication has completed."}
	status, err = backend.Status(ctx, principal.OrganizationID)
	if err != nil {
		out.NextAction = fmt.Sprintf("Version %d committed. Re-read get_platform_context for live status; do not repeat this write.", version)
		return out, nil
	}
	out.Status = provisioningContext(status)
	return out, nil
}
