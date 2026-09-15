package idjag

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

// IssuerKeyCache persists issuer keys on the trusted remote issuer row. Its
// key binds the row, organization, issuer, and URI, so configuration changes
// cannot reuse an older endpoint's cached key set.
type IssuerKeyCache struct {
	repo *remotesessionsrepo.Queries
}

// NewIssuerKeyCache builds a durable cache for trusted issuer key sets.
func NewIssuerKeyCache(db *pgxpool.Pool) (*IssuerKeyCache, error) {
	if db == nil {
		return nil, errors.New("idjag: database is required for issuer key cache")
	}
	return &IssuerKeyCache{repo: remotesessionsrepo.New(db)}, nil
}

var _ jwks.Cache = (*IssuerKeyCache)(nil)
var _ jwks.ConsultFailureMarker = (*IssuerKeyCache)(nil)

func issuerCacheKey(organizationID string, id uuid.UUID, issuer, uri string) string {
	return url.QueryEscape(organizationID) + "|" + id.String() + "|" + url.QueryEscape(issuer) + "|" + uri
}

func splitIssuerCacheKey(key string) (issuerCacheIdentity, error) {
	organizationText, rest, ok := strings.Cut(key, "|")
	if !ok {
		return issuerCacheIdentity{}, errors.New("idjag: invalid issuer cache key")
	}
	idText, rest, ok := strings.Cut(rest, "|")
	if !ok {
		return issuerCacheIdentity{}, errors.New("idjag: invalid issuer cache key")
	}
	issuerText, uri, ok := strings.Cut(rest, "|")
	if !ok || uri == "" {
		return issuerCacheIdentity{}, errors.New("idjag: invalid issuer cache key")
	}
	id, err := uuid.Parse(idText)
	if err != nil {
		return issuerCacheIdentity{}, fmt.Errorf("idjag: invalid issuer cache row id: %w", err)
	}
	organizationID, err := url.QueryUnescape(organizationText)
	if err != nil || organizationID == "" {
		return issuerCacheIdentity{}, errors.New("idjag: invalid issuer cache organization")
	}
	issuer, err := url.QueryUnescape(issuerText)
	if err != nil || issuer == "" {
		return issuerCacheIdentity{}, errors.New("idjag: invalid issuer cache issuer")
	}
	return issuerCacheIdentity{organizationID: organizationID, id: id, issuer: issuer, uri: uri}, nil
}

// Get returns even expired keys for conditional refresh and bounded stale
// use. The URI must still match the trusted issuer's current configuration.
func (c *IssuerKeyCache) Get(ctx context.Context, key string) (jwks.CacheState, error) {
	identity, err := splitIssuerCacheKey(key)
	if err != nil {
		return jwks.CacheState{Document: nil, ETag: "", ExpiresAt: time.Time{}, RefreshedAt: time.Time{}, LastErrorAt: time.Time{}, LastError: "", Revision: ""}, err
	}
	row, err := c.repo.GetTrustedIssuerJWKSCache(ctx, remotesessionsrepo.GetTrustedIssuerJWKSCacheParams{
		ID: identity.id, OrganizationID: identity.organizationID, Issuer: identity.issuer, JwksUri: identity.uri,
	})
	if err != nil {
		return jwks.CacheState{Document: nil, ETag: "", ExpiresAt: time.Time{}, RefreshedAt: time.Time{}, LastErrorAt: time.Time{}, LastError: "", Revision: ""}, fmt.Errorf("read trusted issuer key cache: %w", err)
	}
	if !row.JwksUri.Valid || row.JwksUri.String != identity.uri {
		return jwks.CacheState{Document: nil, ETag: "", ExpiresAt: time.Time{}, RefreshedAt: time.Time{}, LastErrorAt: time.Time{}, LastError: "", Revision: ""}, errors.New("idjag: trusted issuer jwks_uri changed")
	}
	state := jwks.CacheState{Document: row.Jwks, ETag: "", ExpiresAt: time.Time{}, RefreshedAt: time.Time{}, LastErrorAt: time.Time{}, LastError: "", Revision: row.Revision}
	if row.JwksEtag.Valid {
		state.ETag = row.JwksEtag.String
	}
	if row.JwksCacheExpiresAt.Valid {
		state.ExpiresAt = row.JwksCacheExpiresAt.Time
	}
	if row.JwksFetchedAt.Valid {
		state.RefreshedAt = row.JwksFetchedAt.Time
	}
	if row.JwksLastErrorAt.Valid {
		state.LastErrorAt = row.JwksLastErrorAt.Time
	}
	if row.JwksLastError.Valid {
		state.LastError = row.JwksLastError.String
	}
	return state, nil
}

// Put satisfies jwks.Cache; resolver writes use PutIfUnchanged with the state
// observed before fetching.
func (c *IssuerKeyCache) Put(ctx context.Context, key string, state jwks.CacheState) error {
	prior, err := c.Get(ctx, key)
	if err != nil {
		return err
	}
	written, err := c.PutIfUnchanged(ctx, key, prior, state)
	if err != nil {
		return err
	}
	if !written {
		return errors.New("idjag: issuer key cache changed during write")
	}
	return nil
}

// PutIfUnchanged writes only if the cache row still matches the state that
// preceded the fetch. PostgreSQL performs the comparison and update atomically.
func (c *IssuerKeyCache) PutIfUnchanged(ctx context.Context, key string, prior, state jwks.CacheState) (bool, error) {
	identity, err := splitIssuerCacheKey(key)
	if err != nil {
		return false, err
	}
	if len(state.Document) == 0 || state.ExpiresAt.IsZero() || state.RefreshedAt.IsZero() {
		return false, errors.New("idjag: incomplete issuer key cache state")
	}
	rows, err := c.repo.UpdateTrustedIssuerJWKSCache(ctx, remotesessionsrepo.UpdateTrustedIssuerJWKSCacheParams{
		Jwks:                state.Document,
		FetchedAt:           pgtype.Timestamptz{Time: state.RefreshedAt, InfinityModifier: pgtype.Finite, Valid: true},
		CacheExpiresAt:      pgtype.Timestamptz{Time: state.ExpiresAt, InfinityModifier: pgtype.Finite, Valid: true},
		Etag:                state.ETag,
		ID:                  identity.id,
		OrganizationID:      identity.organizationID,
		Issuer:              identity.issuer,
		JwksUri:             pgtype.Text{String: identity.uri, Valid: true},
		PriorRevision:       prior.Revision,
		PriorJwks:           prior.Document,
		PriorFetchedAt:      pgtype.Timestamptz{Time: prior.RefreshedAt, InfinityModifier: pgtype.Finite, Valid: !prior.RefreshedAt.IsZero()},
		PriorCacheExpiresAt: pgtype.Timestamptz{Time: prior.ExpiresAt, InfinityModifier: pgtype.Finite, Valid: !prior.ExpiresAt.IsZero()},
	})
	if err != nil {
		return false, fmt.Errorf("write trusted issuer key cache: %w", err)
	}
	return rows == 1, nil
}

// MarkConsultFailure records a safe reason and attempt time without changing
// the last successful fetch time or a newer concurrent cache state.
func (c *IssuerKeyCache) MarkConsultFailure(ctx context.Context, key string, prior jwks.CacheState, consultedAt time.Time, reason string) error {
	identity, err := splitIssuerCacheKey(key)
	if err != nil {
		return err
	}
	_, err = c.repo.MarkTrustedIssuerJWKSConsultFailure(ctx, remotesessionsrepo.MarkTrustedIssuerJWKSConsultFailureParams{
		Reason:              reason,
		ConsultedAt:         pgtype.Timestamptz{Time: consultedAt, InfinityModifier: pgtype.Finite, Valid: true},
		ID:                  identity.id,
		OrganizationID:      identity.organizationID,
		Issuer:              identity.issuer,
		JwksUri:             pgtype.Text{String: identity.uri, Valid: true},
		PriorRevision:       prior.Revision,
		PriorJwks:           prior.Document,
		PriorFetchedAt:      pgtype.Timestamptz{Time: prior.RefreshedAt, InfinityModifier: pgtype.Finite, Valid: !prior.RefreshedAt.IsZero()},
		PriorCacheExpiresAt: pgtype.Timestamptz{Time: prior.ExpiresAt, InfinityModifier: pgtype.Finite, Valid: !prior.ExpiresAt.IsZero()},
	})
	if err != nil {
		return fmt.Errorf("mark trusted issuer key consult failure: %w", err)
	}
	return nil
}
