package migration_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/audit"
	"github.com/speakeasy-api/gram/server/internal/audit/audittest"
	"github.com/speakeasy-api/gram/server/internal/conv"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/migration"
	"github.com/speakeasy-api/gram/server/internal/externalmcp/migration/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

func migrate(t *testing.T, ctx context.Context, ti *testInstance, dryRun bool, servers ...fixtureServer) []migration.Outcome {
	t.Helper()
	requests := make([]migration.Request, 0, len(servers))
	for _, server := range servers {
		requests = append(requests, migration.Request{ToolsetID: server.toolset.ID})
	}
	return ti.migrator.Run(ctx, testActor, requests, dryRun)
}

func readServer(t *testing.T, ctx context.Context, ti *testInstance, server fixtureServer) mcpserversrepo.McpServer {
	t.Helper()
	row, err := mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: server.toolset.ID, ProjectID: ti.projectID})
	require.NoError(t, err)
	return row
}

func TestRunSwitchesServerInPlace(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMigrator(t)
	server := seedServer(t, ctx, ti, serverOptions{slug: "templated", remoteURL: "https://mcp.example.com/v1/mcp?{TOOLSETS}"})

	endpointsBefore, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: ti.projectID, McpServerID: server.toolset.ID})
	require.NoError(t, err)
	require.Len(t, endpointsBefore, 1)

	pluginServerID, err := repo.New(ti.conn).SeedPluginServerFixture(ctx, repo.SeedPluginServerFixtureParams{
		OrganizationID: ti.organizationID, ProjectID: conv.ToNullUUID(ti.projectID), Name: "support-tools",
		McpServerID: conv.ToNullUUID(server.toolset.ID),
	})
	require.NoError(t, err)
	sessionID, err := repo.New(ti.conn).SeedRemoteSessionFixture(ctx, repo.SeedRemoteSessionFixtureParams{
		SubjectUrn: urn.NewUserSubject("person"), UserSessionIssuerID: server.issuerID, RemoteSessionClientID: server.clientID, Resource: pgtype.Text{},
	})
	require.NoError(t, err)

	before := countIssuersAndClients(t, ctx, ti)
	migrationsBefore, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetMigrateToRemoteMcp)
	require.NoError(t, err)

	outcomes := migrate(t, ctx, ti, false, server)
	require.Len(t, outcomes, 1)
	outcome := outcomes[0]
	require.Equal(t, migration.StatusMigrated, outcome.Status, outcome.Reason)
	require.Equal(t, "https://mcp.example.com/v1/mcp", outcome.RemoteURL)
	require.True(t, containsSubstring(outcome.Actions, "Drop the unfilled template"), "actions %q", outcome.Actions)

	// The row the toolset's id names is the server, now remote-backed on the
	// same issuer, and no issuer, client or client link was created.
	require.Equal(t, server.toolset.ID, outcome.McpServerID)
	switched := readServer(t, ctx, ti, server)
	require.False(t, switched.ToolsetID.Valid)
	require.Equal(t, conv.ToNullUUID(outcome.RemoteMcpServerID), switched.RemoteMcpServerID)
	require.Equal(t, conv.ToNullUUID(server.issuerID), switched.UserSessionIssuerID)
	require.True(t, switched.RemoteSessionIssuerID.Valid)
	require.Equal(t, before, countIssuersAndClients(t, ctx, ti))

	// The address and the plugin entry still point at the same row.
	endpointsAfter, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: ti.projectID, McpServerID: server.toolset.ID})
	require.NoError(t, err)
	require.Len(t, endpointsAfter, 1)
	require.Equal(t, endpointsBefore[0].ID, endpointsAfter[0].ID)
	pluginServer, err := repo.New(ti.conn).GetPluginServerFixture(ctx, pluginServerID)
	require.NoError(t, err)
	require.False(t, pluginServer.Deleted)
	require.Equal(t, conv.ToNullUUID(server.toolset.ID), pluginServer.McpServerID)

	// The toolset is kept, so the switch can be undone.
	deleted, err := repo.New(ti.conn).GetToolsetStateFixture(ctx, server.toolset.ID)
	require.NoError(t, err)
	require.False(t, deleted)

	resource, err := repo.New(ti.conn).GetRemoteSessionResourceFixture(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, conv.ToPGText("https://mcp.example.com/v1/mcp"), resource)

	migrationsAfter, err := audittest.AuditLogCountByAction(ctx, ti.conn, audit.ActionToolsetMigrateToRemoteMcp)
	require.NoError(t, err)
	require.Equal(t, migrationsBefore+1, migrationsAfter)

	// The disabled toolset is listed again, to be deleted once the switch is
	// confirmed; deleting it leaves the server alone.
	leftover := findCandidate(t, ctx, ti, server)
	require.Equal(t, migration.ActionDelete, leftover.Action)
	require.True(t, containsSubstring(leftover.DeleteReasons, "already migrated"), "delete reasons %q", leftover.DeleteReasons)
	deletedOutcomes := migrate(t, ctx, ti, false, server)
	require.Equal(t, migration.StatusDeleted, deletedOutcomes[0].Status, deletedOutcomes[0].Reason)
	require.Equal(t, switched, readServer(t, ctx, ti, server))
}

func TestRunDialsTheRecordedSessionResource(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMigrator(t)
	server := seedServer(t, ctx, ti, serverOptions{slug: "recorded", remoteURL: "https://mcp.example.com/mcp?{TOOLSETS}"})
	queries := repo.New(ti.conn)
	_, err := queries.SeedRemoteSessionFixture(ctx, repo.SeedRemoteSessionFixtureParams{
		SubjectUrn: urn.NewUserSubject("signed-in"), UserSessionIssuerID: server.issuerID, RemoteSessionClientID: server.clientID,
		Resource: conv.ToPGText("https://mcp.example.com/recorded/"),
	})
	require.NoError(t, err)
	unrecorded, err := queries.SeedRemoteSessionFixture(ctx, repo.SeedRemoteSessionFixtureParams{
		SubjectUrn: urn.NewUserSubject("unrecorded"), UserSessionIssuerID: server.issuerID, RemoteSessionClientID: server.clientID, Resource: pgtype.Text{},
	})
	require.NoError(t, err)

	outcomes := migrate(t, ctx, ti, false, server)
	require.Equal(t, migration.StatusMigrated, outcomes[0].Status, outcomes[0].Reason)
	require.Equal(t, "https://mcp.example.com/recorded", outcomes[0].RemoteURL)

	resource, err := queries.GetRemoteSessionResourceFixture(ctx, unrecorded)
	require.NoError(t, err)
	require.Equal(t, conv.ToPGText("https://mcp.example.com/recorded"), resource)
}

func TestRunCopiesHeaders(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMigrator(t)
	server := seedServer(t, ctx, ti, serverOptions{
		slug: "copy-headers", remoteURL: "https://mcp.example.com/mcp", requiresOAuth: true,
		headers: []byte(`[{"name":"Authorization","isSecret":true},{"name":"Accept"}]`),
	})
	require.NoError(t, repo.New(ti.conn).SeedToolsetEnvironmentFixture(ctx, repo.SeedToolsetEnvironmentFixtureParams{
		OrganizationID: ti.organizationID, ProjectID: ti.projectID, Slug: "copy-headers-env",
		EntryName: "FIXTURE_TOKEN", EntryValue: "stored-value", ToolsetID: server.toolset.ID,
	}))

	outcomes := migrate(t, ctx, ti, false, server)
	require.Equal(t, migration.StatusMigrated, outcomes[0].Status, outcomes[0].Reason)

	headers, err := repo.New(ti.conn).ListRemoteHeadersFixture(ctx, outcomes[0].RemoteMcpServerID)
	require.NoError(t, err)
	require.Len(t, headers, 2)

	// Authorization is left to the upstream sign-in; Accept passes through
	// from the request header the external MCP proxy read it from.
	require.Equal(t, "Accept", headers[0].Name)
	require.Equal(t, conv.ToPGText("MCP-COPY-HEADERS-ACCEPT"), headers[0].ValueFromRequestHeader)
	require.False(t, headers[0].IsSecret)

	// The system value becomes a stored secret, encrypted at rest.
	require.Equal(t, "Fixture-Token", headers[1].Name)
	require.True(t, headers[1].IsSecret)
	require.True(t, headers[1].Value.Valid)
	require.NotEqual(t, "stored-value", headers[1].Value.String)
}

func TestRunMigratesServersWithoutUpstreamSignIn(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		opts    serverOptions
		warning string
	}{
		{name: "no sign-in provider", opts: serverOptions{slug: "no-issuer", remoteURL: "https://mcp.example.com/mcp", noIssuer: true}},
		{name: "sign-in provider without an upstream client", opts: serverOptions{slug: "no-client", remoteURL: "https://mcp.example.com/mcp", noClient: true}},
		{name: "headers users send", opts: serverOptions{slug: "user-headers", remoteURL: "https://mcp.example.com/mcp", noIssuer: true, headers: []byte(`[{"name":"X-Api-Key"}]`)}},
		{name: "per-tool definitions", opts: serverOptions{slug: "per-tool", remoteURL: "https://mcp.example.com/mcp", noIssuer: true, perTool: true, headers: []byte(`[{"name":"X-Api-Key"}]`)}},
		{name: "other tools", opts: serverOptions{slug: "other-tools", remoteURL: "https://mcp.example.com/mcp", otherTool: true}, warning: "which are dropped"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMigrator(t)
			server := seedServer(t, ctx, ti, tc.opts)
			before := countIssuersAndClients(t, ctx, ti)

			outcomes := migrate(t, ctx, ti, false, server)
			require.Equal(t, migration.StatusMigrated, outcomes[0].Status, outcomes[0].Reason)
			if tc.warning != "" {
				require.True(t, containsSubstring(outcomes[0].Warnings, tc.warning), "warnings %q do not mention %q", outcomes[0].Warnings, tc.warning)
			} else {
				require.False(t, containsSubstring(outcomes[0].Warnings, "dropped"), "warnings %q", outcomes[0].Warnings)
			}

			// The server keeps whatever sign-in provider it had and gains no
			// upstream issuer, and nothing is created to sign in with.
			switched := readServer(t, ctx, ti, server)
			require.False(t, switched.ToolsetID.Valid)
			require.Equal(t, conv.ToNullUUID(outcomes[0].RemoteMcpServerID), switched.RemoteMcpServerID)
			require.Equal(t, uuid.NullUUID{UUID: server.issuerID, Valid: server.issuerID != uuid.Nil}, switched.UserSessionIssuerID)
			if !tc.opts.otherTool {
				require.False(t, switched.RemoteSessionIssuerID.Valid)
			}
			require.Equal(t, before, countIssuersAndClients(t, ctx, ti))

			// Declared headers carry over as pass-throughs, whichever kind of
			// definition declared them.
			if tc.opts.headers != nil {
				headers, err := repo.New(ti.conn).ListRemoteHeadersFixture(ctx, outcomes[0].RemoteMcpServerID)
				require.NoError(t, err)
				require.Len(t, headers, 1)
				require.Equal(t, "X-Api-Key", headers[0].Name)
				require.True(t, headers[0].ValueFromRequestHeader.Valid)
			}
		})
	}
}

func TestListCandidatesBlocksIneligibleServers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		blocker string
		seed    func(t *testing.T, ctx context.Context, ti *testInstance) fixtureServer
	}{
		{name: "enterprise-managed bindings", blocker: "enterprise-managed", seed: func(t *testing.T, ctx context.Context, ti *testInstance) fixtureServer {
			t.Helper()
			server := seedServer(t, ctx, ti, serverOptions{slug: "ema", remoteURL: "https://mcp.example.com/mcp"})
			require.NoError(t, repo.New(ti.conn).SeedEmaBindingFixture(ctx, repo.SeedEmaBindingFixtureParams{UserSessionIssuerID: server.issuerID, RemoteSessionClientID: server.clientID}))
			return server
		}},
		{name: "self-owned client", blocker: "holds its own credentials", seed: func(t *testing.T, ctx context.Context, ti *testInstance) fixtureServer {
			t.Helper()
			server := seedServer(t, ctx, ti, serverOptions{slug: "self-owned", remoteURL: "https://mcp.example.com/mcp"})
			require.NoError(t, repo.New(ti.conn).MakeClientSelfOwnedFixture(ctx, server.clientID))
			return server
		}},
		{name: "several recorded resources", blocker: "more than one upstream resource", seed: func(t *testing.T, ctx context.Context, ti *testInstance) fixtureServer {
			t.Helper()
			server := seedServer(t, ctx, ti, serverOptions{slug: "resources", remoteURL: "https://mcp.example.com/mcp"})
			for i, resource := range []string{"https://mcp.example.com/a", "https://mcp.example.com/b"} {
				_, err := repo.New(ti.conn).SeedRemoteSessionFixture(ctx, repo.SeedRemoteSessionFixtureParams{
					SubjectUrn: urn.NewUserSubject("person-" + string(rune('a'+i))), UserSessionIssuerID: server.issuerID, RemoteSessionClientID: server.clientID,
					Resource: conv.ToPGText(resource),
				})
				require.NoError(t, err)
			}
			return server
		}},
		{name: "issuer shared with another hosted server", blocker: "not a remote MCP server", seed: func(t *testing.T, ctx context.Context, ti *testInstance) fixtureServer {
			t.Helper()
			server := seedServer(t, ctx, ti, serverOptions{slug: "shared-a", remoteURL: "https://mcp.example.com/mcp"})
			seedServer(t, ctx, ti, serverOptions{slug: "shared-b", remoteURL: "https://mcp.example.com/mcp", sharedIssuer: &server})
			return server
		}},
		{name: "issuer shared with an enabled toolset", blocker: "another enabled toolset", seed: func(t *testing.T, ctx context.Context, ti *testInstance) fixtureServer {
			t.Helper()
			server := seedServer(t, ctx, ti, serverOptions{slug: "toolset-a", remoteURL: "https://mcp.example.com/mcp"})
			seedServer(t, ctx, ti, serverOptions{slug: "toolset-b", remoteURL: "https://mcp.example.com/mcp", sharedIssuer: &server})
			return server
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMigrator(t)
			server := tc.seed(t, ctx, ti)

			candidates, err := ti.migrator.ListCandidates(ctx)
			require.NoError(t, err)
			var blockers []string
			for _, candidate := range candidates {
				if candidate.Row.ToolsetID == server.toolset.ID {
					blockers = candidate.Blockers
				}
			}
			require.True(t, containsSubstring(blockers, tc.blocker), "blockers %q do not mention %q", blockers, tc.blocker)

			outcomes := migrate(t, ctx, ti, false, server)
			require.Equal(t, migration.StatusFailed, outcomes[0].Status)
			require.Equal(t, conv.ToNullUUID(server.toolset.ID), readServer(t, ctx, ti, server).ToolsetID)
		})
	}
}

func TestPreviewRollsBack(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMigrator(t)
	server := seedServer(t, ctx, ti, serverOptions{slug: "previewed", remoteURL: "https://mcp.example.com/mcp"})

	outcomes := migrate(t, ctx, ti, true, server)
	require.Equal(t, migration.StatusPreviewed, outcomes[0].Status, outcomes[0].Reason)
	require.NotEmpty(t, outcomes[0].Actions)

	unchanged := readServer(t, ctx, ti, server)
	require.Equal(t, conv.ToNullUUID(server.toolset.ID), unchanged.ToolsetID)
	require.False(t, unchanged.RemoteMcpServerID.Valid)
	require.Equal(t, "private", unchanged.Visibility)
}

func TestRunStopsAtFirstFailure(t *testing.T) {
	t.Parallel()

	ctx, ti := newTestMigrator(t)
	blocked := seedServer(t, ctx, ti, serverOptions{slug: "blocked", remoteURL: "https://mcp.example.com/mcp"})
	require.NoError(t, repo.New(ti.conn).MakeClientSelfOwnedFixture(ctx, blocked.clientID))
	ready := seedServer(t, ctx, ti, serverOptions{slug: "ready", remoteURL: "https://mcp.example.com/mcp"})

	outcomes := migrate(t, ctx, ti, false, blocked, ready)
	require.Equal(t, migration.StatusFailed, outcomes[0].Status)
	require.Equal(t, migration.StatusNotAttempted, outcomes[1].Status)
	require.Equal(t, conv.ToNullUUID(ready.toolset.ID), readServer(t, ctx, ti, ready).ToolsetID)
}

func containsSubstring(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

func findCandidate(t *testing.T, ctx context.Context, ti *testInstance, server fixtureServer) migration.Candidate {
	t.Helper()
	candidates, err := ti.migrator.ListCandidates(ctx)
	require.NoError(t, err)
	for _, candidate := range candidates {
		if candidate.Row.ToolsetID == server.toolset.ID {
			return candidate
		}
	}
	require.FailNow(t, "candidate not listed", "toolset %s", server.toolset.Slug)
	return migration.Candidate{}
}

func TestRunDeletesToolsetsLeftWithNothingToMigrate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		reason string
		seed   func(t *testing.T, ctx context.Context, ti *testInstance) fixtureServer
	}{
		{name: "attachment retired from the active deployment", reason: "no longer on the project's active deployment", seed: func(t *testing.T, ctx context.Context, ti *testInstance) fixtureServer {
			t.Helper()
			server := seedServer(t, ctx, ti, serverOptions{slug: "retired", remoteURL: "https://mcp.example.com/mcp"})
			_, err := repo.New(ti.conn).SeedCompletedDeploymentFixture(ctx, repo.SeedCompletedDeploymentFixtureParams{
				ProjectID: ti.projectID, OrganizationID: ti.organizationID, IdempotencyKey: uuid.NewString(),
			})
			require.NoError(t, err)
			return server
		}},
		{name: "MCP off with no server row", reason: "nothing serves it", seed: func(t *testing.T, ctx context.Context, ti *testInstance) fixtureServer {
			t.Helper()
			return seedServer(t, ctx, ti, serverOptions{slug: "unserved", remoteURL: "https://mcp.example.com/mcp", mcpOff: true, noWrapper: true})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, ti := newTestMigrator(t)
			server := tc.seed(t, ctx, ti)

			candidate := findCandidate(t, ctx, ti, server)
			require.Equal(t, migration.ActionDelete, candidate.Action)
			require.True(t, containsSubstring(candidate.DeleteReasons, tc.reason), "delete reasons %q do not mention %q", candidate.DeleteReasons, tc.reason)
			require.Empty(t, candidate.Blockers)

			// A preview deletes nothing.
			previewed := migrate(t, ctx, ti, true, server)
			require.Equal(t, migration.StatusPreviewed, previewed[0].Status, previewed[0].Reason)
			require.Equal(t, migration.ActionDelete, previewed[0].Action)
			deleted, err := repo.New(ti.conn).GetToolsetStateFixture(ctx, server.toolset.ID)
			require.NoError(t, err)
			require.False(t, deleted)

			outcomes := migrate(t, ctx, ti, false, server)
			require.Equal(t, migration.StatusDeleted, outcomes[0].Status, outcomes[0].Reason)
			require.True(t, containsSubstring(outcomes[0].Actions, "Delete the toolset"), "actions %q", outcomes[0].Actions)
			deleted, err = repo.New(ti.conn).GetToolsetStateFixture(ctx, server.toolset.ID)
			require.NoError(t, err)
			require.True(t, deleted)

			// The toolset's server and its address go with it.
			_, err = mcpserversrepo.New(ti.conn).GetMCPServerByIDAndProjectID(ctx, mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: server.toolset.ID, ProjectID: ti.projectID})
			require.ErrorIs(t, err, pgx.ErrNoRows)
			endpoints, err := mcpendpointsrepo.New(ti.conn).ListMCPEndpointsByMCPServerID(ctx, mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{ProjectID: ti.projectID, McpServerID: server.toolset.ID})
			require.NoError(t, err)
			require.Empty(t, endpoints)
		})
	}
}
