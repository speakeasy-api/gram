package productfeatures

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/mcpservers/upstreamclients/authclientbackfill"
)

// Gateway opt-in changes configured EMA reach, even when no individual server
// changes. Reconcile only registrations used by gateway bindings, in the same
// transaction as the setting. Runtime availability is deliberately unchanged.
func prepareGatewayAuthClients(ctx context.Context, tx pgx.Tx, organizationID string) (func() error, error) {
	servers, err := authclientbackfill.LockEMAGatewayScope(ctx, tx, organizationID)
	if err != nil {
		return nil, fmt.Errorf("lock gateway EMA scope: %w", err)
	}
	return func() error { return authclientbackfill.SyncEMAServers(ctx, tx, organizationID, servers) }, nil
}
