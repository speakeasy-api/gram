package authclientbackfill

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SyncEMABinding reconciles the binding's current registration. A mutation
// replacing or removing ownership must instead capture both registrations and
// call SyncEMARegistrations after acquiring its complete preparation scope.
// Call this before taking registration locks, never from a single-source writer.
func SyncEMABinding(ctx context.Context, tx pgx.Tx, organizationID string, bindingID uuid.UUID) error {
	if organizationID == "" || bindingID == uuid.Nil {
		return errors.New("sync EMA binding: missing scope")
	}
	q := New(tx)
	params := GetEMABindingLockScopeParams{OrganizationID: organizationID, BindingID: bindingID}
	scope, err := q.GetEMABindingLockScope(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read EMA lock scope: %w", err)
	}
	if err := LockEMAPreparationScope(ctx, tx, organizationID, scope.ProjectID, scope.RemoteSessionIssuerID); err != nil {
		return err
	}
	// The registration may have changed while the source locks were pending.
	current, err := q.GetEMABindingLockScope(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reread EMA binding ownership: %w", err)
	}
	return SyncEMARegistrations(ctx, tx, organizationID, []uuid.UUID{scope.RemoteSessionClientID.UUID, current.RemoteSessionClientID.UUID})
}
