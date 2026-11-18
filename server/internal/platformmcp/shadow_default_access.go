package platformmcp

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// shadowDefaultAccessAllowed is the built-in mode: a server connects
	// unless an administrator denies it. It needs no policy.
	shadowDefaultAccessAllowed = "allowed"

	// shadowDefaultAccessBlocked is the mode a managed policy sets: every
	// server nobody approved is blocked.
	shadowDefaultAccessBlocked = "blocked"

	// shadowDefaultAccessHeldByShadowAI means a Shadow AI policy that allows
	// servers by default holds the project's one blocking slot.
	shadowDefaultAccessHeldByShadowAI = "shadow_ai"

	// shadowDefaultAccessHeldByGuardrails means a Guardrails policy that also
	// scans for other risks, such as secrets or PII, holds the slot.
	shadowDefaultAccessHeldByGuardrails = "guardrails"

	shadowDefaultAccessAudienceEveryone = "everyone"
	shadowDefaultAccessAudienceSelected = "selected"

	shadowDefaultAccessVersionDomain = "shadow_default_access"

	// shadowDefaultAccessPolicyName is what the dashboard calls the policy
	// "blocked by default" creates.
	shadowDefaultAccessPolicyName = "Shadow MCP"

	// shadowDefaultAccessPolicyScore matches the dashboard's managed policy.
	shadowDefaultAccessPolicyScore = 8.0

	// maxShadowDefaultAccessAudiences bounds who "blocked by default" can be
	// narrowed to, matching the risk policy audience bound.
	maxShadowDefaultAccessAudiences = 100
)

// ShadowMCPDefaultAccess is what a project does with an MCP server nobody
// decided on. It never names people: a narrowed audience lists its roles and
// counts the people chosen one by one.
type ShadowMCPDefaultAccess struct {
	Mode            string                   `json:"mode" jsonschema:"allowed: servers connect unless denied; blocked: servers are blocked until approved"`
	HeldBy          string                   `json:"held_by,omitempty" jsonschema:"set when a Guardrails policy decides the mode and set_shadow_mcp_default_access cannot change it: shadow_ai (a Shadow AI policy that allows by default) or guardrails (a policy that also scans for other risks)"`
	Audience        string                   `json:"audience,omitempty" jsonschema:"for blocked: everyone, or selected when it applies only to chosen roles or people"`
	Roles           []ShadowDecisionAudience `json:"roles,omitempty" jsonschema:"for a selected audience: the roles it applies to"`
	SelectedPeople  int                      `json:"selected_people,omitempty" jsonschema:"for a selected audience: how many people were chosen one by one"`
	PolicyName      string                   `json:"policy_name,omitempty" jsonschema:"the policy behind a blocked or held mode"`
	Version         string                   `json:"version" jsonschema:"opaque version to pass as expected_version to set_shadow_mcp_default_access"`
	DashboardEditor string                   `json:"dashboard_editor,omitempty" jsonschema:"where a held mode is changed instead"`
}

// shadowDefaultAccessCurrent is the project's blocking Shadow MCP policy,
// when there is one, with its audience.
type shadowDefaultAccessCurrent struct {
	row      *riskrepo.RiskPolicy
	audience []string
}

type shadowDefaultAccessVersionState struct {
	ProjectID     string   `json:"project_id"`
	PolicyID      string   `json:"policy_id,omitempty"`
	PolicyVersion int64    `json:"policy_version,omitempty"`
	UpdatedAt     string   `json:"updated_at,omitempty"`
	PolicyType    string   `json:"policy_type,omitempty"`
	Enabled       bool     `json:"enabled,omitempty"`
	Action        string   `json:"action,omitempty"`
	Sources       []string `json:"sources,omitempty"`
	Disposition   string   `json:"disposition,omitempty"`
	AudienceType  string   `json:"audience_type,omitempty"`
	Audience      []string `json:"audience,omitempty"`
}

// loadShadowDefaultAccess reads the project's blocking Shadow MCP policy the
// way the dashboard finds it: the enabled policy that detects Shadow MCP and
// blocks. The server allows only one.
func loadShadowDefaultAccess(ctx context.Context, db riskrepo.DBTX, organizationID string, projectID uuid.UUID) (shadowDefaultAccessCurrent, error) {
	rows, err := riskrepo.New(db).ListEnabledShadowMCPPoliciesByProject(ctx, projectID)
	if err != nil {
		return shadowDefaultAccessCurrent{}, fmt.Errorf("list shadow mcp policies: %w", err)
	}
	for _, row := range rows {
		if row.Action != "block" || row.OrganizationID != organizationID {
			continue
		}
		audience, err := policycore.New(db).AudiencePrincipalURNs(ctx, organizationID, row.ID.String())
		if err != nil {
			return shadowDefaultAccessCurrent{}, fmt.Errorf("load shadow mcp policy audience: %w", err)
		}
		return shadowDefaultAccessCurrent{row: &row, audience: canonicalStrings(audience)}, nil
	}
	return shadowDefaultAccessCurrent{row: nil, audience: nil}, nil
}

// mode mirrors the dashboard's shadowMCPModeOf.
func (c shadowDefaultAccessCurrent) mode() (string, string) {
	if c.row == nil {
		return shadowDefaultAccessAllowed, ""
	}
	allowsByDefault := shadowmcp.EffectiveDisposition(c.row.ShadowMcpDisposition, c.row.Sources, c.row.Action) == shadowmcp.DispositionAllowAll
	managed := c.row.PolicyType != "prompt_based" && len(c.row.Sources) == 1
	switch {
	case !managed && allowsByDefault:
		return shadowDefaultAccessAllowed, shadowDefaultAccessHeldByGuardrails
	case !managed:
		return shadowDefaultAccessBlocked, shadowDefaultAccessHeldByGuardrails
	case allowsByDefault:
		return shadowDefaultAccessAllowed, shadowDefaultAccessHeldByShadowAI
	default:
		return shadowDefaultAccessBlocked, ""
	}
}

func (c shadowDefaultAccessCurrent) versionState(projectID uuid.UUID) shadowDefaultAccessVersionState {
	state := shadowDefaultAccessVersionState{ProjectID: projectID.String(), PolicyID: "", PolicyVersion: 0, UpdatedAt: "", PolicyType: "", Enabled: false, Action: "", Sources: nil, Disposition: "", AudienceType: "", Audience: nil}
	if c.row == nil {
		return state
	}
	state.PolicyID = c.row.ID.String()
	state.PolicyVersion = c.row.Version
	state.UpdatedAt = c.row.UpdatedAt.Time.UTC().Format(time.RFC3339Nano)
	state.PolicyType = c.row.PolicyType
	state.Enabled = c.row.Enabled
	state.Action = c.row.Action
	state.Sources = canonicalStrings(c.row.Sources)
	state.Disposition = shadowmcp.EffectiveDisposition(c.row.ShadowMcpDisposition, c.row.Sources, c.row.Action)
	state.AudienceType = c.row.AudienceType
	state.Audience = canonicalStrings(c.audience)
	return state
}

// targetedPrincipals is the narrowed audience, or nil when the policy
// applies to everyone.
func (c shadowDefaultAccessCurrent) targetedPrincipals() []string {
	if c.row == nil || c.row.AudienceType != "targeted" {
		return nil
	}
	return c.audience
}

// projectShadowDefaultAccess builds the read model. Role names come from the
// same options plugin assignments use; people are only counted.
func projectShadowDefaultAccess(ctx context.Context, db riskrepo.DBTX, organizationID string, projectID uuid.UUID, current shadowDefaultAccessCurrent, versions *shadowDecisionVersionCodec) (ShadowMCPDefaultAccess, error) {
	version, err := versions.encodeSetting(shadowDefaultAccessVersionDomain, current.versionState(projectID))
	if err != nil {
		return ShadowMCPDefaultAccess{}, err
	}
	mode, heldBy := current.mode()
	result := ShadowMCPDefaultAccess{Mode: mode, HeldBy: heldBy, Audience: "", Roles: nil, SelectedPeople: 0, PolicyName: "", Version: version, DashboardEditor: ""}
	if current.row != nil {
		result.PolicyName = current.row.Name
	}
	if heldBy != "" {
		result.DashboardEditor = "guardrails"
	}
	if mode != shadowDefaultAccessBlocked {
		return result, nil
	}
	targeted := current.targetedPrincipals()
	if current.row.AudienceType != "targeted" {
		result.Audience = shadowDefaultAccessAudienceEveryone
		return result, nil
	}
	result.Audience = shadowDefaultAccessAudienceSelected
	roles := make([]string, 0, len(targeted))
	for _, value := range targeted {
		principal, err := urn.ParsePrincipal(value)
		if err != nil {
			continue
		}
		if principal.Type == urn.PrincipalTypeRole {
			roles = append(roles, principal.String())
			continue
		}
		if principal.Type == urn.PrincipalTypeUser && principal != authz.AllUsersPrincipal() {
			result.SelectedPeople++
		}
	}
	summaries, err := shadowRoleSummaries(ctx, db, organizationID, roles)
	if err != nil {
		return ShadowMCPDefaultAccess{}, err
	}
	result.Roles = summaries
	return result, nil
}

func shadowRoleSummaries(ctx context.Context, db riskrepo.DBTX, organizationID string, roles []string) ([]ShadowDecisionAudience, error) {
	if len(roles) == 0 {
		return []ShadowDecisionAudience{}, nil
	}
	rows, err := platformrepo.New(db).ListPlatformMCPPluginAssignmentOptions(ctx, platformrepo.ListPlatformMCPPluginAssignmentOptionsParams{
		SelectedPrincipalUrns: roles,
		ResultLimit:           maxShadowDefaultAccessAudiences + 1,
		OrganizationID:        organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("list shadow mcp default access roles: %w", err)
	}
	result := make([]ShadowDecisionAudience, 0, len(rows))
	for _, row := range rows {
		if row.Kind != "role" {
			continue
		}
		var count *SubjectCount
		if row.MemberCount.Valid {
			value := NewSubjectCount(row.MemberCount.Int64)
			count = &value
		}
		result = append(result, ShadowDecisionAudience{Kind: row.Kind, DisplayName: row.DisplayName, MemberCount: count})
	}
	slices.SortFunc(result, func(a, b ShadowDecisionAudience) int { return compareStrings(a.DisplayName, b.DisplayName) })
	return result, nil
}

// readShadowDefaultAccess is the live read the inventory, the review and the
// mutation result share.
func readShadowDefaultAccess(ctx context.Context, db riskrepo.DBTX, organizationID string, projectID uuid.UUID, versions *shadowDecisionVersionCodec) (ShadowMCPDefaultAccess, error) {
	current, err := loadShadowDefaultAccess(ctx, db, organizationID, projectID)
	if err != nil {
		return ShadowMCPDefaultAccess{}, err
	}
	return projectShadowDefaultAccess(ctx, db, organizationID, projectID, current, versions)
}
