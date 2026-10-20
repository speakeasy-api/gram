package identitychaining

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/oauth/protectedresource"
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

	// provider is the EMA provider serving this attempt; set by acquireFrom.
	// A token is released or published only while it can still serve.
	provider remotesessions.EMAProvider

	// Speakeasy provenance snapshots fence issuer and sign-in changes during exchange.
	internalIssuerVersion time.Time
	internalKeyID         uuid.UUID
	// internalKeyPublishedAt is when the signing key was first published.
	internalKeyPublishedAt time.Time
	userSessionID          uuid.UUID
	signInID               uuid.UUID
	email                  string

	// audience is the ID-JAG audience: an administrator-confirmed Okta
	// resource app audience when one exists, else the resource authorization
	// server's issuer. Filled in by authorize.
	audience string
}

// selectBinding applies explicit binding selection: exactly one ready binding
// must name the endpoint's upstream and still pass readiness. Implicit
// selection is not supported, so an upstream without a ready binding keeps the
// interactive path.
func (g *Governor) selectBinding(ctx context.Context, logger *slog.Logger, req Request) (selection, Outcome) {
	sel, outcome, _ := g.selectBindingWith(ctx, logger, req, g.ResolveResource)
	return sel, outcome
}

// selectBindingWith is selectBinding over resolve, which names the endpoint's
// authorization resource. A failed configuration read also returns its error
// beside the outcome the runtime gives it, so a caller that must not mistake
// unknown configuration for absent configuration can tell them apart.
func (g *Governor) selectBindingWith(ctx context.Context, logger *slog.Logger, req Request, resolve func(context.Context, Request) (protectedresource.EndpointResource, error)) (selection, Outcome, error) {
	var none selection
	// Every caller has already authenticated the human or management
	// principal. Unconfigured endpoints perform no discovery at all.
	resource, err := resolve(ctx, req)
	if err != nil {
		logger.WarnContext(ctx, "resolve identity chaining resource", attr.SlogError(err))
		// Failed discovery is never read as "no metadata". It governs only an
		// upstream with a bound resource on its origin, so endpoints without
		// a binding keep their interactive behavior unchanged. Candidates are
		// diagnostic ownership only; fresh, valid metadata is still required
		// to select a resource or token.
		resources, readErr := repo.New(g.db).ListEMAChainingResourceCandidates(ctx, repo.ListEMAChainingResourceCandidatesParams{
			ProjectID: req.ProjectID, OrganizationID: req.OrganizationID, UserSessionIssuerID: req.UserSessionIssuerID,
			RemoteSessionIssuerID: req.RemoteSessionIssuerID,
		})
		if readErr != nil {
			// Unknown configuration keeps the upstream on the interactive path.
			logger.ErrorContext(ctx, "list identity chaining diagnostic candidates", attr.SlogError(readErr))
			return none, notApplicable, fmt.Errorf("list identity chaining diagnostic candidates: %w", readErr)
		}
		if !slices.ContainsFunc(resources, func(resource string) bool { return MatchesDiagnosticOrigin(req.UpstreamResource, resource) }) {
			return none, notApplicable, nil
		}
		if errors.Is(err, protectedresource.ErrEndpointResourceInvalid) {
			return none, newOutcome(StageSelection, ReasonConfigurationRequired, ConfidenceVerified, false), nil
		}
		return none, newOutcome(StageSelection, ReasonTransientFailure, ConfidenceUnknown, true), nil
	}
	bindings, err := repo.New(g.db).ListEMAChainingBindings(ctx, repo.ListEMAChainingBindingsParams{
		ProjectID:             req.ProjectID,
		OrganizationID:        req.OrganizationID,
		UserSessionIssuerID:   req.UserSessionIssuerID,
		UpstreamResource:      strings.TrimRight(resource.Resource, "/"),
		RemoteSessionIssuerID: req.RemoteSessionIssuerID,
		RemoteSessionClientID: req.RemoteSessionClientID,
	})
	if err != nil {
		// Unknown configuration keeps the upstream on the interactive path.
		logger.ErrorContext(ctx, "list identity chaining bindings", attr.SlogError(err))
		return none, notApplicable, fmt.Errorf("list identity chaining bindings: %w", err)
	}
	switch len(bindings) {
	case 0:
		return none, notApplicable, nil
	case 1:
	default:
		return none, newOutcome(StageSelection, ReasonConfigurationRequired, ConfidenceVerified, false), nil
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
		return none, bindingNotReady, fmt.Errorf("read identity chaining binding readiness: %w", err)
	case result.State != remotesessions.PreparationStateReady || result.BindingID != b.ID || result.ClientID == uuid.Nil || result.ExternalClientID == "":
		return none, bindingNotReady, nil
	case req.RemoteSessionClientID.Valid && result.ClientID != req.RemoteSessionClientID.UUID:
		// The caller chose another client for this server; this binding is
		// not one it may be served by.
		return none, notApplicable, nil
	}
	if resource.Discovered && !slices.ContainsFunc(resource.AuthorizationServers, func(server string) bool { return sameIssuer(server, result.Issuer) }) {
		return none, newOutcome(StageSelection, ReasonConfigurationRequired, ConfidenceVerified, false), nil
	}
	scopes := remotesessions.GrantScopes(result.Scopes)
	if len(scopes) == 0 && len(result.Scopes) > 0 {
		logger.DebugContext(ctx, "identity chaining dropped every configured scope as OIDC reserved", attr.SlogOAuthScope(strings.Join(result.Scopes, " ")))
	}
	return selection{
		bindingID:             result.BindingID,
		generation:            result.Generation,
		remoteIssuerID:        b.RemoteSessionIssuerID,
		clientID:              result.ClientID,
		externalClientID:      result.ExternalClientID,
		issuer:                result.Issuer,
		resource:              result.Resource,
		scopes:                scopes,
		configuredScopes:      result.Scopes,
		trustedIssuerID:       uuid.Nil,
		trustedClientID:       uuid.Nil,
		provider:              remotesessions.EMAProvider{Kind: "", RemoteSessionIssuerID: uuid.Nil, InternalSessionIssuerID: uuid.Nil, Unavailable: "", LastAcceptedAt: nil},
		audience:              "",
		internalIssuerVersion: time.Time{}, internalKeyID: uuid.Nil, internalKeyPublishedAt: time.Time{}, userSessionID: uuid.Nil, signInID: uuid.Nil, email: "",
	}, success, nil
}

// errResourceUncached means no stored state names the endpoint's
// authorization resource without discovery.
var errResourceUncached = errors.New("identitychaining: endpoint resource is not cached")

// storedResource is ResolveResource from stored state alone, for the token
// endpoint, which never probes an upstream. A tunneled resource is explicit
// configuration and a cached discovery answers exactly as ResolveResource
// would, including a cached answer that the endpoint publishes no metadata.
// Any other miss is errResourceUncached, even when a binding names the
// transport URL itself: the runtime selects by the resource discovery
// returns, which may be a different binding, so selection reads the miss as
// transient when a binding sits on the upstream's origin.
func (g *Governor) storedResource(ctx context.Context, req Request) (protectedresource.EndpointResource, error) {
	var none protectedresource.EndpointResource
	if g.resolveResource == nil || req.RemoteSessionIssuerID.Valid {
		return protectedresource.EndpointResource{Resource: req.UpstreamResource, AuthorizationServers: nil, Discovered: false}, nil
	}
	if g.cachedResource != nil {
		if resource, ok, err := g.cachedResource(ctx, req.OrganizationID, req.ProjectID, req.UpstreamResource); ok {
			return resource, err
		}
	}
	return none, errResourceUncached
}

// sameIssuer compares issuer identifiers in canonical form, so a trailing
// slash or host case difference between metadata and configuration matches.
func sameIssuer(a, b string) bool {
	if a == b {
		return true
	}
	ca, errA := remotesessions.NormalizeVendorIssuer(a)
	cb, errB := remotesessions.NormalizeVendorIssuer(b)
	return errA == nil && errB == nil && ca == cb
}

// MatchesDiagnosticOrigin does not establish authorization-resource identity.
// It only keeps a known origin's invalid metadata from masquerading as an
// unconfigured endpoint. Exact non-URL resources remain scoped to themselves.
func MatchesDiagnosticOrigin(endpoint, resource string) bool {
	if strings.TrimRight(endpoint, "/") == strings.TrimRight(resource, "/") {
		return true
	}
	a, errA := url.Parse(endpoint)
	b, errB := url.Parse(resource)
	if errA != nil || errB != nil || a.Hostname() == "" || b.Hostname() == "" {
		return false
	}
	port := func(u *url.URL) string {
		if u.Port() != "" {
			return u.Port()
		}
		if u.Scheme == "https" {
			return "443"
		}
		if u.Scheme == "http" {
			return "80"
		}
		return ""
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b)
}
