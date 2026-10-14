// Package internalsessionissuers runs the issuers Speakeasy operates for its
// own signed-in users: an organization's enterprise-managed authorization
// issuer, its discovery documents, its signer and its management API.
package internalsessionissuers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	goahttp "goa.design/goa/v3/http"
	"golang.org/x/sync/singleflight"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/customdomains"
	"github.com/speakeasy-api/gram/server/internal/httpcache"
	"github.com/speakeasy-api/gram/server/internal/jsonwebkeysets/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/urls"
)

// CacheTTL bounds how long a verifier may cache the issuer's documents.
const CacheTTL = 60 * time.Second

// ActivationDelay is how long a published key waits before it signs. Two
// cache windows cover a shared cache in front of a verifier's own cache.
const ActivationDelay = 2 * CacheTTL

// RetiredKeyOverlap keeps a retired key published until every assertion it
// signed has expired and every verifier cache has refreshed.
const RetiredKeyOverlap = 10 * time.Minute

// MaxAssertionLifetime caps an assertion's exp so RetiredKeyOverlap holds.
const MaxAssertionLifetime = RetiredKeyOverlap - ActivationDelay

// notFoundMaxAge briefly caches a missing issuer so probes stay cheap.
const notFoundMaxAge = 30

// Public documents are cached per process, found or not, so unauthenticated
// traffic reaches the database at most once per document per window. The
// window stays well under CacheTTL so revocations reach verifiers on time.
const (
	documentCacheTTL  = 10 * time.Second
	documentCacheSize = 4096
	// Misses get their own smaller cache, so probes for many ids can't
	// evict the documents of real issuers.
	missingCacheSize = 1024
	// At most this many distinct documents are read at once per process;
	// further misses are refused rather than queued on the database pool.
	documentFillLimit = 16
	// A read gets this long whatever its callers do.
	documentFillTimeout = 2 * time.Second
	// A cached JWKS older than this is checked against a fingerprint of the
	// issuer's key rows before it is served, so a replacement or revocation
	// reaches every process within about a second while the full read still
	// happens once per documentCacheTTL. The check shares the fill limit;
	// when it is busy or fails, the cached document is served until its TTL.
	keysRevalidateAfter = time.Second
)

// organizationIDPattern bounds a path organization id before it reaches a
// cache or the database.
var organizationIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// errDiscoveryBusy refuses a miss while every fill slot is taken.
var errDiscoveryBusy = errors.New("internalsessionissuers: discovery is busy")

var (
	// ErrNotFound is returned when an organization has no live issuer.
	ErrNotFound = errors.New("internalsessionissuers: organization has no issuer")
	// ErrUnknownKid means a rotation named a kid never published by the issuer.
	ErrUnknownKid = errors.New("internalsessionissuers: kid was never published by this issuer")
	// ErrRotationConflict means a concurrent change moved the active key.
	ErrRotationConflict = errors.New("internalsessionissuers: the active key changed during the request")
	// ErrMisconfigured means the platform signing setup is unusable; retrying won't help.
	ErrMisconfigured = errors.New("internalsessionissuers: platform signing is misconfigured")
)

// SweepProgress is one bounded pass of the system key sweep.
type SweepProgress struct {
	// Swept counts organizations visited.
	Swept int
	// Activated counts ready pending keys the sweep activated.
	Activated int
	// Pending is the fleet-wide count of revoked keys awaiting KMS cleanup,
	// or -1 when unknown.
	Pending int
	// Next is the organization id to resume after when Done is false.
	Next string
	// Done means every organization with work was visited.
	Done bool
}

// RotationPendingError means a new key is published and can be activated
// from ReadyAt, once verifier caches have seen it.
type RotationPendingError struct{ ReadyAt time.Time }

func (e *RotationPendingError) Error() string {
	return "internalsessionissuers: rotation pending until " + e.ReadyAt.Format(time.RFC3339Nano)
}

// URL is the organization's permanent issuer identifier. It never names a key,
// so key rotation and replacement never change what vendors trust. It must be
// HTTPS; allowLoopback admits plain-HTTP loopback for local development only.
func URL(origin *url.URL, organizationID string, allowLoopback bool) (*url.URL, error) {
	// Discovery is mounted at the root, so an origin with a path can't serve it.
	valid := origin != nil && (origin.Path == "" || origin.Path == "/") &&
		(urls.IsAbsoluteHTTPS(origin.String()) || (allowLoopback && urls.IsAbsoluteHTTPSOrLoopback(origin.String())))
	if !valid || organizationID == "" {
		return nil, errors.New("internalsessionissuers: issuer origin is unavailable")
	}
	return origin.JoinPath("oauth", "idp", "org", organizationID), nil
}

// JWKSURI is where the stored issuer publishes its verification keys.
func JWKSURI(issuer string) (string, error) {
	u, err := url.Parse(issuer)
	if err != nil {
		return "", fmt.Errorf("parse stored issuer: %w", err)
	}
	return u.JoinPath("jwks").String(), nil
}

// discoveryDocument advertises only the trust anchor: issuance is internal.
type discoveryDocument struct {
	Issuer  string `json:"issuer"`
	JWKSURI string `json:"jwks_uri"`
}

// AttachDiscovery mounts the public discovery and JWKS documents.
func AttachDiscovery(mux goahttp.Muxer, db *pgxpool.Pool, logger *slog.Logger) {
	h := &handler{
		db:      db,
		logger:  logger,
		cache:   expirable.NewLRU[documentKey, cachedDocument](documentCacheSize, nil, documentCacheTTL),
		missing: expirable.NewLRU[documentKey, struct{}](missingCacheSize, nil, documentCacheTTL),
		flights: singleflight.Group{},
		fills:   make(chan struct{}, documentFillLimit),
		now:     time.Now,
	}
	const path = "/oauth/idp/org/{organizationID}"
	for _, route := range []string{
		path,
		path + "/.well-known/openid-configuration",
		"/.well-known/openid-configuration" + path,
		"/.well-known/oauth-authorization-server" + path,
	} {
		o11y.AttachHandler(mux, http.MethodGet, route, oops.ErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
			return h.serve(w, r, false)
		}).ServeHTTP)
	}
	o11y.AttachHandler(mux, http.MethodGet, path+"/jwks", oops.ErrHandle(logger, func(w http.ResponseWriter, r *http.Request) error {
		return h.serve(w, r, true)
	}).ServeHTTP)
}

type handler struct {
	db     *pgxpool.Pool
	logger *slog.Logger
	cache  *expirable.LRU[documentKey, cachedDocument]
	// missing caches ids with no issuer.
	missing *expirable.LRU[documentKey, struct{}]
	// flights collapses concurrent misses for one document into one read.
	flights singleflight.Group
	// fills bounds concurrent reads across documents.
	fills chan struct{}
	now   func() time.Time
}

type documentKey struct {
	organizationID string
	keys           bool
}

// cachedDocument is a rendered document; a nil body means no issuer and is
// cached in missing instead. A JWKS also records the key fingerprint read
// before its keys and when that fingerprint was last confirmed.
type cachedDocument struct {
	body       []byte
	renderedAt time.Time
	version    keysVersion
	checkedAt  *atomic.Int64
}

// missingDocument stands for an organization with no issuer.
var missingDocument = cachedDocument{body: nil, renderedAt: time.Time{}, version: keysVersion{JsonWebKeySetID: uuid.Nil, KeysDigest: ""}, checkedAt: nil}

// keysVersion fingerprints an issuer's key rows; any key write changes it.
type keysVersion = repo.GetOrganizationEMAIssuerKeysVersionRow

func (h *handler) serve(w http.ResponseWriter, r *http.Request, keys bool) error {
	ctx := r.Context()
	// The issuer lives only on the platform origin, never a custom domain.
	if customdomains.FromContext(ctx) != nil {
		return oops.E(oops.CodeNotFound, nil, "issuer not found")
	}
	key := documentKey{organizationID: chi.URLParam(r, "organizationID"), keys: keys}
	if !organizationIDPattern.MatchString(key.organizationID) {
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", notFoundMaxAge))
		return oops.E(oops.CodeNotFound, nil, "issuer not found")
	}
	cached, ok := h.cache.Get(key)
	if ok && key.keys && !h.keysCurrent(ctx, key, cached) {
		h.cache.Remove(key)
		ok = false
	}
	if !ok {
		if _, missing := h.missing.Get(key); missing {
			cached, ok = missingDocument, true
		}
	}
	if !ok {
		flight := fmt.Sprintf("%t/%s", key.keys, key.organizationID)
		loaded, err, _ := h.flights.Do(flight, func() (any, error) {
			if cached, ok := h.cache.Get(key); ok {
				return cached, nil
			}
			if _, missing := h.missing.Get(key); missing {
				return missingDocument, nil
			}
			select {
			case h.fills <- struct{}{}:
				defer func() { <-h.fills }()
			default:
				return nil, errDiscoveryBusy
			}
			// The flight outlives any one caller, so it ignores their
			// cancellation but keeps its own deadline.
			fillCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), documentFillTimeout)
			defer cancel()
			document, err := h.render(fillCtx, key)
			switch {
			case errors.Is(err, ErrNotFound):
			case err != nil:
				return nil, err
			}
			if document.body == nil {
				h.missing.Add(key, struct{}{})
			} else {
				h.cache.Add(key, document)
			}
			return document, nil
		})
		switch {
		case err != nil && !errors.Is(err, errDiscoveryBusy) && (errors.Is(err, context.DeadlineExceeded) || transientDatabaseError(err)):
			h.logger.WarnContext(ctx, "issuer discovery database read failed transiently", attr.SlogError(err))
			w.Header().Set("Retry-After", "1")
			w.Header().Set("Cache-Control", "no-store")
			return oops.E(oops.CodeUnavailable, err, "issuer discovery is busy, try again shortly")
		case errors.Is(err, errDiscoveryBusy):
			w.Header().Set("Retry-After", "1")
			w.Header().Set("Cache-Control", "no-store")
			return oops.E(oops.CodeUnavailable, err, "issuer discovery is busy, try again shortly")
		case err != nil:
			return oops.E(oops.CodeUnexpected, err, "load issuer document").LogError(ctx, h.logger)
		}
		cached, ok = loaded.(cachedDocument)
		if !ok {
			return oops.E(oops.CodeUnexpected, nil, "load issuer document").LogError(ctx, h.logger)
		}
	}
	if cached.body == nil {
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", notFoundMaxAge))
		return oops.E(oops.CodeNotFound, nil, "issuer not found")
	}
	contentType := "application/json"
	if keys {
		contentType = "application/jwk-set+json"
	}
	// Time spent in this process's cache counts against the verifier's
	// window, so a revoked key is never cached longer than CacheTTL in total.
	maxAge := int((CacheTTL - time.Since(cached.renderedAt)) / time.Second)
	return httpcache.WriteCacheableJSON(ctx, w, r, h.logger, contentType, max(maxAge, 0), cached.body)
}

// transientDatabaseError is a failure a retry can clear: a lost or refused
// connection, or a server shutting down or starting up.
func transientDatabaseError(err error) bool {
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pgerrcode.IsConnectionException(pgErr.Code) || pgErr.Code == pgerrcode.AdminShutdown || pgErr.Code == pgerrcode.CrashShutdown || pgErr.Code == pgerrcode.CannotConnectNow || pgErr.Code == pgerrcode.TooManyConnections
	}
	if _, ok := errors.AsType[net.Error](err); ok {
		return true
	}
	return pgconn.SafeToRetry(err)
}

// keysCurrent reports whether a cached JWKS may be served: it was confirmed
// within keysRevalidateAfter, or its key fingerprint still matches. A busy
// process or a failed check keeps serving it; documentCacheTTL still bounds
// it.
func (h *handler) keysCurrent(ctx context.Context, key documentKey, cached cachedDocument) bool {
	now := h.now()
	if now.Sub(time.Unix(0, cached.checkedAt.Load())) < keysRevalidateAfter {
		return true
	}
	loaded, err, _ := h.flights.Do("version/"+key.organizationID, func() (any, error) {
		select {
		case h.fills <- struct{}{}:
			defer func() { <-h.fills }()
		default:
			return nil, errDiscoveryBusy
		}
		checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), documentFillTimeout)
		defer cancel()
		version, err := repo.New(h.db).GetOrganizationEMAIssuerKeysVersion(checkCtx, key.organizationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("load issuer key version: %w", err)
		}
		return version, nil
	})
	switch {
	case errors.Is(err, ErrNotFound):
		return false
	case errors.Is(err, errDiscoveryBusy):
		return true
	case err != nil:
		h.logger.WarnContext(ctx, "issuer key version check failed; serving the cached key set", attr.SlogError(err))
		cached.checkedAt.Store(now.UnixNano())
		return true
	}
	version, ok := loaded.(keysVersion)
	if !ok || version != cached.version {
		return false
	}
	cached.checkedAt.Store(now.UnixNano())
	return true
}

// render builds a document. Metadata needs only the issuer; keys are read
// for the JWKS document alone, after their fingerprint, so a write between
// the two reads makes the next check re-render rather than miss it.
func (h *handler) render(ctx context.Context, key documentKey) (cachedDocument, error) {
	none := missingDocument
	var document any
	var version keysVersion
	if key.keys {
		var err error
		version, err = repo.New(h.db).GetOrganizationEMAIssuerKeysVersion(ctx, key.organizationID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return none, ErrNotFound
		case err != nil:
			return none, fmt.Errorf("load issuer key version: %w", err)
		}
		_, set, err := PublicKeys(ctx, h.db, key.organizationID)
		if err != nil {
			return none, err
		}
		document = set
	} else {
		issuer, err := repo.New(h.db).GetOrganizationInternalSessionIssuerURL(ctx, key.organizationID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return none, ErrNotFound
		case err != nil:
			return none, fmt.Errorf("load issuer: %w", err)
		}
		jwksURI, err := JWKSURI(issuer)
		if err != nil {
			return none, err
		}
		document = discoveryDocument{Issuer: issuer, JWKSURI: jwksURI}
	}
	body, err := json.Marshal(document)
	if err != nil {
		return none, fmt.Errorf("encode issuer document: %w", err)
	}
	renderedAt := h.now()
	checkedAt := new(atomic.Int64)
	checkedAt.Store(renderedAt.UnixNano())
	return cachedDocument{body: body, renderedAt: renderedAt, version: version, checkedAt: checkedAt}, nil
}

// PublicKeys returns the organization's pinned issuer and its published keys:
// pending, active and retired-within-overlap. Stored documents are
// re-validated as public.
func PublicKeys(ctx context.Context, db repo.DBTX, organizationID string) (string, *jose.JSONWebKeySet, error) {
	row, err := repo.New(db).GetOrganizationEMAIssuerKeys(ctx, repo.GetOrganizationEMAIssuerKeysParams{OrganizationID: organizationID, RetiredOverlapSeconds: int32(RetiredKeyOverlap / time.Second)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", nil, ErrNotFound
	case err != nil:
		return "", nil, fmt.Errorf("load issuer keys: %w", err)
	}
	var keys []jose.JSONWebKey
	if err := json.Unmarshal(row.Keys, &keys); err != nil {
		return "", nil, fmt.Errorf("decode issuer keys: %w", err)
	}
	for _, key := range keys {
		if !key.IsPublic() || !key.Valid() || key.KeyID == "" || (key.Algorithm != string(jose.RS256) && key.Algorithm != string(jose.ES256)) {
			return "", nil, errors.New("internalsessionissuers: stored verification key is not a valid public signing key")
		}
	}
	return row.Issuer, &jose.JSONWebKeySet{Keys: keys}, nil
}
