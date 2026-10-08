package main

import (
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/speakeasy-api/gram/server/internal/audit"
	auditrepo "github.com/speakeasy-api/gram/server/internal/audit/repo"
	mcprepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/plugins"
	pluginsrepo "github.com/speakeasy-api/gram/server/internal/plugins/repo"
	projectsrepo "github.com/speakeasy-api/gram/server/internal/projects/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/testenv/testrepo"
	toolsetsrepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
	"github.com/speakeasy-api/gram/server/internal/urn"
	"github.com/stretchr/testify/require"
)

func TestCleanupOptionsRequireExplicitApplyTargets(t *testing.T) {
	t.Parallel()
	base := []string{"-org", "test-org", "-project", uuid.NewString()}
	for _, tc := range []struct {
		name  string
		args  []string
		valid bool
	}{
		{name: "preview default", valid: true},
		{name: "explicit apply", args: []string{"-apply", "-actor", "test-user", "-memberships", uuid.NewString()}, valid: true},
		{name: "apply without IDs", args: []string{"-apply", "-actor", "test-user"}},
		{name: "apply without actor", args: []string{"-apply", "-memberships", uuid.NewString()}},
		{name: "IDs without apply", args: []string{"-memberships", uuid.NewString()}},
		{name: "unbounded preview", args: []string{"-limit", "101"}},
		{name: "malformed target", args: []string{"-apply", "-actor", "test-user", "-memberships", "invalid"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := parseOptions(append(append([]string{}, base...), tc.args...), io.Discard)
			if !tc.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.name == "explicit apply", cfg.apply)
		})
	}
}

func TestCleanupPreviewAndApply(t *testing.T) {
	t.Parallel()
	for _, connected := range []bool{true, false} {
		name := "without_marketplace"
		if connected {
			name = "with_marketplace"
		}
		t.Run(name, func(t *testing.T) { t.Parallel(); testCleanupPreviewAndApply(t, connected) })
	}
}

func testCleanupPreviewAndApply(t *testing.T, connected bool) {
	t.Helper()
	ctx := t.Context()
	infra, cleanup, err := testenv.Launch(ctx, testenv.LaunchOptions{Postgres: true})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	db, err := infra.CloneTestDatabase(t, "cleanup")
	require.NoError(t, err)
	org := "test-cleanup-org"
	require.NoError(t, testrepo.New(db).CreateOrganizationMetadataFixture(ctx, testrepo.CreateOrganizationMetadataFixtureParams{ID: org, Name: "Cleanup test", Slug: "cleanup-test", GramAccountType: "enterprise", FreeTrialStartedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, FreeTrialEndsAt: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true}}))
	project, err := projectsrepo.New(db).CreateProject(ctx, projectsrepo.CreateProjectParams{OrganizationID: org, Name: "Cleanup", Slug: "cleanup"})
	require.NoError(t, err)
	q := pluginsrepo.New(db)
	automatic, err := q.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{OrganizationID: org, ProjectID: project.ID, Name: "Automatic", Slug: "automatic"})
	require.NoError(t, err)
	manual, err := q.CreatePlugin(ctx, pluginsrepo.CreatePluginParams{OrganizationID: org, ProjectID: project.ID, Name: "Manual", Slug: "manual"})
	require.NoError(t, err)
	if connected {
		_, err = q.UpsertGitHubConnection(ctx, pluginsrepo.UpsertGitHubConnectionParams{ProjectID: project.ID, InstallationID: 1, RepoOwner: "test-owner", RepoName: "test-marketplace"})
		require.NoError(t, err)
	}

	toolset, err := toolsetsrepo.New(db).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{OrganizationID: org, ProjectID: project.ID, Name: "Platform", Slug: "platform"})
	require.NoError(t, err)
	_, err = toolsetsrepo.New(db).CreateToolsetVersion(ctx, toolsetsrepo.CreateToolsetVersionParams{ToolsetID: toolset.ID, Version: 1, ToolUrns: []urn.Tool{urn.NewTool(urn.ToolKindPlatform, "slack", "send_message")}, ResourceUrns: []urn.Resource{}})
	require.NoError(t, err)
	ordinary, err := toolsetsrepo.New(db).CreateToolset(ctx, toolsetsrepo.CreateToolsetParams{OrganizationID: org, ProjectID: project.ID, Name: "Ordinary", Slug: "ordinary"})
	require.NoError(t, err)
	wrapper, err := mcprepo.New(db).CreateMCPServer(ctx, mcprepo.CreateMCPServerParams{ID: uuid.New(), ProjectID: project.ID, Name: pgtype.Text{String: "Wrapper", Valid: true}, ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true}, Visibility: "private"})
	require.NoError(t, err)
	autoIDs := []uuid.UUID{}
	var manualID uuid.UUID
	for _, item := range []struct {
		name            string
		plugin          pluginsrepo.Plugin
		toolset, server uuid.NullUUID
		automatic       bool
	}{
		{name: "direct", plugin: automatic, toolset: uuid.NullUUID{UUID: toolset.ID, Valid: true}, automatic: true},
		{name: "wrapped", plugin: automatic, server: uuid.NullUUID{UUID: wrapper.ID, Valid: true}, automatic: true},
		{name: "manual", plugin: manual, toolset: uuid.NullUUID{UUID: toolset.ID, Valid: true}},
		{name: "ordinary", plugin: automatic, toolset: uuid.NullUUID{UUID: ordinary.ID, Valid: true}, automatic: true},
	} {
		membership, err := q.AddPluginServer(ctx, pluginsrepo.AddPluginServerParams{PluginID: item.plugin.ID, ToolsetID: item.toolset, McpServerID: item.server, DisplayName: item.name, Policy: "required"})
		require.NoError(t, err)
		if !item.automatic {
			manualID = membership.ID
			continue
		}
		var toolsetURN *urn.Toolset
		var mcpURN *urn.McpServer
		if item.toolset.Valid {
			value := urn.NewToolset(item.toolset.UUID)
			toolsetURN = &value
		}
		if item.server.Valid {
			value := urn.NewMcpServer(item.server.UUID)
			mcpURN = &value
		}
		require.NoError(t, audit.NewLogger().LogPluginServerAdd(ctx, db, audit.LogPluginServerAddEvent{OrganizationID: org, ProjectID: project.ID, Actor: urn.NewSystemPrincipal("automatic-role-distribution"), PluginID: item.plugin.ID, PluginName: item.plugin.Name, PluginSlug: item.plugin.Slug, ServerID: membership.ID, ServerDisplayName: membership.DisplayName, ServerPolicy: membership.Policy, ToolsetURN: toolsetURN, McpServerURN: mcpURN}))
		if item.name != "ordinary" {
			autoIDs = append(autoIDs, membership.ID)
		}
	}
	cfg := options{org: org, project: project.ID, limit: 1}
	candidates, ambiguous := []uuid.UUID{}, []uuid.UUID{}
	scanned := 0
	for {
		page, err := execute(ctx, db, cfg)
		require.NoError(t, err)
		require.LessOrEqual(t, page.Scanned, 1)
		scanned += page.Scanned
		for _, item := range page.Candidates {
			candidates = append(candidates, item.MembershipID)
		}
		for _, item := range page.Ambiguous {
			ambiguous = append(ambiguous, item.MembershipID)
		}
		if page.NextCursor == nil {
			break
		}
		cfg.after = *page.NextCursor
	}
	require.Equal(t, 4, scanned)
	require.ElementsMatch(t, autoIDs, candidates)
	require.Equal(t, []uuid.UUID{manualID}, ambiguous)
	rows, err := q.ListPluginServers(ctx, automatic.ID)
	require.NoError(t, err)
	require.Len(t, rows, 3, "preview does not remove anything")
	count := func() int64 {
		n, err := testrepo.New(db).CountPublishOutboxRowsByTopic(ctx, testrepo.CountPublishOutboxRowsByTopicParams{OrganizationID: org, Topic: "gram.plugins.v1.PublicationRequested"})
		require.NoError(t, err)
		return n
	}
	require.Zero(t, count(), "preview must not request publication")
	cfg = options{org: "wrong-org", project: project.ID, apply: true, memberships: append(candidates, manualID), actor: "test-operator"}
	wrongScope, err := execute(ctx, db, cfg)
	require.NoError(t, err)
	require.Empty(t, wrongScope.RemovedPluginIDs)
	cfg.org = org
	applied, err := execute(ctx, db, cfg)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{automatic.ID}, applied.RemovedPluginIDs)
	expectedPublications := int64(0)
	if connected {
		require.Equal(t, string(plugins.ProjectPublicationEnqueued), applied.Publication)
		expectedPublications = 1
	} else {
		require.Equal(t, string(plugins.ProjectPublicationNotConfigured), applied.Publication)
	}
	require.Equal(t, expectedPublications, count())
	assertRemovalAudit := func() {
		var removedIDs []uuid.UUID
		audits, err := auditrepo.New(db).ListAuditLogs(ctx, auditrepo.ListAuditLogsParams{OrganizationID: org, ProjectID: uuid.NullUUID{UUID: project.ID, Valid: true}, Action: pgtype.Text{String: "plugin:server_remove", Valid: true}})
		require.NoError(t, err)
		for _, event := range audits {
			require.Equal(t, "user", event.ActorType)
			require.Equal(t, cfg.actor, event.ActorID)
			var metadata struct {
				ServerID uuid.UUID `json:"server_id"`
			}
			require.NoError(t, json.Unmarshal(event.Metadata, &metadata))
			removedIDs = append(removedIDs, metadata.ServerID)
		}

		require.ElementsMatch(t, autoIDs, removedIDs)
	}
	assertRemovalAudit()
	rows, err = q.ListPluginServers(ctx, automatic.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, ordinary.ID, rows[0].ToolsetID.UUID)
	rows, err = q.ListPluginServers(ctx, manual.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	replay, err := execute(ctx, db, cfg)
	require.NoError(t, err)
	require.Empty(t, replay.RemovedPluginIDs)
	require.Equal(t, "not_required", replay.Publication)
	require.Equal(t, expectedPublications, count())
	assertRemovalAudit()
}
