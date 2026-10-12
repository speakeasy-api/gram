// Package emaissuer publishes an organization's Speakeasy ID-JAG issuer for
// enterprise-managed authorization and signs assertions with its key.
package emaissuer

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-jose/go-jose/v4"
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

// CacheTTL bounds how long a verifier may cache the issuer's public keys.
const CacheTTL = 60 * time.Second

// RetiredKeyOverlap keeps a retired key published until every assertion it
// signed has expired and every verifier cache has refreshed.
const RetiredKeyOverlap = 10 * time.Minute

var (
	// ErrNotFound is returned when an organization has no live issuer.
	ErrNotFound = errors.New("emaissuer: organization has no issuer")
	// ErrKeyNotFound means the named kid is not a live issuer key.
	ErrKeyNotFound = errors.New("emaissuer: issuer key not found")
	// ErrUnknownKid means a rotation named a kid never published by the issuer.
	ErrUnknownKid = errors.New("emaissuer: kid was never published by this issuer")
)

// RotationPendingError means a new key is published and can be activated
// from ReadyAt, once verifier caches have seen it.
type RotationPendingError struct{ ReadyAt time.Time }

func (e *RotationPendingError) Error() string {
	return "emaissuer: rotation pending until " + e.ReadyAt.Format(time.RFC3339Nano)
}

// URL is the organization's permanent issuer identifier. It never names a key,
// so key rotation and replacement never change what vendors trust.
func URL(origin *url.URL, organizationID string) (*url.URL, error) {
	if origin == nil || !urls.IsAbsoluteHTTPSOrLoopback(origin.String()) || organizationID == "" {
		return nil, errors.New("emaissuer: issuer origin is unavailable")
	}
	return origin.JoinPath("oauth", "idp", "org", organizationID), nil
}

// JWKSURL serves the issuer's public verification keys.
func JWKSURL(origin *url.URL, organizationID string) (*url.URL, error) {
	issuer, err := URL(origin, organizationID)
	if err != nil {
		return nil, err
	}
	return issuer.JoinPath("jwks"), nil
}

// discoveryDocument advertises only the trust anchor: issuance is internal.
type discoveryDocument struct {
	Issuer  string `json:"issuer"`
	JWKSURI string `json:"jwks_uri"`
}

// Attach mounts the public discovery and JWKS documents.
func Attach(mux goahttp.Muxer, db *pgxpool.Pool, logger *slog.Logger, origin *url.URL) {
	h := &handler{db: db, logger: logger, origin: origin}
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
	origin *url.URL
}

func (h *handler) serve(w http.ResponseWriter, r *http.Request, keys bool) error {
	ctx := r.Context()
	// The issuer lives only on the platform origin, never a custom domain.
	if customdomains.FromContext(ctx) != nil {
		return oops.E(oops.CodeNotFound, nil, "issuer not found")
	}
	organizationID := chi.URLParam(r, "organizationID")
	set, err := PublicKeys(ctx, h.db, organizationID)
	switch {
	case errors.Is(err, ErrNotFound):
		return oops.E(oops.CodeNotFound, nil, "issuer not found")
	case err != nil:
		return oops.E(oops.CodeUnexpected, err, "load issuer keys").LogError(ctx, h.logger)
	}
	issuer, err := URL(h.origin, organizationID)
	if err != nil {
		return oops.E(oops.CodeUnavailable, err, "issuer origin is unavailable").LogError(ctx, h.logger)
	}
	var document any = discoveryDocument{Issuer: issuer.String(), JWKSURI: issuer.JoinPath("jwks").String()}
	contentType := "application/json"
	if keys {
		document = set
		contentType = "application/jwk-set+json"
	}
	body, err := json.Marshal(document)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "encode issuer document").LogError(ctx, h.logger)
	}
	return httpcache.WriteCacheableJSON(ctx, w, r, h.logger, contentType, int(CacheTTL/time.Second), body)
}

// PublicKeys returns the organization's published keys: pending, active and
// retired-within-overlap. Stored documents are re-validated as public.
func PublicKeys(ctx context.Context, db repo.DBTX, organizationID string) (*jose.JSONWebKeySet, error) {
	raw, err := repo.New(db).GetOrganizationEMAIssuerKeys(ctx, repo.GetOrganizationEMAIssuerKeysParams{OrganizationID: organizationID, RetiredOverlapSeconds: int32(RetiredKeyOverlap / time.Second)})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, ErrNotFound
	case err != nil:
		return nil, err
	}
	var keys []jose.JSONWebKey
	if err := json.Unmarshal(raw, &keys); err != nil {
		return nil, err
	}
	for _, key := range keys {
		if !key.IsPublic() || !key.Valid() || key.KeyID == "" || (key.Algorithm != string(jose.RS256) && key.Algorithm != string(jose.ES256)) {
			return nil, errors.New("emaissuer: stored verification key is not a valid public signing key")
		}
	}
	return &jose.JSONWebKeySet{Keys: keys}, nil
}
