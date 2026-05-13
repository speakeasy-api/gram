package otelforwarding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	"github.com/speakeasy-api/gram/server/internal/encryption"
	"github.com/speakeasy-api/gram/server/internal/oops"
	"github.com/speakeasy-api/gram/server/internal/otelforwarding/repo"
)

// Client owns DB + cache access for the per-org forwarding config. Both the
// body-tee middleware (read path) and the management API (write path) use
// this same client so cache invalidation stays consistent. Writes accept a
// repo.DBTX so callers can keep the row and any audit-log entry in the same
// transaction.
type Client struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	repo   *repo.Queries
	enc    *encryption.Client
	cache  cache.TypedCacheObject[CachedConfig]
}

func NewClient(logger *slog.Logger, db *pgxpool.Pool, enc *encryption.Client, cacheImpl cache.Cache) *Client {
	logger = logger.With(attr.SlogComponent("otelforwarding"))
	return &Client{
		logger: logger,
		db:     db,
		repo:   repo.New(db),
		enc:    enc,
		cache:  cache.NewTypedObjectCache[CachedConfig](logger.With(attr.SlogCacheNamespace("otel-forwarding")), cacheImpl, cache.SuffixNone),
	}
}

// GetForOrg returns the cached/decoded config for an org. A returned
// CachedConfig with URL == "" means "no config configured" — this is also
// cached to avoid hammering the DB on the hot OTEL ingest path.
func (c *Client) GetForOrg(ctx context.Context, orgID string) (CachedConfig, error) {
	cacheKey := emptyCachedConfig(orgID).CacheKey()
	if cached, err := c.cache.Get(ctx, cacheKey); err == nil {
		return cached, nil
	}

	cfg, err := c.LoadForOrg(ctx, orgID)
	if err != nil {
		return emptyCachedConfig(orgID), err
	}

	if storeErr := c.cache.Store(ctx, cfg); storeErr != nil {
		c.logger.WarnContext(ctx, "failed to cache otel forwarding config",
			attr.SlogError(storeErr),
			attr.SlogOrganizationID(orgID),
		)
	}
	return cfg, nil
}

// LoadForOrg bypasses the cache and reads the row directly from the
// database. Returns an empty CachedConfig (with URL == "") if the org has
// no active config. Pairs with LoadForOrgRow when callers need the full
// repo row (e.g. for audit subject IDs).
func (c *Client) LoadForOrg(ctx context.Context, orgID string) (CachedConfig, error) {
	row, err := c.repo.GetOrgOTELForwardingConfig(ctx, orgID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return emptyCachedConfig(orgID), nil
	case err != nil:
		return emptyCachedConfig(orgID), oops.E(oops.CodeUnexpected, err, "failed to load otel forwarding config")
	}
	return c.rowToConfig(orgID, row)
}

// LoadForOrgRow returns the full repo row alongside the decoded config. A
// nil row means the org has no active forwarding config. Callers that only
// need the config payload should use LoadForOrg.
func (c *Client) LoadForOrgRow(ctx context.Context, orgID string) (CachedConfig, *repo.OtelForwardingConfig, error) {
	row, err := c.repo.GetOrgOTELForwardingConfig(ctx, orgID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return emptyCachedConfig(orgID), nil, nil
	case err != nil:
		return emptyCachedConfig(orgID), nil, oops.E(oops.CodeUnexpected, err, "failed to load otel forwarding config")
	}
	cfg, err := c.rowToConfig(orgID, row)
	if err != nil {
		return emptyCachedConfig(orgID), nil, err
	}
	return cfg, &row, nil
}

// UpsertWithTx encrypts the headers and writes the row via the given dbtx.
// Caller is responsible for committing the transaction and then calling
// RefreshCache(ctx, returned) so subsequent reads see the new value.
func (c *Client) UpsertWithTx(ctx context.Context, dbtx repo.DBTX, orgID, url string, headers map[string]string, enabled bool) (CachedConfig, *repo.OtelForwardingConfig, error) {
	headersEncrypted, err := c.encryptHeaders(headers)
	if err != nil {
		return emptyCachedConfig(orgID), nil, err
	}

	row, err := repo.New(dbtx).UpsertOrgOTELForwardingConfig(ctx, repo.UpsertOrgOTELForwardingConfigParams{
		OrganizationID:   orgID,
		EndpointUrl:      url,
		HeadersEncrypted: headersEncrypted,
		Enabled:          enabled,
	})
	if err != nil {
		return emptyCachedConfig(orgID), nil, oops.E(oops.CodeUnexpected, err, "failed to save otel forwarding config")
	}

	cfg := CachedConfig{
		OrganizationID: orgID,
		URL:            url,
		Headers:        headers,
		Enabled:        enabled,
	}
	return cfg, &row, nil
}

// SoftDeleteWithTx soft-deletes the org's forwarding config via the given
// dbtx. Caller is responsible for committing and then calling
// InvalidateCache(ctx, orgID).
func (c *Client) SoftDeleteWithTx(ctx context.Context, dbtx repo.DBTX, orgID string) error {
	if err := repo.New(dbtx).SoftDeleteOrgOTELForwardingConfig(ctx, orgID); err != nil {
		return oops.E(oops.CodeUnexpected, err, "failed to delete otel forwarding config")
	}
	return nil
}

// RefreshCache stores the given config in the cache so subsequent reads
// don't wait for the TTL.
func (c *Client) RefreshCache(ctx context.Context, cfg CachedConfig) {
	if err := c.cache.Store(ctx, cfg); err != nil {
		c.logger.WarnContext(ctx, "failed to refresh otel forwarding cache",
			attr.SlogError(err),
			attr.SlogOrganizationID(cfg.OrganizationID),
		)
	}
}

// InvalidateCache removes the cached config for the org.
func (c *Client) InvalidateCache(ctx context.Context, orgID string) {
	if err := c.cache.DeleteByKey(ctx, emptyCachedConfig(orgID).CacheKey()); err != nil {
		c.logger.WarnContext(ctx, "failed to invalidate otel forwarding cache",
			attr.SlogError(err),
			attr.SlogOrganizationID(orgID),
		)
	}
}

func (c *Client) rowToConfig(orgID string, row repo.OtelForwardingConfig) (CachedConfig, error) {
	headers, err := c.decryptHeaders(row.HeadersEncrypted)
	if err != nil {
		return emptyCachedConfig(orgID), err
	}
	return CachedConfig{
		OrganizationID: orgID,
		URL:            row.EndpointUrl,
		Headers:        headers,
		Enabled:        row.Enabled,
	}, nil
}

func (c *Client) encryptHeaders(headers map[string]string) (pgtype.Text, error) {
	null := pgtype.Text{String: "", Valid: false}
	if len(headers) == 0 {
		return null, nil
	}
	b, err := json.Marshal(headers)
	if err != nil {
		return null, oops.E(oops.CodeUnexpected, err, "marshal forwarding headers")
	}
	ct, err := c.enc.Encrypt(b)
	if err != nil {
		return null, oops.E(oops.CodeUnexpected, err, "encrypt forwarding headers")
	}
	return pgtype.Text{String: ct, Valid: true}, nil
}

func (c *Client) decryptHeaders(stored pgtype.Text) (map[string]string, error) {
	if !stored.Valid || stored.String == "" {
		return nil, nil
	}
	plaintext, err := c.enc.Decrypt(stored.String)
	if err != nil {
		return nil, oops.E(oops.CodeUnexpected, err, "decrypt forwarding headers")
	}
	var headers map[string]string
	if err := json.Unmarshal([]byte(plaintext), &headers); err != nil {
		return nil, oops.E(oops.CodeUnexpected, fmt.Errorf("unmarshal forwarding headers: %w", err), "decode forwarding headers")
	}
	return headers, nil
}
