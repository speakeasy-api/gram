package authclients

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/speakeasy-api/gram/server/internal/authclients/repo"
)

// lockAuthClientForUpdate takes offering servers before the typed client, the
// same order used by preparation and upstream requirement writers. A link can
// appear during the first lock wait: reread after locking the client and restart
// only acquisition if the offering set changed. Never add server locks while
// holding the client. The client lock also serializes administrator detaches.
func lockAuthClientForUpdate(ctx context.Context, tx pgx.Tx, organizationID string, clientID uuid.UUID) (repo.McpAuthClient, []repo.LockAuthClientServerRequirementsRow, error) {
	for {
		attempt, err := tx.Begin(ctx)
		if err != nil {
			return repo.McpAuthClient{}, nil, err
		}
		q := repo.New(attempt)
		servers, err := q.LockAuthClientServerRequirements(ctx, repo.LockAuthClientServerRequirementsParams{AuthClientID: clientID, OrganizationID: organizationID})
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
			return repo.McpAuthClient{}, nil, err
		}
		same := len(current) == len(servers)
		if same {
			for i := range current {
				if current[i].ID != servers[i].ID {
					same = false
					break
				}
			}
		}
		if same {
			return client, servers, attempt.Commit(ctx)
		}
		if err := attempt.Rollback(ctx); err != nil {
			return repo.McpAuthClient{}, nil, err
		}
	}
}
