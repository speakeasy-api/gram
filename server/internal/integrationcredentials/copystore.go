package integrationcredentials

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/integrationcredentials/repo"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

// copyQueries are one table's CopyStore queries. Row ids are the row's uuid
// formatted with uuid.UUID.String, which matches the id::text the count
// queries join organization_encryption_unreadable_values on.
type copyQueries struct {
	listWaiting func(ctx context.Context, q *repo.Queries, organizationID string, after uuid.NullUUID, limit int32) ([]orgencryption.WaitingValue, error)
	sealWaiting func(ctx context.Context, q *repo.Queries, organizationID string, generation int64, id uuid.UUID, platform, org string) (int64, error)
	listSealed  func(ctx context.Context, q *repo.Queries, organizationID string, after uuid.NullUUID, limit int32) ([]orgencryption.SealedValue, error)
	count       func(ctx context.Context, q *repo.Queries, organizationID string) (orgencryption.CopyCounts, error)
	clear       func(ctx context.Context, q *repo.Queries, organizationID string, generation int64, limit int32) (int64, error)
}

// CopyStore is how the orgencryption backfill, verification and finalize
// reach the two copies of one integration's credentials.
type CopyStore struct {
	db      *pgxpool.Pool
	columns orgencryption.CopyColumns
	queries copyQueries
}

var _ orgencryption.CopyStore = (*CopyStore)(nil)

// CopyStores returns the copy stores of every integration, which the
// integration-credentials use case backfills, verifies and clears together.
func CopyStores(db *pgxpool.Pool) []orgencryption.CopyStore {
	return []orgencryption.CopyStore{
		&CopyStore{db: db, columns: SlackDirectoryConnections, queries: slackQueries},
		&CopyStore{db: db, columns: DeviceIntegrationConfigs, queries: deviceQueries},
		&CopyStore{db: db, columns: NetworkIngresses, queries: ingressQueries},
	}
}

func (c *CopyStore) Columns() orgencryption.CopyColumns {
	return c.columns
}

func (c *CopyStore) ListWaiting(ctx context.Context, organizationID string, after string, limit int32) ([]orgencryption.WaitingValue, error) {
	afterID, err := parseAfter(after)
	if err != nil {
		return nil, err
	}

	values, err := c.queries.listWaiting(ctx, repo.New(c.db), organizationID, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list waiting %s: %w", c.columns.PlatformColumn, err)
	}

	return values, nil
}

func (c *CopyStore) SealWaiting(ctx context.Context, organizationID string, generation int64, value orgencryption.WaitingValue, orgCiphertext string) (bool, error) {
	id, err := uuid.Parse(value.RowID)
	if err != nil {
		return false, fmt.Errorf("parse row id %q: %w", value.RowID, err)
	}

	landed, err := c.queries.sealWaiting(ctx, repo.New(c.db), organizationID, generation, id, value.Platform, orgCiphertext)
	if err != nil {
		return false, fmt.Errorf("seal waiting %s: %w", c.columns.PlatformColumn, err)
	}

	return landed == 1, nil
}

func (c *CopyStore) ListSealed(ctx context.Context, organizationID string, after string, limit int32) ([]orgencryption.SealedValue, error) {
	afterID, err := parseAfter(after)
	if err != nil {
		return nil, err
	}

	values, err := c.queries.listSealed(ctx, repo.New(c.db), organizationID, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list sealed %s: %w", c.columns.PlatformColumn, err)
	}

	return values, nil
}

func (c *CopyStore) Count(ctx context.Context, organizationID string) (orgencryption.CopyCounts, error) {
	counts, err := c.queries.count(ctx, repo.New(c.db), organizationID)
	if err != nil {
		return orgencryption.CopyCounts{}, fmt.Errorf("count %s copies: %w", c.columns.PlatformColumn, err)
	}

	return counts, nil
}

func (c *CopyStore) ClearPlatformCopies(ctx context.Context, organizationID string, generation int64, limit int32) (int64, error) {
	cleared, err := c.queries.clear(ctx, repo.New(c.db), organizationID, generation, limit)
	if err != nil {
		return 0, fmt.Errorf("clear %s: %w", c.columns.PlatformColumn, err)
	}

	return cleared, nil
}

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

func waiting[R any](rows []R, get func(R) (uuid.UUID, string)) []orgencryption.WaitingValue {
	values := make([]orgencryption.WaitingValue, 0, len(rows))
	for _, row := range rows {
		id, platform := get(row)
		values = append(values, orgencryption.WaitingValue{RowID: id.String(), Platform: platform})
	}
	return values
}

func sealed[R any](rows []R, get func(R) (uuid.UUID, string, string)) []orgencryption.SealedValue {
	values := make([]orgencryption.SealedValue, 0, len(rows))
	for _, row := range rows {
		id, platform, org := get(row)
		values = append(values, orgencryption.SealedValue{RowID: id.String(), Platform: platform, Org: org})
	}
	return values
}

func counts(waiting, unreadable, platformCopies int64) orgencryption.CopyCounts {
	return orgencryption.CopyCounts{Waiting: waiting, Unreadable: unreadable, Unattributed: 0, PlatformCopies: platformCopies}
}

var slackQueries = copyQueries{
	listWaiting: func(ctx context.Context, q *repo.Queries, organizationID string, after uuid.NullUUID, limit int32) ([]orgencryption.WaitingValue, error) {
		rows, err := q.ListSlackDirectoryConnectionCredentialsWaiting(ctx, repo.ListSlackDirectoryConnectionCredentialsWaitingParams{OrganizationID: organizationID, After: after, LimitValue: limit})
		return waiting(rows, func(r repo.ListSlackDirectoryConnectionCredentialsWaitingRow) (uuid.UUID, string) {
			return r.ID, r.PlatformCopy
		}), err
	},
	sealWaiting: func(ctx context.Context, q *repo.Queries, organizationID string, generation int64, id uuid.UUID, platform, org string) (int64, error) {
		return q.SealSlackDirectoryConnectionCredentialsWaiting(ctx, repo.SealSlackDirectoryConnectionCredentialsWaitingParams{OrgCopy: conv.ToPGText(org), ID: id, OrganizationID: organizationID, PlatformCopy: conv.ToPGText(platform), DecidedGeneration: generation})
	},
	listSealed: func(ctx context.Context, q *repo.Queries, organizationID string, after uuid.NullUUID, limit int32) ([]orgencryption.SealedValue, error) {
		rows, err := q.ListSlackDirectoryConnectionCredentialsSealed(ctx, repo.ListSlackDirectoryConnectionCredentialsSealedParams{OrganizationID: organizationID, After: after, LimitValue: limit})
		return sealed(rows, func(r repo.ListSlackDirectoryConnectionCredentialsSealedRow) (uuid.UUID, string, string) {
			return r.ID, r.PlatformCopy, r.OrgCopy
		}), err
	},
	count: func(ctx context.Context, q *repo.Queries, organizationID string) (orgencryption.CopyCounts, error) {
		row, err := q.CountSlackDirectoryConnectionCredentialCopies(ctx, organizationID)
		return counts(row.Waiting, row.Unreadable, row.PlatformCopies), err
	},
	clear: func(ctx context.Context, q *repo.Queries, organizationID string, generation int64, limit int32) (int64, error) {
		return q.ClearSlackDirectoryConnectionCredentialPlatformCopies(ctx, repo.ClearSlackDirectoryConnectionCredentialPlatformCopiesParams{OrganizationID: organizationID, LimitValue: limit, DecidedGeneration: generation})
	},
}

var deviceQueries = copyQueries{
	listWaiting: func(ctx context.Context, q *repo.Queries, organizationID string, after uuid.NullUUID, limit int32) ([]orgencryption.WaitingValue, error) {
		rows, err := q.ListDeviceIntegrationConfigCredentialsWaiting(ctx, repo.ListDeviceIntegrationConfigCredentialsWaitingParams{OrganizationID: organizationID, After: after, LimitValue: limit})
		return waiting(rows, func(r repo.ListDeviceIntegrationConfigCredentialsWaitingRow) (uuid.UUID, string) {
			return r.ID, r.PlatformCopy
		}), err
	},
	sealWaiting: func(ctx context.Context, q *repo.Queries, organizationID string, generation int64, id uuid.UUID, platform, org string) (int64, error) {
		return q.SealDeviceIntegrationConfigCredentialsWaiting(ctx, repo.SealDeviceIntegrationConfigCredentialsWaitingParams{OrgCopy: conv.ToPGText(org), ID: id, OrganizationID: organizationID, PlatformCopy: conv.ToPGText(platform), DecidedGeneration: generation})
	},
	listSealed: func(ctx context.Context, q *repo.Queries, organizationID string, after uuid.NullUUID, limit int32) ([]orgencryption.SealedValue, error) {
		rows, err := q.ListDeviceIntegrationConfigCredentialsSealed(ctx, repo.ListDeviceIntegrationConfigCredentialsSealedParams{OrganizationID: organizationID, After: after, LimitValue: limit})
		return sealed(rows, func(r repo.ListDeviceIntegrationConfigCredentialsSealedRow) (uuid.UUID, string, string) {
			return r.ID, r.PlatformCopy, r.OrgCopy
		}), err
	},
	count: func(ctx context.Context, q *repo.Queries, organizationID string) (orgencryption.CopyCounts, error) {
		row, err := q.CountDeviceIntegrationConfigCredentialCopies(ctx, organizationID)
		return counts(row.Waiting, row.Unreadable, row.PlatformCopies), err
	},
	clear: func(ctx context.Context, q *repo.Queries, organizationID string, generation int64, limit int32) (int64, error) {
		return q.ClearDeviceIntegrationConfigCredentialPlatformCopies(ctx, repo.ClearDeviceIntegrationConfigCredentialPlatformCopiesParams{OrganizationID: organizationID, LimitValue: limit, DecidedGeneration: generation})
	},
}

var ingressQueries = copyQueries{
	listWaiting: func(ctx context.Context, q *repo.Queries, organizationID string, after uuid.NullUUID, limit int32) ([]orgencryption.WaitingValue, error) {
		rows, err := q.ListNetworkIngressCredentialsWaiting(ctx, repo.ListNetworkIngressCredentialsWaitingParams{OrganizationID: organizationID, After: after, LimitValue: limit})
		return waiting(rows, func(r repo.ListNetworkIngressCredentialsWaitingRow) (uuid.UUID, string) { return r.ID, r.PlatformCopy }), err
	},
	sealWaiting: func(ctx context.Context, q *repo.Queries, organizationID string, generation int64, id uuid.UUID, platform, org string) (int64, error) {
		return q.SealNetworkIngressCredentialsWaiting(ctx, repo.SealNetworkIngressCredentialsWaitingParams{OrgCopy: conv.ToPGText(org), ID: id, OrganizationID: organizationID, PlatformCopy: conv.ToPGText(platform), DecidedGeneration: generation})
	},
	listSealed: func(ctx context.Context, q *repo.Queries, organizationID string, after uuid.NullUUID, limit int32) ([]orgencryption.SealedValue, error) {
		rows, err := q.ListNetworkIngressCredentialsSealed(ctx, repo.ListNetworkIngressCredentialsSealedParams{OrganizationID: organizationID, After: after, LimitValue: limit})
		return sealed(rows, func(r repo.ListNetworkIngressCredentialsSealedRow) (uuid.UUID, string, string) {
			return r.ID, r.PlatformCopy, r.OrgCopy
		}), err
	},
	count: func(ctx context.Context, q *repo.Queries, organizationID string) (orgencryption.CopyCounts, error) {
		row, err := q.CountNetworkIngressCredentialCopies(ctx, organizationID)
		return counts(row.Waiting, row.Unreadable, row.PlatformCopies), err
	},
	clear: func(ctx context.Context, q *repo.Queries, organizationID string, generation int64, limit int32) (int64, error) {
		return q.ClearNetworkIngressCredentialPlatformCopies(ctx, repo.ClearNetworkIngressCredentialPlatformCopiesParams{OrganizationID: organizationID, LimitValue: limit, DecidedGeneration: generation})
	},
}
