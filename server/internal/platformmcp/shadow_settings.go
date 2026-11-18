package platformmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	ra "github.com/speakeasy-api/gram/server/internal/background/activities/risk_analysis"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/mcpapproval"
	"github.com/speakeasy-api/gram/server/internal/risk/policycore"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	shadowadmission "github.com/speakeasy-api/gram/server/internal/shadowmcp/admission"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp/officiallink"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	operationSetShadowMCPDefaultAccess  = "set_shadow_mcp_default_access"
	operationSetShadowMCPOfficialServer = "set_shadow_mcp_official_server"

	shadowOfficialLinkVersionDomain = "shadow_official_link"

	shadowOfficialLinkActionLink      = "link"
	shadowOfficialLinkActionSwitchOff = "switch_off"
	shadowOfficialLinkActionSwitchOn  = "switch_on"
	shadowOfficialLinkActionRemove    = "remove"

	shadowOfficialLinkOn   = "on"
	shadowOfficialLinkOff  = "off"
	shadowOfficialLinkNone = "none"

	shadowSettingChanged   = "changed"
	shadowSettingUnchanged = "unchanged"

	// maxShadowSettingIdempotencyKey matches the other Platform MCP writes.
	maxShadowSettingIdempotencyKey = 128
)

// ShadowMCPSettingsService changes the two Shadow MCP settings an
// administrator owns on the dashboard's Shadow MCP page: what a project does
// with a server nobody decided on, and which project MCP server is a Shadow
// MCP server's official version. Both writes share the decision tool's
// rollout cohort, budget, receipts and refusals.
type ShadowMCPSettingsService struct {
	db            *pgxpool.Pool
	shadow        *ShadowInventoryService
	audiences     shadowDecisionAudienceResolver
	policies      *policycore.Core
	links         *officiallink.Writer
	flags         feature.Provider
	organizations OrganizationSlugResolver
	budget        OperationBudget
	now           func() time.Time
	cleaner       shadowPolicyResultsCleaner
	cleanerLogger *slog.Logger
}

// shadowPolicyResultsCleaner removes the results of a deleted policy, as the
// dashboard does after it deletes one.
type shadowPolicyResultsCleaner interface {
	Clean(ctx context.Context, projectID, policyID uuid.UUID) error
}

// WithResultsCleaner removes a deleted policy's results after
// set_shadow_mcp_default_access switches a project back to allowed by default.
func (s *ShadowMCPSettingsService) WithResultsCleaner(cleaner shadowPolicyResultsCleaner, logger *slog.Logger) *ShadowMCPSettingsService {
	s.cleaner = cleaner
	s.cleanerLogger = logger
	return s
}

func (s *ShadowMCPSettingsService) cleanPolicyResults(ctx context.Context, projectID, policyID uuid.UUID) {
	if s.cleaner == nil || s.cleanerLogger == nil {
		return
	}
	if err := s.cleaner.Clean(ctx, projectID, policyID); err != nil {
		s.cleanerLogger.ErrorContext(ctx, "trigger risk policy results cleanup", attr.SlogError(err), attr.SlogProjectID(projectID.String()), attr.SlogRiskPolicyID(policyID.String()))
	}
}

func NewShadowMCPSettingsService(db *pgxpool.Pool, shadow *ShadowInventoryService, audiences shadowDecisionAudienceResolver, policies *policycore.Core, links *officiallink.Writer, flags feature.Provider, organizations OrganizationSlugResolver, budget OperationBudget) *ShadowMCPSettingsService {
	return &ShadowMCPSettingsService{db: db, shadow: shadow, audiences: audiences, policies: policies, links: links, flags: flags, organizations: organizations, budget: budget, now: time.Now}
}

func (s *ShadowMCPSettingsService) valid() bool {
	return s != nil && s.db != nil && s.shadow != nil && s.shadow.valid() && s.audiences != nil && s.policies != nil && s.links != nil && s.flags != nil && s.organizations != nil && s.budget.valid() && s.now != nil
}

type SetShadowMCPDefaultAccessInput struct {
	ProjectSlug string `json:"project_slug" jsonschema:"the project's slug, as list_projects returns it"`
	// ProjectID replaces ProjectSlug before the handler runs: the ID of the project it names.
	ProjectID          string   `json:"-"`
	Mode               string   `json:"mode" jsonschema:"allowed: every server connects unless denied, and the managed policy is removed; blocked: every server nobody approved is blocked"`
	AudienceReferences []string `json:"audience_references,omitempty" jsonschema:"for blocked only, optional: opaque role references from list_plugin_assignments to block by default only for those roles; omit them, or pass only the Everyone reference, to block for everyone. Directory groups and attributes are refused"`
	ExpectedVersion    string   `json:"expected_version" jsonschema:"default_access.version from the list_shadow_mcp_inventory or get_shadow_mcp_review read immediately before this write"`
	IdempotencyKey     string   `json:"idempotency_key" jsonschema:"stable unique key for safely retrying this exact change"`
	Confirmed          bool     `json:"confirmed" jsonschema:"set true only after the user confirms the exact project, mode and audience"`
}

type ShadowDefaultAccessReceiptResult struct {
	Mode           string                   `json:"mode"`
	Audience       string                   `json:"audience,omitempty"`
	Roles          []ShadowDecisionAudience `json:"roles"`
	ResultCategory string                   `json:"result_category" jsonschema:"created, updated or removed when the managed policy changed; unchanged when the project was already in this state"`
}

type SetShadowMCPDefaultAccessOutput struct {
	ShadowDefaultAccessReceiptResult
	State       *ShadowMCPDefaultAccess `json:"state,omitempty" jsonschema:"the project's default access read after the write"`
	StateStatus string                  `json:"state_status" jsonschema:"complete when state was read after the write; pending when it could not be read yet"`
	Receipt     RiskMutationToolReceipt `json:"receipt"`
}

type normalizedShadowDefaultAccess struct {
	ProjectID          string   `json:"project_id"`
	Mode               string   `json:"mode"`
	AudienceReferences []string `json:"audience_references"`
	ExpectedVersion    string   `json:"expected_version"`
}

type SetShadowMCPOfficialServerInput struct {
	ProjectSlug string `json:"project_slug" jsonschema:"the project's slug, as list_projects returns it"`
	// ProjectID replaces ProjectSlug before the handler runs: the ID of the project it names.
	ProjectID       string `json:"-"`
	TargetReference string `json:"target_reference" jsonschema:"short-lived opaque target reference of a remote Shadow MCP server, returned by list_shadow_mcp_inventory or get_shadow_mcp_review"`
	Action          string `json:"action" jsonschema:"link: make mcp_id the official version and block direct use; switch_off: keep the link but stop blocking direct use; switch_on: block direct use again; remove: delete the link"`
	MCPID           string `json:"mcp_id,omitempty" jsonschema:"for link only: the project MCP server's ID from find_mcp, or suggested_official_server.mcp_id from the inventory"`
	ExpectedVersion string `json:"expected_version" jsonschema:"official_link_version of this target from the list_shadow_mcp_inventory or get_shadow_mcp_review read immediately before this write"`
	IdempotencyKey  string `json:"idempotency_key" jsonschema:"stable unique key for safely retrying this exact change"`
	Confirmed       bool   `json:"confirmed" jsonschema:"set true only after the user confirms the exact project, target, action and MCP server"`
}

type ShadowOfficialLinkReceiptResult struct {
	Action         string `json:"action"`
	LinkState      string `json:"link_state" jsonschema:"on, off, or none when no server is linked"`
	MCPID          string `json:"mcp_id,omitempty"`
	ResultCategory string `json:"result_category" jsonschema:"changed, or unchanged when the link already matched"`
}

type SetShadowMCPOfficialServerOutput struct {
	ShadowOfficialLinkReceiptResult
	State       *GetShadowMCPReviewOutput `json:"state,omitempty" jsonschema:"the target's review read after the write"`
	StateStatus string                    `json:"state_status" jsonschema:"complete when state was read after the write; pending when it could not be read yet"`
	Receipt     RiskMutationToolReceipt   `json:"receipt"`
}

type normalizedShadowOfficialLink struct {
	ProjectID       string `json:"project_id"`
	TargetReference string `json:"target_reference"`
	Action          string `json:"action"`
	MCPID           string `json:"mcp_id"`
	ExpectedVersion string `json:"expected_version"`
}

type shadowOfficialLinkVersionStateValue struct {
	ProjectID         string `json:"project_id"`
	ServerKey         string `json:"server_key"`
	MCPServerID       string `json:"mcp_server_id,omitempty"`
	AllowDirectAccess bool   `json:"allow_direct_access,omitempty"`
}

func shadowOfficialLinkVersionState(projectID uuid.UUID, key string, link officiallink.Link) shadowOfficialLinkVersionStateValue {
	state := shadowOfficialLinkVersionStateValue{ProjectID: projectID.String(), ServerKey: key, MCPServerID: "", AllowDirectAccess: false}
	if link.Linked() {
		state.MCPServerID = link.MCPServerID.String()
		state.AllowDirectAccess = link.AllowDirectAccess
	}
	return state
}

func shadowOfficialLinkState(link officiallink.Link) string {
	switch {
	case !link.Linked():
		return shadowOfficialLinkNone
	case link.AllowDirectAccess:
		return shadowOfficialLinkOff
	default:
		return shadowOfficialLinkOn
	}
}

// SetDefaultAccess switches a project between allowed and blocked by
// default, or changes who blocked by default applies to. It writes only the
// managed policy the dashboard's mode switch writes, through the same policy
// core, and refuses when a Guardrails policy holds the slot.
func (s *ShadowMCPSettingsService) SetDefaultAccess(ctx context.Context, principal Principal, input SetShadowMCPDefaultAccessInput) (SetShadowMCPDefaultAccessOutput, error) {
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.Mode = strings.TrimSpace(input.Mode)
	input.ExpectedVersion = strings.TrimSpace(input.ExpectedVersion)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.ProjectID == "" || input.ExpectedVersion == "" || !validShadowSettingKey(input.IdempotencyKey) || len(input.AudienceReferences) > maxShadowDefaultAccessAudiences {
		return SetShadowMCPDefaultAccessOutput{}, shadowDecisionInvalid("The Shadow MCP default access request is invalid.")
	}
	if input.Mode != shadowDefaultAccessAllowed && input.Mode != shadowDefaultAccessBlocked {
		return SetShadowMCPDefaultAccessOutput{}, shadowDecisionInvalid("Mode must be allowed or blocked.")
	}
	references, err := normalizeShadowAudienceReferences(input.AudienceReferences)
	if err != nil {
		return SetShadowMCPDefaultAccessOutput{}, err
	}
	if input.Mode == shadowDefaultAccessAllowed && len(references) > 0 {
		return SetShadowMCPDefaultAccessOutput{}, shadowDecisionInvalid("Allowed by default applies to everyone, so it takes no audience references.")
	}
	if !s.valid() || principal.OrganizationID == "" || principal.UserID == "" {
		return SetShadowMCPDefaultAccessOutput{}, shadowDecisionUnavailable(nil)
	}
	project, err := s.shadow.projects.Resolve(ctx, principal.OrganizationID, input.ProjectID, "")
	if err != nil {
		return SetShadowMCPDefaultAccessOutput{}, mapShadowDecisionProjectError(err)
	}
	if err := admitShadowAccessWrites(ctx, s.flags, s.organizations, principal, project); err != nil {
		return SetShadowMCPDefaultAccessOutput{}, err
	}
	if !input.Confirmed {
		return SetShadowMCPDefaultAccessOutput{}, s.previewDefaultAccess(ctx, principal, project, input.Mode, references)
	}
	normalized := normalizedShadowDefaultAccess{ProjectID: project.ID.String(), Mode: input.Mode, AudienceReferences: references, ExpectedVersion: input.ExpectedVersion}
	var afterCommit func(context.Context)
	receipt, err := executeShadowSettingReceipt(ctx, s.db, s.now, principal, project, operationSetShadowMCPDefaultAccess, input.IdempotencyKey, normalized, chargeShadowAccessWrite(s.budget, principal), validShadowDefaultAccessReceipt,
		func(ctx context.Context, tx pgx.Tx) (ShadowDefaultAccessReceiptResult, error) {
			result, after, err := s.applyDefaultAccess(ctx, tx, principal, project, input.Mode, references, input.ExpectedVersion)
			afterCommit = after
			return result, err
		})
	if err != nil {
		return SetShadowMCPDefaultAccessOutput{}, err
	}
	if afterCommit != nil {
		afterCommit(ctx)
	}
	var stored ShadowDefaultAccessReceiptResult
	if err := json.Unmarshal(receipt.ResultPayload, &stored); err != nil || !validShadowDefaultAccessReceipt(stored) {
		return SetShadowMCPDefaultAccessOutput{}, shadowDecisionUnavailable(err)
	}
	output := SetShadowMCPDefaultAccessOutput{ShadowDefaultAccessReceiptResult: stored, State: nil, StateStatus: "pending", Receipt: riskMutationToolReceipt(receipt)}
	state, stateErr := readShadowDefaultAccess(ctx, s.db, principal.OrganizationID, project.ID, s.shadow.versions)
	if stateErr == nil {
		output.State = &state
		output.StateStatus = "complete"
	}
	return output, nil
}

// previewDefaultAccess answers an unconfirmed call. It changes nothing: it
// reads the current mode and says what confirming would do, or refuses
// up front when a Guardrails policy holds the setting.
func (s *ShadowMCPSettingsService) previewDefaultAccess(ctx context.Context, principal Principal, project ResolvedProject, mode string, references []string) error {
	current, err := loadShadowDefaultAccess(ctx, s.db, principal.OrganizationID, project.ID)
	if err != nil {
		return shadowDecisionUnavailable(err)
	}
	currentMode, heldBy := current.mode()
	if err := shadowDefaultAccessHeldRefusal(current, mode, heldBy); err != nil {
		return err
	}
	change := "switches this project from " + currentMode + " by default to " + mode + " by default"
	if currentMode == mode {
		change = "keeps this project " + mode + " by default"
	}
	consequence := "Every MCP server connects unless an administrator denies it; denials and official server links still apply."
	if mode == shadowDefaultAccessBlocked {
		consequence = "Every MCP server nobody approved is blocked for everyone, including servers people use today, until an administrator approves it."
		if len(references) > 0 {
			consequence = fmt.Sprintf("Every MCP server nobody approved is blocked for the %d selected audience reference(s), including servers they use today; everyone else is still allowed by default.", len(references))
		}
	}
	return shadowDecisionError("confirmation_required", "Nothing was changed. Confirming "+change+". "+consequence+" Show this to the user, then resend the same inputs with confirmed: true.", ErrShadowDecisionInvalid)
}

// shadowDefaultAccessHeldRefusal refuses a change the dashboard would not
// offer: a Guardrails policy that also scans other risks owns the setting,
// and a Shadow AI policy that allows by default cannot be switched to
// blocked here.
func shadowDefaultAccessHeldRefusal(current shadowDefaultAccessCurrent, mode, heldBy string) error {
	if heldBy == shadowDefaultAccessHeldByGuardrails {
		return shadowDecisionError("held_by_guardrails", fmt.Sprintf("The %q policy in Guardrails sets this project's default access because it also scans for other risks. Change it in Guardrails; nothing was changed.", current.row.Name), ErrShadowDecisionInvalid)
	}
	if mode == shadowDefaultAccessBlocked && heldBy == shadowDefaultAccessHeldByShadowAI {
		return shadowDecisionError("held_by_shadow_ai", fmt.Sprintf("The %q Shadow AI policy in Guardrails allows servers by default and holds this project's one blocking slot. Change it in Guardrails; nothing was changed.", current.row.Name), ErrShadowDecisionInvalid)
	}
	return nil
}

// applyDefaultAccess runs inside the receipt transaction. It returns the
// post-commit convergence for the policy core to run once the receipt
// commits.
func (s *ShadowMCPSettingsService) applyDefaultAccess(ctx context.Context, tx pgx.Tx, principal Principal, project ResolvedProject, mode string, references []string, expectedVersion string) (ShadowDefaultAccessReceiptResult, func(context.Context), error) {
	// The same locks, in the same order, the policy core takes, so the state
	// checked here is the state the write replaces.
	if err := shadowadmission.LockProject(ctx, tx, project.ID); err != nil {
		return ShadowDefaultAccessReceiptResult{}, nil, shadowDecisionUnavailable(err)
	}
	if err := riskrepo.New(tx).LockRiskPolicyMutations(ctx, project.ID.String()); err != nil {
		return ShadowDefaultAccessReceiptResult{}, nil, shadowDecisionUnavailable(err)
	}
	current, err := loadShadowDefaultAccess(ctx, tx, principal.OrganizationID, project.ID)
	if err != nil {
		return ShadowDefaultAccessReceiptResult{}, nil, shadowDecisionUnavailable(err)
	}
	if !s.shadow.versions.matchSetting(shadowDefaultAccessVersionDomain, expectedVersion, current.versionState(project.ID)) {
		return ShadowDefaultAccessReceiptResult{}, nil, shadowDecisionError("conflict", "The project's Shadow MCP default access changed after it was read. Read it again and retry with the new version.", ErrShadowDecisionConflict)
	}
	currentMode, heldBy := current.mode()
	if err := shadowDefaultAccessHeldRefusal(current, mode, heldBy); err != nil {
		return ShadowDefaultAccessReceiptResult{}, nil, err
	}
	actor := policycore.Actor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID), DisplayName: nil, Slug: nil}

	if mode == shadowDefaultAccessAllowed {
		// Built in and policy-free; a Shadow AI policy that allows by
		// default already gives this mode.
		if currentMode == shadowDefaultAccessAllowed {
			return ShadowDefaultAccessReceiptResult{Mode: mode, Audience: "", Roles: []ShadowDecisionAudience{}, ResultCategory: shadowSettingUnchanged}, nil, nil
		}
		deleted, err := s.policies.DeletePolicyInTransaction(ctx, tx, policycore.DeleteMutation{
			ProjectID: project.ID, PolicyID: current.row.ID, Actor: actor,
			ValidateLocked: func(_ context.Context, _ pgx.Tx, locked policycore.Policy) error {
				if locked.Version != current.row.Version {
					return shadowDecisionError("conflict", "The project's Shadow MCP default access changed during the write. Read it again and retry.", ErrShadowDecisionConflict)
				}
				return nil
			},
		})
		if err != nil {
			return ShadowDefaultAccessReceiptResult{}, nil, mapShadowSettingPolicyError(err)
		}
		return ShadowDefaultAccessReceiptResult{Mode: mode, Audience: "", Roles: []ShadowDecisionAudience{}, ResultCategory: "removed"}, func(ctx context.Context) {
			s.policies.AfterDeletePolicy(ctx, deleted)
			s.cleanPolicyResults(ctx, project.ID, deleted.Row.ID)
		}, nil
	}

	principals, everyone, err := s.resolveDefaultAccessAudience(ctx, tx, principal, project, references)
	if err != nil {
		return ShadowDefaultAccessReceiptResult{}, nil, err
	}
	audienceType := "targeted"
	audience := shadowDefaultAccessAudienceSelected
	if everyone {
		audienceType = "everyone"
		audience = shadowDefaultAccessAudienceEveryone
		principals = []urn.Principal{authz.AllUsersPrincipal()}
	}
	roleURNs := make([]string, 0, len(principals))
	for _, item := range principals {
		if item.Type == urn.PrincipalTypeRole {
			roleURNs = append(roleURNs, item.String())
		}
	}
	roles, err := shadowRoleSummaries(ctx, tx, principal.OrganizationID, roleURNs)
	if err != nil {
		return ShadowDefaultAccessReceiptResult{}, nil, shadowDecisionUnavailable(err)
	}
	result := ShadowDefaultAccessReceiptResult{Mode: mode, Audience: audience, Roles: roles, ResultCategory: shadowSettingUnchanged}

	if current.row == nil {
		created, err := s.createBlockedByDefault(ctx, tx, principal, project, audienceType, principals, actor)
		if err != nil {
			return ShadowDefaultAccessReceiptResult{}, nil, err
		}
		result.ResultCategory = "created"
		return result, func(ctx context.Context) { s.policies.AfterCreatePolicy(ctx, created) }, nil
	}

	desired := principalStrings(principals)
	if current.row.AudienceType == audienceType && slices.Equal(canonicalStrings(current.audience), desired) {
		return result, nil, nil
	}
	updated, err := s.updateBlockedByDefaultAudience(ctx, tx, principal, project, *current.row, audienceType, principals, actor)
	if err != nil {
		return ShadowDefaultAccessReceiptResult{}, nil, err
	}
	result.ResultCategory = "updated"
	return result, func(ctx context.Context) { s.policies.AfterUpdatePolicy(ctx, updated) }, nil
}

// resolveDefaultAccessAudience turns audience references into the policy
// audience. A risk policy audience holds roles and people only, so directory
// groups and attributes are refused rather than dropped.
func (s *ShadowMCPSettingsService) resolveDefaultAccessAudience(ctx context.Context, tx pgx.Tx, principal Principal, project ResolvedProject, references []string) ([]urn.Principal, bool, error) {
	if len(references) == 0 {
		return nil, true, nil
	}
	urns, _, err := s.audiences.ResolveAssignmentReferences(ctx, tx, principal, project, references)
	if err != nil {
		return nil, false, mapShadowAudienceError(err)
	}
	if slices.Contains(urns, urn.PrincipalWildcard) {
		if len(urns) > 1 {
			return nil, false, shadowDecisionInvalid("The Everyone reference already covers every role. Pass it alone, or pass only roles.")
		}
		return nil, true, nil
	}
	principals := make([]urn.Principal, 0, len(urns))
	for _, value := range urns {
		parsed, err := urn.ParsePrincipal(value)
		if err != nil || parsed.Type != urn.PrincipalTypeRole {
			return nil, false, shadowDecisionInvalid("Blocked by default can be narrowed to roles only. Directory groups and attributes are not supported here; choose roles, or block for everyone.")
		}
		if err := authz.ValidatePrincipal(ctx, tx, principal.OrganizationID, parsed); err != nil {
			if errors.Is(err, authz.ErrPrincipalInvalid) || errors.Is(err, authz.ErrPrincipalNotFound) {
				return nil, false, shadowDecisionNotFound()
			}
			return nil, false, shadowDecisionUnavailable(err)
		}
		principals = append(principals, parsed)
	}
	slices.SortFunc(principals, func(a, b urn.Principal) int { return compareStrings(a.String(), b.String()) })
	return principals, false, nil
}

func (s *ShadowMCPSettingsService) createBlockedByDefault(ctx context.Context, tx pgx.Tx, principal Principal, project ResolvedProject, audienceType string, principals []urn.Principal, actor policycore.Actor) (policycore.MutationResult, error) {
	sources := []string{shadowmcp.SourceShadowMCP}
	if err := policycore.ValidateSourceAction(sources, "block"); err != nil {
		return policycore.MutationResult{}, shadowDecisionUnavailable(err)
	}
	analyzer, err := ra.WithPresidioScoreThreshold(nil, nil)
	if err != nil {
		return policycore.MutationResult{}, shadowDecisionUnavailable(err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return policycore.MutationResult{}, shadowDecisionUnavailable(err)
	}
	created, err := s.policies.CreatePolicyInTransaction(ctx, tx, policycore.CreateMutation{
		Params: riskrepo.CreateRiskPolicyParams{
			ID: id, ProjectID: project.ID, OrganizationID: principal.OrganizationID, Name: shadowDefaultAccessPolicyName,
			PolicyType: ra.PolicyTypeStandard, Sources: sources, PresidioEntities: []string{}, AnalyzerConfig: analyzer, McpScope: nil, ApprovalConfig: nil,
			PromptInjectionRules: []string{}, DisabledRules: []string{}, CustomRuleIds: []string{},
			Enabled: true, Action: "block", AudienceType: audienceType, ShadowMcpDisposition: conv.ToPGText(shadowmcp.DispositionBlockAll), AutoName: false,
			UserMessage: pgtype.Text{String: "", Valid: false}, Prompt: pgtype.Text{String: "", Valid: false}, ModelConfig: nil,
			Score: pgtype.Float8{Float64: shadowDefaultAccessPolicyScore, Valid: true},
		},
		AudiencePrincipals: principals,
		AllowedURLs:        nil,
		AllowedURLsSet:     false,
		BlockedURLs:        nil,
		BlockedURLsSet:     false,
		Actor:              actor,
	})
	if err != nil {
		return policycore.MutationResult{}, mapShadowSettingPolicyError(err)
	}
	return created, nil
}

func (s *ShadowMCPSettingsService) updateBlockedByDefaultAudience(ctx context.Context, tx pgx.Tx, principal Principal, project ResolvedProject, current riskrepo.RiskPolicy, audienceType string, principals []urn.Principal, actor policycore.Actor) (policycore.MutationResult, error) {
	params := riskrepo.UpdateRiskPolicyParams{
		ID: current.ID, ProjectID: project.ID, Name: current.Name, Sources: slices.Clone(current.Sources), PresidioEntities: slices.Clone(current.PresidioEntities), AnalyzerConfig: slices.Clone(current.AnalyzerConfig), McpScope: slices.Clone(current.McpScope), ApprovalConfig: slices.Clone(current.ApprovalConfig),
		PromptInjectionRules: slices.Clone(current.PromptInjectionRules), DisabledRules: slices.Clone(current.DisabledRules), CustomRuleIds: slices.Clone(current.CustomRuleIds),
		Enabled: current.Enabled, Action: current.Action, AudienceType: audienceType, AutoName: current.AutoName,
		UserMessage: current.UserMessage, Prompt: current.Prompt, ModelConfig: slices.Clone(current.ModelConfig), Score: pgtype.Float8{Float64: current.Score, Valid: true},
	}
	updated, err := s.policies.UpdatePolicyInTransaction(ctx, tx, policycore.UpdateMutation{
		Current: current, Params: params, AudiencePrincipals: principals, AudienceChanged: true, AudienceDelta: nil,
		AllowedURLs: nil, AllowedURLsSet: false, BlockedURLs: nil, BlockedURLsSet: false,
		EffectiveDisposition: shadowmcp.EffectiveDisposition(current.ShadowMcpDisposition, current.Sources, current.Action),
		SupersedeDecisions:   false,
		ValidateLocked: func(_ context.Context, _ pgx.Tx, locked policycore.Policy) (policycore.Policy, error) {
			if locked.Version != current.Version || locked.OrganizationID != principal.OrganizationID {
				return policycore.Policy{}, shadowDecisionError("conflict", "The project's Shadow MCP default access changed during the write. Read it again and retry.", ErrShadowDecisionConflict)
			}
			return locked, nil
		},
		Actor: actor,
	})
	if err != nil {
		return policycore.MutationResult{}, mapShadowSettingPolicyError(err)
	}
	return updated, nil
}

// SetOfficialServer links a remote Shadow MCP server to a project MCP
// server, switches the link off or on, or removes it, through the same write
// path as the dashboard.
func (s *ShadowMCPSettingsService) SetOfficialServer(ctx context.Context, principal Principal, input SetShadowMCPOfficialServerInput) (SetShadowMCPOfficialServerOutput, error) {
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.TargetReference = strings.TrimSpace(input.TargetReference)
	input.Action = strings.TrimSpace(input.Action)
	input.MCPID = strings.TrimSpace(input.MCPID)
	input.ExpectedVersion = strings.TrimSpace(input.ExpectedVersion)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.ProjectID == "" || input.TargetReference == "" || input.ExpectedVersion == "" || !validShadowSettingKey(input.IdempotencyKey) {
		return SetShadowMCPOfficialServerOutput{}, shadowDecisionInvalid("The Shadow MCP official server request is invalid.")
	}
	if !slices.Contains([]string{shadowOfficialLinkActionLink, shadowOfficialLinkActionSwitchOff, shadowOfficialLinkActionSwitchOn, shadowOfficialLinkActionRemove}, input.Action) {
		return SetShadowMCPOfficialServerOutput{}, shadowDecisionInvalid("Action must be link, switch_off, switch_on or remove.")
	}
	chosen := uuid.NullUUID{UUID: uuid.Nil, Valid: false}
	if input.Action == shadowOfficialLinkActionLink {
		id, err := uuid.Parse(input.MCPID)
		if err != nil {
			return SetShadowMCPOfficialServerOutput{}, shadowDecisionInvalid("Link needs the mcp_id of a project MCP server from find_mcp.")
		}
		chosen = uuid.NullUUID{UUID: id, Valid: true}
		input.MCPID = id.String()
	}
	if input.Action != shadowOfficialLinkActionLink && input.MCPID != "" {
		return SetShadowMCPOfficialServerOutput{}, shadowDecisionInvalid("Only link takes an mcp_id; switch_off, switch_on and remove act on the current link.")
	}
	if !s.valid() || principal.OrganizationID == "" || principal.UserID == "" {
		return SetShadowMCPOfficialServerOutput{}, shadowDecisionUnavailable(nil)
	}
	project, err := s.shadow.projects.Resolve(ctx, principal.OrganizationID, input.ProjectID, "")
	if err != nil {
		return SetShadowMCPOfficialServerOutput{}, mapShadowDecisionProjectError(err)
	}
	if err := admitShadowAccessWrites(ctx, s.flags, s.organizations, principal, project); err != nil {
		return SetShadowMCPOfficialServerOutput{}, err
	}
	if !input.Confirmed {
		return SetShadowMCPOfficialServerOutput{}, shadowDecisionError("confirmation_required", "Nothing was changed. Confirming "+shadowOfficialLinkActionSummary(input.Action)+" Show this to the user with the Shadow MCP server and the MCP server it names, then resend the same inputs with confirmed: true.", ErrShadowDecisionInvalid)
	}
	normalized := normalizedShadowOfficialLink{ProjectID: project.ID.String(), TargetReference: input.TargetReference, Action: input.Action, MCPID: input.MCPID, ExpectedVersion: input.ExpectedVersion}
	receipt, err := executeShadowSettingReceipt(ctx, s.db, s.now, principal, project, operationSetShadowMCPOfficialServer, input.IdempotencyKey, normalized, chargeShadowAccessWrite(s.budget, principal), validShadowOfficialLinkReceipt,
		func(ctx context.Context, tx pgx.Tx) (ShadowOfficialLinkReceiptResult, error) {
			targetKind, targetKey, err := s.shadow.ResolveTargetReference(principal, project.ID.String(), input.TargetReference)
			if err != nil {
				return ShadowOfficialLinkReceiptResult{}, shadowDecisionNotFound()
			}
			if targetKind != shadowTargetKindServerURL {
				return ShadowOfficialLinkReceiptResult{}, shadowDecisionInvalid("Only a remote Shadow MCP server can have an official server. Local commands cannot be linked.")
			}
			result, err := s.links.SetInTransaction(ctx, tx, officiallink.SetInput{
				OrganizationID:    principal.OrganizationID,
				ProjectID:         project.ID,
				ServerURL:         targetKey,
				MCPServerID:       chosen,
				KeepLinkedServer:  input.Action == shadowOfficialLinkActionSwitchOff || input.Action == shadowOfficialLinkActionSwitchOn,
				AllowDirectAccess: input.Action == shadowOfficialLinkActionSwitchOff,
				ActorUserID:       principal.UserID,
				Actor:             urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID),
				ActorDisplayName:  nil,
				ValidateLocked: func(key string, link officiallink.Link) error {
					if !s.shadow.versions.matchSetting(shadowOfficialLinkVersionDomain, input.ExpectedVersion, shadowOfficialLinkVersionState(project.ID, key, link)) {
						return shadowDecisionError("conflict", "The Shadow MCP server's official link changed after it was read. Read it again and retry with the new version.", ErrShadowDecisionConflict)
					}
					return nil
				},
			})
			if err != nil {
				return ShadowOfficialLinkReceiptResult{}, mapShadowOfficialLinkError(err)
			}
			category := shadowSettingUnchanged
			if result.Changed {
				category = shadowSettingChanged
			}
			receiptResult := ShadowOfficialLinkReceiptResult{Action: input.Action, LinkState: shadowOfficialLinkState(result.After), MCPID: "", ResultCategory: category}
			if result.After.Linked() {
				receiptResult.MCPID = result.After.MCPServerID.String()
			}
			return receiptResult, nil
		})
	if err != nil {
		return SetShadowMCPOfficialServerOutput{}, err
	}
	var stored ShadowOfficialLinkReceiptResult
	if err := json.Unmarshal(receipt.ResultPayload, &stored); err != nil || !validShadowOfficialLinkReceipt(stored) {
		return SetShadowMCPOfficialServerOutput{}, shadowDecisionUnavailable(err)
	}
	output := SetShadowMCPOfficialServerOutput{ShadowOfficialLinkReceiptResult: stored, State: nil, StateStatus: "pending", Receipt: riskMutationToolReceipt(receipt)}
	state, stateErr := s.shadow.GetReview(ctx, principal, GetShadowMCPReviewInput{ProjectSlug: "", ProjectID: project.ID.String(), TargetReference: input.TargetReference})
	if stateErr == nil {
		output.State = &state
		output.StateStatus = "complete"
	}
	return output, nil
}

func shadowOfficialLinkActionSummary(action string) string {
	switch action {
	case shadowOfficialLinkActionLink:
		return "makes the chosen MCP server this Shadow MCP server's official version: agents that connect to the Shadow MCP server directly are blocked and sent to the official server, whatever the default access."
	case shadowOfficialLinkActionSwitchOff:
		return "switches the link off: the official server stays linked, but direct use of the Shadow MCP server follows the ordinary Shadow MCP rules again."
	case shadowOfficialLinkActionSwitchOn:
		return "switches the link back on: direct use of the Shadow MCP server is blocked again and agents are sent to the official server."
	default:
		return "removes the link: the Shadow MCP server has no official version, and direct use follows the ordinary Shadow MCP rules."
	}
}

// admitShadowAccessWrites checks the exact-project rollout cohort every
// Shadow MCP write shares. Missing providers, errors and indeterminate
// evaluations fail closed.
func admitShadowAccessWrites(ctx context.Context, flags feature.Provider, organizations OrganizationSlugResolver, principal Principal, project ResolvedProject) error {
	organizationSlug, err := organizations.OrganizationSlug(ctx, principal.OrganizationID)
	if err != nil || organizationSlug == "" {
		return shadowDecisionUnavailable(err)
	}
	groups := feature.OrgProjectGroups(organizationSlug, project.Slug)
	evaluation, err := feature.EvaluateFlag(ctx, flags, feature.FlagPlatformMCPShadowAccessDecisions, principal.OrganizationID, groups)
	if err != nil || evaluation != feature.EvaluationEnabled {
		return shadowDecisionUnavailable(err)
	}
	return nil
}

// chargeShadowAccessWrite returns the budget charge for one Shadow MCP
// write. The receipt store runs it only when no completed receipt answers
// the request, and outside any transaction.
func chargeShadowAccessWrite(budget OperationBudget, principal Principal) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := budget.AllowConnectionOrOrganization(ctx, principal); err != nil {
			if errors.Is(err, ErrOperationRateLimited) {
				return shadowDecisionError("rate_limited", "The Shadow MCP write rate limit was reached.", err)
			}
			return shadowDecisionUnavailable(err)
		}
		return nil
	}
}

// executeShadowSettingReceipt writes a Shadow MCP setting change and its
// operation receipt in one transaction.
func executeShadowSettingReceipt[T any](ctx context.Context, db *pgxpool.Pool, now func() time.Time, principal Principal, project ResolvedProject, operation, key string, normalized any, charge func(context.Context) error, valid func(T) bool, mutate func(context.Context, pgx.Tx) (T, error)) (OperationReceipt, error) {
	payload, err := json.Marshal(normalized)
	if err != nil {
		return OperationReceipt{}, shadowDecisionInvalid("The Shadow MCP request could not be normalized.")
	}
	digest := uuid.NewSHA1(uuid.NameSpaceOID, append([]byte(operation), payload...)).String()
	return executeChargedMutationReceipt(ctx, charge, mutationReceiptExecution[T]{
		DB: db, Now: now, Principal: principal, Project: project, Operation: operation,
		IdempotencyKey: key, InputHash: digest, Label: strings.ReplaceAll(operation, "_", " "),
		Invalid:     func(error) error { return shadowDecisionInvalid("The caller identity is invalid.") },
		Conflict:    func(message string) error { return shadowDecisionError("conflict", message, ErrShadowDecisionConflict) },
		Unavailable: shadowDecisionUnavailable,
		ValidateReplay: func(payload []byte) bool {
			var result T
			return json.Unmarshal(payload, &result) == nil && valid(result)
		},
		EncodeResult: func(result T) ([]byte, error) {
			if !valid(result) {
				return nil, shadowDecisionUnavailable(fmt.Errorf("invalid %s receipt result", operation))
			}
			return json.Marshal(result)
		},
		Mutate: mutate,
	})
}

func validShadowSettingKey(key string) bool {
	return key != "" && len(key) <= maxShadowSettingIdempotencyKey
}

func validShadowDefaultAccessReceipt(result ShadowDefaultAccessReceiptResult) bool {
	if result.Roles == nil || !slices.Contains([]string{"created", "updated", "removed", shadowSettingUnchanged}, result.ResultCategory) {
		return false
	}
	switch result.Mode {
	case shadowDefaultAccessAllowed:
		return result.Audience == ""
	case shadowDefaultAccessBlocked:
		return result.Audience == shadowDefaultAccessAudienceEveryone || result.Audience == shadowDefaultAccessAudienceSelected
	default:
		return false
	}
}

func validShadowOfficialLinkReceipt(result ShadowOfficialLinkReceiptResult) bool {
	return slices.Contains([]string{shadowOfficialLinkActionLink, shadowOfficialLinkActionSwitchOff, shadowOfficialLinkActionSwitchOn, shadowOfficialLinkActionRemove}, result.Action) &&
		slices.Contains([]string{shadowOfficialLinkOn, shadowOfficialLinkOff, shadowOfficialLinkNone}, result.LinkState) &&
		(result.LinkState == shadowOfficialLinkNone) == (result.MCPID == "") &&
		(result.ResultCategory == shadowSettingChanged || result.ResultCategory == shadowSettingUnchanged)
}

func mapShadowSettingPolicyError(err error) error {
	if _, ok := errors.AsType[*ShadowDecisionError](err); ok {
		return err
	}
	var blocking *policycore.BlockingPolicyConflictError
	var stale *policycore.StalePolicyError
	var decisions *policycore.DecisionConflictError
	var validation *policycore.ValidationError
	switch {
	case errors.Is(err, mcpapproval.ErrNarrowDenialUnderEveryoneBlockAll):
		return shadowDecisionInvalid("This project has standing denials for specific people, and blocking by default for everyone would block those servers for everyone. Re-decide those servers as denied for everyone with decide_shadow_mcp_access, or block by default only for specific roles.")
	case errors.As(err, &blocking):
		return shadowDecisionError("conflict", "Another policy that blocks Shadow MCP servers was turned on in this project. Read the default access again before retrying.", ErrShadowDecisionConflict)
	case errors.As(err, &stale), errors.As(err, &decisions), errors.Is(err, policycore.ErrLoadPolicy):
		return shadowDecisionError("conflict", "The project's Shadow MCP default access changed during the write. Read it again and retry.", ErrShadowDecisionConflict)
	case errors.As(err, &validation):
		return shadowDecisionInvalid(validation.Message)
	default:
		return shadowDecisionUnavailable(err)
	}
}

func mapShadowOfficialLinkError(err error) error {
	if _, ok := errors.AsType[*ShadowDecisionError](err); ok {
		return err
	}
	switch {
	case errors.Is(err, officiallink.ErrMCPServerNotFound):
		return shadowDecisionError("not_found", "That MCP server is not live in this project. Call find_mcp in the same project and use its id.", ErrShadowDecisionNotFound)
	case errors.Is(err, officiallink.ErrMCPServerDisabled):
		return shadowDecisionInvalid("That MCP server is disabled, so it cannot be the official version. Turn it on first, or choose another server.")
	case errors.Is(err, officiallink.ErrLinkedServerRequired):
		return shadowDecisionInvalid("This Shadow MCP server has no official server linked, so there is no link to switch. Use link with an mcp_id first.")
	case errors.Is(err, officiallink.ErrInvalidServerURL):
		return shadowDecisionInvalid("This Shadow MCP server cannot be linked.")
	default:
		return shadowDecisionUnavailable(err)
	}
}

// principalStrings is the canonical, sorted form of a policy audience.
func principalStrings(principals []urn.Principal) []string {
	values := make([]string, 0, len(principals))
	for _, item := range principals {
		values = append(values, item.String())
	}
	return canonicalStrings(values)
}
