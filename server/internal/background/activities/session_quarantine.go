package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/sessionquarantine"
)

const sessionQuarantineReassertBatchSize int32 = 500

type SessionQuarantineReassert struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	cache  cache.Cache
}

func NewSessionQuarantineReassert(logger *slog.Logger, db *pgxpool.Pool, cacheAdapter cache.Cache) *SessionQuarantineReassert {
	return &SessionQuarantineReassert{
		logger: logger.With(attr.SlogComponent("session_quarantine_reassert")),
		db:     db,
		cache:  cacheAdapter,
	}
}

func (a *SessionQuarantineReassert) Do(ctx context.Context) error {
	queries := riskrepo.New(a.db)
	var afterCreatedAt pgtype.Timestamptz
	var afterID uuid.NullUUID
	var reassertErr error
	for {
		rows, err := queries.ListActiveSessionQuarantinesPage(ctx, riskrepo.ListActiveSessionQuarantinesPageParams{
			AfterCreatedAt: afterCreatedAt,
			AfterID:        afterID,
			PageLimit:      sessionQuarantineReassertBatchSize,
		})
		if err != nil {
			return fmt.Errorf("list active session quarantine page: %w", err)
		}

		for _, row := range rows {
			if err := sessionquarantine.Write(ctx, a.cache, sessionquarantine.FromRow(row)); err != nil {
				reassertErr = errors.Join(reassertErr, fmt.Errorf("reassert session quarantine %s: %w", row.ID, err))
			}
		}
		if len(rows) < int(sessionQuarantineReassertBatchSize) {
			break
		}

		last := rows[len(rows)-1]
		afterCreatedAt = last.CreatedAt
		afterID = uuid.NullUUID{UUID: last.ID, Valid: true}
	}
	if reassertErr != nil {
		return reassertErr
	}

	a.logger.DebugContext(ctx, "reasserted session quarantine circuits")
	return nil
}
