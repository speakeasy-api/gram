package authclients

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
)

// lockAuthClientForUpdate takes offering servers before the typed client, the
// same order used by preparation writers. A link can
// appear during the first lock wait: reread after locking the client and restart
// only acquisition if the offering set changed. Never add server locks while
// holding the client. The client lock also serializes administrator detaches.
func lockAuthClientForUpdate(ctx context.Context, tx pgx.Tx, organizationID string, clientID uuid.UUID) (repo.McpAuthClient, error) {
	for {
		attempt, err := tx.Begin(ctx)
		if err != nil {
			return repo.McpAuthClient{}, fmt.Errorf("lock auth client for update: %w", err)
		}
		q := repo.New(attempt)
		servers, err := q.LockAuthClientOfferingServers(ctx, repo.LockAuthClientOfferingServersParams{AuthClientID: clientID, OrganizationID: organizationID})
		var client repo.McpAuthClient
		if err == nil {
			client, err = q.LockAuthClient(ctx, repo.LockAuthClientParams{ID: clientID, OrganizationID: organizationID})
		}
		var current []repo.ListAuthClientServersRow
		if err == nil {
			current, err = q.ListAuthClientServers(ctx, repo.ListAuthClientServersParams{AuthClientIds: []uuid.UUID{clientID}, OrganizationID: organizationID})
		}
		if err != nil {
			_ = attempt.Rollback(context.WithoutCancel(ctx))
			return repo.McpAuthClient{}, fmt.Errorf("lock auth client for update: %w", err)
		}
		same := len(current) == len(servers)
		if same {
			for i := range current {
				if current[i].ID != servers[i] {
					same = false
					break
				}
			}
		}
		if same {
			if err := attempt.Commit(ctx); err != nil {
				return repo.McpAuthClient{}, fmt.Errorf("commit auth client lock attempt: %w", err)
			}
			return client, nil
		}
		if err := attempt.Rollback(ctx); err != nil {
			return repo.McpAuthClient{}, fmt.Errorf("lock auth client for update: %w", err)
		}
	}
}
