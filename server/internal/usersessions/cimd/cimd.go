// Package cimd implements the inbound side of OAuth Client ID Metadata
// Documents (draft-ietf-oauth-client-id-metadata-document-02): resolving a
// URL-shaped client_id presented to the user-session authorization server by
// fetching the document it names, parsing it, and validating it against the
// spec's rules plus Gram's own origin-binding policy.
//
// The Resolver owns the fetch lifecycle and its telemetry (metrics + logs) but
// deliberately nothing else: no caching (until AIS-216) and no persistence.
// Callers own the upsert of the resolved client and the mapping of returned
// errors onto their wire format.
//
// # Two entry points, two disclosure levels
//
// Resolve is the OAuth path. It serves an UNAUTHENTICATED surface, so it
// deliberately discloses as little as possible: spec-defined rejections come
// back as *oauthwire.Error with a client-safe description, while every
// transport-level failure comes back as a plain wrapped error whose text may
// reference internal details and MUST NOT be echoed to the OAuth client
// verbatim. The opacity rule extends to the result taxonomy — a parse failure
// is distinguished from a fetch failure only in metrics and logs, never in the
// returned error shape, so an unauthenticated caller cannot use the wire
// response as an oracle for probing external hosts through Gram.
//
// Inspect is the management path. It serves an AUTHENTICATED, project-scoped
// surface where the caller is an operator configuring their own issuer, and
// the oracle concern above does not apply: they are entitled to know whether
// the URL they just typed is unreachable, serving something that is not JSON,
// or serving a document that violates the spec, because that is the whole
// point of asking. It therefore returns the full outcome taxonomy plus an
// operator-facing explanation.
//
// Inspect still does NOT leak Gram's internals. Its Detail is composed from
// the outcome, never from the raw transport error, so guardian SSRF denials,
// DNS failures, and internal hostnames stay in the logs where they belong.
// Both entry points run exactly the same fetch and validation logic and emit
// exactly the same telemetry; they differ only in how much of what was
// learned reaches the caller.
package cimd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/usersessions/cimd/admission"
)

const (
	// maxClientIDLength caps the URL-shaped client_id in application code.
	// The client_id column is unconstrained TEXT feeding a btree unique
	// index with a ~2704-byte entry limit, so oversized URLs must be
	// rejected here with a clean OAuth error rather than a Postgres index
	// failure.
	//
	// Derived from the admission package rather than declared independently:
	// admission applies the same bound earlier (before a catalog miss turns
	// the client_id into a database query parameter), and two copies of a
	// security-relevant cap would eventually drift.
	maxClientIDLength = admission.MaxClientIDLength

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

	// TokenEndpointAuthMethod must be "none" or absent — only public
	// clients are accepted. Absence is NOT a rejection: -02 does not
	// require the field, and RFC 7591's client_secret_basic default cannot
	// apply because §4.1 bans every shared-symmetric-secret method for CIMD.
	// Several real clients (OpenAI's among them) omit it.
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

// Resolver fetches, parses, and validates Client ID Metadata Documents and
// records the telemetry for each attempt. It is safe for concurrent use; one
// instance should live for the process lifetime so instruments are created
// once.
type Resolver struct {
	client  *guardian.HTTPClient
	logger  *slog.Logger
	metrics *metrics
}

// NewResolver builds the production resolver from a guardian policy: the SSRF
// dialer's post-DNS IP check satisfies -02 §8.6's ban on fetching RFC 6890
// special-use addresses and is immune to DNS rebinding. Tests that host
// documents on httptest servers should use newResolver with
// newFetchClientFrom(server.Client()) — or build the policy with
// guardian.WithTLSRootCAs — since httptest certificates are self-signed.
func NewResolver(policy *guardian.Policy, meterProvider metric.MeterProvider, logger *slog.Logger) *Resolver {
	return newResolver(newFetchClientFrom(policy.Client()), meterProvider, logger)
}

// newResolver is the injection seam for tests that need a fetch client built
// from an httptest server instead of a guardian policy.
func newResolver(client *guardian.HTTPClient, meterProvider metric.MeterProvider, logger *slog.Logger) *Resolver {
	logger = logger.With(attr.SlogComponent("cimd"))
	return &Resolver{
		client:  client,
		logger:  logger,
		metrics: newMetrics(meterProvider, logger),
	}
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
// named by clientID. The returned Document has passed every check this AS
// imposes: -02 §3 URL syntax, §4 triple client_id equality (the fetch never
// follows redirects, so the fetched URL is the presented URL by
// construction), required client_name + redirect_uris, public-client-only
// auth method, secret/private-key bans, and Gram's same-origin redirect-URI
// binding.
func (r *Resolver) Resolve(ctx context.Context, clientID string) (*Document, error) {
	// The OAuth path takes only the document and the error, discarding the
	// outcome taxonomy. That discard is the disclosure boundary described in
	// the package doc: everything Inspect would reveal is computed here too,
	// and deliberately dropped before it can reach an unauthenticated caller.
	result := r.inspect(ctx, clientID)
	if result.err != nil {
		return nil, result.err
	}
	return result.Document, nil
}

// inspect runs the full resolution and records the telemetry. It is the sole
// implementation behind both Resolve and Inspect, so the two can never drift
// in what they fetch, what they accept, or what they report to o11y.
func (r *Resolver) inspect(ctx context.Context, clientID string) inspection {
	clientIDURL, err := ValidateClientIDURL(clientID)
	if err != nil {
		// Pre-fetch rejection: no origin has been established (the URL did
		// not parse or failed syntax rules), so the origin attribute and the
		// duration histogram are both deliberately omitted.
		r.observe(ctx, resolveObservation{
			clientID:      clientID,
			origin:        "",
			result:        fetchResultValidationError,
			reason:        validationReasonOf(err),
			status:        0,
			duration:      0,
			fetched:       false,
			responseBytes: 0,
			err:           err,
		})
		return inspection{
			Document:        nil,
			outcome:         OutcomeInvalidURL,
			status:          0,
			reason:          validationReasonOf(err),
			err:             err,
			safeDescription: safeDescriptionOf(err),
		}
	}
	origin := clientIDURL.Host

	start := time.Now()
	body, status, err := r.fetchDocument(ctx, origin, clientID)
	if err != nil {
		r.observe(ctx, resolveObservation{
			clientID:      clientID,
			origin:        origin,
			result:        fetchResultFetchError,
			reason:        "",
			status:        status,
			duration:      time.Since(start),
			fetched:       true,
			responseBytes: 0,
			err:           err,
		})
		return inspection{
			Document:        nil,
			outcome:         OutcomeUnreachable,
			status:          status,
			reason:          "",
			err:             fmt.Errorf("fetch client metadata document: %w", err),
			safeDescription: "",
		}
	}

	// On the OAuth path a malformed body is reported like any other fetch
	// failure (plain wrapped error, generic wire response) rather than as a
	// distinct OAuth error: a distinguishable "reachable but not JSON"
	// response would give unauthenticated callers an oracle for probing
	// external hosts through Gram. The distinction lives in telemetry and on
	// the authenticated Inspect path only.
	var doc Document
	if err := json.Unmarshal(body, &doc); err != nil {
		r.observe(ctx, resolveObservation{
			clientID:      clientID,
			origin:        origin,
			result:        fetchResultParseError,
			reason:        "",
			status:        status,
			duration:      time.Since(start),
			fetched:       true,
			responseBytes: len(body),
			err:           err,
		})
		return inspection{
			Document:        nil,
			outcome:         OutcomeUnparseable,
			status:          status,
			reason:          "",
			err:             fmt.Errorf("parse client metadata document: %w", err),
			safeDescription: "",
		}
	}

	if err := validateDocument(&doc, clientID, clientIDURL); err != nil {
		r.observe(ctx, resolveObservation{
			clientID:      clientID,
			origin:        origin,
			result:        fetchResultValidationError,
			reason:        validationReasonOf(err),
			status:        status,
			duration:      time.Since(start),
			fetched:       true,
			responseBytes: len(body),
			err:           err,
		})
		return inspection{
			Document:        nil,
			outcome:         OutcomeInvalidDocument,
			status:          status,
			reason:          validationReasonOf(err),
			err:             err,
			safeDescription: safeDescriptionOf(err),
		}
	}

	r.observe(ctx, resolveObservation{
		clientID:      clientID,
		origin:        origin,
		result:        fetchResultSuccess,
		reason:        "",
		status:        status,
		duration:      time.Since(start),
		fetched:       true,
		responseBytes: len(body),
		err:           nil,
	})
	return inspection{
		Document:        &doc,
		outcome:         OutcomeValid,
		status:          status,
		reason:          "",
		err:             nil,
		safeDescription: "",
	}
}

// resolveObservation carries everything one Resolve attempt learned to the
// single metrics + log emission point.
type resolveObservation struct {
	clientID string
	origin   string // empty until the client_id URL has parsed
	result   fetchResult
	reason   validationReason // non-empty only for validation_error
	status   int              // HTTP status, 0 when no response was received
	duration time.Duration
	fetched  bool // whether an upstream fetch actually ran
	// responseBytes is the completed body read length, 0 when no body was
	// read; log-only (the cimd.fetch.response_size histogram is recorded
	// inside fetchDocument, where the cap-hit case is also visible).
	responseBytes int
	err           error
}

func (r *Resolver) observe(ctx context.Context, o resolveObservation) {
	r.metrics.RecordAttempt(ctx, o.origin, o.result)
	if o.fetched {
		r.metrics.RecordFetchDuration(ctx, o.origin, o.result, o.duration)
	}
	if o.result == fetchResultValidationError {
		r.metrics.RecordValidationFailure(ctx, o.reason)
	}

	// The client_id is attacker-chosen on an unauthenticated surface and, on
	// the client_id_too_long rejection specifically, has not yet been bounded
	// by the length cap — truncate so a request-sized URL cannot inflate
	// every warn line.
	clientID := o.clientID
	if len(clientID) > maxClientIDLength {
		clientID = clientID[:maxClientIDLength] + "…(truncated)"
	}
	logAttrs := []any{
		attr.SlogOAuthClientID(clientID),
		attr.SlogOutcome(string(o.result)),
	}
	if o.origin != "" {
		logAttrs = append(logAttrs, attr.SlogCIMDOrigin(o.origin))
	}
	if o.status != 0 {
		logAttrs = append(logAttrs, attr.SlogHTTPResponseStatusCode(o.status))
	}
	if o.fetched {
		logAttrs = append(logAttrs, attr.SlogHTTPClientRequestDuration(float64(o.duration)/float64(time.Millisecond)))
	}
	if o.responseBytes > 0 {
		logAttrs = append(logAttrs, attr.SlogHTTPResponseBodyBytes(o.responseBytes))
	}
	if o.reason != "" {
		logAttrs = append(logAttrs, attr.SlogCIMDValidationReason(o.reason))
	}
	if o.err != nil {
		logAttrs = append(logAttrs, attr.SlogError(o.err))
		r.logger.WarnContext(ctx, "cimd document resolution failed", logAttrs...)
		return
	}
	r.logger.InfoContext(ctx, "cimd document resolved", logAttrs...)
}

// fetchDocument retrieves the metadata document. Only HTTP 200 is accepted
// (-02 §5 MUST; other statuses — including the unfollowed redirects — are
// fetch failures and are never cached per §5.2), and the body read is capped
// at maxDocumentBytes. The returned status is 0 when no response was
// received. Response size is recorded here because the byte count only
// exists inside the read: the full body length on success, or the cap itself
// when the read tripped it.
func (r *Resolver) fetchDocument(ctx context.Context, origin string, clientID string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	// A build failure here is practically unreachable — clientID already
	// passed ValidateClientIDURL — so the fetch_error result it produces is
	// accepted despite no request having been sent.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build document request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request document: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("document endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(http.MaxBytesReader(nil, resp.Body, maxDocumentBytes))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			r.metrics.RecordResponseSize(ctx, origin, maxDocumentBytes)
			return nil, resp.StatusCode, fmt.Errorf("document exceeds %d byte limit", maxDocumentBytes)
		}
		return nil, resp.StatusCode, fmt.Errorf("read document body: %w", err)
	}
	r.metrics.RecordResponseSize(ctx, origin, int64(len(body)))

	return body, resp.StatusCode, nil
}
