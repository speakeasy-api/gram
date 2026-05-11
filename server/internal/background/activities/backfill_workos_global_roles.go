package activities

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/oops"
)

type BackfillWorkOSGlobalRoles struct {
	logger *slog.Logger
	db     *pgxpool.Pool
	workos WorkOSBackfillClient
}

func NewBackfillWorkOSGlobalRoles(logger *slog.Logger, db *pgxpool.Pool, workosClient WorkOSBackfillClient) *BackfillWorkOSGlobalRoles {
	return &BackfillWorkOSGlobalRoles{
		logger: logger.With(attr.SlogComponent("backfill_workos_global_roles")),
		db:     db,
		workos: workosClient,
	}
}

func (b *BackfillWorkOSGlobalRoles) Do(ctx context.Context) error {
	roles, err := b.workos.ListGlobalRoles(ctx)
	if err != nil {
		return oops.E(oops.CodeUnexpected, err, "list WorkOS global roles").Log(ctx, b.logger)
	}

	tx, err := b.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	repo := accessrepo.New(tx)
	snapshotSlugs := make(map[string]time.Time, len(roles))
	for _, role := range roles {
		createdAt, err := parseWorkOSTime(role.CreatedAt)
		if err != nil {
			return fmt.Errorf("parse global role %q created_at: %w", role.Slug, err)
		}
		updatedAt, err := parseWorkOSTime(role.UpdatedAt)
		if err != nil {
			return fmt.Errorf("parse global role %q updated_at: %w", role.Slug, err)
		}
		snapshotSlugs[role.Slug] = updatedAt

		existing, err := repo.GetGlobalRoleBySlug(ctx, role.Slug)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("get global role %q: %w", role.Slug, err)
		}

		var lastEventID *string
		if existing.WorkosLastEventID.Valid {
			lastEventID = &existing.WorkosLastEventID.String
		}
		var rowUpdatedAt *time.Time
		if existing.WorkosUpdatedAt.Valid {
			rowUpdatedAt = &existing.WorkosUpdatedAt.Time
		}
		if !ShouldProcessEvent(lastEventID, rowUpdatedAt, "", updatedAt) {
			continue
		}

		if err := repo.UpsertGlobalRole(ctx, accessrepo.UpsertGlobalRoleParams{
			WorkosSlug:        role.Slug,
			WorkosName:        role.Name,
			WorkosDescription: conv.ToPGTextEmpty(role.Description),
			WorkosCreatedAt:   conv.ToPGTimestamptz(createdAt),
			WorkosUpdatedAt:   conv.ToPGTimestamptz(updatedAt),
			WorkosLastEventID: conv.ToPGText(""),
		}); err != nil {
			return fmt.Errorf("upsert global role %q: %w", role.Slug, err)
		}
	}

	localRoles, err := repo.ListGlobalRoles(ctx)
	if err != nil {
		return fmt.Errorf("list local global roles: %w", err)
	}
	for _, localRole := range localRoles {
		if _, ok := snapshotSlugs[localRole.WorkosSlug]; ok {
			continue
		}

		var lastEventID *string
		if localRole.WorkosLastEventID.Valid {
			lastEventID = &localRole.WorkosLastEventID.String
		}
		var rowUpdatedAt *time.Time
		if localRole.WorkosUpdatedAt.Valid {
			rowUpdatedAt = &localRole.WorkosUpdatedAt.Time
		}
		deletedAt := time.Now().UTC()
		if !ShouldProcessEvent(lastEventID, rowUpdatedAt, "", deletedAt) {
			continue
		}

		if _, err := repo.MarkGlobalRoleDeleted(ctx, accessrepo.MarkGlobalRoleDeletedParams{
			WorkosSlug:        localRole.WorkosSlug,
			WorkosDeletedAt:   conv.ToPGTimestamptz(deletedAt),
			WorkosLastEventID: conv.ToPGText(""),
		}); err != nil {
			return fmt.Errorf("mark global role %q deleted: %w", localRole.WorkosSlug, err)
		}
		b.logger.DebugContext(ctx, "soft-deleted WorkOS global role missing from snapshot", attr.SlogAccessRoleSlug(localRole.WorkosSlug))
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}
