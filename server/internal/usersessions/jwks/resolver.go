package jwks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-jose/go-jose/v4"
	"go.opentelemetry.io/otel/metric"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/guardian"
	"github.com/speakeasy-api/gram/server/internal/httpcache"
	"github.com/speakeasy-api/gram/server/internal/o11y"
)

// Resolver fetches, parses, and screens remote key sets and records the
// telemetry for each attempt. It owns no storage: callers pass stored
// CacheState in and persist the returned Result themselves. It is safe for
// concurrent use; one instance should live for the process lifetime so
// instruments are created once.
type Resolver struct {
	client  *guardian.HTTPClient
	logger  *slog.Logger
	metrics *metrics
}

// NewResolver builds the production resolver from a guardian policy, whose
// SSRF dialer checks the post-DNS resolved IP on every connection (immune to
// DNS rebinding) against the internal-target blocklist. Tests that host key
// sets on httptest servers should use newResolver with
// newFetchClientFrom(server.Client()), since httptest certificates are
// self-signed.
func NewResolver(policy *guardian.Policy, meterProvider metric.MeterProvider, logger *slog.Logger) *Resolver {
	return newResolver(newFetchClientFrom(policy.Client()), meterProvider, logger)
}

// newResolver is the injection seam for tests that need a fetch client built
// from an httptest server instead of a guardian policy.
func newResolver(client *guardian.HTTPClient, meterProvider metric.MeterProvider, logger *slog.Logger) *Resolver {
	logger = logger.With(attr.SlogComponent("jwks"))
	return &Resolver{
		client:  client,
		logger:  logger,
		metrics: newMetrics(meterProvider, logger),
	}
}

// newFetchClientFrom applies the key set fetch rules to a copy of the base
// client: redirects are never followed (guardian's client would otherwise
// follow up to 10 hops), so a redirect status surfaces as a non-200 fetch
// failure. No-redirect is also the simplest way to guarantee the URL the
// Source constructor validated is the only URL ever dialed. The copy keeps
// the caller's client untouched in case it is shared.
//
// The base MUST be a guardian client built WITHOUT retry options. A
// retry-configured guardian client is retryablehttp's StandardClient, whose
// inner client performs the actual exchange — including following redirects
// with its own nil CheckRedirect — before this outer copy's CheckRedirect is
// ever consulted, silently voiding the no-redirect guarantee. (The guardian
// dialer would still IP-screen each hop, but the origin binding would not
// hold.) NewResolver satisfies this by passing a plain policy.Client().
func newFetchClientFrom(base *guardian.HTTPClient) *guardian.HTTPClient {
	client := *base
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &client
}

// Resolve returns the key set state for source, fetching only when the
// passed cache state says it must.
//
// The cache policy, in order: an inline source parses its document and never
// fetches; a stored document whose expiry is still in the future — and which
// still passes the current screening rules — short-circuits with
// CacheOutcomeCached and no upstream request; otherwise a GET runs, conditional
// (If-None-Match) when the stored state has a usable validator and a
// parseable document, yielding CacheOutcomeNotModified on 304 or CacheOutcomeRefreshed
// on 200. Every failure — transport, status, parse, or screening — returns
// an error and leaves the caller's stored state untouched; serving stale on
// a failed refresh is a storage-policy decision that belongs to callers that
// need it, not to this core.
//
// Rate limiting is deliberately absent here: Resolve fetches whenever the
// state it is handed demands one. The unknown-kid refresh limit lives in
// KeyResolver, which is what unauthenticated request paths must go through.
func (r *Resolver) Resolve(ctx context.Context, source Source, cache CacheState) (*Result, error) {
	switch source.kind {
	case sourceInline:
		keys, err := parseKeySet(source.inline)
		if err != nil {
			r.observe(ctx, resolveObservation{
				uri:           "",
				origin:        "",
				result:        resultOfParseError(err),
				reason:        validationReasonOf(err),
				status:        0,
				duration:      0,
				fetched:       false,
				responseBytes: 0,
				err:           err,
			})
			return nil, fmt.Errorf("parse inline key set: %w", err)
		}
		r.observe(ctx, resolveObservation{
			uri:           "",
			origin:        "",
			result:        fetchResultInline,
			reason:        "",
			status:        0,
			duration:      0,
			fetched:       false,
			responseBytes: 0,
			err:           nil,
		})
		return &Result{
			Outcome:  CacheOutcomeInline,
			KeySet:   keys,
			Document: source.inline,
			ETag:     "",
			TTL:      0,
		}, nil
	case sourceRemote:
		return r.resolveRemote(ctx, source, cache)
	default:
		return nil, errors.New("zero Source: construct one with NewInlineSource or NewRemoteSource")
	}
}

func (r *Resolver) resolveRemote(ctx context.Context, source Source, cache CacheState) (*Result, error) {
	// The stored document is re-parsed and re-screened under the rules
	// compiled into this build before it is served or revalidated. A stored
	// document that no longer passes is treated as absent — the fetch below
	// runs unconditionally, so a screening rule tightened after a document
	// was cached takes effect immediately rather than after the TTL lapses.
	var storedKeys jose.JSONWebKeySet
	storedUsable := false
	if len(cache.Document) > 0 {
		if keys, err := parseKeySet(cache.Document); err == nil {
			storedKeys = keys
			storedUsable = true
		}
	}

	if storedUsable && !cache.ExpiresAt.IsZero() && cache.ExpiresAt.After(time.Now()) {
		r.observe(ctx, resolveObservation{
			uri:           source.uri,
			origin:        source.origin,
			result:        fetchResultCached,
			reason:        "",
			status:        0,
			duration:      0,
			fetched:       false,
			responseBytes: 0,
			err:           nil,
		})
		return &Result{
			Outcome: CacheOutcomeCached,
			KeySet:  storedKeys,
			// Sanitized even though the caller persists nothing on a cache
			// hit: every etag this package hands back must be safe to store.
			Document: cache.Document,
			ETag:     httpcache.SanitizeETag(cache.ETag, maxETagLength),
			TTL:      0,
		}, nil
	}

	// A conditional request is only worth making when a 304 answer would be
	// honoured, which requires a stored document that still screens clean.
	etag := ""
	if storedUsable {
		etag = httpcache.SanitizeETag(cache.ETag, maxETagLength)
	}

	start := time.Now()
	fetched, err := r.fetchKeySet(ctx, source, etag)
	if err != nil {
		r.observe(ctx, resolveObservation{
			uri:           source.uri,
			origin:        source.origin,
			result:        fetchResultFetchError,
			reason:        "",
			status:        fetched.status,
			duration:      time.Since(start),
			fetched:       true,
			responseBytes: 0,
			err:           err,
		})
		return nil, fmt.Errorf("fetch key set: %w", err)
	}

	if fetched.notModified {
		r.observe(ctx, resolveObservation{
			uri:           source.uri,
			origin:        source.origin,
			result:        fetchResultConditionalNotModified,
			reason:        "",
			status:        fetched.status,
			duration:      time.Since(start),
			fetched:       true,
			responseBytes: 0,
			err:           nil,
		})
		// RFC 9110 §15.4.5 lets a 304 carry a new validator; one that offers
		// none — or an unusable one — leaves the stored validator in place,
		// since it still identifies content the upstream just confirmed.
		refreshedETag := etag
		if refreshed := httpcache.SanitizeETag(fetched.header.Get("ETag"), maxETagLength); refreshed != "" {
			refreshedETag = refreshed
		}
		return &Result{
			Outcome:  CacheOutcomeNotModified,
			KeySet:   storedKeys,
			Document: cache.Document,
			ETag:     refreshedETag,
			// The new lifetime comes from the 304's own headers. This is a
			// deliberate RFC 9111 §4.3.4 deviation (the RFC keeps the stored
			// response's values for headers the 304 omits): only the absolute
			// expiry is stored, so the original grant is not recoverable, and
			// a bare 304 therefore resets freshness to the default rather
			// than the origin's earlier grant. Both drift directions are
			// bounded by the [minCacheTTL, maxCacheTTL] clamp.
			TTL: r.cacheTTL(fetched.header),
		}, nil
	}

	keys, err := parseKeySet(fetched.body)
	if err != nil {
		r.observe(ctx, resolveObservation{
			uri:           source.uri,
			origin:        source.origin,
			result:        resultOfParseError(err),
			reason:        validationReasonOf(err),
			status:        fetched.status,
			duration:      time.Since(start),
			fetched:       true,
			responseBytes: len(fetched.body),
			err:           err,
		})
		return nil, fmt.Errorf("parse fetched key set: %w", err)
	}

	r.observe(ctx, resolveObservation{
		uri:           source.uri,
		origin:        source.origin,
		result:        fetchResultSuccess,
		reason:        "",
		status:        fetched.status,
		duration:      time.Since(start),
		fetched:       true,
		responseBytes: len(fetched.body),
		err:           nil,
	})
	return &Result{
		Outcome:  CacheOutcomeRefreshed,
		KeySet:   keys,
		Document: fetched.body,
		ETag:     httpcache.SanitizeETag(fetched.header.Get("ETag"), maxETagLength),
		TTL:      r.cacheTTL(fetched.header),
	}, nil
}

func (r *Resolver) cacheTTL(header http.Header) time.Duration {
	policy := httpcache.FreshnessPolicy{
		Default: defaultCacheTTL,
		Min:     minCacheTTL,
		Max:     maxCacheTTL,
	}
	return policy.TTL(header, time.Now())
}

// fetchedKeySet is one HTTP exchange with a key set host.
type fetchedKeySet struct {
	// body is the key set bytes, empty unless the response was a 200.
	body []byte

	// status is the HTTP status, 0 when no response was received.
	status int

	// notModified reports a 304 answer to a conditional request, meaning
	// body is empty and the caller's stored document still stands.
	notModified bool

	// header is the response header, read for the freshness directives and
	// the ETag. Nil when no response was received.
	header http.Header
}

// fetchKeySet retrieves the key set document, conditionally when etag is
// non-empty. HTTP 200 is the only status that yields a body — other
// statuses, including the unfollowed redirects, are fetch failures — and the
// body read is capped at maxKeySetBytes.
//
// A 304 is accepted only in answer to a conditional request. An
// unconditional GET that draws one is a broken or intermediary-fronted host
// rather than a revalidation, and treating it as a cache confirmation would
// mean confirming a document this process has never seen.
func (r *Resolver) fetchKeySet(ctx context.Context, source Source, etag string) (fetchedKeySet, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.uri, nil)
	if err != nil {
		return fetchedKeySet{body: nil, status: 0, notModified: false, header: nil}, fmt.Errorf("build key set request: %w", err)
	}
	// RFC 7517 §8.5.1 registers application/jwk-set+json; plain
	// application/json is what most providers actually serve.
	req.Header.Set("Accept", "application/jwk-set+json, application/json")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return fetchedKeySet{body: nil, status: 0, notModified: false, header: nil}, fmt.Errorf("request key set: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return resp.Body.Close() })

	if etag != "" && resp.StatusCode == http.StatusNotModified {
		return fetchedKeySet{body: nil, status: resp.StatusCode, notModified: true, header: resp.Header}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return fetchedKeySet{body: nil, status: resp.StatusCode, notModified: false, header: resp.Header}, fmt.Errorf("key set endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(http.MaxBytesReader(nil, resp.Body, maxKeySetBytes))
	if err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			r.metrics.RecordResponseSize(ctx, source.origin, maxKeySetBytes)
			return fetchedKeySet{body: nil, status: resp.StatusCode, notModified: false, header: resp.Header}, fmt.Errorf("key set exceeds %d byte limit: %w", maxKeySetBytes, ErrKeySetTooLarge)
		}
		return fetchedKeySet{body: nil, status: resp.StatusCode, notModified: false, header: resp.Header}, fmt.Errorf("read key set body: %w", err)
	}
	r.metrics.RecordResponseSize(ctx, source.origin, int64(len(body)))

	return fetchedKeySet{body: body, status: resp.StatusCode, notModified: false, header: resp.Header}, nil
}

// resolveObservation carries everything one Resolve attempt learned to the
// single metrics + log emission point.
type resolveObservation struct {
	uri    string // empty for inline sources
	origin string // empty for inline sources
	result fetchResult
	reason validationReason // non-empty only for validation_error
	status int              // HTTP status, 0 when no response was received
	// duration covers the whole fetch-and-screen span, matching what the
	// cimd duration histogram measures.
	duration time.Duration
	fetched  bool // whether an upstream fetch actually ran
	// responseBytes is the completed body read length; log-only (the
	// jwks.fetch.response_size histogram is recorded inside fetchKeySet,
	// where the cap-hit case is also visible).
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

	logAttrs := []any{attr.SlogOutcome(string(o.result))}
	if o.uri != "" {
		logAttrs = append(logAttrs, attr.SlogURLFull(o.uri))
	}
	if o.origin != "" {
		logAttrs = append(logAttrs, attr.SlogJWKSOrigin(o.origin))
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
		logAttrs = append(logAttrs, attr.SlogJWKSValidationReason(o.reason))
	}
	if o.err != nil {
		logAttrs = append(logAttrs, attr.SlogError(o.err))
		r.logger.WarnContext(ctx, "jwks key set resolution failed", logAttrs...)
		return
	}
	r.logger.InfoContext(ctx, "jwks key set resolved", logAttrs...)
}
