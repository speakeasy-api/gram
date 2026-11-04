package agentmanagement

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	orgrepo "github.com/speakeasy-api/gram/server/internal/organizations/repo"
)

// OwnerPredicate names one of the intrinsic, nondelegable owner capabilities.
type OwnerPredicate string

const (
	OwnedAgentRead      OwnerPredicate = "owned_agent_read"
	OwnedAgentSetup     OwnerPredicate = "owned_agent_setup"
	OwnedAgentAuthorize OwnerPredicate = "owned_agent_authorize"
	OwnedAgentTransfer  OwnerPredicate = "owned_agent_transfer"
)

// HumanContext is identity proven by an ordinary, nonsupport Speakeasy session or
// scoped OAuth consent and an active organization membership.
type HumanContext struct {
	Auth   *contextvalues.AuthContext
	grants []authz.Grant
	access authz.AgentAccess
	// A permitted RBAC scope override only narrows agent management checks:
	// they must pass both the live grants and the override. Delegation
	// containment and LiveGrants always use the live grants, so an override
	// never widens.
	overridden     bool
	overrideAccess authz.AgentAccess
}

// LiveGrants returns an isolated copy of the caller grants loaded by the
// authorizer from the database, not session-cached grants. Reuse this snapshot
// only within the operation/transaction that obtained the HumanContext.
func (h HumanContext) LiveGrants() []authz.Grant {
	grants := slices.Clone(h.grants)
	for i := range grants {
		grants[i].Selector = maps.Clone(grants[i].Selector)
	}
	return grants
}

// AgentPermissions reports the four independent management decisions for a
// selected agent. It is suitable for driving disabled UI states, but mutations
// must always authorize again server-side.
type AgentPermissions struct {
	Read      bool
	Write     bool
	Authorize bool
	Transfer  bool
}

type authorizationEngine interface {
	EvaluateLoadedGrants(context.Context, []authz.Grant, ...authz.Check) error
	GetScopeOverrides(context.Context) ([]authz.RoleGrant, bool)
	EvaluateLoadedAuthority(context.Context, []authz.Grant, authz.Scope, authz.Selector) error
}

// Authorizer implements the reusable human-only agent management seam.
type Authorizer struct {
	authz authorizationEngine
	// client decides existing-agent operations through the typed
	// authorization client. Without one they are decided by the engine.
	client *authz.Client
}

// NewAuthorizer builds the agent management authorizer. client may be nil,
// in which case existing-agent decisions stay on the engine.
func NewAuthorizer(engine authorizationEngine, client *authz.Client) *Authorizer {
	return &Authorizer{authz: engine, client: client}
}

// RequireHuman rejects credentials that only carry human attribution. Only an
// ordinary validated Speakeasy session with an active membership is accepted.
func (a *Authorizer) RequireHuman(ctx context.Context, dbtx repo.DBTX) (HumanContext, error) {
	return a.requireHumanWithMemberships(ctx, dbtx)
}

// ordinaryHumanAuth validates the session without acquiring any database locks.
func ordinaryHumanAuth(ctx context.Context) (*contextvalues.AuthContext, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || !contextvalues.HasValidatedGramSession(ctx) || authCtx.SessionID == nil || *authCtx.SessionID == "" || authCtx.UserID == "" || authCtx.ActiveOrganizationID == "" {
		return nil, oops.C(oops.CodeUnauthorized)
	}

	if authCtx.APIKeyID != "" || authCtx.APIKeyName != "" || len(authCtx.APIKeyScopes) != 0 || authCtx.OrgWidePluginHooksKey {
		return nil, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.PrincipalCredentialAuthorization(ctx); ok {
		return nil, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetAssistantPrincipal(ctx); ok {
		return nil, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetOAuthClientID(ctx); ok {
		return nil, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetActingSurface(ctx); ok {
		return nil, oops.C(oops.CodeForbidden)
	}
	if contextvalues.IsSupportSession(ctx) || contextvalues.IsLegacyImpersonatedSession(ctx) {
		return nil, oops.C(oops.CodeForbidden)
	}

	return authCtx, nil
}

// requireHumanWithMemberships must be called before acquiring any membership or
// agent lock. Sort the complete membership set first, including the caller, so
// opposing create/transfer/credential operations cannot reverse the lock order.
func (a *Authorizer) requireHumanWithMemberships(ctx context.Context, dbtx repo.DBTX, otherUserIDs ...string) (HumanContext, error) {
	authCtx, err := ordinaryHumanAuth(ctx)
	if err != nil {
		return HumanContext{}, err
	}
	return a.lockHumanMemberships(ctx, dbtx, authCtx, otherUserIDs...)
}

// Both validated dashboard sessions and scoped consent use the same live
// membership locks and grants. Authentication stays at their separate entry points.
func (a *Authorizer) lockHumanMemberships(ctx context.Context, dbtx repo.DBTX, authCtx *contextvalues.AuthContext, otherUserIDs ...string) (HumanContext, error) {
	userIDs := append([]string{authCtx.UserID}, otherUserIDs...)
	slices.Sort(userIDs)
	for _, userID := range slices.Compact(userIDs) {
		_, err := orgrepo.New(dbtx).LockActiveOrganizationUser(ctx, orgrepo.LockActiveOrganizationUserParams{
			UserID:         conv.ToPGText(userID),
			OrganizationID: authCtx.ActiveOrganizationID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return HumanContext{}, oops.C(oops.CodeForbidden)
		}
		if err != nil {
			return HumanContext{}, fmt.Errorf("lock active organization membership: %w", err)
		}
	}

	principals, err := authz.ResolveUserPrincipals(ctx, dbtx, authCtx.ActiveOrganizationID, authCtx.UserID)
	if err != nil {
		return HumanContext{}, fmt.Errorf("resolve live authorization principals: %w", err)
	}
	grants, err := authz.LoadGrants(ctx, dbtx, authCtx.ActiveOrganizationID, principals)
	if err != nil {
		return HumanContext{}, fmt.Errorf("load live authorization grants: %w", err)
	}

	human := HumanContext{Auth: authCtx, grants: grants, access: authz.NewAgentAccess(authCtx.UserID, grants), overridden: false, overrideAccess: authz.AgentAccess{}}
	if a.authz != nil {
		if overrides, ok := a.authz.GetScopeOverrides(ctx); ok {
			human.overridden = true
			human.overrideAccess = authz.NewAgentAccess(authCtx.UserID, authz.GrantsFromOverrides(overrides))
		}
	}
	return human, nil
}

// require authorizes an existing-agent operation against the live grants and,
// when a scope override is active, against the override as well.
func (a *Authorizer) require(ctx context.Context, human HumanContext, scope authz.Scope, agent repo.Agent) error {
	if a.client != nil {
		op, ok := agentOperation(scope, agent)
		if !ok {
			return oops.C(oops.CodeForbidden)
		}
		if err := a.client.RequireLoaded(ctx, human.loadedPolicies(), op); err != nil {
			return fmt.Errorf("authorize existing agent: %w", err)
		}
		return nil
	}
	if err := human.access.Require(ctx, a.authz, scope, agent.ID.String(), agent.OwnerUserID); err != nil {
		return fmt.Errorf("evaluate live grants: %w", err)
	}
	if !human.overridden {
		return nil
	}
	if err := human.overrideAccess.Require(ctx, a.authz, scope, agent.ID.String(), agent.OwnerUserID); err != nil {
		return fmt.Errorf("evaluate scope override: %w", err)
	}
	return nil
}

// requireCreate authorizes creation for ownerUserID against the live grants
// and, when a scope override is active, against the override as well.
func (a *Authorizer) requireCreate(ctx context.Context, human HumanContext, ownerUserID string) error {
	if err := human.access.RequireCreate(ctx, a.authz, ownerUserID); err != nil {
		return fmt.Errorf("evaluate live grants: %w", err)
	}
	if !human.overridden {
		return nil
	}
	if err := human.overrideAccess.RequireCreate(ctx, a.authz, ownerUserID); err != nil {
		return fmt.Errorf("evaluate scope override: %w", err)
	}
	return nil
}

// allows is the capability projection of require.
func (a *Authorizer) allows(ctx context.Context, human HumanContext, scope authz.Scope, agent repo.Agent) (bool, error) {
	if a.client == nil {
		if !human.access.Allows(scope, agent.ID.String(), agent.OwnerUserID) {
			return false, nil
		}
		return !human.overridden || human.overrideAccess.Allows(scope, agent.ID.String(), agent.OwnerUserID), nil
	}
	op, ok := agentOperation(scope, agent)
	if !ok {
		return false, nil
	}
	allowed, err := a.client.EvaluateLoaded(ctx, human.loadedPolicies(), op)
	if err != nil {
		return false, fmt.Errorf("evaluate existing agent: %w", err)
	}
	return allowed, nil
}

// visible reports whether the caller's live grants let them read agent. A
// scope override narrows what the caller may do with an agent, not which
// agents they see.
func (a *Authorizer) visible(ctx context.Context, human HumanContext, agent repo.Agent) (bool, error) {
	live := human
	live.overridden = false
	return a.allows(ctx, live, authz.ScopeAgentRead, agent)
}

// loadedPolicies are the caller's live grants and, under a scope override,
// the override grants, each with owner selectors bound to the caller. Every
// policy must allow.
func (h HumanContext) loadedPolicies() authz.LoadedPolicies {
	policies := [][]authz.Grant{h.access.Grants()}
	if h.overridden {
		policies = append(policies, h.overrideAccess.Grants())
	}
	return authz.LoadedPolicies{OrganizationID: h.Auth.ActiveOrganizationID, User: h.Auth.UserID, Policies: policies}
}

// agentOperation is the typed operation for scope on agent. An agent without
// a persisted owner has none: no grant may cover an agent whose owner is
// unknown.
func agentOperation(scope authz.Scope, agent repo.Agent) (authz.Operation, bool) {
	if agent.OwnerUserID == "" || agent.OwnerUserID == authz.WildcardResource {
		return authz.Operation{}, false
	}
	ref := authz.Agent(agent.ID.String(), agent.OwnerUserID)
	switch scope {
	case authz.ScopeAgentRead:
		return ref.Read(), true
	case authz.ScopeAgentWrite:
		return ref.Write(), true
	case authz.ScopeAgentAuthorize:
		return ref.Authorize(), true
	case authz.ScopeAgentTransfer:
		return ref.Transfer(), true
	default:
		return authz.Operation{}, false
	}
}

// creationOwners lists the owners the caller may create agents for. A scope
// override narrows the live answer: an owner must be allowed by both.
func (h HumanContext) creationOwners() (all bool, ownerIDs []string) {
	all, ownerIDs = h.access.CreationOwners()
	if !h.overridden {
		return all, ownerIDs
	}
	overrideAll, overrideIDs := h.overrideAccess.CreationOwners()
	switch {
	case all && overrideAll:
		return true, []string{}
	case all:
		return false, overrideIDs
	case overrideAll:
		return false, ownerIDs
	}
	both := make([]string, 0, len(ownerIDs))
	for _, id := range ownerIDs {
		if slices.Contains(overrideIDs, id) {
			both = append(both, id)
		}
	}
	return false, both
}

// RequireCreate evaluates creation eligibility against the intended owner and
// locks both active memberships until the insert commits. The recipient need
// not hold write permission; owners retain intrinsic management access.
func (a *Authorizer) RequireCreate(ctx context.Context, dbtx repo.DBTX, ownerUserID string) (HumanContext, error) {
	human, err := a.requireHumanWithMemberships(ctx, dbtx, ownerUserID)
	if err != nil {
		return HumanContext{}, err
	}

	if a.authz == nil {
		return HumanContext{}, errors.New("agent authorization engine is unavailable")
	}
	if err := a.requireCreate(ctx, human, ownerUserID); err != nil {
		return HumanContext{}, fmt.Errorf("require agent creation: %w", err)
	}
	return human, nil
}

// RequireAgent authorizes a selected tenant-bound agent without locking it.
func (a *Authorizer) RequireAgent(ctx context.Context, dbtx repo.DBTX, agentID uuid.UUID, predicate OwnerPredicate) (HumanContext, repo.Agent, error) {
	return a.requireAgent(ctx, dbtx, agentID, predicate, false)
}

// RequireAgentForUpdate locks and authorizes a selected tenant-bound agent so
// ownership, latch, and lifecycle cannot change before the mutation commits.
func (a *Authorizer) RequireAgentForUpdate(ctx context.Context, dbtx repo.DBTX, agentID uuid.UUID, predicate OwnerPredicate) (HumanContext, repo.Agent, error) {
	return a.requireAgent(ctx, dbtx, agentID, predicate, true)
}

// RequireTransfer pins the replacement owner's active user and membership
// rows, then locks and authorizes the selected agent until commit.
func (a *Authorizer) RequireTransfer(ctx context.Context, dbtx repo.DBTX, agentID uuid.UUID, ownerUserID string) (HumanContext, repo.Agent, error) {
	human, err := a.requireHumanWithMemberships(ctx, dbtx, ownerUserID)
	if err != nil {
		return HumanContext{}, repo.Agent{}, err
	}
	return a.requireAgentWithHuman(ctx, dbtx, human, agentID, OwnedAgentTransfer, true)
}

func (a *Authorizer) requireAgent(ctx context.Context, dbtx repo.DBTX, agentID uuid.UUID, predicate OwnerPredicate, forUpdate bool) (HumanContext, repo.Agent, error) {
	human, err := a.RequireHuman(ctx, dbtx)
	if err != nil {
		return HumanContext{}, repo.Agent{}, err
	}
	return a.requireAgentWithHuman(ctx, dbtx, human, agentID, predicate, forUpdate)
}

// RequireAgentOwnerForUpdate pins the caller and observed active owner before
// locking the agent. Call this at the start of a transaction, before any other
// membership locks. Ownership changes during acquisition fail closed rather than
// acquiring another membership out of order. Owner-loss recovery uses
// RequireTransfer instead, which does not require an active previous owner.
// Scoped OAuth consent is accepted only for the selected agent and authorize
// predicate; it cannot authenticate general dashboard management operations.
func (a *Authorizer) RequireAgentOwnerForUpdate(ctx context.Context, dbtx repo.DBTX, agentID uuid.UUID, predicate OwnerPredicate) (HumanContext, repo.Agent, error) {
	authCtx, err := ordinaryHumanAuth(ctx)
	if consent, ok := contextvalues.GetConsentBindingAuthorization(ctx); ok && (predicate == OwnedAgentAuthorize || predicate == OwnedAgentSetup) && consent.AgentID == agentID {
		authCtx, _ = contextvalues.GetAuthContext(ctx)
		err = nil
	}
	if err != nil {
		return HumanContext{}, repo.Agent{}, err
	}
	observed, err := repo.New(dbtx).GetAgentByID(ctx, repo.GetAgentByIDParams{OrganizationID: authCtx.ActiveOrganizationID, ID: agentID})
	if errors.Is(err, pgx.ErrNoRows) {
		return HumanContext{}, repo.Agent{}, oops.C(oops.CodeForbidden)
	}
	if err != nil {
		return HumanContext{}, repo.Agent{}, fmt.Errorf("observe selected agent owner: %w", err)
	}
	human, err := a.lockHumanMemberships(ctx, dbtx, authCtx, observed.OwnerUserID)
	if err != nil {
		return HumanContext{}, repo.Agent{}, err
	}
	human, agent, err := a.requireAgentWithHuman(ctx, dbtx, human, agentID, predicate, true)
	if err != nil {
		return HumanContext{}, repo.Agent{}, err
	}
	if agent.OwnerUserID != observed.OwnerUserID {
		return HumanContext{}, repo.Agent{}, oops.C(oops.CodeForbidden)
	}
	return human, agent, nil
}

func (a *Authorizer) requireAgentWithHuman(ctx context.Context, dbtx repo.DBTX, human HumanContext, agentID uuid.UUID, predicate OwnerPredicate, forUpdate bool) (HumanContext, repo.Agent, error) {
	scope, ok := scopeForOwnerPredicate(predicate)
	if !ok {
		return HumanContext{}, repo.Agent{}, fmt.Errorf("unknown owner predicate %q", predicate)
	}
	var err error

	params := repo.GetAgentByIDParams{OrganizationID: human.Auth.ActiveOrganizationID, ID: agentID}
	var agent repo.Agent
	if forUpdate {
		agent, err = repo.New(dbtx).GetAgentByIDForUpdate(ctx, repo.GetAgentByIDForUpdateParams(params))
	} else {
		agent, err = repo.New(dbtx).GetAgentByID(ctx, params)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return HumanContext{}, repo.Agent{}, oops.C(oops.CodeForbidden)
	}
	if err != nil {
		return HumanContext{}, repo.Agent{}, fmt.Errorf("load selected agent: %w", err)
	}

	if ownsUnblockedAgent(human, agent) {
		return human, agent, nil
	}
	if a.authz == nil {
		return HumanContext{}, repo.Agent{}, errors.New("agent authorization engine is unavailable")
	}
	if err := a.require(ctx, human, scope, agent); err != nil {
		// Selected-agent denials deliberately do not distinguish absent,
		// cross-tenant, or unauthorized resources.
		return HumanContext{}, repo.Agent{}, oops.C(oops.CodeForbidden)
	}
	return human, agent, nil
}

func (a *Authorizer) Permissions(ctx context.Context, human HumanContext, agent repo.Agent) (AgentPermissions, error) {
	if ownsUnblockedAgent(human, agent) {
		return AgentPermissions{Read: true, Write: true, Authorize: true, Transfer: true}, nil
	}
	if a.authz == nil {
		return AgentPermissions{}, errors.New("agent authorization engine is unavailable")
	}

	var permissions AgentPermissions
	for _, decision := range []struct {
		scope authz.Scope
		into  *bool
	}{
		{authz.ScopeAgentRead, &permissions.Read},
		{authz.ScopeAgentWrite, &permissions.Write},
		{authz.ScopeAgentAuthorize, &permissions.Authorize},
		{authz.ScopeAgentTransfer, &permissions.Transfer},
	} {
		allowed, err := a.allows(ctx, human, decision.scope, agent)
		if err != nil {
			return AgentPermissions{}, err
		}
		*decision.into = allowed
	}
	return permissions, nil
}

func ownsUnblockedAgent(human HumanContext, agent repo.Agent) bool {
	return human.Auth != nil && agent.OwnerUserID == human.Auth.UserID && !agent.OwnerReassignmentRequiredAt.Valid
}

func scopeForOwnerPredicate(predicate OwnerPredicate) (authz.Scope, bool) {
	switch predicate {
	case OwnedAgentRead:
		return authz.ScopeAgentRead, true
	case OwnedAgentSetup:
		return authz.ScopeAgentWrite, true
	case OwnedAgentAuthorize:
		return authz.ScopeAgentAuthorize, true
	case OwnedAgentTransfer:
		return authz.ScopeAgentTransfer, true
	default:
		return "", false
	}
}
