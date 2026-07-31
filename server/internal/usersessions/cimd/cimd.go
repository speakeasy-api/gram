// Package cimd implements the inbound side of OAuth Client ID Metadata
// Documents (draft-ietf-oauth-client-id-metadata-document-02): resolving a
// URL-shaped client_id presented to the user-session authorization server by
// fetching the document it names, parsing it, and validating it against the
// spec's rules plus Gram's own origin-binding policy.
//
// The package is deliberately stateless: no caching, no persistence, no
// logging. Callers own the upsert of the resolved client and
// the mapping of returned errors onto their wire format. Spec-defined
// rejections are returned as *usersessions.OAuthError with a client-safe
// description; transport-level fetch failures are returned as plain wrapped
// errors whose text may reference internal details and MUST NOT be echoed to
// the OAuth client verbatim.
package cimd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

const (
	// maxClientIDLength caps the URL-shaped client_id in application code.
	// The client_id column is unconstrained TEXT feeding a btree unique
	// index with a ~2704-byte entry limit, so oversized URLs must be
	// rejected here with a clean OAuth error rather than a Postgres index
	// failure.
	maxClientIDLength = 2048

	// maxDocumentBytes is the read cap on the fetched document body. Draft
	// -02 §8.7 frames the size limit as a read cap, not a Content-Length
	// check — the header can lie. Hitting the cap is a fetch failure.
	maxDocumentBytes = 5 * 1024

	// fetchTimeout bounds a single document fetch. The spec is silent on
	// timeouts; this is our choice, applied per Resolve call on top of the
	// caller's context.
	fetchTimeout = 10 * time.Second

	// maxClientNameLength caps the display name an attacker-controlled
	// document can persist and render on the consent page. The whole-body
	// cap alone would allow a ~5 KB name, enough to push the consent
	// screen's origin line — the anti-spoofing trust anchor — out of view.
	maxClientNameLength = 256

	// maxRedirectURIs / maxRedirectURILength bound the TEXT[] a document
	// can write into user_session_clients.redirect_uris. Real clients
	// register a handful of URIs; the caps only exist to stop abuse.
	maxRedirectURIs      = 32
	maxRedirectURILength = 2048
)

// Document is the subset of a Client ID Metadata Document that the
// user-session AS honours, plus the fields it must detect to reject a
// document (client_secret and friends). Unknown members are deliberately
// tolerated (default encoding/json behavior): metadata fields come from the
// open RFC 7591 IANA registry, -02 §4.3 allows an embedded
// software_statement, and future fields (e.g. client_id_expires_at) may
// appear. Rejecting unknown fields is an interop bug, not strictness.
type Document struct {
	// ClientID is the document's own client_id member. -02 §4 requires it
	// to equal the Client Identifier URL the document was fetched from; the
	// validator enforces that equality.
	ClientID string `json:"client_id"`

	// ClientName is the human-readable display name shown on the consent
	// page. Required (MCP mandates it, and the user_session_clients column
	// is NOT NULL) and attacker-chosen for any accepted document — the
	// consent page pairs it with the client_id origin as the verifiable
	// trust anchor.
	ClientName string `json:"client_name"`

	// ClientSecret is captured as a raw presence marker only: -02 §4.1
	// forbids a metadata document from containing client_secret (the
	// document is public by definition), so any appearance — whatever the
	// value type — invalidates the document.
	ClientSecret json.RawMessage `json:"client_secret"`

	// ClientSecretExpiresAt is a raw presence marker like ClientSecret;
	// its appearance invalidates the document for the same reason.
	ClientSecretExpiresAt json.RawMessage `json:"client_secret_expires_at"`

	// ClientURI is the client's informational homepage URL. Optional and
	// currently unused by the AS.
	ClientURI string `json:"client_uri"`

	// GrantTypes is parsed but not enforced: the AS only ever issues
	// authorization_code (+ refresh_token), and request-time validation
	// already rejects anything else. Rejecting a document that merely
	// *declares* additional grant types the client uses elsewhere would be
	// an interop bug.
	GrantTypes []string `json:"grant_types"`

	// JWKS is inspected for private key material (-02 §4.1 bans it) and
	// otherwise ignored (no private_key_jwt support).
	JWKS json.RawMessage `json:"jwks"`

	// LogoURI is the client's logo URL. Optional and deliberately not
	// rendered: like ClientName it is attacker-controllable, and an image
	// is a stronger spoofing primitive than text.
	LogoURI string `json:"logo_uri"`

	// RedirectURIs is the registered redirect set. Required; each entry
	// must pass the standard redirect-URI scheme rules plus Gram's
	// same-origin binding with the loopback exception.
	RedirectURIs []string `json:"redirect_uris"`

	// ResponseTypes is parsed but not enforced, for the same reason as
	// GrantTypes — the AS only supports response type "code".
	ResponseTypes []string `json:"response_types"`

	// TokenEndpointAuthMethod must be "none" — only public clients are
	// accepted. An absent value is also rejected: the RFC 7591 default is
	// client_secret_basic, a symmetric method -02 bans for CIMD.
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method"`
}

// IsClientIDURL reports whether a presented client_id should be treated as a
// Client ID Metadata Document URL. Draft -02 §3 requires the https scheme
// with no normalization anywhere in the protocol, so a case-sensitive prefix
// check is the correct discriminator: DCR-issued ids never look like this,
// and anything https-shaped that fails the full §3 syntax check is an
// invalid CIMD client_id rather than an unknown DCR client.
func IsClientIDURL(clientID string) bool {
	return len(clientID) > len("https://") && clientID[:len("https://")] == "https://"
}

// NewFetchClient builds the production document-fetch client from a
// guardian policy: the SSRF dialer's post-DNS IP check satisfies -02 §8.6's
// ban on fetching RFC 6890 special-use addresses and is immune to DNS
// rebinding. Tests that host documents on httptest servers should pass
// newFetchClientFrom(server.Client()) — or build the policy with
// guardian.WithTLSRootCAs — since httptest certificates are self-signed.
func NewFetchClient(policy *guardian.Policy) *guardian.HTTPClient {
	return newFetchClientFrom(policy.Client())
}

// newFetchClientFrom applies the CIMD fetch rules to a copy of the base
// client: redirects are never followed (-02 §5 MUST NOT; guardian's client
// would otherwise follow up to 10 hops), so a redirect status surfaces as a
// non-200 fetch failure. The copy keeps the caller's client untouched in
// case it is shared.
func newFetchClientFrom(base *guardian.HTTPClient) *guardian.HTTPClient {
	client := *base
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &client
}

// Resolve fetches, parses, and validates the Client ID Metadata Document
// named by clientID. client should come from NewFetchClient. The returned
// Document has passed every check this AS imposes: -02 §3 URL syntax, §4
// triple client_id equality (the fetch never follows redirects, so the
// fetched URL is the presented URL by construction), required client_name +
// redirect_uris, public-client-only auth method, secret/private-key bans,
// and Gram's same-origin redirect-URI binding.
func Resolve(ctx context.Context, client *guardian.HTTPClient, clientID string) (*Document, error) {
	clientIDURL, err := validateClientIDURL(clientID)
	if err != nil {
		return nil, err
	}

	body, err := fetchDocument(ctx, client, clientID)
	if err != nil {
		return nil, fmt.Errorf("fetch client metadata document: %w", err)
	}

	// A malformed body is reported like any other fetch failure (plain
	// wrapped error, generic wire response) rather than as a distinct OAuth
	// error: a distinguishable "reachable but not JSON" response would give
	// unauthenticated callers an oracle for probing external hosts through
	// Gram.
	var doc Document
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse client metadata document: %w", err)
	}

	if err := validateDocument(&doc, clientID, clientIDURL); err != nil {
		return nil, err
	}

	return &doc, nil
}

// fetchDocument retrieves the metadata document. Only HTTP 200 is accepted
// (-02 §5 MUST; other statuses — including the unfollowed redirects — are
// fetch failures and are never cached per §5.2), and the body read is capped
// at maxDocumentBytes.
func fetchDocument(ctx context.Context, client *guardian.HTTPClient, clientID string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return nil, fmt.Errorf("build document request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request document: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("document endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(http.MaxBytesReader(nil, resp.Body, maxDocumentBytes))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return nil, fmt.Errorf("document exceeds %d byte limit", maxDocumentBytes)
		}
		return nil, fmt.Errorf("read document body: %w", err)
	}

	return body, nil
}
