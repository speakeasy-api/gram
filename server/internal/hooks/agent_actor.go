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

// errAgentHooksDenied marks an authenticated agent key that failed admission,
// its hooks grant, or project access; it is rejected, never downgraded.
var errAgentHooksDenied = errors.New("agent key denied hooks ingest")

// clearAgentAccountIdentity drops the AI-account identity a batch self-reports
// so agent rows never carry a human's account or device attribution.
func clearAgentAccountIdentity(meta *SessionMetadata) {
	meta.ExternalOrgID = ""
	meta.ExternalAccountUUID = ""
	meta.ExternalAccountID = ""
	meta.DeviceID = ""
	meta.UserAccountID = ""
	meta.ObservedUserEmail = ""
}

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

// agentSessionView keeps only the surface fields of a cached session, and only
// when orgID+projectID seeded it, so an agent event never inherits a human's
// identity, account attribution, or another tenant's metadata.
func agentSessionView(cached SessionMetadata, orgID, projectID string) SessionMetadata {
	view := SessionMetadata{
		SessionID:           cached.SessionID,
		ServiceName:         "",
		UserEmail:           "",
		UserID:              "",
		Provider:            "",
		ExternalOrgID:       "",
		ExternalAccountUUID: "",
		ExternalAccountID:   "",
		DeviceID:            "",
		Hostname:            "",
		Cwd:                 "",
		AccountType:         "",
		BillingMode:         "",
		UserAccountID:       "",
		ObservedUserEmail:   "",
		GramOrgID:           orgID,
		ProjectID:           projectID,
	}
	if cached.GramOrgID != orgID || cached.ProjectID != projectID {
		return view
	}
	view.ServiceName = cached.ServiceName
	view.Provider = cached.Provider
	view.Hostname = cached.Hostname
	view.Cwd = cached.Cwd
	return view
}

// withAgentActor strips client-supplied actor attributes from a telemetry row,
// then stamps the trusted agent actor when one authenticated the request.
func withAgentActor(ctx context.Context, attrs map[attr.Key]any) map[attr.Key]any {
	delete(attrs, attr.AuthorizationActorTypeKey)
	delete(attrs, attr.AuthorizationActorIDKey)
	if actor, ok := contextvalues.AuthenticatedActor(ctx); ok && actor.Type == urn.PrincipalTypeAgent {
		attrs[attr.AuthorizationActorTypeKey] = string(actor.Type)
		attrs[attr.AuthorizationActorIDKey] = actor.ID
	}
	return attrs
}
