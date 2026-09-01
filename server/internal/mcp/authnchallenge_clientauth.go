// Client authentication for the issuer-gated OAuth surface: the one decision
// both the token and revocation endpoints make before doing anything on a
// client's behalf. The persisted token_endpoint_auth_method is the control:
// it says what the client committed to at registration, and the request has
// to present exactly that, no more and no less.

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"golang.org/x/crypto/bcrypt"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/usersessions/clientauth"
	"github.com/speakeasy-api/gram/server/internal/usersessions/clientcred"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
	usersessions_repo "github.com/speakeasy-api/gram/server/internal/usersessions/repo"
)

// clientAuthFailureDescription is the one description every client
// authentication failure carries on the wire, from an unregistered client_id
// through a wrong secret to a bad assertion. A CIMD client_id is a URL, so a
// response that distinguished "no such client" from "bad credential" would
// tell an unauthenticated caller which vendors' clients an issuer has seen.
// The reason label in the logs keeps every distinction for operators, which
// is where a misconfigured client gets diagnosed.
//
// Two rejections keep their own description on purpose. "client_id is
// required" is decided before any lookup, so it discloses nothing about the
// row. The CIMD admission `disabled` message names customer-configured
// policy rather than a secret, and its explicitness is what makes it
// actionable.
const clientAuthFailureDescription = "client authentication failed"

// clientKeySource builds the key source an assertion client's row records.
// Exactly one of the two columns is set for a well-formed row; neither set is
// a persistence gap, reported rather than treated as a public client.
func clientKeySource(row *usersessions_repo.UserSessionClient) (jwks.Source, error) {
	switch {
	case len(row.ClientJwks) > 0:
		source, err := jwks.NewInlineSource(row.ClientJwks)
		if err != nil {
			return jwks.Source{}, fmt.Errorf("stored client_jwks: %w", err)
		}
		return source, nil
	case row.ClientJwksUri.Valid && row.ClientJwksUri.String != "":
		source, err := jwks.NewRemoteSource(row.ClientJwksUri.String)
		if err != nil {
			return jwks.Source{}, fmt.Errorf("stored client_jwks_uri: %w", err)
		}
		return source, nil
	default:
		return jwks.Source{}, errors.New("client declares private_key_jwt but stores no key source")
	}
}

// authenticateOAuthClient checks the presented credentials against what the
// client row requires. Shared by the token and revocation endpoints so the
// two cannot drift, and so a jti spent at one is spent at the other. Returns
// the failure reason for the credential event log, or "" when the client
// authenticated; every failure is answered on the wire with
// clientAuthFailureDescription.
//
// The rule is exact match in both directions. A public client presenting any
// credential, a symmetric client presenting an assertion, and an assertion
// client presenting a secret are all refused: an unrequested credential is
// not an upgrade, it is a client that does not match its registration.
func (s *Service) authenticateOAuthClient(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint, at clientAssertionEndpoint, row *usersessions_repo.UserSessionClient, creds presentedClientCredentials, baseURL string) string {
	kind, err := clientcred.KindOf(row.TokenEndpointAuthMethod, row.ClientSecretHash.Valid)
	if err != nil {
		logger.ErrorContext(ctx, "user session client row is not authenticatable, failing closed",
			attr.SlogOAuthClientID(row.ClientID),
			attr.SlogError(err),
		)
		return "client_method_unreadable"
	}

	switch kind {
	case clientcred.KindPublic:
		if creds.method != oauthwire.AuthMethodNone {
			return "public_client_presented_credentials"
		}
		return ""

	case clientcred.KindSecret:
		if creds.assertion.Presented() {
			return "secret_client_presented_assertion"
		}
		if err := bcrypt.CompareHashAndPassword([]byte(row.ClientSecretHash.String), []byte(creds.secret)); err != nil {
			return "client_secret_mismatch"
		}
		return ""

	case clientcred.KindKey:
		// Anything secret-shaped alongside the assertion is refused, which
		// covers HTTP Basic with an empty password: RFC 6749 §2.3 forbids
		// more than one authentication method per request, and the
		// extractor labels every such mix. Only a bare assertion, or
		// nothing at all (refused as missing by the verifier), gets through.
		if creds.method != oauthwire.AuthMethodPrivateKeyJWT && creds.method != oauthwire.AuthMethodNone {
			return "assertion_client_presented_secret"
		}
		return s.verifyClientAssertion(ctx, logger, endpoint, at, row, creds.assertion, baseURL)

	default:
		logger.ErrorContext(ctx, "unhandled client credential kind, failing closed",
			attr.SlogOAuthClientID(row.ClientID),
		)
		return "client_method_unreadable"
	}
}

// verifyClientAssertion runs an assertion client through the verifier with
// the expectation this endpoint imposes, returning the failure reason or ""
// when the assertion verified. Every rejection reason comes from the
// verifier's vocabulary so the logs read the same at both endpoints.
func (s *Service) verifyClientAssertion(ctx context.Context, logger *slog.Logger, endpoint *ResolvedMcpEndpoint, at clientAssertionEndpoint, row *usersessions_repo.UserSessionClient, assertion clientauth.Assertion, baseURL string) string {
	if s.clientAssertionVerifier == nil {
		// No shared store, no single-use guarantee. Refused, and loudly:
		// this is a surface that should not be receiving these requests.
		logger.ErrorContext(ctx, "client assertion presented on a surface with no verifier, refusing",
			attr.SlogOAuthClientID(row.ClientID),
		)
		return "assertion_verifier_unavailable"
	}

	keySource, err := clientKeySource(row)
	if err != nil {
		logger.ErrorContext(ctx, "assertion client has no usable key source, failing closed",
			attr.SlogOAuthClientID(row.ClientID),
			attr.SlogError(err),
		)
		return "assertion_key_source_missing"
	}

	urls, err := endpoint.AuthorizationServerURLs(baseURL)
	if err != nil {
		// Cannot compute what aud may name, so nothing can be accepted.
		logger.ErrorContext(ctx, "cannot derive assertion audiences for endpoint, failing closed", attr.SlogError(err))
		return string(clientauth.ReasonVerifierMisconfigured)
	}

	result, err := s.clientAssertionVerifier.Verify(ctx, assertion, clientauth.ClientExpectation(
		row.ClientID,
		// Scoped to the issuer, so every key set its clients name draws on
		// one fetch budget.
		keySource.WithFetchScope(endpoint.UserSessionIssuerID.String()),
		// The issuer row id, never a URL: an endpoint reachable on a custom
		// domain and the default host has two issuer URLs, and keying the
		// replay guard on either would let one assertion be spent per host.
		endpoint.UserSessionIssuerID.String(),
		urls.clientAssertionAudiences(at),
	))
	if err != nil {
		reason := clientauth.ReasonOf(err)
		if reason == "" {
			reason = "assertion_rejected"
		}
		// The cause can name key set URLs or resolver internals, which is
		// why it goes to the log and never to the description.
		logger.WarnContext(ctx, "client assertion rejected",
			attr.SlogOAuthClientID(row.ClientID),
			attr.SlogOAuthFailureReason(string(reason)),
			attr.SlogError(err),
		)
		return string(reason)
	}

	// Which audience form real clients send, and how long they mint
	// assertions for, are the two things the accepted rules were chosen
	// without evidence of. This line is where that evidence accumulates.
	logger.InfoContext(ctx, "client assertion accepted",
		attr.SlogOAuthClientID(row.ClientID),
		attr.SlogOAuthAssertionAudience(string(result.Audience)),
		attr.SlogOAuthAssertionExpiresAt(result.ExpiresAt),
	)
	return ""
}
