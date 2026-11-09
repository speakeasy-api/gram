package authz

import (
	"context"

	"github.com/google/uuid"

	agentsrepo "github.com/speakeasy-api/gram/server/internal/agents/repo"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

// ReadScopeKind says how much of a project's rows a caller may read.
type ReadScopeKind uint8

const (
	// ReadScopeUnrestricted means the caller reads the whole project.
	ReadScopeUnrestricted ReadScopeKind = iota
	// ReadScopeOwn means the caller reads only rows attributed to them.
	ReadScopeOwn
)

// ReadScope is the authorization answer for a project read: everything, or
// only rows attributed to UserID, UserEmail or one of AgentIDs. Callers turn
// it into query predicates; they do not decide access themselves.
type ReadScope struct {
	Kind      ReadScopeKind
	UserID    string
	UserEmail string
	// AgentIDs are the agents the caller currently owns. Latched, deleted and
	// transferred agents are not theirs and never appear here.
	AgentIDs []uuid.UUID
}

// Unrestricted reports whether the caller may read the whole project.
func (s ReadScope) Unrestricted() bool { return s.Kind == ReadScopeUnrestricted }

// allowsOwnFallback reports whether a member lacking scope still reads their
// own share of the project. Every other scope is all or nothing.
func allowsOwnFallback(scope Scope) bool {
	return scope == ScopeLogRead
}

// ResolveReadScope decides how much of projectID the caller reads under scope.
// Holding the grant, or RBAC being off, gives ReadScopeUnrestricted. A member
// reading as themselves without it gets ReadScopeOwn when scope allows the
// fallback. API keys, the assistant runtime and embedded chat-session callers
// never fall back: they get the plain Require and its denial.
func (e *Engine) ResolveReadScope(ctx context.Context, scope Scope, projectID string) (ReadScope, error) {
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return ReadScope{}, oops.C(oops.CodeUnauthorized)
	}
	check := Check{Scope: scope, ResourceKind: "", ResourceID: projectID, Dimensions: nil, selectorMatch: selectorMatchNormal, representedUser: ""}
	_, isAssistantCall := contextvalues.GetAssistantPrincipal(ctx)
	humanMember := authCtx.UserID != "" && authCtx.ExternalUserID == "" && authCtx.APIKeyID == "" && !isAssistantCall
	if !allowsOwnFallback(scope) || !humanMember {
		if err := e.Require(ctx, check); err != nil {
			return ReadScope{}, err
		}
		return ReadScope{Kind: ReadScopeUnrestricted, UserID: "", UserEmail: "", AgentIDs: nil}, nil
	}
	// Evaluate, not Require: a member without the grant is expected, not a
	// denial to record. It also keeps project-wide reads where RBAC is off.
	allowed, err := e.Evaluate(ctx, check)
	if err != nil {
		return ReadScope{}, oops.E(oops.CodeUnexpected, err, "evaluate read scope")
	}
	if allowed {
		return ReadScope{Kind: ReadScopeUnrestricted, UserID: "", UserEmail: "", AgentIDs: nil}, nil
	}
	if e.db == nil {
		return ReadScope{}, oops.E(oops.CodeUnexpected, nil, "resolve owned agents: engine has no database")
	}
	agentIDs, err := agentsrepo.New(e.db).ListOwnedAgentIDs(ctx, agentsrepo.ListOwnedAgentIDsParams{
		OrganizationID: authCtx.ActiveOrganizationID,
		OwnerUserID:    authCtx.UserID,
	})
	if err != nil {
		return ReadScope{}, oops.E(oops.CodeUnexpected, err, "resolve owned agents").LogError(ctx, e.logger)
	}
	return ReadScope{
		Kind:      ReadScopeOwn,
		UserID:    authCtx.UserID,
		UserEmail: conv.PtrValOr(authCtx.Email, ""),
		AgentIDs:  agentIDs,
	}, nil
}
