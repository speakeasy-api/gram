package remotesessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
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
		Issuer:                            conv.Default(doc.Issuer, issuerURL),
		AuthorizationEndpoint:             conv.PtrEmpty(doc.AuthorizationEndpoint),
		TokenEndpoint:                     conv.PtrEmpty(doc.TokenEndpoint),
		RevocationEndpoint:                conv.PtrEmpty(doc.RevocationEndpoint),
		RegistrationEndpoint:              conv.PtrEmpty(doc.RegistrationEndpoint),
		JwksURI:                           conv.PtrEmpty(doc.JwksURI),
		ServiceDocumentation:              conv.PtrEmpty(doc.ServiceDocumentation),
		OpPolicyURI:                       conv.PtrEmpty(doc.OpPolicyURI),
		OpTosURI:                          conv.PtrEmpty(doc.OpTosURI),
		ScopesSupported:                   doc.ScopesSupported,
		GrantTypesSupported:               doc.GrantTypesSupported,
		ResponseTypesSupported:            doc.ResponseTypesSupported,
		TokenEndpointAuthMethodsSupported: doc.TokenEndpointAuthMethodsSupported,

		// Copied as-is, nil included, so absent stays distinguishable from
		// advertised-empty for as long as the draft lives. The dashboard's
		// discovery flow still captures omission as an empty array when it
		// submits the create form — discovery ran, so "advertises nothing" is
		// a captured fact — while hand-typed setups omit the field and store
		// NULL ("never captured").
		CodeChallengeMethodsSupported: doc.CodeChallengeMethodsSupported,

		ClientIDMetadataDocumentSupported: doc.ClientIDMetadataDocumentSupported,

		UserinfoEndpoint:                           conv.PtrEmpty(doc.UserinfoEndpoint),
		IntrospectionEndpoint:                      conv.PtrEmpty(doc.IntrospectionEndpoint),
		IntrospectionEndpointAuthMethodsSupported:  doc.IntrospectionEndpointAuthMethodsSupported,
		IDTokenSigningAlgValuesSupported:           doc.IDTokenSigningAlgValuesSupported,
		ClaimsSupported:                            doc.ClaimsSupported,
		BackchannelLogoutSupported:                 doc.BackchannelLogoutSupported,
		AuthorizationResponseIssParameterSupported: doc.AuthorizationResponseIssParameterSupported,

		// Gram behavior flags and operator knobs, not discovered metadata. A
		// draft never proposes them; the operator opts in on the create form.
		Oidc:                       false,
		Passthrough:                false,
		ScopeOverride:              nil,
		ResourceIndicatorSupported: nil,
		DiscoveryWarnings:          warnings,
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
	msg, _ := discoveryFailureMessage(err)
	_, untrusted := errors.AsType[*untrustedDocumentError](err)
	_, fetchFailed := errors.AsType[*discoveryError](err)
	_, keySetFetchFailed := errors.AsType[*keySetRefreshError](err)
	switch {
	case untrusted:
		return oops.E(oops.CodeInvalid, err, "%s", msg).LogError(ctx, logger)
	case fetchFailed, keySetFetchFailed:
		return oops.E(unreachable, err, "%s", msg).LogError(ctx, logger)
	default:
		// Unreachable today: discoverIssuerMetadata only ever returns the two
		// types above. An unexpected error is Gram's to explain rather than
		// the caller's to correct.
		return oops.E(oops.CodeUnexpected, err, "%s", msg).LogError(ctx, logger)
	}
}

// discoveryFailureMessage describes a discovery failure in text safe to store
// on the issuer row and show to its operators: it names well-known URLs and
// upstream statuses but never dial errors or the outbound policy's internals.
// transient reports whether the failure says nothing about what the issuer
// serves, so a retry later may succeed.
func discoveryFailureMessage(err error) (msg string, transient bool) {
	if ude, ok := errors.AsType[*untrustedDocumentError](err); ok {
		return ude.reason, false
	}
	if de, ok := errors.AsType[*discoveryError](err); ok {
		return de.UserMessage(), de.transient()
	}
	if ke, ok := errors.AsType[*keySetRefreshError](err); ok {
		return fmt.Sprintf("Could not refresh the JWK Set at %s", ke.uri), true
	}
	return "issuer metadata could not be fetched", false
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
func refreshIssuerMetadata(ctx context.Context, policy *guardian.Policy, resolver *jwks.Resolver, issuer repo.RemoteSessionIssuer) (repo.UpdateRemoteSessionIssuerDiscoveredMetadataParams, []string, error) {
	var zero repo.UpdateRemoteSessionIssuerDiscoveredMetadataParams

	discovered, err := discoverIssuerMetadata(ctx, policy, issuer.Issuer)
	if err != nil {
		return zero, nil, err
	}
	doc := discovered.doc

	if err := vetRefreshedDocument(doc, issuer); err != nil {
		return zero, nil, err
	}

	// A refresh restates the issuer's whole discovered surface, so a member
	// only an unreadable candidate advertises would read as withdrawn. The
	// stored document fills those gaps until the next complete refresh, but
	// only when it names the same issuer the fetched document names: a row
	// repointed to another issuer, including a sibling path on the same
	// host, must not inherit the previous issuer's endpoints. The fill runs
	// after vetRefreshedDocument so a stored member never satisfies a check
	// the upstream failed.
	if discovered.unreadable != "" {
		if storedIssuer := rawDocumentIssuer(issuer.Metadata); storedIssuer != "" && issuerURLsEqual(storedIssuer, doc.Issuer) {
			doc = mergeIssuerMetadata(doc, documentFromRaw(issuer.Metadata))
		}
	}

	// The origin fallback is safe for ordinary metadata, but its key URL is
	// authoritative only for the exact configured issuer. Otherwise a
	// path-scoped issuer could silently adopt keys advertised for another
	// authorization server at the same origin.
	if doc.JwksURI != "" && doc.Issuer != issuer.Issuer {
		return zero, nil, &untrustedDocumentError{
			reason: fmt.Sprintf("metadata document advertises issuer %q, but this identity provider is configured as %q; refusing to trust its jwks_uri", truncateForMessage(doc.Issuer), issuer.Issuer),
		}
	}

	keySet, err := refreshIssuerKeySet(ctx, resolver, doc.JwksURI, issuer)
	if err != nil {
		return zero, nil, err
	}

	return discoveredMetadataParams(doc, discovered.unreadable, keySet, issuer), discovered.warnings, nil
}

type refreshedIssuerKeySet struct {
	document  json.RawMessage
	fetchedAt pgtype.Timestamptz
	expiresAt pgtype.Timestamptz
	etag      string
}

func refreshIssuerKeySet(ctx context.Context, resolver *jwks.Resolver, jwksURI string, issuer repo.RemoteSessionIssuer) (refreshedIssuerKeySet, error) {
	var zero refreshedIssuerKeySet
	if jwksURI == "" {
		return zero, nil
	}

	source, err := jwks.NewRemoteSource(jwksURI)
	if err != nil {
		return zero, &untrustedDocumentError{reason: fmt.Sprintf("metadata document advertises an invalid jwks_uri: %v", err)}
	}

	cache := jwks.CacheState{Document: nil, ETag: "", ExpiresAt: time.Time{}, RefreshedAt: time.Time{}}
	if issuer.JwksUri.Valid && issuer.JwksUri.String == jwksURI {
		cache.Document = json.RawMessage(issuer.Jwks)
		cache.ETag = conv.FromPGTextOrEmpty[string](issuer.JwksEtag)
		if issuer.JwksFetchedAt.Valid {
			cache.RefreshedAt = issuer.JwksFetchedAt.Time
		}
	}
	// ExpiresAt deliberately remains zero: this is the explicit refresh
	// operation, so it must consult the upstream even when the stored cache is
	// still fresh. A usable stored document and ETag still enable a 304.
	result, err := resolver.Resolve(ctx, source, cache)
	if err != nil {
		if errors.Is(err, jwks.ErrKeySetInvalid) || errors.Is(err, jwks.ErrKeySetTooLarge) || errors.Is(err, jwks.ErrPrivateKeyMaterial) || errors.Is(err, jwks.ErrSymmetricKeyMaterial) {
			return zero, &untrustedDocumentError{reason: "jwks_uri did not return a valid public JWK Set"}
		}
		return zero, &keySetRefreshError{uri: jwksURI, cause: err}
	}
	if result.Outcome != jwks.CacheOutcomeRefreshed && result.Outcome != jwks.CacheOutcomeNotModified {
		return zero, fmt.Errorf("explicit JWKS refresh unexpectedly returned %q", result.Outcome)
	}

	now := time.Now()
	return refreshedIssuerKeySet{
		document:  result.Document,
		fetchedAt: pgtype.Timestamptz{Time: now, InfinityModifier: pgtype.Finite, Valid: true},
		expiresAt: pgtype.Timestamptz{Time: now.Add(result.TTL), InfinityModifier: pgtype.Finite, Valid: true},
		etag:      result.ETag,
	}, nil
}

type keySetRefreshError struct {
	uri   string
	cause error
}

func (e *keySetRefreshError) Error() string {
	return fmt.Sprintf("refresh key set at %s: %v", e.uri, e.cause)
}
func (e *keySetRefreshError) Unwrap() error { return e.cause }

// vetRefreshedDocument is the distrust gate a fetched document must pass
// before it may overwrite an issuer's stored metadata. Gram distrusts the
// whole document rather than salvaging parts of it: a refresh overwrites
// metadata that currently works, so a document that deviates on anything
// load-bearing is more likely to be a captive portal, an error page rendered
// as JSON, or a misconfigured gateway than a genuine change the operator wants
// persisted.
//
// The issuer claim is checked first. The update query cannot write the issuer
// column, so the stored URL is never repointed; what the check prevents is
// adopting some *other* authorization server's endpoints, which would send
// users somewhere else at the next sign-in.
//
// An advertised issuer equal to the stored URL's origin is accepted, because
// that is the shape issuerProbeCandidates itself manufactures: when the
// path-aware candidates 404, it falls back to the origin-root well-known URL,
// and gateways that serve metadata only there advertise the origin. Rejecting
// it would make every issuer created through that fallback permanently
// unrefreshable. The relaxation is deliberately no wider than the fallback: a
// sibling path on the same host (a different tenant on a multi-tenant IdP)
// still aborts. collectDiscoveryWarnings has already recorded the divergence,
// so the operator still sees it.
//
// An issuer advertising neither endpoint is unusable for OAuth. Discovery
// returns such a document as a last resort when no probe candidate yields a
// better one, which on create leaves the operator to fill the endpoints in by
// hand. Persisting it over an issuer that currently has working endpoints
// would break every session it mints.
func vetRefreshedDocument(doc rfc8414Document, issuer repo.RemoteSessionIssuer) error {
	switch {
	case doc.Issuer == "":
		return &untrustedDocumentError{
			reason: fmt.Sprintf("metadata document at %s advertises no issuer", issuer.Issuer),
		}
	case !issuerURLsEqual(doc.Issuer, issuer.Issuer) && !issuerURLsEqual(doc.Issuer, issuerOrigin(issuer.Issuer)):
		return &untrustedDocumentError{
			reason: fmt.Sprintf("metadata document advertises issuer %q, but this identity provider is configured as %q; refusing to adopt another authorization server's endpoints", truncateForMessage(doc.Issuer), issuer.Issuer),
		}
	case doc.AuthorizationEndpoint == "":
		return &untrustedDocumentError{
			reason: fmt.Sprintf("metadata document at %s advertises no authorization_endpoint", issuer.Issuer),
		}
	case doc.TokenEndpoint == "":
		return &untrustedDocumentError{
			reason: fmt.Sprintf("metadata document at %s advertises no token_endpoint", issuer.Issuer),
		}
	default:
		return nil
	}
}

// discoveredMetadataParams maps a vetted document onto the parameters that
// persist it over issuer's row. unreadable is the well-known URL discovery
// could not read this run, or "" when every candidate answered.
func discoveredMetadataParams(doc rfc8414Document, unreadable string, keySet refreshedIssuerKeySet, issuer repo.RemoteSessionIssuer) repo.UpdateRemoteSessionIssuerDiscoveredMetadataParams {
	return repo.UpdateRemoteSessionIssuerDiscoveredMetadataParams{
		// An endpoint the issuer has stopped advertising arrives here as an
		// empty string, which the query clears to NULL. Manual endpoint
		// overrides are not preserved: they are rare to nonexistent, and
		// keeping them would make a refresh's result depend on invisible
		// history rather than on what the issuer advertises right now.
		AuthorizationEndpoint: doc.AuthorizationEndpoint,
		TokenEndpoint:         doc.TokenEndpoint,
		// Deliberately absent from vetRefreshedDocument: an issuer that
		// advertises no revocation endpoint is the common case, not a signal
		// that the document is untrustworthy. It clears to NULL like any other
		// endpoint the issuer has stopped advertising, and revoking a session
		// against such an issuer stays a local soft-delete.
		RevocationEndpoint:                doc.RevocationEndpoint,
		RegistrationEndpoint:              doc.RegistrationEndpoint,
		JwksUri:                           doc.JwksURI,
		Jwks:                              string(keySet.document),
		JwksFetchedAt:                     keySet.fetchedAt,
		JwksCacheExpiresAt:                keySet.expiresAt,
		JwksEtag:                          keySet.etag,
		ServiceDocumentation:              doc.ServiceDocumentation,
		OpPolicyUri:                       doc.OpPolicyURI,
		OpTosUri:                          doc.OpTosURI,
		ScopesSupported:                   orEmptySlice(doc.ScopesSupported),
		GrantTypesSupported:               orEmptySlice(doc.GrantTypesSupported),
		ResponseTypesSupported:            orEmptySlice(doc.ResponseTypesSupported),
		TokenEndpointAuthMethodsSupported: orEmptySlice(doc.TokenEndpointAuthMethodsSupported),

		// orEmptySlice is load-bearing here beyond its NOT NULL siblings: this
		// column is nullable, and a refresh is the capture event, so a document
		// omitting the field must persist the empty array ("captured; the
		// upstream advertises nothing") — never NULL, which would revert the
		// row to "never captured".
		CodeChallengeMethodsSupported: orEmptySlice(doc.CodeChallengeMethodsSupported),

		ClientIDMetadataDocumentSupported: doc.ClientIDMetadataDocumentSupported,

		// Session-enrichment capabilities. A refresh is the capture event for
		// these nullable columns too: an omitted endpoint clears to NULL, an
		// omitted array persists as empty, an omitted flag as false.
		UserinfoEndpoint:                           doc.UserinfoEndpoint,
		IntrospectionEndpoint:                      doc.IntrospectionEndpoint,
		IntrospectionEndpointAuthMethodsSupported:  orEmptySlice(doc.IntrospectionEndpointAuthMethodsSupported),
		IDTokenSigningAlgValuesSupported:           orEmptySlice(doc.IDTokenSigningAlgValuesSupported),
		ClaimsSupported:                            orEmptySlice(doc.ClaimsSupported),
		BackchannelLogoutSupported:                 doc.BackchannelLogoutSupported,
		AuthorizationResponseIssParameterSupported: doc.AuthorizationResponseIssParameterSupported,
		Metadata: string(retainableDocument(doc.raw)),

		// Recorded so the next refresh, and anyone reading the row, can tell
		// which members were kept from the stored document rather than read.
		MetadataLastError:    unreadableCandidateMessage(unreadable),
		MetadataLastErrorUrl: unreadable,

		// The identity the update re-asserts, so a concurrent move or issuer
		// rename aborts the write instead of applying it to a row Gram no
		// longer holds the same authorization over.
		ID:             issuer.ID,
		Issuer:         issuer.Issuer,
		ProjectID:      issuer.ProjectID,
		OrganizationID: issuer.OrganizationID,
	}
}

// refreshConflictMessage is what a caller reports when the shared update
// matches no rows: the load succeeded, so the row existed and the caller was
// allowed to write it, and only a concurrent move, rename, or delete explains
// the miss.
const refreshConflictMessage = "identity provider changed while its metadata was being fetched; retry the refresh"

func sameMetadataRefreshSnapshot(a, b repo.RemoteSessionIssuer) bool {
	return sameTimestamp(a.MetadataFetchedAt, b.MetadataFetchedAt) && sameTimestamp(a.UpdatedAt, b.UpdatedAt)
}

// discoveryRetryURL is the well-known URL a transient failure left unread, or "" when the failure is definitive.
func discoveryRetryURL(err error) string {
	if de, ok := errors.AsType[*discoveryError](err); ok && de.transient() {
		return de.WellKnownURL
	}
	if ke, ok := errors.AsType[*keySetRefreshError](err); ok {
		return ke.uri
	}
	return ""
}
