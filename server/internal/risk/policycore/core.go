package policycore

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	gentypes "github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ErrLoadPolicy identifies a failure while loading the policy row, before
// audience or progress enrichment. Existing transports use it to preserve their
// established not-found mapping.
var ErrLoadPolicy = errors.New("load risk policy")

// ToolAnnotationsResolver resolves the effective hints for one called tool.
type ToolAnnotationsResolver interface {
	ToolAnnotations(ctx context.Context, mcpServerID, projectID uuid.UUID, toolName string) (*gentypes.ToolAnnotations, error)
}

// MCPTarget identifies one concrete policy-evaluation subject.
type MCPTarget struct {
	// ServerID is the persisted server ID or stable Platform MCP toolset ID.
	ServerID uuid.UUID

	// ToolName is empty when matching only at the server level.
	ToolName string

	// ToolAnnotations carries code-owned annotations when no database resolver applies.
	ToolAnnotations *gentypes.ToolAnnotations

	// PlatformToolset bypasses persisted server ownership and gateway lookup.
	PlatformToolset bool

	// Principal limits results to policies whose audience covers the caller.
	// Nil lists policies for every audience; enforcement seams always set it.
	Principal *MCPPrincipal
}

// MCPPrincipal is the caller an MCP-scoped policy is evaluated for. The zero
// value is unattributed and matches only everyone-audience policies.
type MCPPrincipal struct {
	// UserID is set only for an authoritative acting user.
	UserID string

	// AgentID is set for an agent principal; its roles are resolved too.
	AgentID string
}

// Core provides transport-neutral policy reads and projections. Authorization
// remains the responsibility of the calling service.
type Core struct {
	db      repo.DBTX
	queries *repo.Queries
}

func New(db repo.DBTX) *Core {
	return &Core{db: db, queries: repo.New(db)}
}

// MCPPolicies matches enabled policies against MCP traffic, resolving tool
// annotations for annotation-scoped policies.
type MCPPolicies struct {
	*Core
	toolAnnotations ToolAnnotationsResolver
}

func NewMCPPolicies(db repo.DBTX, resolver ToolAnnotationsResolver) *MCPPolicies {
	return &MCPPolicies{Core: New(db), toolAnnotations: resolver}
}

// PageCursor identifies one policy in deterministic keyset order.
type PageCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// List returns all policies for a project with their audiences. Progress is
// intentionally omitted to avoid aggregate queries on list paths.
func (c *Core) List(ctx context.Context, organizationID string, projectID uuid.UUID) ([]Policy, error) {
	rows, err := c.queries.ListRiskPolicies(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list risk policies: %w", err)
	}
	if len(rows) == 0 {
		return []Policy{}, nil
	}

	policyIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		policyIDs = append(policyIDs, row.ID.String())
	}
	audienceByPolicy, err := c.audienceURNsByPolicy(ctx, organizationID, policyIDs)
	if err != nil {
		return nil, fmt.Errorf("load risk policy audiences: %w", err)
	}

	policies := make([]Policy, 0, len(rows))
	for _, row := range rows {
		policies = append(policies, Project(row, audienceByPolicy[row.ID.String()], nil))
	}
	return policies, nil
}

// ListEnabledForMCP returns enabled MCP-scoped policies that apply to one
// concrete MCP target. Policies without an MCP scope are excluded. Gateway
// membership is resolved on every persisted-server call.
func (c *MCPPolicies) ListEnabledForMCP(
	ctx context.Context,
	organizationID string,
	projectID uuid.UUID,
	target MCPTarget,
) ([]Policy, error) {
	rows, err := c.queries.ListEnabledRiskPoliciesByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list enabled risk policies: %w", err)
	}
	// Runs on every MCP call, so projects without scoped policies stop here.
	scopedRows := make([]repo.RiskPolicy, 0, len(rows))
	scopes := make([]*MCPScope, 0, len(rows))
	needsAnnotations := false
	for _, row := range rows {
		scope := Project(row, nil, nil).MCPScope
		if scope == nil {
			continue
		}
		scopedRows = append(scopedRows, row)
		scopes = append(scopes, scope)
		needsAnnotations = needsAnnotations || len(scope.ToolAnnotations) > 0
	}
	if len(scopedRows) == 0 {
		return []Policy{}, nil
	}

	var gatewayIDs []uuid.UUID
	if target.ServerID != uuid.Nil && !target.PlatformToolset {
		ownedIDs, err := c.queries.ListRiskPolicyMCPScopeServerIDs(ctx, repo.ListRiskPolicyMCPScopeServerIDsParams{
			ProjectID:    projectID,
			McpServerIds: []uuid.UUID{target.ServerID},
		})
		if err != nil {
			return nil, fmt.Errorf("validate MCP server project: %w", err)
		}
		if len(ownedIDs) == 0 {
			return []Policy{}, nil
		}

		gatewayIDs, err = c.queries.ListMetaMCPServerIDsContainingMCPServer(ctx, repo.ListMetaMCPServerIDsContainingMCPServerParams{
			ProjectID:   projectID,
			McpServerID: target.ServerID,
		})
		if err != nil {
			return nil, fmt.Errorf("list MCP server gateways: %w", err)
		}
	}

	annotations := target.ToolAnnotations
	if annotations == nil && !target.PlatformToolset && needsAnnotations && target.ToolName != "" {
		annotations, err = c.toolAnnotations.ToolAnnotations(ctx, target.ServerID, projectID, target.ToolName)
		if err != nil {
			return nil, fmt.Errorf("resolve MCP tool annotations: %w", err)
		}
	}

	matchedRows := make([]repo.RiskPolicy, 0, len(scopedRows))
	policyIDs := make([]string, 0, len(scopedRows))
	for i, row := range scopedRows {
		if !scopes[i].Applies(target.ServerID, target.ToolName, annotations, gatewayIDs) {
			continue
		}
		matchedRows = append(matchedRows, row)
		policyIDs = append(policyIDs, row.ID.String())
	}
	if len(matchedRows) > 0 && target.Principal != nil {
		matchedRows, policyIDs, err = c.filterByAudience(ctx, organizationID, *target.Principal, matchedRows)
		if err != nil {
			return nil, err
		}
	}
	if len(matchedRows) == 0 {
		return []Policy{}, nil
	}

	audienceByPolicy, err := c.audienceURNsByPolicy(ctx, organizationID, policyIDs)
	if err != nil {
		return nil, fmt.Errorf("load risk policy audiences: %w", err)
	}
	policies := make([]Policy, 0, len(matchedRows))
	for _, row := range matchedRows {
		policies = append(policies, Project(row, audienceByPolicy[row.ID.String()], nil))
	}
	return policies, nil
}

// ListPage returns at most limit+1 policies in deterministic keyset order. The
// extra row lets a transport decide whether to issue a next cursor without a
// separate count query.
func (c *Core) ListPage(ctx context.Context, organizationID string, projectID uuid.UUID, cursor *PageCursor, limit int32) ([]Policy, error) {
	params := repo.ListRiskPoliciesPageParams{
		ProjectID:       projectID,
		CursorCreatedAt: pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false},
		CursorID:        uuid.NullUUID{UUID: uuid.Nil, Valid: false},
		PageLimit:       limit + 1,
	}
	if cursor != nil {
		params.CursorCreatedAt = pgtype.Timestamptz{Time: cursor.CreatedAt, InfinityModifier: pgtype.Finite, Valid: true}
		params.CursorID = uuid.NullUUID{UUID: cursor.ID, Valid: true}
	}
	rows, err := c.queries.ListRiskPoliciesPage(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list risk policies page: %w", err)
	}
	if len(rows) == 0 {
		return []Policy{}, nil
	}
	policyIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		policyIDs = append(policyIDs, row.ID.String())
	}
	audienceByPolicy, err := c.audienceURNsByPolicy(ctx, organizationID, policyIDs)
	if err != nil {
		return nil, fmt.Errorf("load risk policy page audiences: %w", err)
	}
	policies := make([]Policy, 0, len(rows))
	for _, row := range rows {
		policies = append(policies, Project(row, audienceByPolicy[row.ID.String()], nil))
	}
	return policies, nil
}

// Get returns one policy with best-effort message-analysis progress.
func (c *Core) Get(ctx context.Context, projectID, policyID uuid.UUID) (Policy, error) {
	row, err := c.queries.GetRiskPolicy(ctx, repo.GetRiskPolicyParams{ID: policyID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Policy{}, fmt.Errorf("%w: %w", ErrLoadPolicy, err)
	}
	if err != nil {
		return Policy{}, fmt.Errorf("get risk policy row: %w", err)
	}
	return c.ProjectWithProgress(ctx, row)
}

// ProjectWithProgress enriches an already-loaded row with its audience and
// best-effort message-analysis progress.
func (c *Core) ProjectWithProgress(ctx context.Context, row repo.RiskPolicy) (Policy, error) {
	total, err := c.queries.CountTotalMessages(ctx, uuid.NullUUID{UUID: row.ProjectID, Valid: true})
	if err != nil {
		total = 0
	}
	analyzed, err := c.queries.CountAnalyzedMessages(ctx, repo.CountAnalyzedMessagesParams{
		ProjectID:         row.ProjectID,
		RiskPolicyID:      row.ID,
		RiskPolicyVersion: row.Version,
	})
	if err != nil {
		analyzed = 0
	}
	audience, err := c.AudiencePrincipalURNs(ctx, row.OrganizationID, row.ID.String())
	if err != nil {
		return Policy{}, fmt.Errorf("load risk policy audience: %w", err)
	}
	return Project(row, audience, &Progress{Total: total, Analyzed: analyzed}), nil
}

// AudiencePrincipalURNs returns the exact-selector audience for one policy,
// sorted and deduplicated.
func (c *Core) AudiencePrincipalURNs(ctx context.Context, organizationID, policyID string) ([]string, error) {
	return audiencePrincipalURNs(ctx, c.db, organizationID, policyID)
}

func audiencePrincipalURNs(ctx context.Context, db repo.DBTX, organizationID, policyID string) ([]string, error) {
	grants, err := authz.ListGrantsForResource(ctx, db, authz.Resource{
		OrganizationID: organizationID,
		Scope:          authz.ScopeRiskPolicyEvaluate,
		ResourceID:     policyID,
	})
	if err != nil {
		return nil, fmt.Errorf("list risk policy audience grants: %w", err)
	}

	principalURNs := make([]string, 0, len(grants))
	for _, grant := range grants {
		if maps.Equal(grant.Selector, authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policyID)) {
			principalURNs = append(principalURNs, grant.PrincipalUrn)
		}
	}
	slices.Sort(principalURNs)
	return slices.Compact(principalURNs), nil
}

// filterByAudience keeps the policies the principal's grants apply, using the
// same rule as chat and hook enforcement.
func (c *Core) filterByAudience(ctx context.Context, organizationID string, principal MCPPrincipal, rows []repo.RiskPolicy) ([]repo.RiskPolicy, []string, error) {
	var principals []urn.Principal
	var err error
	if principal.AgentID != "" {
		principals, err = authz.ResolveAgentPrincipals(ctx, c.db, organizationID, urn.NewPrincipal(urn.PrincipalTypeAgent, principal.AgentID))
	} else {
		principals, err = authz.ResolveUserPrincipals(ctx, c.db, organizationID, principal.UserID)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("resolve risk policy audience principals: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, c.db, organizationID, principals)
	if err != nil {
		return nil, nil, fmt.Errorf("load risk policy audience grants: %w", err)
	}

	applicable := make([]repo.RiskPolicy, 0, len(rows))
	policyIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		application, err := authz.RiskPolicyApplies(row.ID.String(), authz.RiskPolicyDimensions{ServerURL: "", ServerIdentity: ""}).Evaluate(grants)
		if err != nil {
			return nil, nil, fmt.Errorf("evaluate risk policy application: %w", err)
		}
		if application.Satisfied {
			applicable = append(applicable, row)
			policyIDs = append(policyIDs, row.ID.String())
		}
	}
	return applicable, policyIDs, nil
}

func (c *Core) audienceURNsByPolicy(ctx context.Context, organizationID string, policyIDs []string) (map[string][]string, error) {
	grants, err := authz.ListGrantsForResourceIDs(ctx, c.db, organizationID, authz.ScopeRiskPolicyEvaluate, policyIDs)
	if err != nil {
		return nil, fmt.Errorf("list risk policy audience grants: %w", err)
	}

	byPolicy := make(map[string][]string)
	for _, grant := range grants {
		policyID := grant.Selector.ResourceID()
		if maps.Equal(grant.Selector, authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policyID)) {
			byPolicy[policyID] = append(byPolicy[policyID], grant.PrincipalUrn)
		}
	}
	for policyID, principalURNs := range byPolicy {
		slices.Sort(principalURNs)
		byPolicy[policyID] = slices.Compact(principalURNs)
	}
	return byPolicy, nil
}
