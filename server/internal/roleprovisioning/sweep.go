package roleprovisioning

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/roleprovisioning/hints"
)

// RequestSweep durably hands one global safety-net tick to the bounded consumer.
// It does not enumerate tenants or depend on provisioning/publication enablement.
// Repeated activity attempts may emit duplicates; reconciliation is idempotent.
func RequestSweep(ctx context.Context, db *pgxpool.Pool) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin role provisioning sweep: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })
	if err := hints.Emit(ctx, tx, hints.Hint{GlobalSweep: true, OrganizationID: "", RoleURN: "", PluginID: uuid.Nil, GlobalRoleURN: "", AfterOrganizationID: "", AfterRoleURN: ""}); err != nil {
		return fmt.Errorf("emit role provisioning sweep: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit role provisioning sweep: %w", err)
	}
	return nil
}
