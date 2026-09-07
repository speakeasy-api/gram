package policycore

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/risk/policybypass"
	"github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/shadowmcp"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// Transactor is the transaction capability required by policy mutations.
type Transactor interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// MutationAuditor records policy writes and their outbox event in the mutation
// transaction. Transport adapters own the final audit serialization shape.
type MutationAuditor interface {
	LogPolicyCreate(ctx context.Context, db repo.DBTX, event CreateAuditEvent) error
	LogPolicyUpdate(ctx context.Context, db repo.DBTX, event UpdateAuditEvent) error
}

// ApprovalCoordinator applies standing Shadow MCP decisions in the policy
// transaction. It is intentionally narrower than the user-facing approval API.
type ApprovalCoordinator interface {
	ReconcileStandingDecisionsForPolicy(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID, policyID uuid.UUID) error
	ReviewShadowMCPPolicyURLEdit(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID, policyID uuid.UUID, disposition string, desiredAllowedURLs []string, desiredBlockedURLs []string) (shadowmcp.StandingDecisionReview, error)
	SupersedeShadowMCPDecisions(ctx context.Context, tx pgx.Tx, organizationID string, projectID uuid.UUID, conflicts []shadowmcp.StandingDecisionConflict, actor urn.Principal, actorDisplayName *string) error
}

// PolicySignaler starts best-effort policy convergence after commit.
type PolicySignaler interface {
	Signal(ctx context.Context, projectID uuid.UUID) error
}

// PolicyCacheInvalidator invalidates policy-derived caches after commit.
type PolicyCacheInvalidator interface {
	Invalidate(ctx context.Context, projectID uuid.UUID)
}

// ReconcilePolicyURLs replaces the URL grants owned by one policy.
type ReconcilePolicyURLs func(ctx context.Context, db repo.DBTX, input policybypass.ReconcilePolicyURLsInput) error

// MutationDependencies are optional for read-only Core users and required by
// CreatePolicy and UpdatePolicy.
type MutationDependencies struct {
	Transactor       Transactor
	Auditor          MutationAuditor
	Approvals        ApprovalCoordinator
	ReconcileURLs    ReconcilePolicyURLs
	Signaler         PolicySignaler
	CacheInvalidator PolicyCacheInvalidator
}

// Actor is the real user attributed to a policy mutation.
type Actor struct {
	Principal   urn.Principal
	DisplayName *string
	Slug        *string
}

// CreateAuditEvent is the transport-neutral policy-create audit contract.
type CreateAuditEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID
	Actor          Actor
	Policy         Policy
}

// UpdateAuditEvent is the transport-neutral policy-update audit contract.
type UpdateAuditEvent struct {
	OrganizationID string
	ProjectID      uuid.UUID
	Actor          Actor
	Before         Policy
	After          Policy
}

// CreateMutation is a fully validated, normalized policy create command.
type CreateMutation struct {
	Params             repo.CreateRiskPolicyParams
	AudiencePrincipals []urn.Principal
	AllowedURLs        []string
	AllowedURLsSet     bool
	BlockedURLs        []string
	BlockedURLsSet     bool
	Actor              Actor
}

// UpdateMutation is a fully validated, normalized policy update command.
type UpdateMutation struct {
	Current              repo.RiskPolicy
	Params               repo.UpdateRiskPolicyParams
	AudiencePrincipals   []urn.Principal
	AudienceChanged      bool
	AllowedURLs          []string
	AllowedURLsSet       bool
	BlockedURLs          []string
	BlockedURLsSet       bool
	EffectiveDisposition string
	SupersedeDecisions   bool
	// ValidateLocked runs after the current row is locked but before any domain
	// write. It returns the authoritative policy snapshot used by the adapter for
	// validation so the core can carry its audience through audit and results.
	ValidateLocked func(context.Context, pgx.Tx, Policy) (Policy, error)
	Actor          Actor
}

// MutationResult is the committed policy row and canonical audience.
type MutationResult struct {
	Row                   repo.RiskPolicy
	AudiencePrincipalURNs []string
}

// MutationError identifies the failed transaction step while retaining its
// underlying cause for the calling transport's established error mapping.
type MutationError struct {
	Message string
	Cause   error
}

func (e *MutationError) Error() string { return e.Message }
func (e *MutationError) Unwrap() error { return e.Cause }

// DecisionConflictError reports standing decisions that require explicit
// supersession before an already-blocking policy's URL grants can change.
type DecisionConflictError struct {
	Targets []string
}

func (e *DecisionConflictError) Error() string {
	return fmt.Sprintf("policy URL edit conflicts with standing decisions for %v", e.Targets)
}

// StalePolicyError reports that the row changed between preparation and lock.
type StalePolicyError struct{}

func (*StalePolicyError) Error() string { return "risk policy changed during update" }

// BlockingPolicyConflictError reports the existing enabled blocking Shadow MCP
// policy that prevents another one from being created or enabled.
type BlockingPolicyConflictError struct {
	PolicyName string
}

func (e *BlockingPolicyConflictError) Error() string {
	return fmt.Sprintf("project already has an enabled shadow mcp blocking policy %q", e.PolicyName)
}

// CreatePolicy commits a fully prepared policy create and all of its domain
// invariants in one transaction, then runs best-effort convergence effects.
func (c *Core) CreatePolicy(ctx context.Context, input CreateMutation) (MutationResult, error) {
	deps, err := c.requireMutationDependencies()
	if err != nil {
		return MutationResult{}, err
	}

	tx, err := deps.Transactor.Begin(ctx)
	if err != nil {
		return MutationResult{}, mutationError("begin transaction", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result, err := c.createPolicyInTransaction(ctx, tx, input, deps)
	if err != nil {
		return MutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MutationResult{}, mutationError("commit risk policy create", err)
	}
	c.AfterCreatePolicy(ctx, result)
	return result, nil
}

// CreatePolicyInTransaction applies the complete policy create and audit to a
// caller-owned transaction. The caller owns commit and must invoke
// AfterCreatePolicy only after that commit succeeds.
func (c *Core) CreatePolicyInTransaction(ctx context.Context, tx pgx.Tx, input CreateMutation) (MutationResult, error) {
	deps, err := c.requireMutationDependencies()
	if err != nil {
		return MutationResult{}, err
	}
	if tx == nil {
		return MutationResult{}, mutationError("policy mutation transaction is not configured", nil)
	}
	return c.createPolicyInTransaction(ctx, tx, input, deps)
}

func (c *Core) createPolicyInTransaction(ctx context.Context, tx pgx.Tx, input CreateMutation, deps *MutationDependencies) (MutationResult, error) {
	queries := repo.New(tx)
	if err := queries.LockRiskPolicyMutations(ctx, input.Params.ProjectID.String()); err != nil {
		return MutationResult{}, mutationError("lock risk policy mutations", err)
	}
	if input.Params.Enabled && input.Params.Action == "block" && slices.Contains(input.Params.Sources, shadowmcp.SourceShadowMCP) {
		if err := requireSingleBlockingPolicy(ctx, queries, input.Params.ProjectID, uuid.Nil); err != nil {
			return MutationResult{}, err
		}
	}
	row, err := queries.CreateRiskPolicy(ctx, input.Params)
	if err != nil {
		return MutationResult{}, mutationError("create risk policy", err)
	}
	if err := replaceAudience(ctx, tx, row.OrganizationID, row.ID.String(), input.AudiencePrincipals); err != nil {
		return MutationResult{}, mutationError("sync risk policy audience", err)
	}

	if input.AllowedURLsSet {
		if err := deps.ReconcileURLs(ctx, tx, policybypass.ReconcilePolicyURLsInput{
			OrganizationID: row.OrganizationID,
			PolicyID:       row.ID.String(),
			Scope:          authz.ScopeRiskPolicyBypass,
			DesiredURLs:    input.AllowedURLs,
			Principals:     input.AudiencePrincipals,
			PreserveURLs:   nil,
		}); err != nil {
			return MutationResult{}, mutationError("reconcile shadow mcp policy allowed urls", err)
		}
	}
	if input.BlockedURLsSet {
		if err := deps.ReconcileURLs(ctx, tx, policybypass.ReconcilePolicyURLsInput{
			OrganizationID: row.OrganizationID,
			PolicyID:       row.ID.String(),
			Scope:          authz.ScopeRiskPolicyBlock,
			DesiredURLs:    input.BlockedURLs,
			Principals:     []urn.Principal{authz.AllUsersPrincipal()},
			PreserveURLs:   nil,
		}); err != nil {
			return MutationResult{}, mutationError("reconcile shadow mcp policy blocked urls", err)
		}
	}

	if deps.Approvals != nil && isBlockingShadowPolicy(row) {
		if err := deps.Approvals.ReconcileStandingDecisionsForPolicy(ctx, tx, row.OrganizationID, row.ProjectID, row.ID); err != nil {
			return MutationResult{}, mutationError("honor standing approval decisions on policy create", err)
		}
	}

	audience := principalStrings(input.AudiencePrincipals)
	if err := deps.Auditor.LogPolicyCreate(ctx, tx, CreateAuditEvent{
		OrganizationID: row.OrganizationID,
		ProjectID:      row.ProjectID,
		Actor:          input.Actor,
		Policy:         AuditSnapshot(Project(row, audience, nil)),
	}); err != nil {
		return MutationResult{}, mutationError("log risk policy create", err)
	}
	return MutationResult{Row: row, AudiencePrincipalURNs: audience}, nil
}

// AfterCreatePolicy runs best-effort convergence only after the transaction
// containing the policy, audit, and any outer receipt has committed.
func (c *Core) AfterCreatePolicy(ctx context.Context, result MutationResult) {
	deps, err := c.requireMutationDependencies()
	if err != nil {
		return
	}
	if deps.CacheInvalidator != nil {
		deps.CacheInvalidator.Invalidate(ctx, result.Row.ProjectID)
	}
	if result.Row.Enabled {
		_ = deps.Signaler.Signal(ctx, result.Row.ProjectID)
	}
}

// UpdatePolicy locks the current row, rejects a stale prepared command, and
// commits the policy row, grants, standing decisions, and audit atomically.
func (c *Core) UpdatePolicy(ctx context.Context, input UpdateMutation) (MutationResult, error) {
	deps, err := c.requireMutationDependencies()
	if err != nil {
		return MutationResult{}, err
	}

	tx, err := deps.Transactor.Begin(ctx)
	if err != nil {
		return MutationResult{}, mutationError("begin transaction", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result, err := c.updatePolicyInTransaction(ctx, tx, input, deps)
	if err != nil {
		return MutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MutationResult{}, mutationError("commit risk policy update", err)
	}
	c.AfterUpdatePolicy(ctx, result)
	return result, nil
}

// UpdatePolicyInTransaction applies the complete locked sparse update and audit
// to a caller-owned transaction. The caller owns commit and must invoke
// AfterUpdatePolicy only after that commit succeeds.
func (c *Core) UpdatePolicyInTransaction(ctx context.Context, tx pgx.Tx, input UpdateMutation) (MutationResult, error) {
	deps, err := c.requireMutationDependencies()
	if err != nil {
		return MutationResult{}, err
	}
	if tx == nil {
		return MutationResult{}, mutationError("policy mutation transaction is not configured", nil)
	}
	return c.updatePolicyInTransaction(ctx, tx, input, deps)
}

func (c *Core) updatePolicyInTransaction(ctx context.Context, tx pgx.Tx, input UpdateMutation, deps *MutationDependencies) (MutationResult, error) {
	queries := repo.New(tx)
	if err := queries.LockRiskPolicyMutations(ctx, input.Params.ProjectID.String()); err != nil {
		return MutationResult{}, mutationError("lock risk policy mutations", err)
	}
	locked, err := queries.GetRiskPolicyForUpdate(ctx, repo.GetRiskPolicyForUpdateParams{
		ID: input.Params.ID, ProjectID: input.Params.ProjectID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MutationResult{}, fmt.Errorf("%w: %w", ErrLoadPolicy, err)
		}
		return MutationResult{}, mutationError("lock risk policy", err)
	}
	if err := requireFreshPolicy(input.Current, locked); err != nil {
		return MutationResult{}, err
	}
	if input.Params.Enabled && input.Params.Action == "block" && slices.Contains(input.Params.Sources, shadowmcp.SourceShadowMCP) {
		if err := requireSingleBlockingPolicy(ctx, queries, input.Params.ProjectID, input.Params.ID); err != nil {
			return MutationResult{}, err
		}
	}

	currentAudience, err := audiencePrincipalURNs(ctx, tx, locked.OrganizationID, locked.ID.String())
	if err != nil {
		return MutationResult{}, mutationError("load risk policy audience snapshot", err)
	}
	if input.ValidateLocked != nil {
		validated, err := input.ValidateLocked(ctx, tx, Project(locked, currentAudience, nil))
		if err != nil {
			return MutationResult{}, err
		}
		currentAudience = validated.AudiencePrincipalURNs
	}
	effectiveAudience := input.AudiencePrincipals
	if !input.AudienceChanged {
		effectiveAudience, err = parsePrincipalURNs(currentAudience)
		if err != nil {
			return MutationResult{}, mutationError("parse risk policy audience snapshot", err)
		}
	}
	row, err := queries.UpdateRiskPolicy(ctx, input.Params)
	if err != nil {
		return MutationResult{}, mutationError("update risk policy", err)
	}
	if input.AudienceChanged {
		if err := replaceAudience(ctx, tx, row.OrganizationID, row.ID.String(), input.AudiencePrincipals); err != nil {
			return MutationResult{}, mutationError("sync risk policy audience", err)
		}
	}

	wasBlocking := isBlockingShadowPolicy(locked)
	nowBlocking := isBlockingShadowPolicy(row)
	var preserveDecisionURLs map[string]struct{}
	if deps.Approvals != nil && wasBlocking && nowBlocking {
		review, err := deps.Approvals.ReviewShadowMCPPolicyURLEdit(
			ctx, tx, row.OrganizationID, row.ProjectID, row.ID,
			input.EffectiveDisposition, optionalURLs(input.AllowedURLs, input.AllowedURLsSet), optionalURLs(input.BlockedURLs, input.BlockedURLsSet),
		)
		if err != nil {
			return MutationResult{}, mutationError("review shadow mcp policy url edit", err)
		}
		if len(review.Conflicts) > 0 {
			if !input.SupersedeDecisions {
				targets := make([]string, 0, len(review.Conflicts))
				for _, conflict := range review.Conflicts {
					targets = append(targets, conflict.TargetRaw)
				}
				return MutationResult{}, &DecisionConflictError{Targets: targets}
			}
			if err := deps.Approvals.SupersedeShadowMCPDecisions(ctx, tx, row.OrganizationID, row.ProjectID, review.Conflicts, input.Actor.Principal, input.Actor.DisplayName); err != nil {
				return MutationResult{}, mutationError("supersede contradicted decisions", err)
			}
		}
		if len(review.StandingURLs) > 0 {
			preserveDecisionURLs = make(map[string]struct{}, len(review.StandingURLs))
			for _, serverURL := range review.StandingURLs {
				preserveDecisionURLs[serverURL] = struct{}{}
			}
		}
	}

	if input.AllowedURLsSet || input.AudienceChanged {
		if err := deps.ReconcileURLs(ctx, tx, policybypass.ReconcilePolicyURLsInput{
			OrganizationID: row.OrganizationID,
			PolicyID:       row.ID.String(),
			Scope:          authz.ScopeRiskPolicyBypass,
			DesiredURLs:    optionalURLs(input.AllowedURLs, input.AllowedURLsSet),
			Principals:     effectiveAudience,
			PreserveURLs:   preserveDecisionURLs,
		}); err != nil {
			return MutationResult{}, mutationError("reconcile shadow mcp policy allowed urls", err)
		}
	}
	if input.BlockedURLsSet {
		if err := deps.ReconcileURLs(ctx, tx, policybypass.ReconcilePolicyURLsInput{
			OrganizationID: row.OrganizationID,
			PolicyID:       row.ID.String(),
			Scope:          authz.ScopeRiskPolicyBlock,
			DesiredURLs:    input.BlockedURLs,
			Principals:     []urn.Principal{authz.AllUsersPrincipal()},
			PreserveURLs:   preserveDecisionURLs,
		}); err != nil {
			return MutationResult{}, mutationError("reconcile shadow mcp policy blocked urls", err)
		}
	}
	if deps.Approvals != nil && nowBlocking && !wasBlocking {
		if err := deps.Approvals.ReconcileStandingDecisionsForPolicy(ctx, tx, row.OrganizationID, row.ProjectID, row.ID); err != nil {
			return MutationResult{}, mutationError("honor standing approval decisions on policy update", err)
		}
	}

	audience := principalStrings(effectiveAudience)
	if err := deps.Auditor.LogPolicyUpdate(ctx, tx, UpdateAuditEvent{
		OrganizationID: row.OrganizationID,
		ProjectID:      row.ProjectID,
		Actor:          input.Actor,
		Before:         AuditSnapshot(Project(locked, currentAudience, nil)),
		After:          AuditSnapshot(Project(row, audience, nil)),
	}); err != nil {
		return MutationResult{}, mutationError("log risk policy update", err)
	}
	return MutationResult{Row: row, AudiencePrincipalURNs: audience}, nil
}

// AfterUpdatePolicy runs best-effort convergence only after the transaction
// containing the policy, audit, and any outer receipt has committed.
func (c *Core) AfterUpdatePolicy(ctx context.Context, result MutationResult) {
	deps, err := c.requireMutationDependencies()
	if err != nil {
		return
	}
	if deps.CacheInvalidator != nil {
		deps.CacheInvalidator.Invalidate(ctx, result.Row.ProjectID)
	}
	_ = deps.Signaler.Signal(ctx, result.Row.ProjectID)
}

func (c *Core) requireMutationDependencies() (*MutationDependencies, error) {
	if c.mutations == nil || c.mutations.Transactor == nil || c.mutations.Auditor == nil || c.mutations.ReconcileURLs == nil || c.mutations.Signaler == nil {
		return nil, mutationError("policy mutation dependencies are not configured", nil)
	}
	return c.mutations, nil
}

func replaceAudience(ctx context.Context, db repo.DBTX, organizationID, policyID string, principals []urn.Principal) error {
	if err := authz.ReplaceGrantAudience(ctx, db, authz.ResourceGrant{
		Resource: authz.Resource{
			OrganizationID: organizationID,
			Scope:          authz.ScopeRiskPolicyEvaluate,
			ResourceID:     policyID,
		},
		Principals: principals,
		Selector:   authz.NewSelector(authz.ScopeRiskPolicyEvaluate, policyID),
	}); err != nil {
		return fmt.Errorf("replace risk policy audience grants: %w", err)
	}
	return nil
}

func principalStrings(principals []urn.Principal) []string {
	values := make([]string, 0, len(principals))
	for _, principal := range principals {
		values = append(values, principal.String())
	}
	return values
}

func parsePrincipalURNs(values []string) ([]urn.Principal, error) {
	principals := make([]urn.Principal, 0, len(values))
	for _, value := range values {
		principal, err := urn.ParsePrincipal(value)
		if err != nil {
			return nil, fmt.Errorf("parse principal %q: %w", value, err)
		}
		principals = append(principals, principal)
	}
	return principals, nil
}

func isBlockingShadowPolicy(row repo.RiskPolicy) bool {
	return row.Enabled && row.Action == "block" && slices.Contains(row.Sources, shadowmcp.SourceShadowMCP)
}

func requireFreshPolicy(current, locked repo.RiskPolicy) error {
	if !locked.UpdatedAt.Time.Equal(current.UpdatedAt.Time) {
		return &StalePolicyError{}
	}
	return nil
}

func requireSingleBlockingPolicy(ctx context.Context, queries *repo.Queries, projectID, excludePolicyID uuid.UUID) error {
	policies, err := queries.ListEnabledShadowMCPPoliciesByProject(ctx, projectID)
	if err != nil {
		return mutationError("list shadow mcp blocking policies", err)
	}
	return blockingPolicyConflict(policies, excludePolicyID)
}

func blockingPolicyConflict(policies []repo.RiskPolicy, excludePolicyID uuid.UUID) error {
	for _, policy := range policies {
		if policy.ID == excludePolicyID || policy.Action != "block" {
			continue
		}
		return &BlockingPolicyConflictError{PolicyName: policy.Name}
	}
	return nil
}

func optionalURLs(urls []string, set bool) []string {
	if !set {
		return nil
	}
	return urls
}

func mutationError(message string, cause error) error {
	return &MutationError{Message: message, Cause: cause}
}
