package upstreamtokens

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
	"github.com/speakeasy-api/gram/server/internal/remotesessions/repo"
)

// CopyStores returns the CopyStore of every value the oauth-session-tokens use
// case holds. The backfill, verification, finalize and clear for the use case
// must always be given all of them: a value left out keeps only its
// platform-key copy, which nothing reads once the organization is finalized.
func CopyStores(db *pgxpool.Pool) []orgencryption.CopyStore {
	return []orgencryption.CopyStore{
		&copyStore{
			db:          db,
			columns:     RemoteSessionAccessToken,
			listWaiting: listRemoteSessionAccessTokensWaiting,
			sealWaiting: sealRemoteSessionAccessTokenWaiting,
			listSealed:  listRemoteSessionAccessTokensSealed,
			count:       countRemoteSessionAccessTokenCopies,
			clear:       clearRemoteSessionAccessTokenPlatformCopies,
		},
		&copyStore{
			db:          db,
			columns:     RemoteSessionRefreshToken,
			listWaiting: listRemoteSessionRefreshTokensWaiting,
			sealWaiting: sealRemoteSessionRefreshTokenWaiting,
			listSealed:  listRemoteSessionRefreshTokensSealed,
			count:       countRemoteSessionRefreshTokenCopies,
			clear:       clearRemoteSessionRefreshTokenPlatformCopies,
		},
		&copyStore{
			db:          db,
			columns:     EMACredentialAccessToken,
			listWaiting: listEMACredentialAccessTokensWaiting,
			sealWaiting: sealEMACredentialAccessTokenWaiting,
			listSealed:  listEMACredentialAccessTokensSealed,
			count:       countEMACredentialAccessTokenCopies,
			clear:       clearEMACredentialAccessTokenPlatformCopies,
		},
	}
}

// copyStore reaches one value's two copies through that value's own sqlc
// queries. Every source table keys rows by a uuid, so row ids are the
// uuid.UUID.String form, which matches the id::text the count queries join
// organization_encryption_unreadable_values on.
type copyStore struct {
	db      *pgxpool.Pool
	columns orgencryption.CopyColumns

	// listWaiting returns a page of values with only a platform-key copy.
	listWaiting func(ctx context.Context, q *repo.Queries, organizationID string, after uuid.NullUUID, limit int32) ([]orgencryption.WaitingValue, error)

	// sealWaiting stores the organization-key copy of one waiting value and
	// reports how many rows it changed.
	sealWaiting func(ctx context.Context, q *repo.Queries, organizationID string, generation int64, id uuid.UUID, platformCopy string, orgCopy string) (int64, error)

	// listSealed returns a page of values with both copies.
	listSealed func(ctx context.Context, q *repo.Queries, organizationID string, after uuid.NullUUID, limit int32) ([]orgencryption.SealedValue, error)

	// count returns the value's counts for the organization.
	count func(ctx context.Context, q *repo.Queries, organizationID string) (orgencryption.CopyCounts, error)

	// clear destroys up to limit platform-key copies and reports how many.
	clear func(ctx context.Context, q *repo.Queries, organizationID string, generation int64, limit int32) (int64, error)
}

var _ orgencryption.CopyStore = (*copyStore)(nil)

// Columns names the value's pair of columns.
func (c *copyStore) Columns() orgencryption.CopyColumns {
	return c.columns
}

// ListWaiting returns the organization's next page of values with only a
// platform-key copy.
func (c *copyStore) ListWaiting(ctx context.Context, organizationID string, after string, limit int32) ([]orgencryption.WaitingValue, error) {
	afterID, err := parseAfter(after)
	if err != nil {
		return nil, err
	}

	values, err := c.listWaiting(ctx, repo.New(c.db), organizationID, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list waiting %s: %w", c.columns.PlatformColumn, err)
	}

	return values, nil
}

// SealWaiting stores the backfill's organization-key copy of one value.
func (c *copyStore) SealWaiting(ctx context.Context, organizationID string, generation int64, value orgencryption.WaitingValue, orgCiphertext string) (bool, error) {
	id, err := uuid.Parse(value.RowID)
	if err != nil {
		return false, fmt.Errorf("parse row id %q: %w", value.RowID, err)
	}

	landed, err := c.sealWaiting(ctx, repo.New(c.db), organizationID, generation, id, value.Platform, orgCiphertext)
	if err != nil {
		return false, fmt.Errorf("seal waiting %s: %w", c.columns.PlatformColumn, err)
	}

	return landed == 1, nil
}

// ListSealed returns the organization's next page of values with both
// copies.
func (c *copyStore) ListSealed(ctx context.Context, organizationID string, after string, limit int32) ([]orgencryption.SealedValue, error) {
	afterID, err := parseAfter(after)
	if err != nil {
		return nil, err
	}

	values, err := c.listSealed(ctx, repo.New(c.db), organizationID, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list sealed %s: %w", c.columns.PlatformColumn, err)
	}

	return values, nil
}

// Count returns the organization's counts for the value.
func (c *copyStore) Count(ctx context.Context, organizationID string) (orgencryption.CopyCounts, error) {
	counts, err := c.count(ctx, repo.New(c.db), organizationID)
	if err != nil {
		return orgencryption.CopyCounts{}, fmt.Errorf("count %s copies: %w", c.columns.PlatformColumn, err)
	}

	return counts, nil
}

// ClearPlatformCopies destroys up to limit of the organization's platform-key
// copies of the value after finalize.
func (c *copyStore) ClearPlatformCopies(ctx context.Context, organizationID string, generation int64, limit int32) (int64, error) {
	cleared, err := c.clear(ctx, repo.New(c.db), organizationID, generation, limit)
	if err != nil {
		return 0, fmt.Errorf("clear %s platform copies: %w", c.columns.PlatformColumn, err)
	}

	return cleared, nil
}

// parseAfter parses a page cursor: "" for the first page, otherwise the
// RowID of the previous page's last value.
func parseAfter(after string) (uuid.NullUUID, error) {
	if after == "" {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}

	id, err := uuid.Parse(after)
	if err != nil {
		return uuid.NullUUID{}, fmt.Errorf("parse page cursor %q: %w", after, err)
	}

	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

func listRemoteSessionAccessTokensWaiting(ctx context.Context, q *repo.Queries, organizationID string, after uuid.NullUUID, limit int32) ([]orgencryption.WaitingValue, error) {
	rows, err := q.ListRemoteSessionAccessTokensWaiting(ctx, repo.ListRemoteSessionAccessTokensWaitingParams{OrganizationID: organizationID, After: after, LimitValue: limit})
	if err != nil {
		return nil, fmt.Errorf("list waiting access tokens: %w", err)
	}

	values := make([]orgencryption.WaitingValue, 0, len(rows))
	for _, row := range rows {
		values = append(values, orgencryption.WaitingValue{RowID: row.ID.String(), Platform: row.PlatformCopy})
	}

	return values, nil
}

func sealRemoteSessionAccessTokenWaiting(ctx context.Context, q *repo.Queries, organizationID string, generation int64, id uuid.UUID, platformCopy string, orgCopy string) (int64, error) {
	n, err := q.SealRemoteSessionAccessTokenWaiting(ctx, repo.SealRemoteSessionAccessTokenWaitingParams{
		OrgCopy:           conv.ToPGText(orgCopy),
		ID:                id,
		OrganizationID:    organizationID,
		PlatformCopy:      conv.ToPGText(platformCopy),
		DecidedGeneration: generation,
	})
	if err != nil {
		return 0, fmt.Errorf("seal waiting access token: %w", err)
	}

	return n, nil
}

func listRemoteSessionAccessTokensSealed(ctx context.Context, q *repo.Queries, organizationID string, after uuid.NullUUID, limit int32) ([]orgencryption.SealedValue, error) {
	rows, err := q.ListRemoteSessionAccessTokensSealed(ctx, repo.ListRemoteSessionAccessTokensSealedParams{OrganizationID: organizationID, After: after, LimitValue: limit})
	if err != nil {
		return nil, fmt.Errorf("list sealed access tokens: %w", err)
	}

	values := make([]orgencryption.SealedValue, 0, len(rows))
	for _, row := range rows {
		values = append(values, orgencryption.SealedValue{RowID: row.ID.String(), Platform: row.PlatformCopy, Org: row.OrgCopy})
	}

	return values, nil
}

func countRemoteSessionAccessTokenCopies(ctx context.Context, q *repo.Queries, organizationID string) (orgencryption.CopyCounts, error) {
	row, err := q.CountRemoteSessionAccessTokenCopies(ctx, organizationID)
	if err != nil {
		return orgencryption.CopyCounts{}, fmt.Errorf("count access token copies: %w", err)
	}

	return orgencryption.CopyCounts{Waiting: row.Waiting, Unreadable: row.Unreadable, Unattributed: row.Unattributed, PlatformCopies: row.PlatformCopies}, nil
}

func clearRemoteSessionAccessTokenPlatformCopies(ctx context.Context, q *repo.Queries, organizationID string, generation int64, limit int32) (int64, error) {
	n, err := q.ClearRemoteSessionAccessTokenPlatformCopies(ctx, repo.ClearRemoteSessionAccessTokenPlatformCopiesParams{OrganizationID: organizationID, LimitValue: limit, DecidedGeneration: generation})
	if err != nil {
		return 0, fmt.Errorf("clear access token platform copies: %w", err)
	}

	return n, nil
}

func listRemoteSessionRefreshTokensWaiting(ctx context.Context, q *repo.Queries, organizationID string, after uuid.NullUUID, limit int32) ([]orgencryption.WaitingValue, error) {
	rows, err := q.ListRemoteSessionRefreshTokensWaiting(ctx, repo.ListRemoteSessionRefreshTokensWaitingParams{OrganizationID: organizationID, After: after, LimitValue: limit})
	if err != nil {
		return nil, fmt.Errorf("list waiting refresh tokens: %w", err)
	}

	values := make([]orgencryption.WaitingValue, 0, len(rows))
	for _, row := range rows {
		values = append(values, orgencryption.WaitingValue{RowID: row.ID.String(), Platform: row.PlatformCopy})
	}

	return values, nil
}

func sealRemoteSessionRefreshTokenWaiting(ctx context.Context, q *repo.Queries, organizationID string, generation int64, id uuid.UUID, platformCopy string, orgCopy string) (int64, error) {
	n, err := q.SealRemoteSessionRefreshTokenWaiting(ctx, repo.SealRemoteSessionRefreshTokenWaitingParams{
		OrgCopy:           conv.ToPGText(orgCopy),
		ID:                id,
		OrganizationID:    organizationID,
		PlatformCopy:      conv.ToPGText(platformCopy),
		DecidedGeneration: generation,
	})
	if err != nil {
		return 0, fmt.Errorf("seal waiting refresh token: %w", err)
	}

	return n, nil
}

func listRemoteSessionRefreshTokensSealed(ctx context.Context, q *repo.Queries, organizationID string, after uuid.NullUUID, limit int32) ([]orgencryption.SealedValue, error) {
	rows, err := q.ListRemoteSessionRefreshTokensSealed(ctx, repo.ListRemoteSessionRefreshTokensSealedParams{OrganizationID: organizationID, After: after, LimitValue: limit})
	if err != nil {
		return nil, fmt.Errorf("list sealed refresh tokens: %w", err)
	}

	values := make([]orgencryption.SealedValue, 0, len(rows))
	for _, row := range rows {
		values = append(values, orgencryption.SealedValue{RowID: row.ID.String(), Platform: row.PlatformCopy, Org: row.OrgCopy})
	}

	return values, nil
}

func countRemoteSessionRefreshTokenCopies(ctx context.Context, q *repo.Queries, organizationID string) (orgencryption.CopyCounts, error) {
	row, err := q.CountRemoteSessionRefreshTokenCopies(ctx, organizationID)
	if err != nil {
		return orgencryption.CopyCounts{}, fmt.Errorf("count refresh token copies: %w", err)
	}

	return orgencryption.CopyCounts{Waiting: row.Waiting, Unreadable: row.Unreadable, Unattributed: row.Unattributed, PlatformCopies: row.PlatformCopies}, nil
}

func clearRemoteSessionRefreshTokenPlatformCopies(ctx context.Context, q *repo.Queries, organizationID string, generation int64, limit int32) (int64, error) {
	n, err := q.ClearRemoteSessionRefreshTokenPlatformCopies(ctx, repo.ClearRemoteSessionRefreshTokenPlatformCopiesParams{OrganizationID: organizationID, LimitValue: limit, DecidedGeneration: generation})
	if err != nil {
		return 0, fmt.Errorf("clear refresh token platform copies: %w", err)
	}

	return n, nil
}

func listEMACredentialAccessTokensWaiting(ctx context.Context, q *repo.Queries, organizationID string, after uuid.NullUUID, limit int32) ([]orgencryption.WaitingValue, error) {
	rows, err := q.ListEMACredentialAccessTokensWaiting(ctx, repo.ListEMACredentialAccessTokensWaitingParams{OrganizationID: organizationID, After: after, LimitValue: limit})
	if err != nil {
		return nil, fmt.Errorf("list waiting EMA access tokens: %w", err)
	}

	values := make([]orgencryption.WaitingValue, 0, len(rows))
	for _, row := range rows {
		values = append(values, orgencryption.WaitingValue{RowID: row.ID.String(), Platform: row.PlatformCopy})
	}

	return values, nil
}

func sealEMACredentialAccessTokenWaiting(ctx context.Context, q *repo.Queries, organizationID string, generation int64, id uuid.UUID, platformCopy string, orgCopy string) (int64, error) {
	n, err := q.SealEMACredentialAccessTokenWaiting(ctx, repo.SealEMACredentialAccessTokenWaitingParams{
		OrgCopy:           conv.ToPGText(orgCopy),
		ID:                id,
		OrganizationID:    organizationID,
		PlatformCopy:      conv.ToPGText(platformCopy),
		DecidedGeneration: generation,
	})
	if err != nil {
		return 0, fmt.Errorf("seal waiting EMA access token: %w", err)
	}

	return n, nil
}

func listEMACredentialAccessTokensSealed(ctx context.Context, q *repo.Queries, organizationID string, after uuid.NullUUID, limit int32) ([]orgencryption.SealedValue, error) {
	rows, err := q.ListEMACredentialAccessTokensSealed(ctx, repo.ListEMACredentialAccessTokensSealedParams{OrganizationID: organizationID, After: after, LimitValue: limit})
	if err != nil {
		return nil, fmt.Errorf("list sealed EMA access tokens: %w", err)
	}

	values := make([]orgencryption.SealedValue, 0, len(rows))
	for _, row := range rows {
		values = append(values, orgencryption.SealedValue{RowID: row.ID.String(), Platform: row.PlatformCopy, Org: row.OrgCopy})
	}

	return values, nil
}

func countEMACredentialAccessTokenCopies(ctx context.Context, q *repo.Queries, organizationID string) (orgencryption.CopyCounts, error) {
	row, err := q.CountEMACredentialAccessTokenCopies(ctx, organizationID)
	if err != nil {
		return orgencryption.CopyCounts{}, fmt.Errorf("count EMA access token copies: %w", err)
	}

	return orgencryption.CopyCounts{Waiting: row.Waiting, Unreadable: row.Unreadable, Unattributed: row.Unattributed, PlatformCopies: row.PlatformCopies}, nil
}

func clearEMACredentialAccessTokenPlatformCopies(ctx context.Context, q *repo.Queries, organizationID string, generation int64, limit int32) (int64, error) {
	n, err := q.ClearEMACredentialAccessTokenPlatformCopies(ctx, repo.ClearEMACredentialAccessTokenPlatformCopiesParams{OrganizationID: organizationID, LimitValue: limit, DecidedGeneration: generation})
	if err != nil {
		return 0, fmt.Errorf("clear EMA access token platform copies: %w", err)
	}

	return n, nil
}
