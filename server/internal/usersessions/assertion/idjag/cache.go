package idjag

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

// IssuerKeyCache persists issuer keys on the trusted remote issuer row. Its
// key contains both row id and URI, so an endpoint change cannot reuse an
// older endpoint's cached key set.
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

func issuerCacheKey(id uuid.UUID, uri string) string { return id.String() + "|" + uri }

func splitIssuerCacheKey(key string) (uuid.UUID, string, error) {
	idText, uri, ok := strings.Cut(key, "|")
	if !ok || uri == "" {
		return uuid.Nil, "", errors.New("idjag: invalid issuer cache key")
	}
	id, err := uuid.Parse(idText)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("idjag: invalid issuer cache row id: %w", err)
	}
	return id, uri, nil
}

// Get returns even expired keys for conditional refresh and bounded stale
// use. The URI must still match the trusted issuer's current configuration.
func (c *IssuerKeyCache) Get(ctx context.Context, key string) (jwks.CacheState, error) {
	id, uri, err := splitIssuerCacheKey(key)
	if err != nil {
		return jwks.CacheState{}, err
	}
	row, err := c.repo.GetTrustedIssuerJWKSCache(ctx, id)
	if err != nil {
		return jwks.CacheState{}, fmt.Errorf("read trusted issuer key cache: %w", err)
	}
	if !row.JwksUri.Valid || row.JwksUri.String != uri {
		return jwks.CacheState{}, errors.New("idjag: trusted issuer jwks_uri changed")
	}
	state := jwks.CacheState{Document: row.Jwks, ETag: "", ExpiresAt: time.Time{}, RefreshedAt: time.Time{}}
	if row.JwksEtag.Valid {
		state.ETag = row.JwksEtag.String
	}
	if row.JwksCacheExpiresAt.Valid {
		state.ExpiresAt = row.JwksCacheExpiresAt.Time
	}
	if row.JwksFetchedAt.Valid {
		state.RefreshedAt = row.JwksFetchedAt.Time
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
	id, uri, err := splitIssuerCacheKey(key)
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
		ID:                  id,
		JwksUri:             pgtype.Text{String: uri, Valid: true},
		PriorJwks:           prior.Document,
		PriorFetchedAt:      pgtype.Timestamptz{Time: prior.RefreshedAt, InfinityModifier: pgtype.Finite, Valid: !prior.RefreshedAt.IsZero()},
		PriorCacheExpiresAt: pgtype.Timestamptz{Time: prior.ExpiresAt, InfinityModifier: pgtype.Finite, Valid: !prior.ExpiresAt.IsZero()},
	})
	if err != nil {
		return false, fmt.Errorf("write trusted issuer key cache: %w", err)
	}
	return rows == 1, nil
}

// MarkConsultFailure advances only the negative-cache timestamp and only if
// the key document still matches the failed attempt's starting state.
func (c *IssuerKeyCache) MarkConsultFailure(ctx context.Context, key string, prior jwks.CacheState, consultedAt time.Time) error {
	id, uri, err := splitIssuerCacheKey(key)
	if err != nil {
		return err
	}
	if len(prior.Document) == 0 || prior.ExpiresAt.IsZero() || prior.RefreshedAt.IsZero() {
		return nil
	}
	_, err = c.repo.MarkTrustedIssuerJWKSConsultFailure(ctx, remotesessionsrepo.MarkTrustedIssuerJWKSConsultFailureParams{
		ConsultedAt:         pgtype.Timestamptz{Time: consultedAt, InfinityModifier: pgtype.Finite, Valid: true},
		ID:                  id,
		JwksUri:             pgtype.Text{String: uri, Valid: true},
		PriorJwks:           prior.Document,
		PriorFetchedAt:      pgtype.Timestamptz{Time: prior.RefreshedAt, InfinityModifier: pgtype.Finite, Valid: true},
		PriorCacheExpiresAt: pgtype.Timestamptz{Time: prior.ExpiresAt, InfinityModifier: pgtype.Finite, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("mark trusted issuer key consult failure: %w", err)
	}
	return nil
}
