package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/cache"
	riskrepo "github.com/speakeasy-api/gram/server/internal/risk/repo"
	"github.com/speakeasy-api/gram/server/internal/sessionquarantine"
)

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
	rows, err := riskrepo.New(a.db).ListAllActiveSessionQuarantines(ctx)
	if err != nil {
		return fmt.Errorf("list active session quarantines: %w", err)
	}

	var reassertErr error
	for _, row := range rows {
		if err := sessionquarantine.Write(ctx, a.cache, sessionquarantine.FromRow(row)); err != nil {
			reassertErr = errors.Join(reassertErr, fmt.Errorf("reassert session quarantine %s: %w", row.ID, err))
		}
	}
	if reassertErr != nil {
		return reassertErr
	}

	a.logger.DebugContext(ctx, "reasserted session quarantine circuits")
	return nil
}
