package telemetry_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	gen "github.com/speakeasy-api/gram/server/gen/telemetry"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	"github.com/speakeasy-api/gram/server/internal/conv"
	mcpserversRepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	toolsetsRepo "github.com/speakeasy-api/gram/server/internal/toolsets/repo"
)

func TestGetObservabilityOverview_CanonicalHostedIdentity(t *testing.T) {
	t.Parallel()
	ctx, ti := newTestLogsService(t)
	ac, _ := contextvalues.GetAuthContext(ctx)
	toolset, err := toolsetsRepo.New(ti.conn).CreateToolset(ctx, toolsetsRepo.CreateToolsetParams{
		OrganizationID: ac.ActiveOrganizationID, ProjectID: *ac.ProjectID,
		Name: "Hosted overview", Slug: "hosted-overview", McpEnabled: true,
	})
	require.NoError(t, err)
	servers := mcpserversRepo.New(ti.conn)
	canonical, err := servers.CreateMCPServer(ctx, mcpserversRepo.CreateMCPServerParams{
		ID: toolset.ID, ProjectID: *ac.ProjectID, Visibility: "private",
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true},
	})
	require.NoError(t, err)
	member, err := servers.CreateMCPServer(ctx, mcpserversRepo.CreateMCPServerParams{
		ID: uuid.New(), ProjectID: *ac.ProjectID, Visibility: "private",
		ToolsetID: uuid.NullUUID{UUID: toolset.ID, Valid: true},
	})
	require.NoError(t, err)
	// Align the window with the unfiltered summary materialized view buckets.
	now := time.Now().UTC().Truncate(time.Hour)
	deploymentID := uuid.NewString()
	remoteID, gatewayID := uuid.NewString(), uuid.NewString()
	for _, event := range []struct {
		server string
		tool   string
		ago    time.Duration
		status int32
	}{
		{"", "legacy", 10 * time.Minute, 200},
		{canonical.ID.String(), "canonical", 9 * time.Minute, 502},
		{member.ID.String(), "member", 8 * time.Minute, 500},
		{"", "legacy", 70 * time.Minute, 200},
		{member.ID.String(), "member", 69 * time.Minute, 500},
		{member.ID.String(), "member", 68 * time.Minute, 500},
	} {
		attrs := map[string]any{"gram.toolset.slug": toolset.Slug}
		if event.server == member.ID.String() {
			attrs["gram.remote_mcp_server.id"] = remoteID
			attrs["gram.meta_mcp_server.id"] = gatewayID
		}
		insertMCPServerToolCallLog(t, ctx, ac.ProjectID.String(), deploymentID, now.Add(-event.ago),
			"tools:remote:"+event.server+":"+event.tool, event.server,
			attrs, event.status, 0.4)
	}
	testenv.FlushClickHouseAsyncInserts(t, ti.chConn)

	payload := &gen.GetObservabilityOverviewPayload{
		From: now.Add(-time.Hour).Format(time.RFC3339), To: now.Format(time.RFC3339),
		ToolsetSlug: &toolset.Slug, IncludeTimeSeries: true,
	}
	got, err := ti.service.GetObservabilityOverview(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, int64(2), got.Summary.TotalToolCalls)
	require.Equal(t, int64(1), got.Summary.FailedToolCalls)
	require.Equal(t, int64(1), got.Comparison.TotalToolCalls)
	var seriesCalls int64
	for _, bucket := range got.TimeSeries {
		seriesCalls += bucket.TotalToolCalls
	}
	require.Equal(t, int64(2), seriesCalls)
	for _, tools := range [][]*gen.ToolMetric{got.TopToolsByCount, got.TopToolsByFailureRate} {
		require.Len(t, tools, 2)
		for _, tool := range tools {
			require.NotContains(t, tool.GramUrn, ":member")
		}
	}

	// Explicit server, remote, and gateway scopes still include their member
	// calls even when a toolset slug is also supplied.
	for _, scope := range []struct {
		name                          string
		serverID, remoteID, gatewayID *string
	}{
		{"member", conv.PtrEmpty(member.ID.String()), nil, nil},
		{"remote", nil, &remoteID, nil},
		{"gateway", nil, nil, &gatewayID},
	} {
		scoped := *payload
		scoped.McpServerID = scope.serverID
		scoped.RemoteMcpServerID = scope.remoteID
		scoped.MetaMcpServerID = scope.gatewayID
		got, err := ti.service.GetObservabilityOverview(ctx, &scoped)
		require.NoError(t, err, scope.name)
		require.Equal(t, int64(1), got.Summary.TotalToolCalls)
		require.Equal(t, int64(2), got.Comparison.TotalToolCalls)
		var calls int64
		for _, bucket := range got.TimeSeries {
			calls += bucket.TotalToolCalls
		}
		require.Equal(t, int64(1), calls)
		for _, tools := range [][]*gen.ToolMetric{got.TopToolsByCount, got.TopToolsByFailureRate} {
			require.Len(t, tools, 1)
			require.Equal(t, "tools:remote:"+member.ID.String()+":member", tools[0].GramUrn)
		}
	}

	// Project-wide metrics remain unchanged.
	payload.ToolsetSlug = nil
	got, err = ti.service.GetObservabilityOverview(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, int64(3), got.Summary.TotalToolCalls)
	require.Equal(t, int64(3), got.Comparison.TotalToolCalls)

	// Historical slugs without a live canonical retain slug-only metrics.
	payload.ToolsetSlug = &toolset.Slug
	_, err = servers.DeleteMCPServer(ctx, mcpserversRepo.DeleteMCPServerParams{ID: canonical.ID, ProjectID: *ac.ProjectID})
	require.NoError(t, err)
	got, err = ti.service.GetObservabilityOverview(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, int64(3), got.Summary.TotalToolCalls)
	_, err = toolsetsRepo.New(ti.conn).DeleteToolset(ctx, toolsetsRepo.DeleteToolsetParams{Slug: toolset.Slug, ProjectID: *ac.ProjectID})
	require.NoError(t, err)
	got, err = ti.service.GetObservabilityOverview(ctx, payload)
	require.NoError(t, err)
	require.Equal(t, int64(3), got.Summary.TotalToolCalls)

}
