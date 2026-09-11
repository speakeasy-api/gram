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

// HumanContext is identity proven by an ordinary, nonsupport Gram session and
// an active organization membership.
type HumanContext struct {
	Auth   *contextvalues.AuthContext
	grants []authz.Grant
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
}

// Authorizer implements the reusable human-only agent management seam.
type Authorizer struct {
	authz authorizationEngine
}

func NewAuthorizer(engine authorizationEngine) *Authorizer {
	return &Authorizer{authz: engine}
}

// RequireHuman rejects credentials that only carry human attribution. Only an
// ordinary validated Gram session with an active membership is accepted.
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
	if _, ok := contextvalues.GetAssistantPrincipal(ctx); ok {
		return nil, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetOAuthClientID(ctx); ok {
		return nil, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetActingSurface(ctx); ok {
		return nil, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetRBACScopeOverride(ctx); ok {
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

	return HumanContext{Auth: authCtx, grants: grants}, nil
}

// RequireCreate authorizes a prospective agent ID and locks the eligible owner
// membership until the insert commits. Self-owned creation is intrinsic; any
// other owner requires agent:write evaluated against the prospective agent.
func (a *Authorizer) RequireCreate(ctx context.Context, dbtx repo.DBTX, agentID uuid.UUID, ownerUserID string) (HumanContext, error) {
	human, err := a.requireHumanWithMemberships(ctx, dbtx, ownerUserID)
	if err != nil {
		return HumanContext{}, err
	}

	if ownerUserID == human.Auth.UserID {
		return human, nil
	}
	if a.authz == nil {
		return HumanContext{}, errors.New("agent authorization engine is unavailable")
	}
	if err := a.authz.EvaluateLoadedGrants(ctx, human.grants, agentCheck(authz.ScopeAgentWrite, agentID)); err != nil {
		return HumanContext{}, fmt.Errorf("authorize agent creation for another owner: %w", err)
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
func (a *Authorizer) RequireAgentOwnerForUpdate(ctx context.Context, dbtx repo.DBTX, agentID uuid.UUID, predicate OwnerPredicate) (HumanContext, repo.Agent, error) {
	authCtx, err := ordinaryHumanAuth(ctx)
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
	human, err := a.requireHumanWithMemberships(ctx, dbtx, observed.OwnerUserID)
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
	if err := a.authz.EvaluateLoadedGrants(ctx, human.grants, agentCheck(scope, agent.ID)); err != nil {
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

	return AgentPermissions{
		Read:      authz.GrantsSatisfy(human.grants, agentCheck(authz.ScopeAgentRead, agent.ID)),
		Write:     authz.GrantsSatisfy(human.grants, agentCheck(authz.ScopeAgentWrite, agent.ID)),
		Authorize: authz.GrantsSatisfy(human.grants, agentCheck(authz.ScopeAgentAuthorize, agent.ID)),
		Transfer:  authz.GrantsSatisfy(human.grants, agentCheck(authz.ScopeAgentTransfer, agent.ID)),
	}, nil
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

func agentCheck(scope authz.Scope, agentID uuid.UUID) authz.Check {
	return authz.Check{
		Scope:        scope,
		ResourceKind: authz.ResourceKindAgent,
		ResourceID:   agentID.String(),
		Dimensions:   nil,
	}
}
