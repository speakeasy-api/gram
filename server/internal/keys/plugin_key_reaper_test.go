package keys_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/keys"
)

func TestReapStalePluginAPIKeys(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestKeysService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	day := 24 * time.Hour
	project := uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}
	orphan := uuid.NullUUID{UUID: uuid.Nil, Valid: false}

	insert := func(name string, prefixMatches bool, projectID uuid.NullUUID, age time.Duration, lastUsed *time.Duration) uuid.UUID {
		t.Helper()
		token := name[len(name)-6:]
		if !prefixMatches {
			token = "zzzzzz"
		}
		var id uuid.UUID
		var lastAccessed *time.Time
		if lastUsed != nil {
			at := time.Now().Add(-*lastUsed)
			lastAccessed = &at
		}
		err := ti.conn.QueryRow(ctx, `
			INSERT INTO api_keys (organization_id, project_id, created_by_user_id, name, key_prefix, key_hash, scopes, created_at, last_accessed_at)
			VALUES ($1, $2, $3, $4, $5, $6, ARRAY['hooks'], $7, $8)
			RETURNING id`,
			authCtx.ActiveOrganizationID, projectID, authCtx.UserID, name, "gram_local_"+token[:5], uuid.NewString(), time.Now().Add(-age), lastAccessed,
		).Scan(&id)
		require.NoError(t, err)
		return id
	}
	recent := 2 * day

	latestPublish := insert("plugins-hooks-20260801-000000-a00001", true, project, 100*day, nil)
	nearPublish := insert("plugins-mcp-20260710-000000-a00002", true, project, 120*day, nil)
	stalePublish := insert("plugins-hooks-20260601-000000-a00003", true, project, 140*day, nil)
	staleDownload := insert("plugins-hooks-download-20260601-000000-a00004", true, project, 140*day, nil)
	usedDownload := insert("plugins-hooks-download-20260601-000000-a00005", true, project, 140*day, &recent)
	newDownload := insert("plugins-hooks-download-20261110-000000-a00006", true, project, 10*day, nil)
	handNamed := insert("plugins-hooks-20260101-000000-a00007", false, project, 300*day, nil)
	claudeWeb := insert("plugins-claude-web-hooks-20260101-000000-a00008", true, project, 300*day, nil)
	orphaned := insert("plugins-hooks-download-20261001-000000-a00009", true, orphan, 40*day, nil)
	postPublishDownload := insert("plugins-hooks-download-20260815-000000-a0000a", true, project, 90*day, nil)

	before, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyRevoke)
	require.NoError(t, err)

	reaped, err := keys.ReapStalePluginAPIKeys(ctx, ti.conn, audit.NewLogger(), 3)
	require.NoError(t, err)
	require.Equal(t, 3, reaped)
	reaped, err = keys.ReapStalePluginAPIKeys(ctx, ti.conn, audit.NewLogger(), 3)
	require.NoError(t, err)
	require.Equal(t, 1, reaped)

	for id, wantDeleted := range map[uuid.UUID]bool{
		latestPublish: false,
		nearPublish:   false,
		stalePublish:  true,
		staleDownload: true,
		usedDownload:  false,
		newDownload:   false,
		handNamed:     false,
		claudeWeb:     false,
		orphaned:      true,
		// Created after the latest publish, so the publish anchor doesn't cover it.
		postPublishDownload: true,
	} {
		require.Equal(t, wantDeleted, isKeyDeleted(t, ti.conn, id), fmt.Sprintf("key %s", id))
	}

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyRevoke)
	require.NoError(t, err)
	require.Equal(t, before+4, after)

	latest, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionKeyRevoke)
	require.NoError(t, err)
	require.Equal(t, "system", latest.ActorType)
	require.Equal(t, "plugin-api-key-reaper", latest.ActorID)

	reaped, err = keys.ReapStalePluginAPIKeys(ctx, ti.conn, audit.NewLogger(), 100)
	require.NoError(t, err)
	require.Zero(t, reaped)
}

func isKeyDeleted(t *testing.T, conn *pgxpool.Pool, id uuid.UUID) bool {
	t.Helper()
	var deleted bool
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT deleted FROM api_keys WHERE id = $1`, id).Scan(&deleted))
	return deleted
}
