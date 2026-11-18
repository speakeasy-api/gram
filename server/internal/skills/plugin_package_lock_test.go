package skills_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/skills"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
)

// A pause holds the exclusive content lock while checking the published
// fingerprint. A concurrent skill write must remain uncommitted until that
// check ends, even when no marketplace or publisher is configured.
func TestSkillVersionCommitWaitsForPluginPauseCheck(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestServiceWithPublisher(t, nil)
	require.Nil(t, ti.publisher)
	created := createSkill(t, ctx, ti, "pause-race", "First version.")
	plugin := createPlugin(t, ctx, ti, ti.projectID, "pause-race")
	_, err := ti.service.Distribute(ctx, &gen.DistributePayload{ID: created.Skill.ID, PluginID: new(plugin.ID.String())})
	require.NoError(t, err)

	pause, err := ti.conn.Begin(ctx) //nolint:glint // notestingrawsql: exercise the publisher's transaction lock
	require.NoError(t, err)
	defer func() { _ = pause.Rollback(context.WithoutCancel(ctx)) }()
	require.NoError(t, pluginsrepo.New(pause).LockPluginPackageContent(ctx, ti.projectID.String()))

	result := make(chan error, 1)
	go func() {
		_, addErr := ti.service.AddVersion(ctx, &gen.AddVersionPayload{ID: created.Skill.ID, Content: skillManifest("pause-race", "Second version.", "Second version.")})
		result <- addErr
	}()

	// Wait for the real service transaction to reach its shared content lock;
	// this avoids a timing-only assertion that could pass before it even starts.
	require.Eventually(t, func() bool {
		var waiting bool
		err := ti.conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname = current_database() AND wait_event = 'advisory' AND query LIKE '%plugin-package-content:%')`).Scan(&waiting) //nolint:glint // notestingrawsql: inspect the concurrent transaction's lock wait
		return err == nil && waiting
	}, 10*time.Second, 10*time.Millisecond)

	select {
	case err := <-result:
		t.Fatalf("skill mutation finished during pause check: %v", err)
	default:
	}
	require.NoError(t, pause.Commit(ctx))
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("skill mutation remained blocked after pause check committed")
	}
}
