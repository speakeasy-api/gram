package agentmanagement

import (
	"context"
	"errors"
	"fmt"

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
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil || !contextvalues.HasValidatedGramSession(ctx) || authCtx.SessionID == nil || *authCtx.SessionID == "" || authCtx.UserID == "" || authCtx.ActiveOrganizationID == "" {
		return HumanContext{}, oops.C(oops.CodeUnauthorized)
	}

	if authCtx.APIKeyID != "" || authCtx.APIKeyName != "" || len(authCtx.APIKeyScopes) != 0 || authCtx.OrgWidePluginHooksKey {
		return HumanContext{}, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetAssistantPrincipal(ctx); ok {
		return HumanContext{}, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetOAuthClientID(ctx); ok {
		return HumanContext{}, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetActingSurface(ctx); ok {
		return HumanContext{}, oops.C(oops.CodeForbidden)
	}
	if _, ok := contextvalues.GetRBACScopeOverride(ctx); ok {
		return HumanContext{}, oops.C(oops.CodeForbidden)
	}
	if contextvalues.IsSupportSession(ctx) || contextvalues.IsLegacyImpersonatedSession(ctx) {
		return HumanContext{}, oops.C(oops.CodeForbidden)
	}

	_, err := orgrepo.New(dbtx).LockActiveOrganizationUser(ctx, orgrepo.LockActiveOrganizationUserParams{
		UserID:         conv.ToPGText(authCtx.UserID),
		OrganizationID: authCtx.ActiveOrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return HumanContext{}, oops.C(oops.CodeForbidden)
	}
	if err != nil {
		return HumanContext{}, fmt.Errorf("lock active organization membership: %w", err)
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
	human, err := a.RequireHuman(ctx, dbtx)
	if err != nil {
		return HumanContext{}, err
	}

	_, err = orgrepo.New(dbtx).LockActiveOrganizationUser(ctx, orgrepo.LockActiveOrganizationUserParams{
		UserID:         conv.ToPGText(ownerUserID),
		OrganizationID: human.Auth.ActiveOrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return HumanContext{}, oops.C(oops.CodeForbidden)
	}
	if err != nil {
		return HumanContext{}, fmt.Errorf("lock eligible agent owner: %w", err)
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

// RequireTransfer locks and authorizes the selected agent, then pins the
// replacement owner's active user and membership rows until commit.
func (a *Authorizer) RequireTransfer(ctx context.Context, dbtx repo.DBTX, agentID uuid.UUID, ownerUserID string) (HumanContext, repo.Agent, error) {
	human, agent, err := a.RequireAgentForUpdate(ctx, dbtx, agentID, OwnedAgentTransfer)
	if err != nil {
		return HumanContext{}, repo.Agent{}, err
	}

	_, err = orgrepo.New(dbtx).LockActiveOrganizationUser(ctx, orgrepo.LockActiveOrganizationUserParams{
		UserID:         conv.ToPGText(ownerUserID),
		OrganizationID: human.Auth.ActiveOrganizationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return HumanContext{}, repo.Agent{}, oops.C(oops.CodeForbidden)
	}
	if err != nil {
		return HumanContext{}, repo.Agent{}, fmt.Errorf("lock eligible replacement owner: %w", err)
	}
	return human, agent, nil
}

func (a *Authorizer) requireAgent(ctx context.Context, dbtx repo.DBTX, agentID uuid.UUID, predicate OwnerPredicate, forUpdate bool) (HumanContext, repo.Agent, error) {
	scope, ok := scopeForOwnerPredicate(predicate)
	if !ok {
		return HumanContext{}, repo.Agent{}, fmt.Errorf("unknown owner predicate %q", predicate)
	}

	human, err := a.RequireHuman(ctx, dbtx)
	if err != nil {
		return HumanContext{}, repo.Agent{}, err
	}

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
