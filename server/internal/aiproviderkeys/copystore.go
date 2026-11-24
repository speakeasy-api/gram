package aiproviderkeys

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/aiproviderkeys/repo"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/orgencryption"
)

// copyQueries are one table's CopyStore queries. A row id is the row's uuid
// formatted with uuid.UUID.String, which matches the id::text the count
// queries join organization_encryption_unreadable_values on, or the key type
// of an openrouter_api_keys row.
type copyQueries struct {
	listWaiting func(ctx context.Context, q *repo.Queries, organizationID string, after string, limit int32) ([]orgencryption.WaitingValue, error)
	sealWaiting func(ctx context.Context, q *repo.Queries, organizationID string, generation int64, rowID, platform, org string) (int64, error)
	listSealed  func(ctx context.Context, q *repo.Queries, organizationID string, after string, limit int32) ([]orgencryption.SealedValue, error)
	count       func(ctx context.Context, q *repo.Queries, organizationID string) (orgencryption.CopyCounts, error)
	clear       func(ctx context.Context, q *repo.Queries, organizationID string, generation int64, limit int32) (orgencryption.ClearedCopies, error)
}

// CopyStore is how the orgencryption backfill, verification and finalize
// reach the two copies of one table's keys.
type CopyStore struct {
	db      *pgxpool.Pool
	columns orgencryption.CopyColumns
	queries copyQueries
}

var _ orgencryption.CopyStore = (*CopyStore)(nil)

// CopyStores returns the copy stores of every table, which the
// ai-provider-keys use case backfills, verifies and clears together.
func CopyStores(db *pgxpool.Pool) []orgencryption.CopyStore {
	return []orgencryption.CopyStore{
		&CopyStore{db: db, columns: AIIntegrationConfigs, queries: aiIntegrationQueries},
		&CopyStore{db: db, columns: ModelProviderKeys, queries: modelProviderQueries},
		&CopyStore{db: db, columns: OpenRouterAPIKeys, queries: openRouterQueries},
	}
}

func (c *CopyStore) Columns() orgencryption.CopyColumns {
	return c.columns
}

func (c *CopyStore) ListWaiting(ctx context.Context, organizationID string, after string, limit int32) ([]orgencryption.WaitingValue, error) {
	values, err := c.queries.listWaiting(ctx, repo.New(c.db), organizationID, after, limit)
	if err != nil {
		return nil, fmt.Errorf("list waiting %s: %w", c.columns.PlatformColumn, err)
	}

	return values, nil
}

func (c *CopyStore) SealWaiting(ctx context.Context, organizationID string, generation int64, value orgencryption.WaitingValue, orgCiphertext string) (bool, error) {
	landed, err := c.queries.sealWaiting(ctx, repo.New(c.db), organizationID, generation, value.RowID, value.Platform, orgCiphertext)
	if err != nil {
		return false, fmt.Errorf("seal waiting %s: %w", c.columns.PlatformColumn, err)
	}

	return landed == 1, nil
}

func (c *CopyStore) ListSealed(ctx context.Context, organizationID string, after string, limit int32) ([]orgencryption.SealedValue, error) {
	values, err := c.queries.listSealed(ctx, repo.New(c.db), organizationID, after, limit)
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

func (c *CopyStore) ClearPlatformCopies(ctx context.Context, organizationID string, generation int64, limit int32) (orgencryption.ClearedCopies, error) {
	cleared, err := c.queries.clear(ctx, repo.New(c.db), organizationID, generation, limit)
	if err != nil {
		return orgencryption.ClearedCopies{}, fmt.Errorf("clear %s: %w", c.columns.PlatformColumn, err)
	}

	return cleared, nil
}

func uuidAfter(after string) (uuid.NullUUID, error) {
	if after == "" {
		return uuid.NullUUID{UUID: uuid.Nil, Valid: false}, nil
	}

	id, err := uuid.Parse(after)
	if err != nil {
		return uuid.NullUUID{}, fmt.Errorf("parse page cursor %q: %w", after, err)
	}

	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

func textAfter(after string) pgtype.Text {
	return pgtype.Text{String: after, Valid: after != ""}
}

func waiting[R any](rows []R, get func(R) (string, string)) []orgencryption.WaitingValue {
	values := make([]orgencryption.WaitingValue, 0, len(rows))
	for _, row := range rows {
		id, platform := get(row)
		values = append(values, orgencryption.WaitingValue{RowID: id, Platform: platform})
	}
	return values
}

func sealed[R any](rows []R, get func(R) (string, string, string)) []orgencryption.SealedValue {
	values := make([]orgencryption.SealedValue, 0, len(rows))
	for _, row := range rows {
		id, platform, org := get(row)
		values = append(values, orgencryption.SealedValue{RowID: id, Platform: platform, Org: org})
	}
	return values
}

func counts(waiting, unreadable, platformCopies int64) orgencryption.CopyCounts {
	return orgencryption.CopyCounts{Waiting: waiting, Unreadable: unreadable, Unattributed: 0, PlatformCopies: platformCopies}
}

var aiIntegrationQueries = copyQueries{
	listWaiting: func(ctx context.Context, q *repo.Queries, organizationID string, after string, limit int32) ([]orgencryption.WaitingValue, error) {
		afterID, err := uuidAfter(after)
		if err != nil {
			return nil, err
		}
		rows, err := q.ListAIIntegrationConfigAPIKeyWaiting(ctx, repo.ListAIIntegrationConfigAPIKeyWaitingParams{OrganizationID: organizationID, After: afterID, LimitValue: limit})
		return waiting(rows, func(r repo.ListAIIntegrationConfigAPIKeyWaitingRow) (string, string) {
			return r.ID.String(), r.PlatformCopy
		}), err
	},
	sealWaiting: func(ctx context.Context, q *repo.Queries, organizationID string, generation int64, rowID, platform, org string) (int64, error) {
		id, err := uuid.Parse(rowID)
		if err != nil {
			return 0, fmt.Errorf("parse row id %q: %w", rowID, err)
		}
		return q.SealAIIntegrationConfigAPIKeyWaiting(ctx, repo.SealAIIntegrationConfigAPIKeyWaitingParams{OrgCopy: conv.ToPGText(org), ID: id, OrganizationID: organizationID, PlatformCopy: conv.ToPGText(platform), DecidedGeneration: generation})
	},
	listSealed: func(ctx context.Context, q *repo.Queries, organizationID string, after string, limit int32) ([]orgencryption.SealedValue, error) {
		afterID, err := uuidAfter(after)
		if err != nil {
			return nil, err
		}
		rows, err := q.ListAIIntegrationConfigAPIKeySealed(ctx, repo.ListAIIntegrationConfigAPIKeySealedParams{OrganizationID: organizationID, After: afterID, LimitValue: limit})
		return sealed(rows, func(r repo.ListAIIntegrationConfigAPIKeySealedRow) (string, string, string) {
			return r.ID.String(), r.PlatformCopy, r.OrgCopy
		}), err
	},
	count: func(ctx context.Context, q *repo.Queries, organizationID string) (orgencryption.CopyCounts, error) {
		row, err := q.CountAIIntegrationConfigAPIKeyCopies(ctx, organizationID)
		return counts(row.Waiting, row.Unreadable, row.PlatformCopies), err
	},
	clear: func(ctx context.Context, q *repo.Queries, organizationID string, generation int64, limit int32) (orgencryption.ClearedCopies, error) {
		row, err := q.ClearAIIntegrationConfigAPIKeyPlatformCopies(ctx, repo.ClearAIIntegrationConfigAPIKeyPlatformCopiesParams{OrganizationID: organizationID, LimitValue: limit, DecidedGeneration: generation})
		if err != nil {
			return orgencryption.ClearedCopies{}, fmt.Errorf("clear AI integration config API key platform copies: %w", err)
		}
		return orgencryption.ClearedCopies{PlatformOnly: row.PlatformOnly, BesideOrgCopy: row.BesideOrgCopy, Empty: 0}, nil
	},
}

var modelProviderQueries = copyQueries{
	listWaiting: func(ctx context.Context, q *repo.Queries, organizationID string, after string, limit int32) ([]orgencryption.WaitingValue, error) {
		afterID, err := uuidAfter(after)
		if err != nil {
			return nil, err
		}
		rows, err := q.ListModelProviderKeyWaiting(ctx, repo.ListModelProviderKeyWaitingParams{OrganizationID: organizationID, After: afterID, LimitValue: limit})
		return waiting(rows, func(r repo.ListModelProviderKeyWaitingRow) (string, string) {
			return r.ID.String(), r.PlatformCopy
		}), err
	},
	sealWaiting: func(ctx context.Context, q *repo.Queries, organizationID string, generation int64, rowID, platform, org string) (int64, error) {
		id, err := uuid.Parse(rowID)
		if err != nil {
			return 0, fmt.Errorf("parse row id %q: %w", rowID, err)
		}
		return q.SealModelProviderKeyWaiting(ctx, repo.SealModelProviderKeyWaitingParams{OrgCopy: conv.ToPGText(org), ID: id, OrganizationID: organizationID, PlatformCopy: conv.ToPGText(platform), DecidedGeneration: generation})
	},
	listSealed: func(ctx context.Context, q *repo.Queries, organizationID string, after string, limit int32) ([]orgencryption.SealedValue, error) {
		afterID, err := uuidAfter(after)
		if err != nil {
			return nil, err
		}
		rows, err := q.ListModelProviderKeySealed(ctx, repo.ListModelProviderKeySealedParams{OrganizationID: organizationID, After: afterID, LimitValue: limit})
		return sealed(rows, func(r repo.ListModelProviderKeySealedRow) (string, string, string) {
			return r.ID.String(), r.PlatformCopy, r.OrgCopy
		}), err
	},
	count: func(ctx context.Context, q *repo.Queries, organizationID string) (orgencryption.CopyCounts, error) {
		row, err := q.CountModelProviderKeyCopies(ctx, organizationID)
		return counts(row.Waiting, row.Unreadable, row.PlatformCopies), err
	},
	clear: func(ctx context.Context, q *repo.Queries, organizationID string, generation int64, limit int32) (orgencryption.ClearedCopies, error) {
		row, err := q.ClearModelProviderKeyPlatformCopies(ctx, repo.ClearModelProviderKeyPlatformCopiesParams{OrganizationID: organizationID, LimitValue: limit, DecidedGeneration: generation})
		if err != nil {
			return orgencryption.ClearedCopies{}, fmt.Errorf("clear model provider key platform copies: %w", err)
		}
		return orgencryption.ClearedCopies{PlatformOnly: row.PlatformOnly, BesideOrgCopy: row.BesideOrgCopy, Empty: 0}, nil
	},
}

var openRouterQueries = copyQueries{
	listWaiting: func(ctx context.Context, q *repo.Queries, organizationID string, after string, limit int32) ([]orgencryption.WaitingValue, error) {
		rows, err := q.ListOpenRouterAPIKeyWaiting(ctx, repo.ListOpenRouterAPIKeyWaitingParams{OrganizationID: organizationID, After: textAfter(after), LimitValue: limit})
		return waiting(rows, func(r repo.ListOpenRouterAPIKeyWaitingRow) (string, string) {
			return r.KeyType, r.PlatformCopy
		}), err
	},
	sealWaiting: func(ctx context.Context, q *repo.Queries, organizationID string, generation int64, rowID, platform, org string) (int64, error) {
		return q.SealOpenRouterAPIKeyWaiting(ctx, repo.SealOpenRouterAPIKeyWaitingParams{OrgCopy: conv.ToPGText(org), ID: rowID, OrganizationID: organizationID, PlatformCopy: conv.ToPGText(platform), DecidedGeneration: generation})
	},
	listSealed: func(ctx context.Context, q *repo.Queries, organizationID string, after string, limit int32) ([]orgencryption.SealedValue, error) {
		rows, err := q.ListOpenRouterAPIKeySealed(ctx, repo.ListOpenRouterAPIKeySealedParams{OrganizationID: organizationID, After: textAfter(after), LimitValue: limit})
		return sealed(rows, func(r repo.ListOpenRouterAPIKeySealedRow) (string, string, string) {
			return r.KeyType, r.PlatformCopy, r.OrgCopy
		}), err
	},
	count: func(ctx context.Context, q *repo.Queries, organizationID string) (orgencryption.CopyCounts, error) {
		row, err := q.CountOpenRouterAPIKeyCopies(ctx, organizationID)
		return counts(row.Waiting, row.Unreadable, row.PlatformCopies), err
	},
	clear: func(ctx context.Context, q *repo.Queries, organizationID string, generation int64, limit int32) (orgencryption.ClearedCopies, error) {
		row, err := q.ClearOpenRouterAPIKeyPlatformCopies(ctx, repo.ClearOpenRouterAPIKeyPlatformCopiesParams{OrganizationID: organizationID, LimitValue: limit, DecidedGeneration: generation})
		if err != nil {
			return orgencryption.ClearedCopies{}, fmt.Errorf("clear OpenRouter API key platform copies: %w", err)
		}
		return orgencryption.ClearedCopies{PlatformOnly: row.PlatformOnly, BesideOrgCopy: row.BesideOrgCopy, Empty: 0}, nil
	},
}
