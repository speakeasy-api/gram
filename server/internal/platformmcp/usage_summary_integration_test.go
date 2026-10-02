package platformmcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/speakeasy-api/gram/server/internal/authz"
	"github.com/speakeasy-api/gram/server/internal/contextvalues"
	mcpserversrepo "github.com/speakeasy-api/gram/server/internal/mcpservers/repo"
	telemetryrepo "github.com/speakeasy-api/gram/server/internal/telemetry/repo"
	"github.com/speakeasy-api/gram/server/internal/testenv"
	"github.com/speakeasy-api/gram/server/internal/urn"
)

// TestGetToolUsageSummaryAttributesToConfiguredServers walks the vertical
// under a real project grant: the read is scoped to the project and window the
// caller named with the project's own matchers, and a hook-observed call the
// pipeline filed as shadow MCP under the configured server's name is reported
// for that server, with its mcp_id, rather than as shadow usage.
func TestGetToolUsageSummaryAttributesToConfiguredServers(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_tool_usage_summary")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)
	servers, err := mcpserversrepo.New(conn).ListMCPServersForTelemetryByProjectID(ctx, project.ID)
	require.NoError(t, err)
	require.Len(t, servers, 1)
	configured := servers[0]
	require.True(t, configured.RemoteMcpServerID.Valid)

	fixedNow := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	toolUsage := &recordingToolUsageReader{
		totals: telemetryrepo.ToolUsageTotalsRow{EventCount: 16, SuccessCount: 13, FailureCount: 3, BlockedCount: 1, UniqueTools: 4, UniqueUsers: 2, UniqueTargets: 3},
		rows: []telemetryrepo.ToolUsageTargetSummaryRow{
			// The gateway lane attributed these by the remote server's source id.
			usageRow(telemetryrepo.ToolUsageTargetTypeHostedMCP, configured.Slug.String, configured.Name.String, 8, 2),
			// A hook reported these under the configured server's display name
			// with no URL, so the pipeline could only file them as shadow MCP.
			usageRow(telemetryrepo.ToolUsageTargetTypeShadowMCP, configured.Name.String, configured.Name.String, 4, 1),
			// A shadow server no configured server is known by.
			usageRow(telemetryrepo.ToolUsageTargetTypeShadowMCP, "/usr/local/bin/private-stdio-server", "/usr/local/bin/private-stdio-server", 4, 0),
		},
	}
	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	reader := NewPostgresReader(testenv.NewLogger(t), conn, nil).WithAuthorization(engine)
	service := NewDiagnosticsService(conn, stubUsageSummaryTelemetry{}, func(context.Context, string) (bool, error) { return false, nil }, reader, nil,
		OperationBudget{Connection: allowOperationLimiter{}, Organization: allowOperationLimiter{}}).
		WithToolUsageBreakdown(toolUsage)
	service.now = func() time.Time { return fixedNow }

	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	ctx = authz.GrantsToContext(ctx, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, project.ID.String())})

	output, err := service.GetToolUsageSummary(ctx, principal, GetToolUsageSummaryInput{ProjectID: project.ID.String(), Window: "24h"})
	require.NoError(t, err)

	require.Equal(t, project.ID.String(), output.ProjectID)
	require.Equal(t, DiagnosticWindowLastDay, output.Envelope.ResolvedWindow.Window)
	require.False(t, output.Envelope.NoObservations)
	require.Equal(t, int64(16), output.ToolCalls)
	require.Equal(t, int64(3), output.FailedToolCalls)
	require.Equal(t, int64(1), output.BlockedToolCalls)
	require.Equal(t, int64(4), output.UniqueTools)
	require.False(t, output.TargetsTruncated)

	hosted := bucketByType(t, output.UsageByTarget, telemetryrepo.ToolUsageTargetTypeHostedMCP)
	require.Equal(t, int64(12), hosted.ToolCalls, "the shadow-filed calls under the configured server's name fold into it")
	require.Equal(t, int64(3), hosted.FailedToolCalls)
	require.Equal(t, 1, hosted.Targets)
	require.InDelta(t, 75.0, hosted.SharePercent, 0.001)
	require.Equal(t, []ToolUsageTarget{{Name: configured.Name.String, MCPID: configured.ID.String(), ToolCalls: 12, FailedToolCalls: 3}}, hosted.TopTargets)

	shadow := bucketByType(t, output.UsageByTarget, telemetryrepo.ToolUsageTargetTypeShadowMCP)
	require.Equal(t, int64(4), shadow.ToolCalls)
	require.Equal(t, 1, shadow.Targets)
	require.Empty(t, shadow.TopTargets)

	// Both reads were scoped to this project and window with its own matchers.
	// Asserted on each read rather than on one shared field: the totals and the
	// per-target rows have to describe the same population of calls, or the
	// shares the result reports are computed against a different denominator
	// than the buckets they divide.
	require.Equal(t, 2, toolUsage.calls)
	expectedMatchers := []telemetryrepo.MCPServerMatcher{{
		SourceID:    configured.RemoteMcpServerID.UUID.String(),
		MCPServerID: configured.ID.String(),
		TargetType:  telemetryrepo.ToolUsageTargetTypeHostedMCP,
		TargetID:    configured.Slug.String,
		TargetLabel: configured.Name.String,
	}}
	for name, params := range map[string]telemetryrepo.GetToolUsageSummaryParams{
		"totals":  toolUsage.totalsParams,
		"targets": toolUsage.targetsParams,
	} {
		require.Equal(t, project.ID.String(), params.GramProjectID, name)
		require.Equal(t, fixedNow.Add(-24*time.Hour).UnixNano(), params.TimeStart, name)
		require.Equal(t, fixedNow.UnixNano(), params.TimeEnd, name)
		require.Empty(t, params.TargetTypes, name)
		require.Empty(t, params.Statuses, name)
		require.Empty(t, params.Query, name)
		require.Empty(t, params.Filters, name)
		require.Empty(t, params.UserFilters, name)
		// One past the aggregation cap, so an omitted target can be told from a
		// result that exactly fills it.
		require.Equal(t, uint64(usageSummaryTargetRowLimit+1), params.TargetLimit, name)
		require.Equal(t, expectedMatchers, params.MCPServerMatchers, name)
	}

	encoded, err := json.Marshal(output)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-stdio-server")
}

// Without a grant on the project the read is refused before any store is
// touched, so a caller cannot learn a project's usage mix by naming its id.
func TestGetToolUsageSummaryRequiresProjectRead(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_tool_usage_summary_denied")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)

	toolUsage := &recordingToolUsageReader{}
	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	reader := NewPostgresReader(testenv.NewLogger(t), conn, nil).WithAuthorization(engine)
	service := NewDiagnosticsService(conn, stubUsageSummaryTelemetry{}, func(context.Context, string) (bool, error) { return false, nil }, reader, nil,
		OperationBudget{Connection: allowOperationLimiter{}, Organization: allowOperationLimiter{}}).
		WithToolUsageBreakdown(toolUsage)

	ctx = contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
	ctx = contextvalues.SetActingSurface(ctx, contextvalues.ActingSurfacePlatformMCP)
	ctx = authz.GrantsToContext(ctx, nil)

	_, err = service.GetToolUsageSummary(ctx, principal, GetToolUsageSummaryInput{ProjectID: project.ID.String()})
	require.Error(t, err)
	require.Zero(t, toolUsage.calls, "a refused caller never reaches the read")
}

// TestGetToolUsageSummaryReportsTruncationOnlyWhenATargetWasOmitted pins the
// difference between a result that exactly fills the aggregation cap and one
// that left a target out. The read asks for one row past the cap; that sentinel
// proves the omission and must not be aggregated, or the buckets would count
// calls the same result reports as missing.
func TestGetToolUsageSummaryReportsTruncationOnlyWhenATargetWasOmitted(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	conn, err := platformMCPInfra.CloneTestDatabase(t, "platform_mcp_tool_usage_truncation")
	require.NoError(t, err)
	principal, project := seedRegistrationLifecycle(t, ctx, conn)

	// One call each on distinct skills, so every row is its own target and none
	// of them depends on name resolution.
	atCap := make([]telemetryrepo.ToolUsageTargetSummaryRow, 0, usageSummaryTargetRowLimit+1)
	for index := range usageSummaryTargetRowLimit {
		name := fmt.Sprintf("skill-%04d", index)
		atCap = append(atCap, usageRow(telemetryrepo.ToolUsageTargetTypeSkill, name, name, 1, 0))
	}
	// The sentinel carries a distinctive count, so aggregating it would be
	// visible in the bucket rather than hiding inside an off-by-one.
	const sentinelCalls = 1000
	pastCap := append(append([]telemetryrepo.ToolUsageTargetSummaryRow{}, atCap...),
		usageRow(telemetryrepo.ToolUsageTargetTypeSkill, "skill-sentinel", "skill-sentinel", sentinelCalls, 0))

	engine := authz.NewEngine(testenv.NewLogger(t), conn, func(context.Context, string) (bool, error) { return false, nil }, nil)
	reader := NewPostgresReader(testenv.NewLogger(t), conn, nil).WithAuthorization(engine)

	for _, test := range []struct {
		name      string
		rows      []telemetryrepo.ToolUsageTargetSummaryRow
		truncated bool
	}{
		{name: "exactly at the cap omits nothing", rows: atCap, truncated: false},
		{name: "one past the cap omits a target", rows: pastCap, truncated: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			toolUsage := &recordingToolUsageReader{
				totals: telemetryrepo.ToolUsageTotalsRow{EventCount: usageSummaryTargetRowLimit, SuccessCount: usageSummaryTargetRowLimit},
				rows:   test.rows,
			}
			service := NewDiagnosticsService(conn, stubUsageSummaryTelemetry{}, func(context.Context, string) (bool, error) { return false, nil }, reader, nil,
				OperationBudget{Connection: allowOperationLimiter{}, Organization: allowOperationLimiter{}}).
				WithToolUsageBreakdown(toolUsage)

			scoped := contextvalues.WithAuthenticatedActor(ctx, &contextvalues.AuthContext{ActiveOrganizationID: principal.OrganizationID, UserID: principal.UserID}, urn.NewPrincipal(urn.PrincipalTypeUser, principal.UserID))
			scoped = contextvalues.SetActingSurface(scoped, contextvalues.ActingSurfacePlatformMCP)
			scoped = authz.GrantsToContext(scoped, []authz.Grant{authz.NewGrant(authz.ScopeProjectRead, project.ID.String())})

			output, err := service.GetToolUsageSummary(scoped, principal, GetToolUsageSummaryInput{ProjectID: project.ID.String()})
			require.NoError(t, err)

			require.Equal(t, test.truncated, output.TargetsTruncated)
			skills := bucketByType(t, output.UsageByTarget, telemetryrepo.ToolUsageTargetTypeSkill)
			require.Equal(t, usageSummaryTargetRowLimit, skills.Targets, "the sentinel is never counted as a target")
			require.Equal(t, int64(usageSummaryTargetRowLimit), skills.ToolCalls, "the sentinel's calls are never aggregated")

			encoded, err := json.Marshal(output)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "skill-sentinel", "the sentinel is dropped, not reported")
		})
	}
}
