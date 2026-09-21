package idjag

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	remotesessionsrepo "github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
	"github.com/speakeasy-api/gram/server/internal/usersessions/jwks"
)

func TestIssuerKeyCacheRejectsLatePreRotationWrite(t *testing.T) {
	t.Parallel()

	db := newIssuerCacheTestDB(t)
	ctx := t.Context()
	uri := "https://issuer.example.com/jwks"
	id, err := remotesessionsrepo.New(db).CreateTestTrustedIssuerJWKSCache(ctx, remotesessionsrepo.CreateTestTrustedIssuerJWKSCacheParams{
		OrganizationID: pgtype.Text{String: "", Valid: false}, Slug: "issuer-" + uuid.NewString(),
		Issuer: "https://issuer.example.com", JwksUri: pgtype.Text{String: uri, Valid: true},
	})
	require.NoError(t, err)
	cache, err := NewIssuerKeyCache(db)
	require.NoError(t, err)
	key := issuerCacheKey("test-org", id, "https://issuer.example.com", uri)
	prior, err := cache.Get(ctx, key)
	require.NoError(t, err)

	base := time.Now().UTC()
	newer := jwks.CacheState{Document: []byte(`{"keys":[{"kid":"rotated"}]}`), ETag: `"rotated"`, ExpiresAt: base.Add(time.Hour), RefreshedAt: base, LastErrorAt: time.Time{}, LastError: "", Revision: ""}
	older := jwks.CacheState{Document: []byte(`{"keys":[{"kid":"old"}]}`), ETag: `"old"`, ExpiresAt: base.Add(time.Hour), RefreshedAt: base.Add(time.Second), LastErrorAt: time.Time{}, LastError: "", Revision: ""}
	written, err := cache.PutIfUnchanged(ctx, key, prior, newer)
	require.NoError(t, err)
	require.True(t, written)
	written, err = cache.PutIfUnchanged(ctx, key, prior, older)
	require.NoError(t, err)
	require.False(t, written)
	require.NoError(t, cache.MarkConsultFailure(ctx, key, prior, base.Add(time.Minute), "JWKS endpoint temporarily unavailable"))
	stored, err := cache.Get(ctx, key)
	require.NoError(t, err)
	require.JSONEq(t, string(newer.Document), string(stored.Document))
	require.Empty(t, stored.LastError)
}

func TestIssuerKeyCachePreservesLastSuccessfulFetchAcrossError(t *testing.T) {
	t.Parallel()
	db := newIssuerCacheTestDB(t)
	ctx := t.Context()
	repo := remotesessionsrepo.New(db)
	uri := "https://issuer.example.com/jwks"
	id, err := repo.CreateTestTrustedIssuerJWKSCache(ctx, remotesessionsrepo.CreateTestTrustedIssuerJWKSCacheParams{
		OrganizationID: pgtype.Text{String: "", Valid: false}, Slug: "issuer-" + uuid.NewString(),
		Issuer: "https://issuer.example.com", JwksUri: pgtype.Text{String: uri, Valid: true},
	})
	require.NoError(t, err)
	cache, err := NewIssuerKeyCache(db)
	require.NoError(t, err)
	key := issuerCacheKey("test-org", id, "https://issuer.example.com", uri)
	prior, err := cache.Get(ctx, key)
	require.NoError(t, err)
	successAt := time.Now().UTC().Truncate(time.Microsecond)
	state := jwks.CacheState{Document: []byte(`{"keys":[{"kid":"known"}]}`), ETag: "", ExpiresAt: successAt.Add(time.Hour), RefreshedAt: successAt, LastErrorAt: time.Time{}, LastError: "", Revision: ""}
	written, err := cache.PutIfUnchanged(ctx, key, prior, state)
	require.NoError(t, err)
	require.True(t, written)
	stored, err := cache.Get(ctx, key)
	require.NoError(t, err)
	require.NoError(t, cache.MarkConsultFailure(ctx, key, stored, successAt.Add(time.Minute), "JWKS endpoint temporarily unavailable (HTTP 503)"))
	failed, err := cache.Get(ctx, key)
	require.NoError(t, err)
	require.True(t, successAt.Equal(failed.RefreshedAt))
	require.True(t, successAt.Add(time.Minute).Equal(failed.LastErrorAt))
	require.Equal(t, "JWKS endpoint temporarily unavailable (HTTP 503)", failed.LastError)
	reconciled := failed
	reconciled.Document = []byte(`{"keys":[{"kid":"rotated"}]}`)
	reconciled.ETag = `"rotated"`
	written, err = cache.PutIfUnchanged(ctx, key, failed, reconciled)
	require.NoError(t, err)
	require.True(t, written)
	reconciled, err = cache.Get(ctx, key)
	require.NoError(t, err)
	require.JSONEq(t, `{"keys":[{"kid":"rotated"}]}`, string(reconciled.Document))
	require.Equal(t, `"rotated"`, reconciled.ETag)
	require.True(t, state.ExpiresAt.Equal(reconciled.ExpiresAt))
	require.True(t, successAt.Equal(reconciled.RefreshedAt))
	require.True(t, failed.LastErrorAt.Equal(reconciled.LastErrorAt))
	require.Equal(t, failed.LastError, reconciled.LastError)

	state.Document = reconciled.Document
	state.ETag = reconciled.ETag
	state.RefreshedAt = successAt.Add(2 * time.Minute)
	written, err = cache.PutIfUnchanged(ctx, key, reconciled, state)
	require.NoError(t, err)
	require.True(t, written)
	recovered, err := cache.Get(ctx, key)
	require.NoError(t, err)
	require.Zero(t, recovered.LastErrorAt)
	require.Empty(t, recovered.LastError)
}

func TestIssuerKeyCacheRejectsConfigurationABA(t *testing.T) {
	t.Parallel()
	db := newIssuerCacheTestDB(t)
	ctx := t.Context()
	repo := remotesessionsrepo.New(db)
	uri := "https://issuer.example.com/jwks"
	issuer := "https://issuer.example.com"
	id, err := repo.CreateTestTrustedIssuerJWKSCache(ctx, remotesessionsrepo.CreateTestTrustedIssuerJWKSCacheParams{
		OrganizationID: pgtype.Text{String: "", Valid: false}, Slug: "issuer-" + uuid.NewString(),
		Issuer: issuer, JwksUri: pgtype.Text{String: uri, Valid: true},
	})
	require.NoError(t, err)
	cache, err := NewIssuerKeyCache(db)
	require.NoError(t, err)
	key := issuerCacheKey("test-org", id, issuer, uri)
	prior, err := cache.Get(ctx, key)
	require.NoError(t, err)
	require.NoError(t, repo.UpdateTestTrustedIssuerConfiguration(ctx, remotesessionsrepo.UpdateTestTrustedIssuerConfigurationParams{
		ID: id, Issuer: "https://other.example.com", JwksUri: pgtype.Text{String: "https://other.example.com/jwks", Valid: true},
	}))
	_, err = cache.Get(ctx, key)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.NoError(t, repo.UpdateTestTrustedIssuerConfiguration(ctx, remotesessionsrepo.UpdateTestTrustedIssuerConfigurationParams{
		ID: id, Issuer: issuer, JwksUri: pgtype.Text{String: uri, Valid: true},
	}))
	state := jwks.CacheState{Document: []byte(`{"keys":[{"kid":"old"}]}`), ETag: "", ExpiresAt: time.Now().Add(time.Hour), RefreshedAt: time.Now(), LastErrorAt: time.Time{}, LastError: "", Revision: ""}
	written, err := cache.PutIfUnchanged(ctx, key, prior, state)
	require.NoError(t, err)
	require.False(t, written, "the row version must change even when configuration returns to its old values")
}

func TestIssuerKeyCacheScopesOrganizationRows(t *testing.T) {
	t.Parallel()
	db := newIssuerCacheTestDB(t)
	ctx := t.Context()
	repo := remotesessionsrepo.New(db)
	organizationID := "test-org-" + uuid.NewString()
	require.NoError(t, repo.CreateTestTrustedIssuerOrganization(ctx, remotesessionsrepo.CreateTestTrustedIssuerOrganizationParams{
		ID: organizationID, Name: "Test organization", Slug: "test-organization-" + uuid.NewString(),
	}))
	uri := "https://issuer.example.com/jwks"
	issuer := "https://issuer.example.com"
	id, err := repo.CreateTestTrustedIssuerJWKSCache(ctx, remotesessionsrepo.CreateTestTrustedIssuerJWKSCacheParams{
		OrganizationID: pgtype.Text{String: organizationID, Valid: true}, Slug: "issuer-" + uuid.NewString(),
		Issuer: issuer, JwksUri: pgtype.Text{String: uri, Valid: true},
	})
	require.NoError(t, err)
	cache, err := NewIssuerKeyCache(db)
	require.NoError(t, err)
	_, err = cache.Get(ctx, issuerCacheKey(organizationID, id, issuer, uri))
	require.NoError(t, err)
	_, err = cache.Get(ctx, issuerCacheKey("another-organization", id, issuer, uri))
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
