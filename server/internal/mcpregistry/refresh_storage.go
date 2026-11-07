package mcpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/speakeasy-api/gram/server/internal/mcpregistry/repo"
)

const refreshStorageJSONLimit = 64 << 20

func refreshCheckStoredJSON(ctx context.Context, tx pgx.Tx, data json.RawMessage) error {
	// Bound transport work first, then measure the exact representation used by
	// the database CHECKs. Separator whitespace and numeric expansion mean the
	// compact JSON byte count is not a substitute for canonical storage size.
	if len(data) > refreshStorageJSONLimit {
		return fmt.Errorf("catalog refresh JSON exceeds %d bytes", refreshStorageJSONLimit)
	}
	if !json.Valid(data) {
		return errors.New("catalog refresh JSON is invalid")
	}
	size, err := repo.New(tx).RefreshCanonicalJSONSize(ctx, data)
	if err != nil {
		return fmt.Errorf("measure catalog refresh JSON: %w", err)
	}
	if size > refreshStorageJSONLimit {
		return fmt.Errorf("catalog refresh canonical JSON exceeds %d bytes", refreshStorageJSONLimit)
	}
	return nil
}

func refreshLoadState(ctx context.Context, tx pgx.Tx, source string) (json.RawMessage, error) {
	data, err := repo.New(tx).RefreshLoadState(ctx, source)
	if errors.Is(err, pgx.ErrNoRows) {
		return json.RawMessage(`{}`), nil
	}
	return data, refreshError(err)
}

func refreshSaveState(ctx context.Context, tx pgx.Tx, source string, data json.RawMessage) error {
	if err := refreshCheckStoredJSON(ctx, tx, data); err != nil {
		return refreshError(err)
	}
	return refreshError(repo.New(tx).RefreshSaveState(ctx, repo.RefreshSaveStateParams{SourceIdentity: source, State: data}))
}

// refreshSavePlan serializes admission across all catalog sources. The caller
// must commit or roll back tx; the advisory lock lasts until that boundary.
func refreshSavePlan(ctx context.Context, tx pgx.Tx, id uuid.UUID, source, owner string, expires time.Time, data json.RawMessage) error {
	if err := refreshCheckStoredJSON(ctx, tx, data); err != nil {
		return refreshError(err)
	}
	q := repo.New(tx)
	if err := q.RefreshLockPlans(ctx); err != nil {
		return refreshError(err)
	}
	if err := q.RefreshPrunePlans(ctx); err != nil {
		return refreshError(err)
	}
	n, err := q.RefreshSavePlan(ctx, repo.RefreshSavePlanParams{
		ID: id, SourceIdentity: source, Owner: owner,
		ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true, InfinityModifier: pgtype.Finite}, Data: data,
	})
	if err != nil {
		return refreshError(err)
	}
	if n == 0 {
		return errors.New("catalog refresh active plan limit reached")
	}
	return nil
}

func refreshLoadPlan(ctx context.Context, tx pgx.Tx, id uuid.UUID, source, owner string) (data, receipt json.RawMessage, expires time.Time, err error) {
	row, err := repo.New(tx).RefreshLoadPlan(ctx, repo.RefreshLoadPlanParams{ID: id, SourceIdentity: source, Owner: owner})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, time.Time{}, ErrNotFound
	}
	if err != nil {
		return nil, nil, time.Time{}, refreshError(err)
	}
	return row.Data, row.Receipt, row.ExpiresAt.Time, nil
}

func refreshSaveReceipt(ctx context.Context, tx pgx.Tx, id uuid.UUID, receipt json.RawMessage) error {
	if err := refreshCheckStoredJSON(ctx, tx, receipt); err != nil {
		return refreshError(err)
	}
	q := repo.New(tx)
	if err := q.RefreshLockPlans(ctx); err != nil {
		return refreshError(err)
	}
	n, err := q.RefreshSaveReceipt(ctx, repo.RefreshSaveReceiptParams{ID: id, Receipt: receipt})
	if err != nil {
		return refreshError(err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return refreshError(q.RefreshPrunePlans(ctx))
}
