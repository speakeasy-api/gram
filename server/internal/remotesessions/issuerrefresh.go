package remotesessions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/urls"
)

// untrustedDocumentError marks a discovery document that parsed but that Gram
// refuses to persist over an issuer's stored metadata. It is distinct from
// *discoveryError, which means the document could not be fetched or read at
// all: here the upstream answered, and what it said is the problem.
//
// Both map to a 4xx. The upstream is the customer's own identity provider, so
// a document Gram will not act on is a fact about their configuration to
// surface, not a Gram fault to page on.
type untrustedDocumentError struct {
	reason string
}

func (e *untrustedDocumentError) Error() string { return e.reason }

// buildIssuerDraft projects a fetched metadata document into the draft the
// three fetchMetadata handlers return. They differ only in what they authorize
// against, so the projection lives here: the rule about which advertised values
// are safe to carry into a create form should not be able to drift between
// tiers.
func buildIssuerDraft(doc rfc8414Document, issuerURL string, warnings []string) *types.RemoteSessionIssuerDraft {
	return &types.RemoteSessionIssuerDraft{
		Issuer:                conv.Default(doc.Issuer, issuerURL),
		AuthorizationEndpoint: conv.PtrEmpty(doc.AuthorizationEndpoint),
		TokenEndpoint:         conv.PtrEmpty(doc.TokenEndpoint),
		RegistrationEndpoint:  conv.PtrEmpty(doc.RegistrationEndpoint),
		JwksURI:               conv.PtrEmpty(doc.JwksURI),
		// The issuer controls these and downstream surfaces render them as
		// links, so a value that is not an absolute http(s) URL is discarded
		// rather than carried into the draft the create form submits back.
		ServiceDocumentation:              conv.PtrEmpty(conv.Ternary(urls.IsAbsoluteHTTP(doc.ServiceDocumentation), doc.ServiceDocumentation, "")),
		OpPolicyURI:                       conv.PtrEmpty(conv.Ternary(urls.IsAbsoluteHTTP(doc.OpPolicyURI), doc.OpPolicyURI, "")),
		OpTosURI:                          conv.PtrEmpty(conv.Ternary(urls.IsAbsoluteHTTP(doc.OpTosURI), doc.OpTosURI, "")),
		ScopesSupported:                   doc.ScopesSupported,
		GrantTypesSupported:               doc.GrantTypesSupported,
		ResponseTypesSupported:            doc.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: doc.TokenEndpointAuthMethodsSupported,
		ClientIDMetadataDocumentSupported: doc.ClientIDMetadataDocumentSupported,
		// Gram behavior flags, not discovered metadata. A draft never proposes
		// them; the operator opts in on the create form.
		Oidc:              false,
		Passthrough:       false,
		DiscoveryWarnings: warnings,
	}
}

// issuerOrigin reduces an issuer URL to its scheme and host. Returns the input
// unchanged when it does not parse as an absolute URL, so a caller comparing
// against it simply finds no match rather than matching everything.
func issuerOrigin(issuerURL string) string {
	u, err := url.Parse(issuerURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return issuerURL
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host}).String()
}

// mapDiscoveryError turns the errors discovery raises into the response a fetch
// or refresh handler returns for them.
//
// A document that was read fine and is merely not one Gram will persist is
// always a 422: nothing about retrying changes the answer, and the upstream is
// the caller's own identity provider.
//
// Whether an unreachable or unparseable upstream is the caller's fault depends
// on which method asked, so callers pass the code. On fetchMetadata the caller
// supplied the URL, and a host that does not resolve is a typo: 400, and
// customer IdP misconfiguration stays out of Gram's error budget. On
// refreshMetadata the caller supplied only an issuer id and Gram chose the URL
// from the stored row, so the same failure means an upstream Gram depends on is
// down or slow. That is a 502 — it is not caller error, and SDK retry policies
// treat 4xx as terminal, which would make a thirty-second outage look permanent.
func mapDiscoveryError(ctx context.Context, logger *slog.Logger, err error, unreachable oops.Code) error {
	if ude, ok := errors.AsType[*untrustedDocumentError](err); ok {
		return oops.E(oops.CodeInvalid, err, "%s", ude.reason).LogError(ctx, logger)
	}
	if df, ok := errors.AsType[*discoveryError](err); ok {
		return oops.E(unreachable, err, "%s", df.UserMessage()).LogError(ctx, logger)
	}
	// Unreachable today: discoverIssuerMetadata only ever returns the two types
	// above. Anything new arrives here, and an unexpected error is Gram's to
	// explain rather than the caller's to correct.
	return oops.E(oops.CodeUnexpected, err, "fetch issuer metadata").LogError(ctx, logger)
}

// refreshIssuerMetadata re-reads an existing issuer's upstream RFC 8414
// metadata document and projects it into the parameters that persist it.
//
// The signature is deliberately narrow: a stored issuer row and the outbound
// policy in, update parameters and warnings out. It touches no auth context, no
// Goa payload, and no database handle, so all three tier handlers share it
// despite authorizing and loading their rows differently — and so a future
// scheduled refresh can call it straight from a Temporal activity, with the
// workflow owning retry, rate limiting, and fan-out, rather than reimplementing
// discovery or driving the HTTP endpoint from inside the worker.
//
// It performs no database work at all, which is what lets callers run it before
// opening the transaction that writes the result. Discovery is a network round
// trip to a third party under a ten-second budget; running it inside the
// transaction would hold a pooled connection open for the duration.
//
// Only RFC 8414-derived columns are represented in the returned parameters.
// Gram's own behavior and display fields cannot be expressed through them —
// see UpdateRemoteSessionIssuerDiscoveredMetadata, which has no parameter for
// slug, issuer, name, logo, client setup documentation, oidc, or passthrough.
func refreshIssuerMetadata(ctx context.Context, policy *guardian.Policy, issuer repo.RemoteSessionIssuer) (repo.UpdateRemoteSessionIssuerDiscoveredMetadataParams, []string, error) {
	var zero repo.UpdateRemoteSessionIssuerDiscoveredMetadataParams

	doc, warnings, err := discoverIssuerMetadata(ctx, policy, issuer.Issuer)
	if err != nil {
		return zero, nil, err
	}

	// Gram distrusts the whole document rather than salvaging parts of it. A
	// refresh overwrites metadata that currently works, so a document that
	// deviates on anything load-bearing is more likely to be a captive portal,
	// an error page rendered as JSON, or a misconfigured gateway than a genuine
	// change the operator wants persisted.
	//
	// The issuer claim is checked first. This query cannot write the issuer
	// column, so the stored URL is never repointed; what the check prevents is
	// adopting some *other* authorization server's endpoints, which would send
	// users somewhere else at the next sign-in.
	//
	// An advertised issuer equal to the stored URL's origin is accepted, because
	// that is the shape issuerProbeCandidates itself manufactures: when the
	// path-aware candidates 404, it falls back to the origin-root well-known
	// URL, and gateways that serve metadata only there advertise the origin.
	// Rejecting it would make every issuer created through that fallback
	// permanently unrefreshable. The relaxation is deliberately no wider than
	// the fallback: a sibling path on the same host (a different tenant on a
	// multi-tenant IdP) still aborts. collectDiscoveryWarnings has already
	// recorded the divergence, so the operator still sees it.
	switch {
	case doc.Issuer == "":
		return zero, nil, &untrustedDocumentError{
			reason: fmt.Sprintf("metadata document at %s advertises no issuer", issuer.Issuer),
		}
	case !issuerURLsEqual(doc.Issuer, issuer.Issuer) && !issuerURLsEqual(doc.Issuer, issuerOrigin(issuer.Issuer)):
		return zero, nil, &untrustedDocumentError{
			reason: fmt.Sprintf("metadata document advertises issuer %q, but this identity provider is configured as %q; refusing to adopt another authorization server's endpoints", doc.Issuer, issuer.Issuer),
		}
	// An issuer advertising neither endpoint is unusable for OAuth. Discovery
	// returns such a document as a last resort when no probe candidate yields a
	// better one, which on create leaves the operator to fill the endpoints in
	// by hand. Persisting it over an issuer that currently has working
	// endpoints would break every session it mints.
	case doc.AuthorizationEndpoint == "":
		return zero, nil, &untrustedDocumentError{
			reason: fmt.Sprintf("metadata document at %s advertises no authorization_endpoint", issuer.Issuer),
		}
	case doc.TokenEndpoint == "":
		return zero, nil, &untrustedDocumentError{
			reason: fmt.Sprintf("metadata document at %s advertises no token_endpoint", issuer.Issuer),
		}
	}

	return repo.UpdateRemoteSessionIssuerDiscoveredMetadataParams{
		// An endpoint the issuer has stopped advertising arrives here as an
		// empty string, which the query clears to NULL. Manual endpoint
		// overrides are not preserved: they are rare to nonexistent, and
		// keeping them would make a refresh's result depend on invisible
		// history rather than on what the issuer advertises right now.
		AuthorizationEndpoint: doc.AuthorizationEndpoint,
		TokenEndpoint:         doc.TokenEndpoint,
		RegistrationEndpoint:  doc.RegistrationEndpoint,
		JwksUri:               doc.JwksURI,
		// Downstream surfaces render these as links, so a value that is not an
		// absolute http(s) URL is dropped rather than stored — matching how the
		// create-time draft filters them.
		ServiceDocumentation:              conv.Ternary(urls.IsAbsoluteHTTP(doc.ServiceDocumentation), doc.ServiceDocumentation, ""),
		OpPolicyUri:                       conv.Ternary(urls.IsAbsoluteHTTP(doc.OpPolicyURI), doc.OpPolicyURI, ""),
		OpTosUri:                          conv.Ternary(urls.IsAbsoluteHTTP(doc.OpTosURI), doc.OpTosURI, ""),
		ScopesSupported:                   orEmptySlice(doc.ScopesSupported),
		GrantTypesSupported:               orEmptySlice(doc.GrantTypesSupported),
		ResponseTypesSupported:            orEmptySlice(doc.ResponseTypesSupported),
		TokenEndpointAuthMethodsSupported: orEmptySlice(doc.TokenEndpointAuthMethodsSupported),
		ClientIDMetadataDocumentSupported: doc.ClientIDMetadataDocumentSupported,
		// The identity the update re-asserts, so a concurrent move or issuer
		// rename aborts the write instead of applying it to a row Gram no
		// longer holds the same authorization over.
		ID:             issuer.ID,
		Issuer:         issuer.Issuer,
		ProjectID:      issuer.ProjectID,
		OrganizationID: issuer.OrganizationID,
	}, warnings, nil
}

// refreshConflictMessage is what a caller reports when the shared update
// matches no rows: the load succeeded, so the row existed and the caller was
// allowed to write it, and only a concurrent move, rename, or delete explains
// the miss.
const refreshConflictMessage = "identity provider changed while its metadata was being fetched; retry the refresh"
