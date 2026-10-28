package assistants

import (
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/gen/types"
	assistantrepo "github.com/speakeasy-api/gram/server/internal/assistants/repo"
	customdomainsrepo "github.com/speakeasy-api/gram/server/internal/customdomains/repo"
	environmentsrepo "github.com/speakeasy-api/gram/server/internal/environments/repo"
	mcpendpointsrepo "github.com/speakeasy-api/gram/server/internal/mcpendpoints/repo"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/networkaccess"
	remotemcprepo "github.com/speakeasy-api/gram/server/internal/remotemcp/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

type hostedAttachmentFixture struct {
	core      *ServiceCore
	db        *pgxpool.Pool
	projectID uuid.UUID
	assistant uuid.UUID
	hosted    toolsetsrepo.Toolset
	legacy    toolsetsrepo.Toolset
	remote    mcpserversrepo.McpServer
}

func newHostedAttachmentFixture(t *testing.T, name string) hostedAttachmentFixture {
	t.Helper()
	ctx := t.Context()
	db, err := assistantsInfra.CloneTestDatabase(t, name)
	require.NoError(t, err)
	projectID := newProvisioningProject(t, db, name)
	core := newProvisioningCore(t, db)

	for _, slug := range []string{"default-env", "staging"} {
		_, err := environmentsrepo.New(db).CreateEnvironment(ctx, environmentsrepo.CreateEnvironmentParams{
			OrganizationID: "org-test", ProjectID: projectID, Name: slug, Slug: slug,
		})
		require.NoError(t, err)
	}

	toolsets := toolsetsrepo.New(db)
	hosted, err := toolsets.CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: "org-test", ProjectID: projectID, Name: "Billing", Slug: "billing",
		DefaultEnvironmentSlug: pgtype.Text{String: "default-env", Valid: true},
	})
	require.NoError(t, err)
	createCanonicalWrapper(t, db, hosted, "org-test-billing-x1", "private")

	// No canonical wrapper, only a legacy alias: not hosted.
	legacy, err := toolsets.CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{
		OrganizationID: "org-test", ProjectID: projectID, Name: "Legacy", Slug: "legacy",
		DefaultEnvironmentSlug: pgtype.Text{String: "default-env", Valid: true},
	})
	require.NoError(t, err)
	testenv.SetLegacyAlias(t, ctx, db, projectID, legacy.ID, "org-test-legacy-x1", uuid.NullUUID{})

	backend, err := remotemcprepo.New(db).CreateServer(ctx, remotemcprepo.CreateServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: pgtype.Text{String: "Remote", Valid: true},
		Slug: pgtype.Text{String: "remote-src", Valid: true}, TransportType: "streamable-http", Url: "https://mcp.example.com/mcp",
	})
	require.NoError(t, err)
	remote, err := mcpserversrepo.New(db).CreateMCPServer(ctx, mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: projectID, Name: pgtype.Text{String: "Remote", Valid: true},
		Slug: pgtype.Text{String: "remote", Valid: true}, RemoteMcpServerID: uuid.NullUUID{UUID: backend.ID, Valid: true}, Visibility: "private",
	})
	require.NoError(t, err)
	_, err = mcpendpointsrepo.New(db).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: projectID, McpServerID: uuid.NullUUID{UUID: remote.ID, Valid: true}, Slug: "team-remote",
	})
	require.NoError(t, err)

	record, err := core.CreateAssistant(ctx, "org-test", projectID, "user-1", "Assistant", "openai/gpt-4o-mini", "", nil, nil, 300, 1, StatusActive, false, nil)
	require.NoError(t, err)

	return hostedAttachmentFixture{core: core, db: db, projectID: projectID, assistant: record.ID, hosted: hosted, legacy: legacy, remote: remote}
}

// createCanonicalWrapper gives a toolset its canonical wrapper with one platform endpoint at slug, as toolset creation does.
func createCanonicalWrapper(t *testing.T, db *pgxpool.Pool, toolset toolsetsrepo.Toolset, slug, visibility string) {
	t.Helper()
	_, err := mcpserversrepo.New(db).CreateMCPServer(t.Context(), mcpserversrepo.CreateMCPServerParams{
		ID: toolset.ID, ProjectID: toolset.ProjectID, Name: pgtype.Text{String: toolset.Name, Valid: true}, Slug: pgtype.Text{String: slug, Valid: true},
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, ToolVariationsGroupID: toolset.ToolVariationsGroupID,
		Visibility: visibility, NetworkAccessMode: networkaccess.Storage(networkaccess.ModePublicOnly),
	})
	require.NoError(t, err)
	testenv.SetHostedMCP(t, t.Context(), db, toolset.ProjectID, toolset.ID, testenv.HostedMCP{Slug: slug})
}

func (f hostedAttachmentFixture) runtime(t *testing.T) []runtimeMCPServer {
	t.Helper()
	rows, err := f.core.loadAssistantMcpServers(t.Context(), f.projectID, []uuid.UUID{f.assistant})
	require.NoError(t, err)
	serverURL, err := url.Parse("https://gram.test")
	require.NoError(t, err)
	return resolveAssistantMCPServers(t.Context(), testenv.NewLogger(t), serverURL, rows[f.assistant], nil)
}

func (f hostedAttachmentFixture) counts(t *testing.T) int64 {
	t.Helper()
	counts, err := testrepo.New(f.db).CountAssistantAttachments(t.Context(), testrepo.CountAssistantAttachmentsParams{ProjectID: f.projectID, AssistantID: f.assistant})
	require.NoError(t, err)
	return counts
}

func (f hostedAttachmentFixture) environmentID(t *testing.T, slug string) uuid.NullUUID {
	t.Helper()
	rows, err := assistantrepo.New(f.db).ResolveEnvironmentsForWrite(t.Context(), assistantrepo.ResolveEnvironmentsForWriteParams{ProjectID: f.projectID, Slugs: []string{slug}})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	return uuid.NullUUID{UUID: rows[0].ID, Valid: true}
}

func (f hostedAttachmentFixture) addServerRow(t *testing.T, serverID uuid.UUID, env uuid.NullUUID) {
	t.Helper()
	_, err := assistantrepo.New(f.db).AddAssistantMcpServers(t.Context(), []assistantrepo.AddAssistantMcpServersParams{{
		AssistantID: f.assistant, McpServerID: serverID, EnvironmentID: env, ProjectID: f.projectID,
	}})
	require.NoError(t, err)
}

func (f hostedAttachmentFixture) disableHosted(t *testing.T) {
	t.Helper()
	testenv.SetHostedMCP(t, t.Context(), f.db, f.projectID, f.hosted.ID, testenv.HostedMCP{Visibility: "disabled"})
}

func (f hostedAttachmentFixture) deleteHostedEndpoints(t *testing.T) {
	t.Helper()
	endpoints, err := mcpendpointsrepo.New(f.db).ListMCPEndpointsByMCPServerID(t.Context(), mcpendpointsrepo.ListMCPEndpointsByMCPServerIDParams{McpServerID: f.hosted.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	for _, e := range endpoints {
		_, err := mcpendpointsrepo.New(f.db).DeleteMCPEndpoint(t.Context(), mcpendpointsrepo.DeleteMCPEndpointParams{ID: e.ID, ProjectID: f.projectID})
		require.NoError(t, err)
	}
}

func (f hostedAttachmentFixture) updateWrapper(t *testing.T, ms mcpserversrepo.McpServer) {
	t.Helper()
	_, err := mcpserversrepo.New(f.db).UpdateMCPServer(t.Context(), mcpserversrepo.UpdateMCPServerParams{
		Name: ms.Name, Slug: ms.Slug, EnvironmentID: ms.EnvironmentID, UserSessionIssuerID: ms.UserSessionIssuerID,
		RemoteMcpServerID: ms.RemoteMcpServerID, TunneledMcpServerID: ms.TunneledMcpServerID, ToolsetID: ms.ToolsetID,
		UnproxiedMcpServerID: ms.UnproxiedMcpServerID, ToolVariationsGroupID: ms.ToolVariationsGroupID, Visibility: ms.Visibility,
		NetworkAccessModeSet: false, NetworkAccessMode: pgtype.Text{}, ID: ms.ID, ProjectID: f.projectID,
	})
	require.NoError(t, err)
}

func (f hostedAttachmentFixture) wrapper(t *testing.T) mcpserversrepo.McpServer {
	t.Helper()
	ms, err := mcpserversrepo.New(f.db).GetMCPServerByIDAndProjectID(t.Context(), mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: f.hosted.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	return ms
}

func (f hostedAttachmentFixture) view(t *testing.T) *types.Assistant {
	t.Helper()
	record, err := f.core.GetAssistant(t.Context(), f.projectID, f.assistant)
	require.NoError(t, err)
	view, err := toHTTPAssistant(record)
	require.NoError(t, err)
	return view
}

// A hosted attachment is stored under its canonical wrapper (wrapper id = toolset id);
// the runner sees the toolset's id, URL and environment.
func TestHostedAttachmentRuntimeConfig(t *testing.T) {
	t.Parallel()
	for _, env := range []string{"", "staging"} {
		t.Run("env="+env, func(t *testing.T) {
			t.Parallel()
			f := newHostedAttachmentFixture(t, "hosted_runtime_"+env)
			var envID uuid.NullUUID
			wantEnv := "default-env"
			if env != "" {
				envID = f.environmentID(t, env)
				wantEnv = env
			}
			f.addServerRow(t, f.hosted.ID, envID)
			require.Equal(t, []runtimeMCPServer{{
				ID:      "billing",
				URL:     "https://gram.test/mcp/org-test-billing-x1",
				Headers: map[string]string{"Gram-Environment": wantEnv},
			}}, f.runtime(t))
			view := f.view(t)
			require.Empty(t, view.McpServers, "hosted servers are listed only under toolsets")
			require.Len(t, view.Toolsets, 1)
		})
	}
}

// Hosted servers are listed under toolsets, other servers under mcp_servers.
func TestHostedAttachmentReadKeepsMainShape(t *testing.T) {
	t.Parallel()
	f := newHostedAttachmentFixture(t, "hosted_read_shape")
	f.addServerRow(t, f.hosted.ID, uuid.NullUUID{})
	f.addServerRow(t, f.remote.ID, uuid.NullUUID{})

	view := f.view(t)
	require.Len(t, view.McpServers, 1)
	require.Equal(t, "remote", view.McpServers[0].McpServerSlug)
	require.Len(t, view.Toolsets, 1)
	require.Equal(t, "billing", view.Toolsets[0].ToolsetSlug)

}

func TestHostedAttachmentWritesLeaveNoTwins(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	name := "updated"

	t.Run("toolsets write stores the hosted server under its wrapper", func(t *testing.T) {
		t.Parallel()
		f := newHostedAttachmentFixture(t, "hosted_write_toolsets")
		f.addServerRow(t, f.hosted.ID, uuid.NullUUID{})
		f.addServerRow(t, f.remote.ID, uuid.NullUUID{})
		before := f.runtime(t)

		_, err := f.core.UpdateAssistant(ctx, f.projectID, f.assistant, &name, nil, nil,
			[]*types.AssistantToolsetRef{{ToolsetSlug: "billing"}}, nil, nil, nil, nil, allowAllMCPEnable)
		require.NoError(t, err)
		counts := f.counts(t)
		require.EqualValues(t, 2, counts, "hosted server and untouched remote server")
		require.ElementsMatch(t, before, f.runtime(t))
	})

	t.Run("mcp servers write leaves an attached hosted server as it is", func(t *testing.T) {
		t.Parallel()
		f := newHostedAttachmentFixture(t, "hosted_write_servers")
		f.addServerRow(t, f.hosted.ID, uuid.NullUUID{})
		before := f.runtime(t)

		_, err := f.core.UpdateAssistant(ctx, f.projectID, f.assistant, &name, nil, nil, nil,
			[]*types.AssistantMCPServerRef{{McpServerSlug: "org-test-billing-x1"}}, nil, nil, nil, allowAllMCPEnable)
		require.NoError(t, err)
		counts := f.counts(t)
		require.EqualValues(t, 1, counts)
		require.ElementsMatch(t, before, f.runtime(t))
	})

	t.Run("older dashboard attaches a hosted server from the server picker", func(t *testing.T) {
		t.Parallel()
		f := newHostedAttachmentFixture(t, "hosted_write_old_picker")
		view := f.view(t)

		_, err := f.core.UpdateAssistant(ctx, f.projectID, f.assistant, &name, nil, nil, view.Toolsets,
			append(view.McpServers, &types.AssistantMCPServerRef{McpServerSlug: "org-test-billing-x1"}), nil, nil, nil, allowAllMCPEnable)
		require.NoError(t, err)
		counts := f.counts(t)
		require.EqualValues(t, 1, counts, "the hosted server is stored under its wrapper")
		require.Equal(t, []string{"billing"}, runtimeIDs(f.runtime(t)))
	})

	t.Run("mcp servers echo of an attached hosted server with MCP off is a no-op", func(t *testing.T) {
		t.Parallel()
		f := newHostedAttachmentFixture(t, "hosted_write_echo_disabled")
		f.addServerRow(t, f.hosted.ID, f.environmentID(t, "staging"))
		f.addServerRow(t, f.remote.ID, uuid.NullUUID{})
		f.disableHosted(t)

		_, err := f.core.UpdateAssistant(ctx, f.projectID, f.assistant, &name, nil, nil, nil,
			[]*types.AssistantMCPServerRef{{McpServerSlug: "org-test-billing-x1"}, {McpServerSlug: "remote"}}, nil, nil, nil, allowAllMCPEnable)
		require.NoError(t, err)
		counts := f.counts(t)
		require.EqualValues(t, 2, counts)
		view := f.view(t)
		require.Len(t, view.Toolsets, 1)
		require.NotNil(t, view.Toolsets[0].EnvironmentSlug)
		require.Equal(t, "staging", *view.Toolsets[0].EnvironmentSlug, "environment not rewritten")
	})

	t.Run("echoing a read back is a no-op", func(t *testing.T) {
		t.Parallel()
		f := newHostedAttachmentFixture(t, "hosted_write_echo")
		f.addServerRow(t, f.hosted.ID, uuid.NullUUID{})
		f.addServerRow(t, f.remote.ID, uuid.NullUUID{})
		before := f.runtime(t)
		view := f.view(t)

		_, err := f.core.UpdateAssistant(ctx, f.projectID, f.assistant, &name, nil, nil, view.Toolsets, view.McpServers, nil, nil, nil, allowAllMCPEnable)
		require.NoError(t, err)
		counts := f.counts(t)
		require.EqualValues(t, 2, counts)
		require.ElementsMatch(t, before, f.runtime(t))
		require.Equal(t, view.McpServers, f.view(t).McpServers)
	})

	t.Run("toolsets govern hosted servers when both arrays are sent", func(t *testing.T) {
		t.Parallel()
		f := newHostedAttachmentFixture(t, "hosted_write_both")
		f.addServerRow(t, f.hosted.ID, uuid.NullUUID{})
		staging := "staging"
		// An older dashboard echoes a hosted server in both arrays.
		_, err := f.core.UpdateAssistant(ctx, f.projectID, f.assistant, &name, nil, nil,
			[]*types.AssistantToolsetRef{{ToolsetSlug: "billing", EnvironmentSlug: &staging}},
			[]*types.AssistantMCPServerRef{{McpServerSlug: "org-test-billing-x1"}}, nil, nil, nil, allowAllMCPEnable)
		require.NoError(t, err)
		require.Equal(t, "staging", f.runtime(t)[0].Headers["Gram-Environment"])

		_, err = f.core.UpdateAssistant(ctx, f.projectID, f.assistant, &name, nil, nil,
			[]*types.AssistantToolsetRef{},
			[]*types.AssistantMCPServerRef{{McpServerSlug: "org-test-billing-x1"}, {McpServerSlug: "remote"}}, nil, nil, nil, allowAllMCPEnable)
		require.NoError(t, err)
		require.Equal(t, []string{"remote"}, runtimeIDs(f.runtime(t)), "removing it from toolsets detaches it")
	})

	t.Run("mcp servers write keeps unlisted hosted servers", func(t *testing.T) {
		t.Parallel()
		f := newHostedAttachmentFixture(t, "hosted_write_keep")
		f.addServerRow(t, f.hosted.ID, uuid.NullUUID{})
		f.addServerRow(t, f.remote.ID, uuid.NullUUID{})

		_, err := f.core.UpdateAssistant(ctx, f.projectID, f.assistant, &name, nil, nil, nil,
			[]*types.AssistantMCPServerRef{}, nil, nil, nil, allowAllMCPEnable)
		require.NoError(t, err)
		require.Equal(t, []string{"billing"}, runtimeIDs(f.runtime(t)))
	})
}

func TestHostedAttachmentCreateByServerSlug(t *testing.T) {
	t.Parallel()
	f := newHostedAttachmentFixture(t, "hosted_create_by_server")
	record, err := f.core.CreateAssistant(t.Context(), "org-test", f.projectID, "user-1", "Second", "openai/gpt-4o-mini", "", nil,
		[]*types.AssistantMCPServerRef{{McpServerSlug: "org-test-billing-x1"}, {McpServerSlug: "remote"}}, 300, 1, StatusActive, false, allowAllMCPEnable)
	require.NoError(t, err)
	f.assistant = record.ID
	counts := f.counts(t)
	require.EqualValues(t, 2, counts)
	require.ElementsMatch(t, []string{"billing", "remote"}, runtimeIDs(f.runtime(t)))
}

// Only the toolsets path turns MCP on; a server ref never enables a disabled hosted server.
func TestHostedAttachmentByServerSlugLeavesMCPOff(t *testing.T) {
	t.Parallel()
	name := "updated"

	t.Run("new attach of a disabled hosted server is rejected", func(t *testing.T) {
		t.Parallel()
		f := newHostedAttachmentFixture(t, "hosted_attach_disabled_rejected")
		f.disableHosted(t)
		wrapper := f.wrapper(t)

		_, err := f.core.UpdateAssistant(t.Context(), f.projectID, f.assistant, &name, nil, nil, nil,
			[]*types.AssistantMCPServerRef{{McpServerSlug: "org-test-billing-x1"}}, nil, nil, nil, allowAllMCPEnable)
		require.ErrorIs(t, err, errAssistantValidation)
		require.ErrorContains(t, err, "is disabled")
		require.Equal(t, "disabled", f.wrapper(t).Visibility)
		require.Equal(t, wrapper.Visibility, f.wrapper(t).Visibility)
		require.Zero(t, f.counts(t))
	})

	t.Run("echo of an attached disabled hosted server is left as it is", func(t *testing.T) {
		t.Parallel()
		f := newHostedAttachmentFixture(t, "hosted_attach_disabled_echo")
		f.addServerRow(t, f.hosted.ID, uuid.NullUUID{})
		f.disableHosted(t)
		wrapper := f.wrapper(t)

		_, err := f.core.UpdateAssistant(t.Context(), f.projectID, f.assistant, &name, nil, nil, nil,
			[]*types.AssistantMCPServerRef{{McpServerSlug: "org-test-billing-x1"}}, nil, nil, nil, allowAllMCPEnable)
		require.NoError(t, err)
		require.Equal(t, "disabled", f.wrapper(t).Visibility)
		require.Equal(t, wrapper.Visibility, f.wrapper(t).Visibility)
		require.EqualValues(t, 1, f.counts(t))
	})
}

// A hosted attachment follows its canonical wrapper's visibility; the legacy alias serves only an enabled wrapper.
func TestHostedAttachmentFollowsWrapperOverToolset(t *testing.T) {
	t.Parallel()
	f := newHostedAttachmentFixture(t, "hosted_drift")
	f.addServerRow(t, f.hosted.ID, uuid.NullUUID{})
	testenv.SetLegacyAlias(t, t.Context(), f.db, f.projectID, f.hosted.ID, "org-test-billing-x1", uuid.NullUUID{})
	want := []runtimeMCPServer{{
		ID:      "billing",
		URL:     "https://gram.test/mcp/org-test-billing-x1",
		Headers: map[string]string{"Gram-Environment": "default-env"},
	}}

	wrapper, err := mcpserversrepo.New(f.db).GetMCPServerByIDAndProjectID(t.Context(), mcpserversrepo.GetMCPServerByIDAndProjectIDParams{ID: f.hosted.ID, ProjectID: f.projectID})
	require.NoError(t, err)
	wrapper.Visibility = "disabled"
	f.updateWrapper(t, wrapper)
	f.deleteHostedEndpoints(t)
	require.Empty(t, f.runtime(t), "disabled, endpointless wrapper; legacy alias still set")

	wrapper.Visibility = "public"
	f.updateWrapper(t, wrapper)
	require.Equal(t, want, f.runtime(t), "endpointless public wrapper keeps the legacy alias")
}

func runtimeIDs(servers []runtimeMCPServer) []string {
	ids := make([]string, 0, len(servers))
	for _, server := range servers {
		ids = append(ids, server.ID)
	}
	return ids
}

// A hosted server sorts first, so it keeps its runtime ID over an older remote server with the same slug.
func TestHostedAttachmentWinsRuntimeIDCollision(t *testing.T) {
	t.Parallel()
	f := newHostedAttachmentFixture(t, "hosted_collision")
	colliding, err := mcpserversrepo.New(f.db).CreateMCPServer(t.Context(), mcpserversrepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: f.projectID, Name: pgtype.Text{String: "Billing remote", Valid: true},
		Slug: pgtype.Text{String: "billing", Valid: true}, RemoteMcpServerID: f.remote.RemoteMcpServerID, Visibility: "private",
	})
	require.NoError(t, err)
	_, err = mcpendpointsrepo.New(f.db).CreateMCPEndpoint(t.Context(), mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: f.projectID, McpServerID: uuid.NullUUID{UUID: colliding.ID, Valid: true}, Slug: "billing-remote",
	})
	require.NoError(t, err)
	f.addServerRow(t, colliding.ID, uuid.NullUUID{})
	f.addServerRow(t, f.hosted.ID, uuid.NullUUID{})

	servers := f.runtime(t)
	require.Len(t, servers, 1)
	require.Equal(t, "https://gram.test/mcp/org-test-billing-x1", servers[0].URL)
}

// A hosted attachment on a disabled wrapper stays listed and is skipped at runtime.
func TestHostedAttachmentOnDisabledWrapper(t *testing.T) {
	t.Parallel()
	f := newHostedAttachmentFixture(t, "hosted_disabled_leftover")
	off, err := toolsetsrepo.New(f.db).CreateToolset(t.Context(), toolsetsrepo.CreateToolsetParams{
		OrganizationID: "org-test", ProjectID: f.projectID, Name: "Off", Slug: "off",
	})
	require.NoError(t, err)
	createCanonicalWrapper(t, f.db, off, "org-test-off-x1", "disabled")
	f.addServerRow(t, off.ID, uuid.NullUUID{})

	require.Empty(t, f.runtime(t))
	view := f.view(t)
	require.Empty(t, view.McpServers)
	require.Len(t, view.Toolsets, 1)
}

// A wrapped toolset the runtime cannot dial is still stored under its wrapper.
func TestHostedAttachmentUndialableToolsetUsesWrapper(t *testing.T) {
	t.Parallel()
	f := newHostedAttachmentFixture(t, "hosted_undialable")
	f.disableHosted(t)
	f.deleteHostedEndpoints(t)
	require.Equal(t, f.hosted.ID, f.wrapper(t).ID, "the wrapper outlives its endpoints")

	name := "updated"
	_, err := f.core.UpdateAssistant(t.Context(), f.projectID, f.assistant, &name, nil, nil,
		[]*types.AssistantToolsetRef{{ToolsetSlug: "billing"}}, nil, nil, nil, nil, allowAllMCPEnable)
	require.NoError(t, err)
	counts := f.counts(t)
	require.EqualValues(t, 1, counts)
}

// A disabled, endpointless hosted server whose legacy alias still serves is enabled when attached.
func TestHostedAttachmentEnablesAliasOnlyWrapper(t *testing.T) {
	t.Parallel()
	f := newHostedAttachmentFixture(t, "hosted_alias_only")
	f.disableHosted(t)
	f.deleteHostedEndpoints(t)
	testenv.SetLegacyAlias(t, t.Context(), f.db, f.projectID, f.hosted.ID, "org-test-billing-x1", uuid.NullUUID{})

	name := "updated"
	_, err := f.core.UpdateAssistant(t.Context(), f.projectID, f.assistant, &name, nil, nil,
		[]*types.AssistantToolsetRef{{ToolsetSlug: "billing"}}, nil, nil, nil, nil, allowAllMCPEnable)
	require.NoError(t, err)
	require.Equal(t, "private", f.wrapper(t).Visibility)
	require.Equal(t, []string{"billing"}, runtimeIDs(f.runtime(t)))
}

// A toolset without a canonical wrapper has no MCP server to attach.
func TestHostedAttachmentWithoutWrapperRejected(t *testing.T) {
	t.Parallel()
	f := newHostedAttachmentFixture(t, "hosted_no_wrapper_rejected")
	name := "updated"
	_, err := f.core.UpdateAssistant(t.Context(), f.projectID, f.assistant, &name, nil, nil,
		[]*types.AssistantToolsetRef{{ToolsetSlug: "legacy"}}, nil, nil, nil, nil, allowAllMCPEnable)
	require.ErrorIs(t, err, errToolsetNoMCPServer)
	counts := f.counts(t)
	require.Zero(t, counts)
}

// A hosted server with a live custom-domain endpoint is dialed there, as endpointaddr.PrimaryEndpoint picks.
func TestHostedAttachmentPrefersLiveCustomDomainEndpoint(t *testing.T) {
	t.Parallel()
	f := newHostedAttachmentFixture(t, "hosted_custom_domain_first")
	ctx := t.Context()
	domains := customdomainsrepo.New(f.db)
	domain, err := domains.CreateCustomDomain(ctx, customdomainsrepo.CreateCustomDomainParams{
		OrganizationID: "org-test", Domain: "assistants.example.com", IngressName: pgtype.Text{String: "ingress", Valid: true},
		CertSecretName: pgtype.Text{String: "cert", Valid: true}, ProvisionerKind: "ingress", IpAllowlist: []string{},
	})
	require.NoError(t, err)
	_, err = domains.UpdateCustomDomain(ctx, customdomainsrepo.UpdateCustomDomainParams{
		ID: domain.ID, Verified: true, Activated: true, IngressName: pgtype.Text{String: "ingress", Valid: true}, CertSecretName: pgtype.Text{String: "cert", Valid: true},
	})
	require.NoError(t, err)
	_, err = mcpendpointsrepo.New(f.db).CreateMCPEndpoint(ctx, mcpendpointsrepo.CreateMCPEndpointParams{
		ProjectID: f.projectID, CustomDomainID: uuid.NullUUID{UUID: domain.ID, Valid: true},
		McpServerID: uuid.NullUUID{UUID: f.hosted.ID, Valid: true}, Slug: "billing-domain",
	})
	require.NoError(t, err)
	f.addServerRow(t, f.hosted.ID, uuid.NullUUID{})

	runtime := f.runtime(t)
	require.Len(t, runtime, 1)
	require.Equal(t, "https://assistants.example.com/mcp/billing-domain", runtime[0].URL)
}
