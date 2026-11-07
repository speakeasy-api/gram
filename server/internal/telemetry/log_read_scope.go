package telemetry

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/authz"
	chatRepo "github.com/speakeasy-api/gram/server/internal/chat/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/telemetry/repo"
)

// OwnedAgentLister lists the agents a member currently owns. The chat
// repository's ListOwnedAgentIDs is the production implementation.
type OwnedAgentLister interface {
	ListOwnedAgentIDs(ctx context.Context, arg chatRepo.ListOwnedAgentIDsParams) ([]uuid.UUID, error)
}

// LogReadScopeResolver decides how much of a project's tool logs a caller
// reads. It is shared by the management API and the Platform MCP so both
// answer the question the same way.
type LogReadScopeResolver struct {
	Authz  *authz.Engine
	Agents OwnedAgentLister
	Logger *slog.Logger
}

// Resolve returns nil when the caller may read the whole project. A member
// without log:read gets an OwnScope instead of a refusal; non-human callers
// keep the plain Require. identityOrg is consulted only for an own scope.
func (r LogReadScopeResolver) Resolve(ctx context.Context, projectID string, identityOrg func() string) (*repo.OwnScope, error) {
	return r.ResolveScope(ctx, authz.ScopeLogRead, projectID, identityOrg)
}

// ResolveScope is Resolve for any project scope that may fall back to the
// caller's own activity: log:read for tool logs, chat:read for sessions.
func (r LogReadScopeResolver) ResolveScope(ctx context.Context, scope authz.Scope, projectID string, identityOrg func() string) (*repo.OwnScope, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return nil, oops.C(oops.CodeUnauthorized)
	}
	check := authz.Check{Scope: scope, ResourceKind: "", ResourceID: projectID, Dimensions: nil}
	// Only a member reading as themselves falls back. API keys, the assistant
	// runtime and embedded chat-session callers keep the plain check.
	_, isAssistantCall := contextvalues.GetAssistantPrincipal(ctx)
	if authCtx.UserID == "" || authCtx.ExternalUserID != "" || authCtx.APIKeyID != "" || isAssistantCall {
		return nil, r.Authz.Require(ctx, check)
	}
	// Evaluate, not Require: a member without the grant is expected, not a
	// denial to record. It also keeps project-wide reads where RBAC is off.
	allowed, err := r.Authz.Evaluate(ctx, check)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "evaluate telemetry read access")
	}
	if allowed {
		return nil, nil
	}
	ownedAgents, err := r.Agents.ListOwnedAgentIDs(ctx, chatRepo.ListOwnedAgentIDsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
	})
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "resolve owned agents").LogError(ctx, r.Logger)
	}
	agentIDs := make([]string, 0, len(ownedAgents))
	for _, id := range ownedAgents {
		agentIDs = append(agentIDs, id.String())
	}
	return &repo.OwnScope{
		UserID:      authCtx.UserID,
		UserEmail:   conv.PtrValOr(authCtx.Email, ""),
		AgentIDs:    agentIDs,
		IdentityOrg: identityOrg(),
	}, nil
}
