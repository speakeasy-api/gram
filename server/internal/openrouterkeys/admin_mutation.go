package openrouterkeys

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/speakeasy-api/gram/server/internal/attr"
	"github.com/speakeasy-api/gram/server/internal/background/activities/keybillinglock"
	"github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter"
	orrepo "github.com/speakeasy-api/gram/server/internal/thirdparty/openrouter/repo"
)

// AdminReconciliationScope is the only data permitted to cross the Temporal
// boundary for an admin key repair. Desired state and actor attribution remain
// exclusively in the local database transaction.
type AdminReconciliationScope struct {
	OrganizationID string `json:"organization_id"`
	KeyType        string `json:"key_type"`
}

// AdminReconciliationCheckpoint is the complete payload permitted for a repair
// activity: privacy-bounded identity plus a non-sensitive monotonic audit cursor.
type AdminReconciliationCheckpoint struct {
	Scope  AdminReconciliationScope `json:"scope"`
	Cursor int64                    `json:"cursor"`
}

type AdminMutationCoordinator interface {
	Begin(context.Context, AdminReconciliationScope) error
	CompleteAndWait(context.Context, AdminReconciliationScope) error
	Abort(context.Context, AdminReconciliationScope) error
}

type PermanentAdminReconciliationError struct{ cause error }

func (e *PermanentAdminReconciliationError) Error() string { return e.cause.Error() }
func (e *PermanentAdminReconciliationError) Unwrap() error { return e.cause }

func IsPermanentAdminReconciliationError(err error) bool {
	var permanent *PermanentAdminReconciliationError
	return errors.As(err, &permanent)
}

type AdminReconciliationExecutor struct {
	logger      *slog.Logger
	db          *pgxpool.Pool
	provisioner openrouter.Provisioner
}

func NewAdminReconciliationExecutor(logger *slog.Logger, db *pgxpool.Pool, provisioner openrouter.Provisioner) *AdminReconciliationExecutor {
	return &AdminReconciliationExecutor{logger: logger.With(attr.SlogComponent("openrouterkeys.admin-reconciliation")), db: db, provisioner: provisioner}
}

func (e *AdminReconciliationExecutor) CaptureCursor(ctx context.Context, scope AdminReconciliationScope) (int64, error) {
	return readAdminMutationAuditCursor(ctx, e.db, scope)
}

// ReconcileSince repairs only when the atomic local transaction advanced the
// matching admin-key audit series. The cursor is read while holding the same
// canonical advisory lock as the mutation, so the proof and PATCH decision
// cannot race a commit.
func (e *AdminReconciliationExecutor) ReconcileSince(ctx context.Context, checkpoint AdminReconciliationCheckpoint) (int64, error) {
	scope := checkpoint.Scope
	logger := e.logger.With(attr.SlogOrganizationID(scope.OrganizationID), attr.SlogOpenRouterKeyType(scope.KeyType))
	var cursor int64
	err := keybillinglock.WithAcquireTimeout(ctx, logger, e.db, scope.OrganizationID, openrouter.KeyType(scope.KeyType), keyBillingLockWaitTimeout, func(conn *pgxpool.Conn) error {
		var err error
		cursor, err = readAdminMutationAuditCursor(ctx, conn, scope)
		if err != nil {
			return err
		}
		if cursor <= checkpoint.Cursor {
			return nil
		}

		row, err := orrepo.New(conn).GetOpenRouterAPIKey(ctx, orrepo.GetOpenRouterAPIKeyParams{OrganizationID: scope.OrganizationID, KeyType: scope.KeyType})
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return &PermanentAdminReconciliationError{cause: errors.New("OpenRouter key no longer exists")}
		case err != nil:
			return fmt.Errorf("read committed OpenRouter key state: %w", err)
		case row.DisableCauses == nil:
			return &PermanentAdminReconciliationError{cause: errors.New("OpenRouter key disable causes are unclassified")}
		}
		provisioner, ok := e.provisioner.(lockedSessionProvisioner)
		if !ok {
			return &PermanentAdminReconciliationError{cause: errors.New("OpenRouter provisioner cannot reconcile on the locked session")}
		}
		return provisioner.ReconcileAPIKeyDisabledWithDB(ctx, conn, scope.OrganizationID, openrouter.KeyType(scope.KeyType))
	})
	return cursor, err
}

func (e *AdminReconciliationExecutor) Reconcile(ctx context.Context, scope AdminReconciliationScope) error {
	_, err := e.ReconcileSince(ctx, AdminReconciliationCheckpoint{Scope: scope, Cursor: -1})
	return err
}

type auditCursorQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readAdminMutationAuditCursor(ctx context.Context, db auditCursorQuerier, scope AdminReconciliationScope) (int64, error) {
	const query = `
SELECT COALESCE(MAX(seq), 0)::bigint
FROM audit_logs
WHERE organization_id = $1
  AND action IN ('openrouter-key:disable', 'openrouter-key:enable')
  AND metadata->>'key_type' = $2`
	var cursor int64
	if err := db.QueryRow(ctx, query, scope.OrganizationID, scope.KeyType).Scan(&cursor); err != nil {
		return 0, fmt.Errorf("read OpenRouter admin mutation audit cursor: %w", err)
	}
	return cursor, nil
}
