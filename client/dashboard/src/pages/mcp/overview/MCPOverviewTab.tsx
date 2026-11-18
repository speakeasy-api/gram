import {
  ObservabilityDisabledState,
  ObservabilitySummaryTiles,
} from "@/components/observe/ObservabilitySummaryTiles";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import type { GetObservabilityOverviewResult } from "@gram/client/models/components/getobservabilityoverviewresult.js";
import { MemberWorkflowCTA } from "@/components/platform-mcp/member-workflow-cta";
import { RankedBarList } from "@/components/chart/RankedBarList";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { ToolCallsTimeSeriesChart } from "@/components/chart/ToolCallsTimeSeriesChart";
import { TopUsersTable } from "./TopUsersTable";
import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { overviewScope } from "./overview-scope";
import { telemetryGetObservabilityOverview } from "@gram/client/funcs/telemetryGetObservabilityOverview";
import { unwrapAsync } from "@gram/client/types/fp";
import { useDateRangeFilter } from "@/components/observe/useDateRangeFilter";
import { useGramContext } from "@gram/client/react-query/_context";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { useProject } from "@/contexts/Auth";
import { useObserveLogsLink } from "@/components/observe/observeDeepLink";
import type { ParsedTargetFilter } from "@/components/observe/observeTargetFilters";

// Both variants share the dashboard, but overview telemetry must use the
// variant's own scope: toolset slug or remote MCP server ID.
export type HostedServerRef =
  | { kind: "toolset"; id: string; slug: string; name: string }
  | { kind: "mcp-server"; id: string; slug: string; name: string };

function toolLabelFromUrn(urn: string): string {
  const parts = urn.split(":");
  return parts[parts.length - 1] || urn;
}

export function MCPOverviewTab({
  server,
  logsTarget,
}: {
  server: HostedServerRef;
  logsTarget?: ParsedTargetFilter | null;
}): React.JSX.Element {
  const client = useGramContext();
  const project = useProject();
  const observeLogsLink = useObserveLogsLink();
  const [expandedChart, setExpandedChart] = useState<string | null>(null);

  const {
    dateRange,
    customRange,
    customRangeLabel,
    from,
    to,
    setDateRangeParam,
    setCustomRangeParam,
    clearCustomRange,
  } = useDateRangeFilter();
  const timeRangeMs = useMemo(() => to.getTime() - from.getTime(), [from, to]);

  const { data, isLoading, isError, isLogsDisabled, refetch } =
    useLogsEnabledErrorCheck(
      useQuery<GetObservabilityOverviewResult>({
        queryKey: [
          "mcp-detail-overview",
          server.kind,
          server.id,
          server.slug,
          from.toISOString(),
          to.toISOString(),
        ],
        queryFn: () =>
          unwrapAsync(
            telemetryGetObservabilityOverview(client, {
              getObservabilityOverviewPayload: {
                from,
                to,
                ...overviewScope(server),
                includeTimeSeries: true,
              },
            }),
          ),
        placeholderData: keepPreviousData,
        throwOnError: false,
      }),
    );

  // With keepPreviousData a failed refetch still has stale data to show, so
  // only fall back to the error state when there is nothing to render at all.
  const showQueryError = isError && !data;

  const summary = data?.summary;
  const comparison = data?.comparison;
  const timeSeries = useMemo(() => data?.timeSeries ?? [], [data]);
  const toolLogsTarget =
    server.kind === "toolset"
      ? ({ type: "hosted", id: server.slug } satisfies ParsedTargetFilter)
      : logsTarget;

  const topByCount = (data?.topToolsByCount ?? []).map((tool) => {
    const toolName = toolLabelFromUrn(tool.gramUrn);
    return {
      key: tool.gramUrn,
      label: toolName,
      value: tool.callCount,
      href: toolLogsTarget
        ? observeLogsLink({ target: toolLogsTarget, toolName })
        : undefined,
    };
  });

  const topByFailureRate = (data?.topToolsByFailureRate ?? [])
    .filter((tool) => tool.failureCount > 0)
    .map((tool) => {
      const toolName = toolLabelFromUrn(tool.gramUrn);
      return {
        key: tool.gramUrn,
        label: toolName,
        value: tool.failureRate * 100,
        valueLabel: `${(tool.failureRate * 100).toFixed(1)}%`,
        href: toolLogsTarget
          ? observeLogsLink({ target: toolLogsTarget, toolName })
          : undefined,
      };
    });

  return (
    // Container queries, not viewport ones: the side panel narrows this column
    // without narrowing the window, and `lg:`/`xl:` would not notice.
    <Stack gap={6} className="@container mb-4">
      {!isLoading && !isLogsDisabled && (
        <MemberWorkflowCTA
          workflow="mcp_diagnostics"
          label="Troubleshoot in your agent"
          description="Investigate this server's recent failures with Platform MCP."
          scope="project:read"
          resourceId={project.id}
          projectSlug={project.slug}
          prompt={`Using Platform MCP, investigate recent failures for the MCP server ${JSON.stringify(server.name)} in project ${JSON.stringify(project.slug)}. Start with the project overview, then inspect this server's diagnostics and recent call summaries. Explain what the evidence supports and suggest next steps. Do not change configuration.`}
        />
      )}

      <div className="flex justify-end">
        <TimeRangePicker
          preset={customRange ? null : dateRange}
          customRange={customRange}
          customRangeLabel={customRangeLabel}
          onPresetChange={setDateRangeParam}
          onCustomRangeChange={setCustomRangeParam}
          onClearCustomRange={clearCustomRange}
        />
      </div>

      {isLogsDisabled && (
        <ObservabilityDisabledState subject="this MCP server" />
      )}
      {!isLogsDisabled && showQueryError && (
        <div role="alert">
          <InlineEmptyState
            icon="triangle-alert"
            heading="Couldn't load usage"
            description="Check your connection, then try again."
            action={
              <Button
                variant="secondary"
                size="sm"
                onClick={() => void refetch()}
              >
                <Button.Text>Retry</Button.Text>
              </Button>
            }
          />
        </div>
      )}
      {!isLogsDisabled && !showQueryError && (
        <>
          <ObservabilitySummaryTiles
            summary={summary}
            comparison={comparison}
            isLoading={isLoading}
            callsTitle="Tool calls"
          />

          <ToolCallsTimeSeriesChart
            title="Tool calls over time"
            chartId="mcp-detail-overview-tool-calls"
            timeSeries={timeSeries}
            timeRangeMs={timeRangeMs}
            expandedChart={expandedChart}
            onExpand={setExpandedChart}
            isLoading={isLoading}
          />

          <div className="grid grid-cols-1 gap-6 @3xl:grid-cols-2">
            <div className="border p-5">
              <h3 className="text-eyebrow mb-3">Top tools by call count</h3>
              {topByCount.length > 0 ? (
                <RankedBarList items={topByCount} />
              ) : (
                <WidgetEmptyState message="No tool calls in the selected range." />
              )}
            </div>
            <div className="border p-5">
              <h3 className="text-eyebrow mb-3">Top tools by failure rate</h3>
              {topByFailureRate.length > 0 ? (
                <RankedBarList items={topByFailureRate} />
              ) : (
                <WidgetEmptyState message="No failed tool calls in the selected range." />
              )}
            </div>
          </div>

          <TopUsersTable server={server} from={from} to={to} />
        </>
      )}
    </Stack>
  );
}
