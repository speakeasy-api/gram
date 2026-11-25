package keys

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	accessrepo "github.com/speakeasy-api/gram/server/internal/access/repo"
	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/o11y"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

const (
	// PluginKeyRetentionDays is how long a plugin key survives after its
	// creation, after the project's latest publish, and after its last use.
	PluginKeyRetentionDays int32 = 30

	pluginKeyReaperComponent = "plugin-api-key-reaper"
)

// ReapStalePluginAPIKeys revokes up to batchSize stale plugin-minted keys in
// one transaction, removing each key's grants and auditing the revocation the
// same way a manual revoke does. It returns how many keys it revoked.
func ReapStalePluginAPIKeys(ctx context.Context, db *pgxpool.Pool, auditLogger *audit.Logger, batchSize int32) (int, error) {
	ctx = contextvalues.SetActingSurface(ctx, string(audit.SurfaceSystem))

	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer o11y.NoLogDefer(func() error { return tx.Rollback(ctx) })

	revoked, err := repo.New(tx).RevokeStalePluginAPIKeys(ctx, repo.RevokeStalePluginAPIKeysParams{
		RetentionDays: PluginKeyRetentionDays,
		BatchSize:     batchSize,
	})
	if err != nil {
		return 0, fmt.Errorf("revoke stale plugin api keys: %w", err)
	}

	access := accessrepo.New(tx)
	actor := urn.NewSystemPrincipal(pluginKeyReaperComponent)
	for _, key := range revoked {
		principal := urn.NewAPIKeyPrincipal(key.ID)
		grants, err := authz.LoadGrants(ctx, tx, key.OrganizationID, []urn.Principal{principal})
		if err != nil {
			return 0, fmt.Errorf("load api key %s policy: %w", key.ID, err)
		}
		policyAudit, err := PolicyAuditPayload(grantsToRoleGrants(grants))
		if err != nil {
			return 0, fmt.Errorf("prepare api key %s audit: %w", key.ID, err)
		}
		if _, err := access.DeletePrincipalGrantsByPrincipal(ctx, accessrepo.DeletePrincipalGrantsByPrincipalParams{
			OrganizationID: key.OrganizationID,
			PrincipalUrn:   principal,
		}); err != nil {
			return 0, fmt.Errorf("remove api key %s policy: %w", key.ID, err)
		}
		if err := auditLogger.LogKeyRevoke(ctx, tx, audit.LogKeyRevokeEvent{
			OrganizationID:   key.OrganizationID,
			ProjectID:        key.ProjectID,
			Actor:            actor,
			ActorDisplayName: nil,
			ActorSlug:        nil,
			KeyURN:           urn.NewAPIKey(key.ID),
			KeyName:          key.Name,
			Scopes:           key.Scopes,
			AgentCredential:  nil,
			PolicyGrants:     policyAudit,
		}); err != nil {
			return 0, fmt.Errorf("audit api key %s revocation: %w", key.ID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit plugin api key reap: %w", err)
	}
	return len(revoked), nil
}
