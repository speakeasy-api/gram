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
	adminrepo "github.com/speakeasy-api/gram/server/internal/openrouterkeys/repo"
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

type ambiguousAdminMutationCommitError struct{ cause error }

func (e *ambiguousAdminMutationCommitError) Error() string { return e.cause.Error() }
func (e *ambiguousAdminMutationCommitError) Unwrap() error { return e.cause }

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
	cursor, err := adminrepo.New(e.db).GetOrganizationAuditCursor(ctx, scope.OrganizationID)
	if err != nil {
		return 0, fmt.Errorf("read organization audit cursor: %w", err)
	}
	return cursor, nil
}

// ReconcileSince repairs only when the bounded organization sequence range
// contains matching admin-key commit evidence. The target watermark is read
// while holding the same canonical advisory lock as the mutation, so the proof
// and PATCH decision cannot race a commit.
func (e *AdminReconciliationExecutor) ReconcileSince(ctx context.Context, checkpoint AdminReconciliationCheckpoint) (int64, error) {
	scope := checkpoint.Scope
	logger := e.logger.With(attr.SlogOrganizationID(scope.OrganizationID), attr.SlogOpenRouterKeyType(scope.KeyType))
	var cursor int64
	err := keybillinglock.WithAcquireTimeout(ctx, logger, e.db, scope.OrganizationID, openrouter.KeyType(scope.KeyType), keyBillingLockWaitTimeout, func(conn *pgxpool.Conn) error {
		queries := adminrepo.New(conn)
		var err error
		cursor, err = queries.GetOrganizationAuditCursor(ctx, scope.OrganizationID)
		if err != nil {
			return fmt.Errorf("read organization audit cursor: %w", err)
		}
		if cursor <= checkpoint.Cursor {
			return nil
		}
		matchingCursor, err := queries.GetAdminMutationAuditCursorSince(ctx, adminrepo.GetAdminMutationAuditCursorSinceParams{
			OrganizationID: scope.OrganizationID,
			Baseline:       checkpoint.Cursor,
			Target:         cursor,
			KeyType:        scope.KeyType,
		})
		if err != nil {
			return fmt.Errorf("read OpenRouter admin mutation audit cursor: %w", err)
		}
		if matchingCursor == 0 {
			return nil
		}

		return e.reconcileLocked(ctx, conn, scope)
	})
	if err != nil {
		return cursor, fmt.Errorf("reconcile OpenRouter admin mutation: %w", err)
	}
	return cursor, nil
}

func (e *AdminReconciliationExecutor) Reconcile(ctx context.Context, scope AdminReconciliationScope) error {
	logger := e.logger.With(attr.SlogOrganizationID(scope.OrganizationID), attr.SlogOpenRouterKeyType(scope.KeyType))
	err := keybillinglock.WithAcquireTimeout(ctx, logger, e.db, scope.OrganizationID, openrouter.KeyType(scope.KeyType), keyBillingLockWaitTimeout, func(conn *pgxpool.Conn) error {
		return e.reconcileLocked(ctx, conn, scope)
	})
	if err != nil {
		return fmt.Errorf("reconcile OpenRouter admin state: %w", err)
	}
	return nil
}

func (e *AdminReconciliationExecutor) reconcileLocked(ctx context.Context, conn *pgxpool.Conn, scope AdminReconciliationScope) error {
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
	if err := provisioner.ReconcileAPIKeyDisabledWithDB(ctx, conn, scope.OrganizationID, openrouter.KeyType(scope.KeyType)); err != nil {
		return fmt.Errorf("reconcile committed OpenRouter key state: %w", err)
	}
	return nil
}
