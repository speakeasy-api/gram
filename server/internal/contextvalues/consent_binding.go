package contextvalues

import (
	"context"

	"github.com/google/uuid"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// ConsentBindingAuthorization is trusted OAuth consent provenance, not a Gram
// session. Only attachment operations for its exact target may consume it.
type ConsentBindingAuthorization struct {
	UserID         string
	OrganizationID string
	ProjectID      uuid.UUID
	AgentID        uuid.UUID
	IssuerID       uuid.UUID
}

// WithConsentBindingAuthorization must only be called after validating the
// challenge browser, CSRF, live endpoint authority and non-impersonated human.
// It deliberately creates no session provenance or session ID.
func WithConsentBindingAuthorization(ctx context.Context, userID, organizationID string, projectID, agentID, issuerID uuid.UUID) context.Context {
	scope := &ConsentBindingAuthorization{UserID: userID, OrganizationID: organizationID, ProjectID: projectID, AgentID: agentID, IssuerID: issuerID}
	var auth AuthContext
	auth.UserID = userID
	auth.ActiveOrganizationID = organizationID
	auth.ProjectID = &projectID
	auth.actor = urn.NewPrincipal(urn.PrincipalTypeUser, userID)
	auth.consentBinding = scope
	return SetAuthContext(ctx, &auth)
}

// GetConsentBindingAuthorization fails closed if the authenticated identity or
// tenant has been replaced since consent established the scoped provenance.
func GetConsentBindingAuthorization(ctx context.Context) (ConsentBindingAuthorization, bool) {
	var zero ConsentBindingAuthorization
	auth, ok := GetAuthContext(ctx)
	if !ok || auth == nil || auth.consentBinding == nil {
		return zero, false
	}
	if auth.SessionID != nil || HasValidatedGramSession(ctx) || auth.APIKeyID != "" || auth.APIKeyName != "" || len(auth.APIKeyScopes) != 0 || auth.OrgWidePluginHooksKey || IsSupportSession(ctx) || IsLegacyImpersonatedSession(ctx) {
		return zero, false
	}
	if _, ok := APIKeyAuthorization(ctx); ok {
		return zero, false
	}
	if _, ok := PrincipalCredentialAuthorization(ctx); ok {
		return zero, false
	}
	if _, ok := GetAssistantPrincipal(ctx); ok {
		return zero, false
	}
	if _, ok := GetOAuthClientID(ctx); ok {
		return zero, false
	}
	if _, ok := GetActingSurface(ctx); ok {
		return zero, false
	}
	if _, ok := GetRBACScopeOverride(ctx); ok {
		return zero, false
	}
	scope := *auth.consentBinding
	actor, ok := AuthenticatedActor(ctx)
	if !ok || actor.Type != urn.PrincipalTypeUser || actor.ID != scope.UserID {
		return zero, false
	}
	if scope.UserID == "" || scope.OrganizationID == "" || scope.ProjectID == uuid.Nil || scope.AgentID == uuid.Nil || scope.IssuerID == uuid.Nil || auth.UserID != scope.UserID || auth.ActiveOrganizationID != scope.OrganizationID || auth.ProjectID == nil || *auth.ProjectID != scope.ProjectID {
		return zero, false
	}
	return scope, true
}
