package directory

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	workosrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/workos/repo"
)

// OrganizationSyncSnapshot fences inventory fetched outside the event transaction.
// Capture it before any inventory requests, then validate inside the transaction
// that persists the complete inventory. The lock is held until that transaction ends.
type OrganizationSyncSnapshot struct {
	organizationID string
	state          workosrepo.GetOrganizationSyncStateRow
}

// SnapshotOrganizationSync captures the cursor and its generation before remote reads.
func SnapshotOrganizationSync(ctx context.Context, beginner interface {
	Begin(context.Context) (pgx.Tx, error)
}, organizationID string) (OrganizationSyncSnapshot, error) {
	tx, err := beginner.Begin(ctx)
	if err != nil {
		return OrganizationSyncSnapshot{}, fmt.Errorf("begin sync cursor snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	state, err := organizationSyncState(ctx, workosrepo.New(tx), organizationID)
	if err != nil {
		return OrganizationSyncSnapshot{}, err
	}
	return OrganizationSyncSnapshot{organizationID: organizationID, state: state}, nil
}

// LockAndValidate serializes with event processing and rejects stale inventory.
func (snapshot OrganizationSyncSnapshot) LockAndValidate(ctx context.Context, tx pgx.Tx) error {
	queries := workosrepo.New(tx)
	if err := queries.LockOrganizationSync(ctx, snapshot.organizationID); err != nil {
		return fmt.Errorf("lock organization event sync: %w", err)
	}
	current, err := organizationSyncState(ctx, queries, snapshot.organizationID)
	if err != nil {
		return err
	}
	// The generation also changes for duplicate replay or a cursor reset.
	if current.ID != snapshot.state.ID || current.LastEventID != snapshot.state.LastEventID ||
		current.UpdatedAt.Valid != snapshot.state.UpdatedAt.Valid || !current.UpdatedAt.Time.Equal(snapshot.state.UpdatedAt.Time) {
		return fmt.Errorf("organization events changed during inventory retrieval; rerun with fresh inventory and review unattributed residuals")
	}
	return nil
}

func organizationSyncState(ctx context.Context, queries *workosrepo.Queries, workosOrganizationID string) (workosrepo.GetOrganizationSyncStateRow, error) {
	state, err := queries.GetOrganizationSyncState(ctx, workosOrganizationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return workosrepo.GetOrganizationSyncStateRow{}, fmt.Errorf("read organization event sync cursor: %w", err)
	}
	return state, nil
}
