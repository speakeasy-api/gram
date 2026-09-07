// Package cimd implements the inbound side of OAuth Client ID Metadata
// Documents (draft-ietf-oauth-client-id-metadata-document-02): resolving a
// URL-shaped client_id presented to the user-session authorization server by
// fetching the document it names, parsing it, and validating it against the
// spec's rules plus this server's own policy rules.
//
// The Resolver owns the fetch lifecycle, its cache policy, and its telemetry
// (metrics + logs) but deliberately not persistence: the caller passes in the
// cache state it has stored and applies the returned outcome to its own
// tables. Callers own the upsert of the resolved client and the mapping of
// returned errors onto their wire format.
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
	"github.com/speakeasy-api/gram/server/internal/usersessions/oauthwire"
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

// ErrDocumentTooLarge marks the one fetch failure that arrives with a
// successful HTTP status: the response began, but the body ran past
// maxDocumentBytes. It exists so Inspect can tell an operator their document
// is oversized instead of guessing at a 200 that failed to read.
//
// It is wrapped behind the existing descriptive prefix rather than replacing
// it, so the "document exceeds N byte limit" text every log and the opaque
// OAuth error already carried is preserved; the sentinel is appended to it.
var ErrDocumentTooLarge = errors.New("document exceeds size limit")

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

	// JWKS is the client's inline public key set, the keys a private_key_jwt
	// client signs its assertions with. Screened for private or symmetric
	// material (-02 §4.1 bans it) whatever method the document declares,
	// and required, in exactly one of this and JWKSURI, when the method is
	// private_key_jwt.
	JWKS json.RawMessage `json:"jwks"`

	// JWKSURI is the https location of the client's public key set, the
	// remote alternative to JWKS. RFC 7591 §2 forbids supplying both.
	JWKSURI string `json:"jwks_uri"`

	// LogoURI is the client's logo URL. Optional and deliberately not
	// rendered: like ClientName it is attacker-controllable, and an image
	// is a stronger spoofing primitive than text.
	LogoURI string `json:"logo_uri"`

	// RedirectURIs is the registered redirect set. Required; each entry
	// must be an https URL or an RFC 8252 loopback http URL. Entries on a
	// different origin than the client_id URL are accepted but observed
	// through the cimd.redirect_uris.cross_origin counter.
	RedirectURIs []string `json:"redirect_uris"`

	// ResponseTypes is parsed but not enforced, for the same reason as
	// GrantTypes — the AS only supports response type "code".
	ResponseTypes []string `json:"response_types"`

	// TokenEndpointAuthMethod is "none", "private_key_jwt", or absent.
	// Absence is NOT a rejection: -02 does not require the field, and RFC
	// 7591's client_secret_basic default cannot apply because §4.1 bans
	// every shared-symmetric-secret method for CIMD. Several real clients
	// (OpenAI's among them) omit it.
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method"`
}

// DeclaredAuthMethod is the client authentication method this document
// commits its client to, with an absent member resolved to "none".
//
// Resolving absence here rather than at each call site is what keeps the
// persisted method honest: a NULL in user_session_clients means "this row
// predates the column", a distinct claim from "this document declined to
// name a method", and every document accepted by validateDocument has made
// the latter claim. Callers persisting a freshly read document should store
// this value, never the raw member.
func (d *Document) DeclaredAuthMethod() string {
	if d.TokenEndpointAuthMethod == "" {
		return oauthwire.AuthMethodNone
	}
	return d.TokenEndpointAuthMethod
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

// Resolve returns the Client ID Metadata Document state for clientID,
// fetching it only when the caller's cache says it must.
//
// The cache policy, in order: a cache whose expiry is still in the future
// short-circuits with CacheOutcomeCached and no upstream request; otherwise a
// conditional GET runs, carrying If-None-Match when cache.ETag is set, and
// yields CacheOutcomeNotModified on 304 or CacheOutcomeRefreshed on 200.
// Every failure — transport, status, parse, or validation — returns an error
// and leaves the caller's cached row untouched. Serving a stale document when
// a refresh fails is deliberately not offered: -02 §5.1 says a fetch failure
// SHOULD abort the authorization request.
//
// A document returned with CacheOutcomeRefreshed has passed every check this
// AS imposes: -02 §3 URL syntax, §4 triple client_id equality (the fetch never
// follows redirects, so the fetched URL is the presented URL by
// construction), required client_name + redirect_uris, public-client-only
// auth method, secret/private-key bans, and the https-or-loopback
// redirect-URI scheme rule. Those checks run against the document as
// fetched, so a cached row carries the verdict of the validation code that
// was deployed when it was last refreshed: tightening a rule in validate.go
// does not re-reject a cached client until its TTL lapses, up to maxCacheTTL
// later. Callers that
// need a rule applied sooner must purge their stored cache state, which
// forces the next resolve to fetch and re-validate a full document.
func (r *Resolver) Resolve(ctx context.Context, clientID string, cache CacheState) (*CacheResult, error) {
	// The OAuth path takes only the cache effect and the error, discarding
	// the outcome taxonomy. That discard is the disclosure boundary described
	// in the package doc: everything Inspect would reveal is computed here
	// too, and deliberately dropped before it can reach an unauthenticated
	// caller.
	result := r.inspect(ctx, clientID, cache)
	if result.err != nil {
		return nil, result.err
	}
	return &CacheResult{
		Outcome:  result.cacheOutcome,
		Document: result.Document,
		ETag:     result.etag,
		TTL:      result.ttl,
	}, nil
}

// inspect runs the full resolution and records the telemetry. It is the sole
// implementation behind both Resolve and Inspect, so the two can never drift
// in what they fetch, what they accept, or what they report to o11y.
//
// cache is always the zero value on the Inspect path: an operator asking what
// a URL serves right now must not be answered from a copy the authorize path
// stored earlier. The two short-circuit outcomes below are therefore
// unreachable from Inspect, which is what lets Inspection keep its invariant
// that a valid outcome carries a document.
func (r *Resolver) inspect(ctx context.Context, clientID string, cache CacheState) inspection {
	clientIDURL, err := ValidateClientIDURL(clientID)
	if err != nil {
		// Pre-fetch rejection: no origin has been established (the URL did
		// not parse or failed syntax rules), so the origin attribute and the
		// duration histogram are both deliberately omitted.
		r.observe(ctx, resolveObservation{
			clientID:                   clientID,
			origin:                     "",
			result:                     fetchResultValidationError,
			reason:                     validationReasonOf(err),
			status:                     0,
			duration:                   0,
			fetched:                    false,
			responseBytes:              0,
			crossOriginRedirectOrigins: nil,
			err:                        err,
		})
		return inspection{
			Document:        nil,
			outcome:         OutcomeInvalidURL,
			status:          0,
			reason:          validationReasonOf(err),
			err:             err,
			safeDescription: safeDescriptionOf(err),
			tooLarge:        false,
			cacheOutcome:    CacheOutcomeRefreshed,
			etag:            "",
			ttl:             0,
		}
	}
	origin := clientIDURL.Host

	// Freshness is consulted only after the URL itself has passed §3, so a
	// cached row can never keep a client_id alive that current syntax rules
	// reject.
	if !cache.ExpiresAt.IsZero() && cache.ExpiresAt.After(time.Now()) {
		r.observe(ctx, resolveObservation{
			clientID:                   clientID,
			origin:                     origin,
			result:                     fetchResultCached,
			reason:                     "",
			status:                     0,
			duration:                   0,
			fetched:                    false,
			responseBytes:              0,
			crossOriginRedirectOrigins: nil,
			err:                        nil,
		})
		return inspection{
			Document:        nil,
			outcome:         OutcomeValid,
			status:          0,
			reason:          "",
			err:             nil,
			safeDescription: "",
			tooLarge:        false,
			cacheOutcome:    CacheOutcomeCached,
			// Sanitized even though the caller persists nothing on a cache
			// hit: every etag this package hands back must be safe to
			// store, or a future caller trusting the field would launder a
			// malformed stored validator back into the database.
			etag: sanitizeETag(cache.ETag),
			ttl:  0,
		}
	}

	start := time.Now()
	fetched, err := r.fetchDocument(ctx, origin, clientID, cache.ETag)
	if err != nil {
		r.observe(ctx, resolveObservation{
			clientID:                   clientID,
			origin:                     origin,
			result:                     fetchResultFetchError,
			reason:                     "",
			status:                     fetched.status,
			duration:                   time.Since(start),
			fetched:                    true,
			responseBytes:              0,
			crossOriginRedirectOrigins: nil,
			err:                        err,
		})
		return inspection{
			Document:        nil,
			outcome:         OutcomeUnreachable,
			status:          fetched.status,
			reason:          "",
			err:             fmt.Errorf("fetch client metadata document: %w", err),
			safeDescription: "",
			tooLarge:        errors.Is(err, ErrDocumentTooLarge),
			cacheOutcome:    CacheOutcomeRefreshed,
			etag:            "",
			ttl:             0,
		}
	}

	if fetched.notModified {
		r.observe(ctx, resolveObservation{
			clientID:                   clientID,
			origin:                     origin,
			result:                     fetchResultConditionalNotModified,
			reason:                     "",
			status:                     fetched.status,
			duration:                   time.Since(start),
			fetched:                    true,
			responseBytes:              0,
			crossOriginRedirectOrigins: nil,
			err:                        nil,
		})
		// RFC 9110 §15.4.5 lets a 304 carry a new validator, and a cache
		// that ignores one revalidates against a superseded ETag forever.
		// A response that offers none — or an unusable one — leaves the
		// stored validator in place: it still identifies content the
		// upstream just confirmed is unchanged.
		//
		// The stored value is re-sanitized so what gets persisted is what
		// went on the wire. Reaching here at all means it survived
		// sanitizing (an empty result suppresses the conditional request and
		// with it this branch), so this only ever normalizes surrounding
		// whitespace, but persisting a value the request did not use would
		// be a quiet inconsistency.
		etag := sanitizeETag(cache.ETag)
		if refreshed := sanitizeETag(fetched.header.Get("ETag")); refreshed != "" {
			etag = refreshed
		}
		return inspection{
			Document:        nil,
			outcome:         OutcomeValid,
			status:          fetched.status,
			reason:          "",
			err:             nil,
			safeDescription: "",
			tooLarge:        false,
			cacheOutcome:    CacheOutcomeNotModified,
			etag:            etag,
			ttl:             cacheTTL(fetched.header, time.Now()),
		}
	}
	body, status := fetched.body, fetched.status

	// On the OAuth path a malformed body is reported like any other fetch
	// failure (plain wrapped error, generic wire response) rather than as a
	// distinct OAuth error: a distinguishable "reachable but not JSON"
	// response would give unauthenticated callers an oracle for probing
	// external hosts through Gram. The distinction lives in telemetry and on
	// the authenticated Inspect path only.
	var doc Document
	if err := json.Unmarshal(body, &doc); err != nil {
		r.observe(ctx, resolveObservation{
			clientID:                   clientID,
			origin:                     origin,
			result:                     fetchResultParseError,
			reason:                     "",
			status:                     status,
			duration:                   time.Since(start),
			fetched:                    true,
			responseBytes:              len(body),
			crossOriginRedirectOrigins: nil,
			err:                        err,
		})
		return inspection{
			Document:        nil,
			outcome:         OutcomeUnparseable,
			status:          status,
			reason:          "",
			err:             fmt.Errorf("parse client metadata document: %w", err),
			safeDescription: "",
			tooLarge:        false,
			cacheOutcome:    CacheOutcomeRefreshed,
			etag:            "",
			ttl:             0,
		}
	}

	findings, err := validateDocument(&doc, clientID, clientIDURL)
	if err != nil {
		r.observe(ctx, resolveObservation{
			clientID:                   clientID,
			origin:                     origin,
			result:                     fetchResultValidationError,
			reason:                     validationReasonOf(err),
			status:                     status,
			duration:                   time.Since(start),
			fetched:                    true,
			responseBytes:              len(body),
			crossOriginRedirectOrigins: nil,
			err:                        err,
		})
		return inspection{
			Document:        nil,
			outcome:         OutcomeInvalidDocument,
			status:          status,
			reason:          validationReasonOf(err),
			err:             err,
			safeDescription: safeDescriptionOf(err),
			tooLarge:        false,
			cacheOutcome:    CacheOutcomeRefreshed,
			etag:            "",
			ttl:             0,
		}
	}

	r.observe(ctx, resolveObservation{
		clientID:                   clientID,
		origin:                     origin,
		result:                     fetchResultSuccess,
		reason:                     "",
		status:                     status,
		duration:                   time.Since(start),
		fetched:                    true,
		responseBytes:              len(body),
		crossOriginRedirectOrigins: findings.crossOriginRedirectOrigins,
		err:                        nil,
	})
	return inspection{
		Document:        &doc,
		outcome:         OutcomeValid,
		status:          status,
		reason:          "",
		err:             nil,
		safeDescription: "",
		tooLarge:        false,
		cacheOutcome:    CacheOutcomeRefreshed,
		etag:            sanitizeETag(fetched.header.Get("ETag")),
		ttl:             cacheTTL(fetched.header, time.Now()),
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

	// crossOriginRedirectOrigins is the validator's cross-origin finding for
	// a document that passed validation; nil on every other result. The
	// origins go to the log line only — the counter they trigger carries
	// just the bounded client_id origin.
	crossOriginRedirectOrigins []string

	err error
}

func (r *Resolver) observe(ctx context.Context, o resolveObservation) {
	r.metrics.RecordAttempt(ctx, o.origin, o.result)
	if o.fetched {
		r.metrics.RecordFetchDuration(ctx, o.origin, o.result, o.duration)
	}
	if o.result == fetchResultValidationError {
		r.metrics.RecordValidationFailure(ctx, o.reason)
	}
	if len(o.crossOriginRedirectOrigins) > 0 {
		r.metrics.RecordCrossOriginRedirects(ctx, o.origin)
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
	if len(o.crossOriginRedirectOrigins) > 0 {
		logAttrs = append(logAttrs, attr.SlogCIMDCrossOriginRedirectOrigins(o.crossOriginRedirectOrigins))
	}
	if o.err != nil {
		logAttrs = append(logAttrs, attr.SlogError(o.err))
		r.logger.WarnContext(ctx, "cimd document resolution failed", logAttrs...)
		return
	}
	r.logger.InfoContext(ctx, "cimd document resolved", logAttrs...)
}

// fetchedDocument is one HTTP exchange with a document host.
type fetchedDocument struct {
	// body is the document bytes, empty unless the response was a 200.
	body []byte

	// status is the HTTP status, 0 when no response was received.
	status int

	// notModified reports a 304 answer to a conditional request, meaning
	// body is empty and the caller's cached document still stands.
	notModified bool

	// header is the response header, read for the freshness directives and
	// the ETag. Nil when no response was received.
	header http.Header
}

// fetchDocument retrieves the metadata document, conditionally when etag is
// non-empty. HTTP 200 is the only status that yields a body (-02 §5 MUST;
// other statuses — including the unfollowed redirects — are fetch failures
// and are never cached per §5.2), and the body read is capped at
// maxDocumentBytes.
//
// A 304 is accepted only in answer to a conditional request. An
// unconditional GET that draws one is a broken or intermediary-fronted host
// rather than a revalidation, and treating it as a cache confirmation would
// mean confirming a document this AS has never seen, so it falls through to
// the same fetch failure as any other unexpected status.
//
// Response size is recorded here because the byte count only exists inside
// the read: the full body length on success, or the cap itself when the read
// tripped it. A 304 records none, having read no body.
func (r *Resolver) fetchDocument(ctx context.Context, origin string, clientID string, etag string) (fetchedDocument, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	// A build failure here is practically unreachable — clientID already
	// passed ValidateClientIDURL — so the fetch_error result it produces is
	// accepted despite no request having been sent.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return fetchedDocument{body: nil, status: 0, notModified: false, header: nil}, fmt.Errorf("build document request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	// Re-sanitize rather than trusting the stored value. Everything written
	// to client_id_metadata_etag passes sanitizeETag today, but that is a
	// caller-side invariant across a process and a database: a row written
	// under laxer rules than the ones now compiled in, or by hand during an
	// incident, would otherwise be replayed forever. Dropping a validator
	// here also correctly suppresses the 304 branch below, so a malformed
	// stored tag cannot make this AS accept a revalidation it never asked
	// for.
	etag = sanitizeETag(etag)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return fetchedDocument{body: nil, status: 0, notModified: false, header: nil}, fmt.Errorf("request document: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if etag != "" && resp.StatusCode == http.StatusNotModified {
		return fetchedDocument{body: nil, status: resp.StatusCode, notModified: true, header: resp.Header}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return fetchedDocument{body: nil, status: resp.StatusCode, notModified: false, header: resp.Header}, fmt.Errorf("document endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(http.MaxBytesReader(nil, resp.Body, maxDocumentBytes))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			r.metrics.RecordResponseSize(ctx, origin, maxDocumentBytes)
			return fetchedDocument{body: nil, status: resp.StatusCode, notModified: false, header: resp.Header}, fmt.Errorf("document exceeds %d byte limit: %w", maxDocumentBytes, ErrDocumentTooLarge)
		}
		return fetchedDocument{body: nil, status: resp.StatusCode, notModified: false, header: resp.Header}, fmt.Errorf("read document body: %w", err)
	}
	r.metrics.RecordResponseSize(ctx, origin, int64(len(body)))

	return fetchedDocument{body: body, status: resp.StatusCode, notModified: false, header: resp.Header}, nil
}
