package platformmcp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	accessgen "github.com/speakeasy-api/gram/server/gen/access"
	"github.com/speakeasy-api/gram/server/internal/access"
	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/feature"
	"github.com/speakeasy-api/gram/server/internal/oops"
	organizationsrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
	platformrepo "github.com/speakeasy-api/gram/server/internal/platformmcp/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	operationCreateMCPAccessRole = "create_mcp_access_role"
	operationUpdateMCPAccessRole = "update_mcp_access_role"
	operationAssignMCPAccessRole = "assign_mcp_access_role"

	maxAccessRoleMutationRules       = 100
	maxAccessRoleMutationNameRunes   = 128
	maxAccessRoleMutationDescription = 1000
)

var (
	ErrAccessRoleMutationUnavailable = errors.New("platform mcp access role mutations unavailable")
	ErrAccessRoleMutationInvalid     = errors.New("invalid platform mcp access role mutation")
	ErrAccessRoleMutationNotFound    = errors.New("platform mcp access role mutation target not found")
	ErrAccessRoleMutationConflict    = errors.New("platform mcp access role mutation conflict")
)

// AccessRoleMutationError is safe to return through Platform MCP. It never
// includes a role ID, principal, grant selector, or configured MCP ID.
type AccessRoleMutationError struct {
	Code    string
	Message string
	Cause   error
}

func (e *AccessRoleMutationError) Error() string { return e.Message }
func (e *AccessRoleMutationError) Unwrap() error { return e.Cause }

// MCPAccessRoleRule is a caller-facing request for one server-generated
// mcp:connect selector. MCPID must be a configured MCP returned for the explicit
// project. Tool, when present, is accepted only from an enumerable current
// catalog. Disposition is a closed annotation class, never an open selector.
type MCPAccessRoleRule struct {
	MCPID       string `json:"mcp_id" jsonschema:"configured MCP ID returned by find_mcp for the explicit project"`
	Tool        string `json:"tool,omitempty" jsonschema:"optional exact tool name currently returned by get_mcp_access"`
	Disposition string `json:"disposition,omitempty" jsonschema:"optional closed tool disposition: read_only, destructive, idempotent, or open_world"`
}

type CreateMCPAccessRoleInput struct {
	ProjectID      string              `json:"project_id" jsonschema:"explicit project ID that owns every configured MCP in rules"`
	Name           string              `json:"name" jsonschema:"display name for the new custom role"`
	Description    string              `json:"description,omitempty" jsonschema:"optional description for the new custom role"`
	Rules          []MCPAccessRoleRule `json:"rules" jsonschema:"MCP access rules generated only for configured MCP IDs in this project"`
	IdempotencyKey string              `json:"idempotency_key" jsonschema:"stable unique key for safely retrying this exact write"`
	Confirmed      bool                `json:"confirmed" jsonschema:"set true only after the user confirms this exact project, role, and MCP access delta"`
}

type UpdateMCPAccessRoleInput struct {
	ProjectID       string              `json:"project_id" jsonschema:"explicit project ID that owns every configured MCP in the delta"`
	RoleReference   string              `json:"role_reference" jsonschema:"opaque role reference returned by list_access_roles, get_mcp_access, or a role mutation"`
	ExpectedVersion string              `json:"expected_version" jsonschema:"opaque role version returned by the preceding role mutation"`
	AddRules        []MCPAccessRoleRule `json:"add_rules,omitempty" jsonschema:"MCP access rules to add without replacing other grants"`
	RemoveRules     []MCPAccessRoleRule `json:"remove_rules,omitempty" jsonschema:"MCP access rules to remove without replacing other grants"`
	IdempotencyKey  string              `json:"idempotency_key" jsonschema:"stable unique key for safely retrying this exact write"`
	Confirmed       bool                `json:"confirmed" jsonschema:"set true only after the user confirms this exact project, role, and MCP access delta"`
}

type AccessRoleMutationSummary struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Reference   string            `json:"reference"`
	Version     string            `json:"version"`
	MCPAccess   MCPConnectSummary `json:"mcp_access"`
}

type CreateMCPAccessRoleOutput struct {
	Role           AccessRoleMutationSummary `json:"role"`
	Reconciliation string                    `json:"reconciliation"`
	Receipt        RiskMutationToolReceipt   `json:"receipt"`
}

type UpdateMCPAccessRoleOutput struct {
	Role           AccessRoleMutationSummary `json:"role"`
	Reconciliation string                    `json:"reconciliation"`
	Receipt        RiskMutationToolReceipt   `json:"receipt"`
}

// AccessRoleMutationBackend matches the transaction-scoped access manager API.
type AccessRoleMutationBackend interface {
	MutationReady() bool
	GetRoleByIDTx(context.Context, pgx.Tx, string, string) (*accessgen.Role, error)
	CreateRoleTx(context.Context, pgx.Tx, string, string, access.RoleAuditActor, *accessgen.CreateRolePayload) (access.RoleCreateResult, access.RoleReconciliation, error)
	UpdateRoleTx(context.Context, pgx.Tx, string, string, access.RoleAuditActor, *accessgen.UpdateRolePayload) (access.RoleUpdateResult, access.RoleReconciliation, error)
	AddMemberRoleTx(context.Context, pgx.Tx, string, string, string, access.RoleAuditActor, access.MemberRoleValidation) (access.MemberRoleAddResult, access.MemberRoleReconciliation, error)
	CurrentMemberRoleReconciliationTx(context.Context, pgx.Tx, string, string) (access.MemberRoleReconciliation, error)
	ReconcileRoleIdentity(context.Context, string, string, string, string, bool)
	ReconcileMemberRoles(context.Context, access.MemberRoleReconciliation)
}

type normalizedMCPAccessRoleRule struct {
	MCPID       string `json:"mcp_id"`
	Tool        string `json:"tool,omitempty"`
	Disposition string `json:"disposition,omitempty"`
}

type normalizedCreateMCPAccessRole struct {
	ProjectID   string                        `json:"project_id"`
	Name        string                        `json:"name"`
	Description string                        `json:"description,omitempty"`
	Rules       []normalizedMCPAccessRoleRule `json:"rules"`
}

type normalizedUpdateMCPAccessRole struct {
	ProjectID       string                        `json:"project_id"`
	RoleID          string                        `json:"role_id"`
	ExpectedVersion string                        `json:"expected_version"`
	AddRules        []normalizedMCPAccessRoleRule `json:"add_rules"`
	RemoveRules     []normalizedMCPAccessRoleRule `json:"remove_rules"`
}

type AccessRoleMutationService struct {
	reads      *AccessReadService
	flags      feature.Provider
	budget     OperationBudget
	backend    AccessRoleMutationBackend
	receipts   *AccessRoleMutationReceiptStore
	versionKey []byte
}

func NewAccessRoleMutationService(reads *AccessReadService, flags feature.Provider, budget OperationBudget, keyMaterial string, backend AccessRoleMutationBackend) (*AccessRoleMutationService, error) {
	if reads == nil || reads.db == nil || reads.references == nil || reads.now == nil || flags == nil || !budget.valid() || keyMaterial == "" || backend == nil || !backend.MutationReady() {
		return nil, ErrAccessRoleMutationUnavailable
	}
	key := sha256.Sum256([]byte("platform-mcp-access-role-version:" + keyMaterial))
	return &AccessRoleMutationService{
		reads: reads, flags: flags, budget: budget, backend: backend,
		receipts: NewAccessRoleMutationReceiptStore(reads.db), versionKey: key[:],
	}, nil
}

func (s *AccessRoleMutationService) valid() bool {
	return s != nil && s.reads != nil && s.reads.valid() && s.flags != nil && s.budget.valid() && s.backend != nil && s.receipts != nil && len(s.versionKey) == sha256.Size
}

func (s *AccessRoleMutationService) Create(ctx context.Context, principal Principal, input CreateMCPAccessRoleInput) (CreateMCPAccessRoleOutput, error) {
	if !input.Confirmed {
		return CreateMCPAccessRoleOutput{}, accessRoleMutationConfirmationRequired()
	}
	if !s.valid() {
		return CreateMCPAccessRoleOutput{}, accessRoleMutationUnavailable(nil)
	}
	name, description, idempotencyKey, err := normalizeAccessRoleIdentity(input.Name, input.Description, input.IdempotencyKey)
	if err != nil {
		return CreateMCPAccessRoleOutput{}, err
	}
	project, workosOrgID, err := s.admit(ctx, principal, input.ProjectID)
	if err != nil {
		return CreateMCPAccessRoleOutput{}, err
	}
	rules, err := s.resolveRules(ctx, principal, project, input.Rules, true, make(map[uuid.UUID]accessRoleRuleTarget))
	if err != nil {
		return CreateMCPAccessRoleOutput{}, err
	}
	if len(rules) == 0 {
		return CreateMCPAccessRoleOutput{}, accessRoleMutationInvalid("At least one MCP access rule is required to create an MCP access role.")
	}
	normalized := normalizedCreateMCPAccessRole{ProjectID: project.ID.String(), Name: name, Description: description, Rules: rules}
	receipt, err := s.receipts.ExecuteCreate(ctx, principal, project, idempotencyKey, normalized, func(ctx context.Context, tx pgx.Tx) (AccessRoleMutationReceiptResult, error) {
		result, _, err := s.backend.CreateRoleTx(ctx, tx, principal.OrganizationID, workosOrgID, access.RoleAuditActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID), DisplayName: nil}, &accessgen.CreateRolePayload{ApikeyToken: nil, SessionToken: nil, Name: name, Description: conv.PtrEmpty(description), Grants: accessRoleRulesToGenGrants(rules, project.ID), MemberIds: nil, AgentIds: nil})
		if err != nil {
			return AccessRoleMutationReceiptResult{}, classifyAccessRoleBackendError(err)
		}
		return s.receiptResult(result.Role, result.Slug, "created", "pending")
	})
	if err != nil {
		return CreateMCPAccessRoleOutput{}, err
	}
	result, err := decodeAccessRoleMutationReceipt(operationCreateMCPAccessRole, receipt.ResultPayload)
	if err != nil {
		return CreateMCPAccessRoleOutput{}, err
	}
	s.backend.ReconcileRoleIdentity(ctx, workosOrgID, result.RoleSlug, result.Name, result.Description, true)
	summary, err := s.outputSummary(principal, result)
	if err != nil {
		return CreateMCPAccessRoleOutput{}, err
	}
	return CreateMCPAccessRoleOutput{Role: summary, Reconciliation: result.Reconciliation, Receipt: riskMutationToolReceipt(receipt)}, nil
}

func (s *AccessRoleMutationService) Update(ctx context.Context, principal Principal, input UpdateMCPAccessRoleInput) (UpdateMCPAccessRoleOutput, error) {
	if !input.Confirmed {
		return UpdateMCPAccessRoleOutput{}, accessRoleMutationConfirmationRequired()
	}
	if !s.valid() {
		return UpdateMCPAccessRoleOutput{}, accessRoleMutationUnavailable(nil)
	}
	input.ProjectID = strings.TrimSpace(input.ProjectID)
	input.RoleReference = strings.TrimSpace(input.RoleReference)
	input.ExpectedVersion = strings.TrimSpace(input.ExpectedVersion)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if principal.UserID == "" || input.RoleReference == "" || !validAccessRoleVersion(input.ExpectedVersion) || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 128 {
		return UpdateMCPAccessRoleOutput{}, accessRoleMutationInvalid("The access role update request is invalid.")
	}
	if len(input.AddRules) == 0 && len(input.RemoveRules) == 0 {
		return UpdateMCPAccessRoleOutput{}, accessRoleMutationInvalid("Supply at least one MCP access rule to add or remove.")
	}
	project, workosOrgID, err := s.admit(ctx, principal, input.ProjectID)
	if err != nil {
		return UpdateMCPAccessRoleOutput{}, err
	}
	roleID, err := s.reads.references.Decode(input.RoleReference, principal, subjectKindAccessRole, s.reads.now().UTC())
	if err != nil {
		return UpdateMCPAccessRoleOutput{}, accessRoleMutationNotFound()
	}
	targets := make(map[uuid.UUID]accessRoleRuleTarget)
	addRules, err := s.resolveRules(ctx, principal, project, input.AddRules, true, targets)
	if err != nil {
		return UpdateMCPAccessRoleOutput{}, err
	}
	// Removal only narrows access. Validate its closed shape, but do not require
	// the server or tool to remain in today's catalog: stale grants must stay
	// removable after a server is deleted or its dynamic catalog changes.
	removeRules, err := s.resolveRules(ctx, principal, project, input.RemoveRules, false, targets)
	if err != nil {
		return UpdateMCPAccessRoleOutput{}, err
	}
	if overlappingAccessRoleRules(addRules, removeRules) {
		return UpdateMCPAccessRoleOutput{}, accessRoleMutationInvalid("The same MCP access rule cannot be both added and removed.")
	}
	normalized := normalizedUpdateMCPAccessRole{
		ProjectID: project.ID.String(), RoleID: roleID, ExpectedVersion: input.ExpectedVersion,
		AddRules: addRules, RemoveRules: removeRules,
	}
	receipt, err := s.receipts.ExecuteUpdate(ctx, principal, project, input.IdempotencyKey, normalized, func(ctx context.Context, tx pgx.Tx) (AccessRoleMutationReceiptResult, error) {
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
		result, _, err := s.backend.UpdateRoleTx(ctx, tx, principal.OrganizationID, workosOrgID, access.RoleAuditActor{Principal: urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID), DisplayName: nil}, &accessgen.UpdateRolePayload{ApikeyToken: nil, SessionToken: nil, ID: roleID, Name: nil, Description: nil, AddGrants: accessRoleRulesToGenGrants(addRules, project.ID), RemoveGrants: accessRoleRulesToGenGrants(removeRules, project.ID), MemberIds: nil, AgentIds: nil})
		if err != nil {
			return AccessRoleMutationReceiptResult{}, classifyAccessRoleBackendError(err)
		}
		if result.After == nil {
			return AccessRoleMutationReceiptResult{}, accessRoleMutationNotFound()
		}
		return s.receiptResult(result.After, result.Slug, "updated", "complete")
	})
	if err != nil {
		return UpdateMCPAccessRoleOutput{}, err
	}
	result, err := decodeAccessRoleMutationReceipt(operationUpdateMCPAccessRole, receipt.ResultPayload)
	if err != nil {
		return UpdateMCPAccessRoleOutput{}, err
	}
	summary, err := s.outputSummary(principal, result)
	if err != nil {
		return UpdateMCPAccessRoleOutput{}, err
	}
	return UpdateMCPAccessRoleOutput{Role: summary, Reconciliation: result.Reconciliation, Receipt: riskMutationToolReceipt(receipt)}, nil
}

func (s *AccessRoleMutationService) admit(ctx context.Context, principal Principal, projectID string) (ResolvedProject, string, error) {
	if principal.OrganizationID == "" || principal.UserID == "" {
		return ResolvedProject{}, "", accessRoleMutationInvalid("An explicit project and attributable user are required for access role mutations.")
	}
	parsed, err := uuid.Parse(strings.TrimSpace(projectID))
	if err != nil || parsed == uuid.Nil {
		return ResolvedProject{}, "", accessRoleMutationNotFound()
	}
	row, err := platformrepo.New(s.reads.db).ResolvePlatformMCPProjectByID(ctx, platformrepo.ResolvePlatformMCPProjectByIDParams{OrganizationID: principal.OrganizationID, ProjectID: parsed})
	if errors.Is(err, pgx.ErrNoRows) {
		return ResolvedProject{}, "", accessRoleMutationNotFound()
	}
	if err != nil {
		return ResolvedProject{}, "", accessRoleMutationUnavailable(err)
	}
	project := ResolvedProject{ID: row.ID, Name: row.Name, Slug: row.Slug}
	organization, err := organizationsrepo.New(s.reads.db).GetOrganizationMetadata(ctx, principal.OrganizationID)
	if err != nil {
		return ResolvedProject{}, "", accessRoleMutationUnavailable(err)
	}
	if !organization.WorkosID.Valid || organization.WorkosID.String == "" || organization.Slug == "" {
		return ResolvedProject{}, "", accessRoleMutationUnavailable(errors.New("organization role provider metadata is unavailable"))
	}
	evaluation, err := feature.EvaluateFlag(ctx, s.flags, feature.FlagPlatformMCPAccessRoleMutations, principal.OrganizationID, feature.OrgProjectGroups(organization.Slug, project.Slug))
	if err != nil || evaluation != feature.EvaluationEnabled {
		return ResolvedProject{}, "", accessRoleMutationUnavailable(err)
	}
	if err := s.budget.AllowConnectionOrOrganization(ctx, principal); err != nil {
		if errors.Is(err, ErrOperationRateLimited) {
			return ResolvedProject{}, "", &AccessRoleMutationError{Code: "rate_limited", Message: "The access role mutation rate limit was reached.", Cause: err}
		}
		return ResolvedProject{}, "", accessRoleMutationUnavailable(err)
	}
	return project, organization.WorkosID.String, nil
}

type accessRoleRuleTarget struct {
	catalog   string
	tools     []MCPAccessTool
	truncated bool
}

func (s *AccessRoleMutationService) resolveRules(ctx context.Context, principal Principal, project ResolvedProject, raw []MCPAccessRoleRule, requireCurrentTarget bool, targets map[uuid.UUID]accessRoleRuleTarget) ([]normalizedMCPAccessRoleRule, error) {
	if len(raw) > maxAccessRoleMutationRules {
		return nil, accessRoleMutationInvalid("Too many MCP access rules were supplied.")
	}
	result := make([]normalizedMCPAccessRoleRule, 0, len(raw))
	for _, requested := range raw {
		mcpID, err := uuid.Parse(strings.TrimSpace(requested.MCPID))
		if err != nil || mcpID == uuid.Nil {
			return nil, accessRoleMutationNotFound()
		}
		tool := strings.TrimSpace(requested.Tool)
		disposition := strings.ToLower(strings.TrimSpace(requested.Disposition))
		if tool != "" && (len(tool) > 256 || strings.ContainsAny(tool, "\x00\r\n")) {
			return nil, accessRoleMutationInvalid("A requested exact tool name is invalid.")
		}
		if disposition != "" && !validAccessRoleDisposition(disposition) {
			return nil, accessRoleMutationInvalid("Rule disposition must be read_only, destructive, idempotent, or open_world.")
		}
		if !requireCurrentTarget {
			result = append(result, normalizedMCPAccessRoleRule{MCPID: mcpID.String(), Tool: tool, Disposition: disposition})
			continue
		}
		target, checked := targets[mcpID]
		if !checked {
			target, err = s.resolveRuleTarget(ctx, principal, project, mcpID)
			if err != nil {
				return nil, err
			}
			targets[mcpID] = target
		}
		if tool != "" {
			if target.catalog == "dynamic" || target.catalog == "unavailable" || target.truncated {
				return nil, accessRoleMutationInvalid("Exact tool rules require a complete enumerable tool catalog. Use a server or disposition rule instead.")
			}
			if !slices.ContainsFunc(target.tools, func(candidate MCPAccessTool) bool { return candidate.Name == tool }) {
				return nil, accessRoleMutationInvalid("The exact tool is not in the configured MCP's current catalog. Read its access details again rather than guessing a tool name.")
			}
		}
		result = append(result, normalizedMCPAccessRoleRule{MCPID: mcpID.String(), Tool: tool, Disposition: disposition})
	}
	slices.SortFunc(result, compareNormalizedAccessRoleRule)
	return slices.Compact(result), nil
}

func (s *AccessRoleMutationService) resolveRuleTarget(ctx context.Context, principal Principal, project ResolvedProject, mcpID uuid.UUID) (accessRoleRuleTarget, error) {
	row, err := platformrepo.New(s.reads.db).GetPlatformMCPInventoryItem(ctx, platformrepo.GetPlatformMCPInventoryItemParams{
		OrganizationID: principal.OrganizationID, ConnectionID: uuid.NullUUID{UUID: uuid.Nil, Valid: false}, ConnectionGeneration: uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		UserID: inventoryText(principal.UserID), ActingSurface: inventoryText(string(principal.surface())),
		McpServerID: mcpID, ProjectID: project.ID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return accessRoleRuleTarget{}, accessRoleMutationNotFound()
	}
	if err != nil {
		return accessRoleRuleTarget{}, accessRoleMutationUnavailable(err)
	}
	if accessAuthorizationMode(row) != "rbac" {
		return accessRoleRuleTarget{}, accessRoleMutationInvalid("MCP access roles can target only active private MCP servers that use role-based access.")
	}
	tools, catalog, truncated, err := s.reads.accessTools(ctx, row)
	if err != nil {
		return accessRoleRuleTarget{}, accessRoleMutationUnavailable(err)
	}
	return accessRoleRuleTarget{catalog: catalog, tools: tools, truncated: truncated}, nil
}

func accessRoleRulesToGenGrants(rules []normalizedMCPAccessRoleRule, projectID uuid.UUID) []*accessgen.RoleGrant {
	grants := make([]*accessgen.RoleGrant, 0, len(rules))
	for _, rule := range rules {
		scope := authz.ScopeMCPConnect
		selector := authz.NewSelector(scope, rule.MCPID)
		selector[authz.SelectorKeyProjectID] = projectID.String()
		if rule.Tool != "" {
			selector[authz.SelectorKeyTool] = rule.Tool
		}
		if rule.Disposition != "" {
			selector[authz.SelectorKeyDisposition] = rule.Disposition
		}
		generated := &accessgen.Selector{ResourceKind: selector[authz.SelectorKeyResourceKind], ResourceID: selector[authz.SelectorKeyResourceID], Disposition: nil, Tool: nil, ProjectID: nil, ServerURL: nil}
		if value := selector[authz.SelectorKeyProjectID]; value != "" {
			generated.ProjectID = &value
		}
		if value := selector[authz.SelectorKeyTool]; value != "" {
			generated.Tool = &value
		}
		if value := selector[authz.SelectorKeyDisposition]; value != "" {
			generated.Disposition = &value
		}
		grants = append(grants, &accessgen.RoleGrant{Scope: string(scope), Selectors: []*accessgen.Selector{generated}})
	}
	return grants
}

func (s *AccessRoleMutationService) receiptResult(state *accessgen.Role, roleSlug, category, reconciliation string) (AccessRoleMutationReceiptResult, error) {
	if state == nil || state.IsSystem || uuid.Validate(state.ID) != nil || roleSlug == "" {
		return AccessRoleMutationReceiptResult{}, accessRoleMutationUnavailable(errors.New("unsafe role backend result"))
	}
	version, err := s.roleVersion(state)
	if err != nil {
		return AccessRoleMutationReceiptResult{}, accessRoleMutationUnavailable(err)
	}
	return AccessRoleMutationReceiptResult{
		RoleID: state.ID, RoleSlug: roleSlug, Name: state.Name, Description: state.Description, Version: version,
		MCPAccess: summarizeMCPConnect(state.Grants), ResultCategory: category, Reconciliation: reconciliation,
	}, nil
}

func (s *AccessRoleMutationService) outputSummary(principal Principal, result AccessRoleMutationReceiptResult) (AccessRoleMutationSummary, error) {
	reference, err := s.reads.references.Encode(principal, subjectKindAccessRole, result.RoleID, s.reads.now().UTC())
	if err != nil {
		return AccessRoleMutationSummary{}, accessRoleMutationUnavailable(err)
	}
	return AccessRoleMutationSummary{Name: result.Name, Description: result.Description, Reference: reference, Version: result.Version, MCPAccess: result.MCPAccess}, nil
}

type canonicalAccessRoleVersion struct {
	RoleID      string                     `json:"role_id"`
	Name        string                     `json:"name"`
	Description string                     `json:"description"`
	Grants      []canonicalAccessRoleGrant `json:"grants"`
}

type canonicalAccessRoleGrant struct {
	Scope        string `json:"scope"`
	ResourceKind string `json:"resource_kind"`
	ResourceID   string `json:"resource_id"`
	ProjectID    string `json:"project_id,omitempty"`
	Tool         string `json:"tool,omitempty"`
	Disposition  string `json:"disposition,omitempty"`
	ServerURL    string `json:"server_url,omitempty"`
}

func (s *AccessRoleMutationService) roleVersion(state *accessgen.Role) (string, error) {
	return accessRoleVersion(s.versionKey, state)
}

func accessRoleVersion(versionKey []byte, state *accessgen.Role) (string, error) {
	if state == nil || len(versionKey) != sha256.Size || uuid.Validate(state.ID) != nil || state.Name == "" {
		return "", ErrAccessRoleMutationInvalid
	}
	canonical := canonicalAccessRoleVersion{RoleID: state.ID, Name: state.Name, Description: state.Description, Grants: canonicalAccessRoleGrants(state.Grants)}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("encode access role version: %w", err)
	}
	mac := hmac.New(sha256.New, versionKey)
	_, _ = mac.Write([]byte("platform-mcp-access-role-version-v1\x00"))
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func canonicalAccessRoleGrants(grants []*accessgen.RoleGrant) []canonicalAccessRoleGrant {
	result := make([]canonicalAccessRoleGrant, 0)
	for _, grant := range grants {
		if grant == nil {
			continue
		}
		if grant.Selectors == nil {
			result = append(result, canonicalAccessRoleGrant{Scope: grant.Scope, ResourceKind: "", ResourceID: authz.WildcardResource, ProjectID: "", Tool: "", Disposition: "", ServerURL: ""})
			continue
		}
		for _, selector := range grant.Selectors {
			if selector == nil {
				continue
			}
			item := canonicalAccessRoleGrant{Scope: grant.Scope, ResourceKind: selector.ResourceKind, ResourceID: selector.ResourceID, ProjectID: "", Tool: "", Disposition: "", ServerURL: ""}
			if selector.ProjectID != nil {
				item.ProjectID = *selector.ProjectID
			}
			if selector.Tool != nil {
				item.Tool = *selector.Tool
			}
			if selector.Disposition != nil {
				item.Disposition = *selector.Disposition
			}
			if selector.ServerURL != nil {
				item.ServerURL = *selector.ServerURL
			}
			result = append(result, item)
		}
	}
	slices.SortFunc(result, func(a, b canonicalAccessRoleGrant) int {
		return strings.Compare(a.Scope+"\x00"+a.ResourceKind+"\x00"+a.ResourceID+"\x00"+a.ProjectID+"\x00"+a.Tool+"\x00"+a.Disposition+"\x00"+a.ServerURL,
			b.Scope+"\x00"+b.ResourceKind+"\x00"+b.ResourceID+"\x00"+b.ProjectID+"\x00"+b.Tool+"\x00"+b.Disposition+"\x00"+b.ServerURL)
	})
	return slices.Compact(result)
}

func normalizeAccessRoleIdentity(rawName, rawDescription, rawKey string) (string, string, string, error) {
	name := strings.TrimSpace(rawName)
	description := strings.TrimSpace(rawDescription)
	key := strings.TrimSpace(rawKey)
	if !validAccessRoleName(name) || len(description) > maxAccessRoleMutationDescription || key == "" || len(key) > 128 {
		return "", "", "", accessRoleMutationInvalid("The access role create request is invalid.")
	}
	return name, description, key, nil
}

func validAccessRoleName(value string) bool {
	if value == "" || len([]rune(value)) > maxAccessRoleMutationNameRunes {
		return false
	}
	for _, r := range value {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != ' ' && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func validAccessRoleDisposition(value string) bool {
	switch value {
	case authz.DispositionReadOnly, authz.DispositionDestructive, authz.DispositionIdempotent, authz.DispositionOpenWorld:
		return true
	default:
		return false
	}
}

func compareNormalizedAccessRoleRule(a, b normalizedMCPAccessRoleRule) int {
	return strings.Compare(a.MCPID+"\x00"+a.Tool+"\x00"+a.Disposition, b.MCPID+"\x00"+b.Tool+"\x00"+b.Disposition)
}

func overlappingAccessRoleRules(add, remove []normalizedMCPAccessRoleRule) bool {
	for _, rule := range add {
		if _, found := slices.BinarySearchFunc(remove, rule, compareNormalizedAccessRoleRule); found {
			return true
		}
	}
	return false
}

func accessMemberRoleVersion(versionKey []byte, memberID string, roleIDs []string) (string, error) {
	if len(versionKey) != sha256.Size || strings.TrimSpace(memberID) == "" {
		return "", ErrAccessRoleMutationInvalid
	}
	canonicalRoles := sortedStrings(roleIDs)
	if canonicalRoles == nil {
		canonicalRoles = []string{}
	}
	payload, err := json.Marshal(struct {
		MemberID string   `json:"member_id"`
		RoleIDs  []string `json:"role_ids"`
	}{MemberID: memberID, RoleIDs: canonicalRoles})
	if err != nil {
		return "", fmt.Errorf("encode access member role version: %w", err)
	}
	mac := hmac.New(sha256.New, versionKey)
	_, _ = mac.Write([]byte("platform-mcp-access-member-role-version-v1\x00"))
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func validAccessRoleVersion(value string) bool {
	if len(value) != base64.RawURLEncoding.EncodedLen(sha256.Size) {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func classifyAccessRoleBackendError(err error) error {
	var shareable *oops.ShareableError
	switch {
	case errors.Is(err, access.ErrRoleNotFound), errors.Is(err, ErrAccessRoleMutationNotFound):
		return accessRoleMutationNotFound()
	case errors.Is(err, ErrAccessRoleMutationConflict):
		return accessRoleMutationConflict("The access role changed before the mutation completed. Read it again and retry.")
	case errors.Is(err, ErrAccessRoleMutationInvalid):
		return accessRoleMutationInvalid("The access role mutation is no longer valid.")
	case errors.As(err, &shareable) && shareable.Code == oops.CodeConflict:
		return accessRoleMutationConflict("A custom role with that name already exists. Choose another name.")
	case errors.As(err, &shareable) && (shareable.Code == oops.CodeBadRequest || shareable.Code == oops.CodeInvalid):
		return accessRoleMutationInvalid("The access role mutation is no longer valid.")
	default:
		return accessRoleMutationUnavailable(err)
	}
}

func accessRoleMutationConfirmationRequired() error {
	return &AccessRoleMutationError{Code: "confirmation_required", Message: "Show the exact project, role name or reference, and complete MCP access delta; ask the user to confirm it, then retry with confirmed: true.", Cause: ErrAccessRoleMutationInvalid}
}

func accessRoleMutationInvalid(message string) error {
	return &AccessRoleMutationError{Code: "invalid_request", Message: message, Cause: ErrAccessRoleMutationInvalid}
}

func accessRoleMutationNotFound() error {
	return &AccessRoleMutationError{Code: "not_found", Message: "The selected project, role, or configured MCP is unavailable. Read the current access state and choose from that result.", Cause: ErrAccessRoleMutationNotFound}
}

func accessRoleMutationConflict(message string) error {
	return &AccessRoleMutationError{Code: "conflict", Message: message, Cause: ErrAccessRoleMutationConflict}
}

func accessRoleMutationUnavailable(cause error) error {
	if cause == nil {
		return &AccessRoleMutationError{Code: unavailableCode, Message: "Access role mutations are not enabled for this project.", Cause: ErrAccessRoleMutationUnavailable}
	}
	return &AccessRoleMutationError{Code: unavailableCode, Message: "Access role mutations are temporarily unavailable.", Cause: fmt.Errorf("%w: %w", ErrAccessRoleMutationUnavailable, cause)}
}
