package keys_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/keys"
	keysrepo "github.com/speakeasy-api/gram/server/internal/keys/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
)

func TestReapStalePluginAPIKeys(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestKeysService(t)
	authCtx, ok := contextvalues.GetAuthContext(ctx)
	require.True(t, ok)

	day := 24 * time.Hour
	project := uuid.NullUUID{UUID: *authCtx.ProjectID, Valid: true}
	orphan := uuid.NullUUID{UUID: uuid.Nil, Valid: false}

	keysQ := keysrepo.New(ti.conn)
	fixtures := testrepo.New(ti.conn)
	insert := func(name string, prefixMatches bool, projectID uuid.NullUUID, age time.Duration, lastUsed *time.Duration) uuid.UUID {
		t.Helper()
		token := name[len(name)-6:]
		if !prefixMatches {
			token = "zzzzzz"
		}
		key, err := keysQ.CreateAPIKey(ctx, keysrepo.CreateAPIKeyParams{
			OrganizationID:  authCtx.ActiveOrganizationID,
			ProjectID:       projectID,
			CreatedByUserID: authCtx.UserID,
			Name:            name,
			KeyPrefix:       "gram_local_" + token[:5],
			KeyHash:         uuid.NewString(),
			Scopes:          []string{"hooks"},
		})
		require.NoError(t, err)
		lastAccessed := pgtype.Timestamptz{Time: time.Time{}, InfinityModifier: pgtype.Finite, Valid: false}
		if lastUsed != nil {
			lastAccessed = pgtype.Timestamptz{Time: time.Now().Add(-*lastUsed), InfinityModifier: pgtype.Finite, Valid: true}
		}
		require.NoError(t, fixtures.SetAPIKeyActivityFixture(ctx, testrepo.SetAPIKeyActivityFixtureParams{
			CreatedAt:      pgtype.Timestamptz{Time: time.Now().Add(-age), InfinityModifier: pgtype.Finite, Valid: true},
			LastAccessedAt: lastAccessed,
			ID:             key.ID,
		}))
		return key.ID
	}
	recent := 2 * day

	latestPublish := insert("plugins-hooks-20260801-000000-a00001", true, project, 100*day, nil)
	nearPublish := insert("plugins-mcp-20260710-000000-a00002", true, project, 120*day, nil)
	stalePublish := insert("plugins-hooks-20260601-000000-a00003", true, project, 140*day, nil)
	staleMCP := insert("plugins-mcp-20260601-000000-a0000b", true, project, 140*day, nil)
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
	require.Equal(t, 2, reaped)

	for id, wantDeleted := range map[uuid.UUID]bool{
		latestPublish: false,
		nearPublish:   false,
		stalePublish:  true,
		staleMCP:      true,
		staleDownload: true,
		usedDownload:  false,
		newDownload:   false,
		handNamed:     false,
		claudeWeb:     false,
		orphaned:      true,
		// Created after the latest publish, so the publish anchor doesn't cover it.
		postPublishDownload: true,
	} {
		key, err := keysQ.GetAPIKeyByID(ctx, keysrepo.GetAPIKeyByIDParams{ID: id, OrganizationID: authCtx.ActiveOrganizationID})
		require.NoError(t, err)
		require.Equal(t, wantDeleted, key.Deleted, "key %s", id)
	}

	after, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionKeyRevoke)
	require.NoError(t, err)
	require.Equal(t, before+5, after)

	latest, err := audittest.LatestAuditLogByAction(ctx, ti.conn, audit.ActionKeyRevoke)
	require.NoError(t, err)
	require.Equal(t, "system", latest.ActorType)
	require.Equal(t, "plugin-api-key-reaper", latest.ActorID)

	reaped, err = keys.ReapStalePluginAPIKeys(ctx, ti.conn, audit.NewLogger(), 100)
	require.NoError(t, err)
	require.Zero(t, reaped)
}
