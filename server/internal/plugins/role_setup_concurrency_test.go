package plugins_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	gen "github.com/speakeasy-api/gram/server/gen/plugins"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/stretchr/testify/require"
)

// This test-only trigger pauses real service transactions before their plugin
// changes become visible. pg_blocking_pids proves the statement reached the
// barrier: timing sleeps are not used to guess whether the race has started.
func pauseRoleSetupPluginWrite(t *testing.T, ctx context.Context, ti *testInstance, slug string) (pgx.Tx, int32) {
	t.Helper()
	q := pluginsrepo.New(ti.conn)
	err := q.CreateRoleSetupPauseFunctionFixture(ctx)
	require.NoError(t, err)
	switch slug {
	case "":
		err = q.CreateRoleSetupPauseAllTriggerFixture(ctx)
	case "sales-team":
		err = q.CreateRoleSetupPauseSalesTriggerFixture(ctx)
	default:
		t.Fatalf("unsupported pause fixture slug: %q", slug)
	}
	require.NoError(t, err)
	gate := testenv.BeginTx(t, ctx, ti.conn)
	err = pluginsrepo.New(gate).LockRoleSetupPauseFixture(ctx)
	require.NoError(t, err)
	return gate, int32(gate.Conn().PgConn().PID())
}

func roleSetupBlockedPID(ctx context.Context, ti *testInstance, blocker int32) (int32, error) {
	pid, err := pluginsrepo.New(ti.conn).GetRoleSetupBlockedPIDFixture(ctx, blocker)
	if err != nil {
		return 0, fmt.Errorf("find blocked role setup: %w", err)
	}
	return pid, nil
}

func waitRoleSetupBlocked(t *testing.T, ctx context.Context, ti *testInstance, blocker int32) int32 {
	t.Helper()
	var pid int32
	require.Eventually(t, func() bool {
		var err error
		pid, err = roleSetupBlockedPID(ctx, ti, blocker)
		require.NoError(t, err)
		return pid != 0
	}, 5*time.Second, 10*time.Millisecond, "public operation never reached the database barrier")
	return pid
}

type roleSetupRunResult struct {
	completed int
	err       error
}

func startRoleSetupRun(ctx context.Context, t *testing.T, ti *testInstance, role string) <-chan roleSetupRunResult {
	t.Helper()
	result := make(chan roleSetupRunResult, 1)
	setup := roleSetupIdentity(t, ctx, ti, role)
	go func() {
		n, err := processRoleSetup(ctx, ti, setup, plugins.PublicationRequests{})
		result <- roleSetupRunResult{n, err}
	}()
	return result
}

func readRoleSetupRun(t *testing.T, ctx context.Context, result <-chan roleSetupRunResult) int {
	t.Helper()
	select {
	case got := <-result:
		require.NoError(t, got.err)
		return got.completed
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return 0
	}
}

func TestProcessRoleDistributionSetup_ConcurrentDistinctNamesSameSlug(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestPluginsService(t)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	firstRole := roleSetupFixture(t, ctx, ti, "Sales Team")
	gate, gatePID := pauseRoleSetupPluginWrite(t, ctx, ti, "")
	first := startRoleSetupRun(ctx, t, ti, firstRole)
	firstPID := waitRoleSetupBlocked(t, ctx, ti, gatePID)

	secondRole := roleSetupFixture(t, ctx, ti, "sALES---TEAM")
	require.NotEqual(t, firstRole, secondRole)
	second := startRoleSetupRun(ctx, t, ti, secondRole)
	var early *roleSetupRunResult
	require.Eventually(t, func() bool {
		select {
		case got := <-second:
			early = &got
			return true
		default:
		}
		pid, err := roleSetupBlockedPID(ctx, ti, firstPID)
		require.NoError(t, err)
		return pid != 0
	}, 5*time.Second, 10*time.Millisecond, "second distinct role never ran while first was in flight")
	require.NoError(t, gate.Commit(ctx))
	total := readRoleSetupRun(t, ctx, first)
	if early != nil {
		require.NoError(t, early.err)
		total += early.completed
	} else {
		total += readRoleSetupRun(t, ctx, second)
	}
	total += runRoleSetup(t, ctx, ti, firstRole) + runRoleSetup(t, ctx, ti, secondRole)
	require.Equal(t, 4, total)
	assertRoleSetupMatchingPlugin(t, ctx, ti, "", []string{firstRole, secondRole})
	require.Equal(t, 1, runRoleSetup(t, ctx, ti, firstRole))
}

func TestProcessRoleDistributionSetup_OrdinaryWriterRacesSelection(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"create", "slug-update"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			ctx, ti := newTestPluginsService(t)
			ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			var existing *gen.Plugin
			if operation == "slug-update" {
				var err error
				slug := "race-writer"
				existing, err = ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Administrator curated tools", Slug: &slug})
				require.NoError(t, err)
			}
			role := roleSetupFixture(t, ctx, ti, "Sales Team")
			gate, gatePID := pauseRoleSetupPluginWrite(t, ctx, ti, "sales-team")
			type writeResult struct {
				plugin *gen.Plugin
				err    error
			}
			written := make(chan writeResult, 1)
			go func() {
				var p *gen.Plugin
				var err error
				description := "Administrator content survives setup"
				if operation == "create" {
					slug := "sales-team"
					p, err = ti.service.CreatePlugin(ctx, &gen.CreatePluginPayload{Name: "Administrator curated tools", Slug: &slug, Description: &description})
				} else {
					p, err = ti.service.UpdatePlugin(ctx, &gen.UpdatePluginPayload{ID: existing.ID, Name: "Administrator curated tools", Slug: "sales-team", Description: &description})
				}
				written <- writeResult{p, err}
			}()
			writerPID := waitRoleSetupBlocked(t, ctx, ti, gatePID)
			worker := startRoleSetupRun(ctx, t, ti, role)
			var early *roleSetupRunResult
			require.Eventually(t, func() bool {
				select {
				case got := <-worker:
					early = &got
					return true
				default:
				}
				pid, err := roleSetupBlockedPID(ctx, ti, writerPID)
				require.NoError(t, err)
				return pid != 0
			}, 5*time.Second, 10*time.Millisecond, "role selection never overlapped the ordinary write")
			require.NoError(t, gate.Commit(ctx))
			var ordinary *gen.Plugin
			select {
			case got := <-written:
				require.NoError(t, got.err)
				ordinary = got.plugin
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if early != nil {
				require.NoError(t, early.err)
				require.Equal(t, 1, early.completed)
			} else {
				require.Equal(t, 1, readRoleSetupRun(t, ctx, worker))
			}
			assertRoleSetupMatchingPlugin(t, ctx, ti, ordinary.ID, []string{"*", role})
			got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: ordinary.ID})
			require.NoError(t, err)
			require.Equal(t, "Administrator curated tools", got.Name)
			require.Equal(t, "sales-team", got.Slug)
			require.NotNil(t, got.Description)
			require.Equal(t, "Administrator content survives setup", *got.Description)
			require.Equal(t, 1, runRoleSetup(t, ctx, ti, role))
		})
	}
}

func assertRoleSetupMatchingPlugin(t *testing.T, ctx context.Context, ti *testInstance, expectedID string, principals []string) {
	t.Helper()
	listed, err := ti.service.ListPlugins(ctx, &gen.ListPluginsPayload{})
	require.NoError(t, err)
	var ids []string
	for _, p := range listed.Plugins {
		if p.Slug == "sales-team" {
			ids = append(ids, p.ID)
		}
	}
	require.Len(t, ids, 1, "concurrent selection must converge on one matching plugin")
	if expectedID != "" {
		require.Equal(t, expectedID, ids[0])
	}
	got, err := ti.service.GetPlugin(ctx, &gen.GetPluginPayload{ID: ids[0]})
	require.NoError(t, err)
	var actual []string
	for _, a := range got.Assignments {
		actual = append(actual, a.PrincipalUrn)
	}
	require.ElementsMatch(t, principals, actual)
	require.Empty(t, got.Servers)
}
