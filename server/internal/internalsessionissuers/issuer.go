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
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-jose/go-jose/v4"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goahttp "goa.design/goa/v3/http"

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
)

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

// RotationPendingError means a new key is published and can be activated
// from ReadyAt, once verifier caches have seen it.
type RotationPendingError struct{ ReadyAt time.Time }

func (e *RotationPendingError) Error() string {
	return "internalsessionissuers: rotation pending until " + e.ReadyAt.Format(time.RFC3339Nano)
}

// URL is the organization's permanent issuer identifier. It never names a key,
// so key rotation and replacement never change what vendors trust.
func URL(origin *url.URL, organizationID string) (*url.URL, error) {
	if origin == nil || !urls.IsAbsoluteHTTPSOrLoopback(origin.String()) || organizationID == "" {
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
	h := &handler{db: db, logger: logger, cache: expirable.NewLRU[documentKey, cachedDocument](documentCacheSize, nil, documentCacheTTL)}
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
}

type documentKey struct {
	organizationID string
	keys           bool
}

// cachedDocument is a rendered document; a nil body means no issuer.
type cachedDocument struct{ body []byte }

func (h *handler) serve(w http.ResponseWriter, r *http.Request, keys bool) error {
	ctx := r.Context()
	// The issuer lives only on the platform origin, never a custom domain.
	if customdomains.FromContext(ctx) != nil {
		return oops.E(oops.CodeNotFound, nil, "issuer not found")
	}
	key := documentKey{organizationID: chi.URLParam(r, "organizationID"), keys: keys}
	cached, ok := h.cache.Get(key)
	if !ok {
		body, err := h.render(ctx, key)
		switch {
		case errors.Is(err, ErrNotFound):
		case err != nil:
			return oops.E(oops.CodeUnexpected, err, "load issuer document").LogError(ctx, h.logger)
		}
		cached = cachedDocument{body: body}
		h.cache.Add(key, cached)
	}
	if cached.body == nil {
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", notFoundMaxAge))
		return oops.E(oops.CodeNotFound, nil, "issuer not found")
	}
	contentType := "application/json"
	if keys {
		contentType = "application/jwk-set+json"
	}
	return httpcache.WriteCacheableJSON(ctx, w, r, h.logger, contentType, int(CacheTTL/time.Second), cached.body)
}

// render builds a document. Metadata needs only the issuer; keys are read
// for the JWKS document alone.
func (h *handler) render(ctx context.Context, key documentKey) ([]byte, error) {
	var document any
	if key.keys {
		_, set, err := PublicKeys(ctx, h.db, key.organizationID)
		if err != nil {
			return nil, err
		}
		document = set
	} else {
		issuer, err := repo.New(h.db).GetOrganizationInternalSessionIssuerURL(ctx, key.organizationID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil, ErrNotFound
		case err != nil:
			return nil, fmt.Errorf("load issuer: %w", err)
		}
		jwksURI, err := JWKSURI(issuer)
		if err != nil {
			return nil, err
		}
		document = discoveryDocument{Issuer: issuer, JWKSURI: jwksURI}
	}
	body, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode issuer document: %w", err)
	}
	return body, nil
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
