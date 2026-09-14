package hooks

import (
	"context"
	"errors"
	"fmt"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

var errAgentHookUnscoped = errors.New("agent hook request has no project scope")

// isAgentActor reports whether an agent-principal credential authenticated the
// request. The agent is then the actor: self-reported emails and cached session
// identity never re-attribute its events to a human.
func isAgentActor(ctx context.Context) bool {
	actor, ok := contextvalues.AuthenticatedActor(ctx)
	return ok && actor.Type == urn.PrincipalTypeAgent
}

// requireAgentHooksIngest admits an agent actor only when it holds
// org:hooks_ingest on its organization. Other callers pass unchanged.
func (s *Service) requireAgentHooksIngest(ctx context.Context) error {
	if !isAgentActor(ctx) {
		return nil
	}
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	if !ok || authCtx == nil {
		return oops.C(oops.CodeUnauthorized)
	}
	if err := s.authz.Require(ctx, authz.Check{Scope: authz.ScopeOrgHooksIngest, ResourceKind: "", ResourceID: authCtx.ActiveOrganizationID, Dimensions: nil}); err != nil {
		return fmt.Errorf("authorize agent hooks ingest: %w", err)
	}
	return nil
}

// agentSessionView keeps only the surface fields of a cached session so an
// agent event never inherits a human's identity or account attribution.
func agentSessionView(cached SessionMetadata) SessionMetadata {
	return SessionMetadata{
		SessionID:           cached.SessionID,
		ServiceName:         cached.ServiceName,
		UserEmail:           "",
		UserID:              "",
		Provider:            cached.Provider,
		ExternalOrgID:       "",
		ExternalAccountUUID: "",
		ExternalAccountID:   "",
		DeviceID:            "",
		Hostname:            cached.Hostname,
		Cwd:                 cached.Cwd,
		AccountType:         "",
		BillingMode:         "",
		UserAccountID:       "",
		ObservedUserEmail:   "",
		GramOrgID:           cached.GramOrgID,
		ProjectID:           cached.ProjectID,
	}
}

// withAgentActor stamps the agent actor onto a per-event hook telemetry row.
func withAgentActor(ctx context.Context, attrs map[attr.Key]any) map[attr.Key]any {
	if actor, ok := contextvalues.AuthenticatedActor(ctx); ok && actor.Type == urn.PrincipalTypeAgent {
		attrs[attr.AuthorizationActorTypeKey] = string(actor.Type)
		attrs[attr.AuthorizationActorIDKey] = actor.ID
	}
	return attrs
}
