package runnerkeys

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/functions/runnerkeys/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

// CopyStore is how the orgencryption backfill, verification and finalize
// reach the two copies of function runner keys. Row ids are the
// functions_access row's uuid formatted with uuid.UUID.String, which matches
// the id::text the count query joins organization_encryption_unreadable_values
// on. The platform-key copy is a bytea column, so its value is the raw bytes.
type CopyStore struct {
	db *pgxpool.Pool
}

var _ orgencryption.CopyStore = (*CopyStore)(nil)

// NewCopyStore builds the function runner key CopyStore.
func NewCopyStore(db *pgxpool.Pool) *CopyStore {
	return &CopyStore{db: db}
}

// Columns names the key's two copies.
func (c *CopyStore) Columns() orgencryption.CopyColumns {
	return Columns
}

// ListWaiting returns the organization's next page of keys with only a
// platform-key copy.
func (c *CopyStore) ListWaiting(ctx context.Context, organizationID string, after string, limit int32) ([]orgencryption.WaitingValue, error) {
	afterID, err := parseAfter(after)
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(c.db).ListRunnerKeysWaiting(ctx, repo.ListRunnerKeysWaitingParams{
		OrganizationID: organizationID,
		After:          afterID,
		LimitValue:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list waiting function runner keys: %w", err)
	}

	values := make([]orgencryption.WaitingValue, 0, len(rows))
	for _, row := range rows {
		values = append(values, orgencryption.WaitingValue{RowID: row.ID.String(), Platform: string(row.EncryptionKey.Reveal())})
	}

	return values, nil
}

// SealWaiting stores the backfill's organization-key copy of one key.
func (c *CopyStore) SealWaiting(ctx context.Context, organizationID string, generation int64, value orgencryption.WaitingValue, orgCiphertext string) (bool, error) {
	id, err := uuid.Parse(value.RowID)
	if err != nil {
		return false, fmt.Errorf("parse function runner key id %q: %w", value.RowID, err)
	}

	landed, err := repo.New(c.db).SealRunnerKeyWaiting(ctx, repo.SealRunnerKeyWaitingParams{
		OrgCopy:           conv.ToPGText(orgCiphertext),
		ID:                id,
		OrganizationID:    organizationID,
		PlatformCopy:      []byte(value.Platform),
		DecidedGeneration: generation,
	})
	if err != nil {
		return false, fmt.Errorf("seal waiting function runner key: %w", err)
	}

	return landed == 1, nil
}

// ListSealed returns the organization's next page of keys with both copies.
func (c *CopyStore) ListSealed(ctx context.Context, organizationID string, after string, limit int32) ([]orgencryption.SealedValue, error) {
	afterID, err := parseAfter(after)
	if err != nil {
		return nil, err
	}

	rows, err := repo.New(c.db).ListRunnerKeysSealed(ctx, repo.ListRunnerKeysSealedParams{
		OrganizationID: organizationID,
		After:          afterID,
		LimitValue:     limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list sealed function runner keys: %w", err)
	}

	values := make([]orgencryption.SealedValue, 0, len(rows))
	for _, row := range rows {
		values = append(values, orgencryption.SealedValue{RowID: row.ID.String(), Platform: string(row.EncryptionKey.Reveal()), Org: row.OrgCopy})
	}

	return values, nil
}

// Count returns the organization's function runner key counts.
func (c *CopyStore) Count(ctx context.Context, organizationID string) (orgencryption.CopyCounts, error) {
	row, err := repo.New(c.db).CountRunnerKeyCopies(ctx, organizationID)
	if err != nil {
		return orgencryption.CopyCounts{}, fmt.Errorf("count function runner key copies: %w", err)
	}

	return orgencryption.CopyCounts{
		Waiting:        row.Waiting,
		Unreadable:     row.Unreadable,
		Unattributed:   row.Unattributed,
		PlatformCopies: row.PlatformCopies,
	}, nil
}

// ClearPlatformCopies destroys up to limit of the organization's platform-key
// copies after finalize.
func (c *CopyStore) ClearPlatformCopies(ctx context.Context, organizationID string, generation int64, limit int32) (orgencryption.ClearedCopies, error) {
	cleared, err := repo.New(c.db).ClearRunnerKeyPlatformCopies(ctx, repo.ClearRunnerKeyPlatformCopiesParams{
		OrganizationID:    organizationID,
		LimitValue:        limit,
		DecidedGeneration: generation,
	})
	if err != nil {
		return orgencryption.ClearedCopies{}, fmt.Errorf("clear function runner key platform copies: %w", err)
	}

	return orgencryption.ClearedCopies{PlatformOnly: cleared.PlatformOnly, BesideOrgCopy: cleared.BesideOrgCopy, Empty: 0}, nil
}

// parseAfter parses a page cursor: "" for the first page, otherwise the RowID
// of the previous page's last value. The first page pages after the nil UUID,
// which sorts before every row id, so the queries always compare against a
// cursor and keep one plan.
func parseAfter(after string) (uuid.UUID, error) {
	if after == "" {
		return uuid.Nil, nil
	}

	id, err := uuid.Parse(after)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse page cursor %q: %w", after, err)
	}

	return id, nil
}
