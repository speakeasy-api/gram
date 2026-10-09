package identitychaining

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oauthwire"
	"github.com/speakeasy-api/gram/server/internal/remotesessions"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// selection is the resource authorization server registration a binding
// selected, as read from one readiness snapshot, plus the endpoint's trusted
// identity provider registration.
type selection struct {
	bindingID        uuid.UUID
	generation       int64
	remoteIssuerID   uuid.UUID
	clientID         uuid.UUID
	externalClientID string
	issuer           string
	resource         string
	scopes           []string

	// configuredScopes is the binding's scope set before OIDC filtering; it
	// bounds an ID-JAG's scope when filtering left none to request.
	configuredScopes []string

	// trustedIssuerID and trustedClientID are the user session issuer's
	// upstream identity provider registration, filled in by authorize.
	trustedIssuerID uuid.UUID
	trustedClientID uuid.UUID

	// audience is the ID-JAG audience: an administrator-confirmed Okta
	// resource app audience when one exists, else the resource authorization
	// server's issuer. Filled in by authorize.
	audience string
}

// GrantScopes drops OIDC-reserved scopes, which Okta rejects in ID-JAG
// requests. An empty result requests no scope.
func GrantScopes(scopes []string) []string {
	var out []string
	for _, scope := range scopes {
		if !oauthwire.IsOIDCReservedScope(scope) {
			out = append(out, scope)
		}
	}
	return out
}

// selectBinding applies explicit binding selection: exactly one ready binding
// must name the endpoint's upstream and still pass readiness. Implicit
// selection is not supported, so an upstream without a ready binding keeps the
// interactive path.
func (g *Governor) selectBinding(ctx context.Context, logger *slog.Logger, req Request) (selection, Outcome) {
	var none selection
	bindings, err := repo.New(g.db).ListEMAChainingBindings(ctx, repo.ListEMAChainingBindingsParams{
		ProjectID:             req.ProjectID,
		OrganizationID:        req.OrganizationID,
		UserSessionIssuerID:   req.UserSessionIssuerID,
		UpstreamResource:      strings.TrimRight(req.UpstreamResource, "/"),
		RemoteSessionIssuerID: req.RemoteSessionIssuerID,
	})
	if err != nil {
		// Unknown configuration keeps the upstream on the interactive path.
		logger.ErrorContext(ctx, "list identity chaining bindings", attr.SlogError(err))
		return none, notApplicable
	}
	switch len(bindings) {
	case 0:
		return none, notApplicable
	case 1:
	default:
		return none, newOutcome(StageSelection, ReasonConfigurationRequired, ConfidenceVerified, false)
	}
	b := bindings[0]

	result, err := remotesessions.ReadIdentityChainingForTenant(ctx, g.db, req.ProjectID, req.OrganizationID, remotesessions.PreparationInput{
		UserSessionIssuerID:     b.UserSessionIssuerID,
		RemoteSessionIssuerID:   b.RemoteSessionIssuerID,
		Resource:                b.Resource,
		ClientID:                uuid.Nil,
		Scopes:                  nil,
		Mechanism:               "",
		ConfirmGrants:           nil,
		ExpectedGeneration:      0,
		TokenEndpointAuthMethod: "",
		ResourceMetadata:        nil,
	})
	switch {
	case err != nil:
		logger.WarnContext(ctx, "read identity chaining binding readiness", attr.SlogError(err))
		return none, bindingNotReady
	case result.State != remotesessions.PreparationStateReady || result.BindingID != b.ID || result.ClientID == uuid.Nil || result.ExternalClientID == "":
		return none, bindingNotReady
	}
	scopes := GrantScopes(result.Scopes)
	if len(scopes) == 0 && len(result.Scopes) > 0 {
		logger.DebugContext(ctx, "identity chaining dropped every configured scope as OIDC reserved", attr.SlogOAuthScope(strings.Join(result.Scopes, " ")))
	}
	return selection{
		bindingID:        result.BindingID,
		generation:       result.Generation,
		remoteIssuerID:   b.RemoteSessionIssuerID,
		clientID:         result.ClientID,
		externalClientID: result.ExternalClientID,
		issuer:           result.Issuer,
		resource:         result.Resource,
		scopes:           scopes,
		configuredScopes: result.Scopes,
		trustedIssuerID:  uuid.Nil,
		trustedClientID:  uuid.Nil,
		audience:         "",
	}, success
}
