import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { Toolset } from "@/lib/toolTypes";
import { getPresetRange } from "@gram-ai/elements";
import { telemetryGetObservabilityOverview } from "@gram/client/funcs/telemetryGetObservabilityOverview";
import type { GetObservabilityOverviewResult } from "@gram/client/models/components";
import { useGramContext } from "@gram/client/react-query/_context";
import { unwrapAsync } from "@gram/client/types/fp";
import { Stack } from "@speakeasy-api/moonshine";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { ToolCallsTimeSeriesChart } from "@/components/chart/ToolCallsTimeSeriesChart";
import { Sparkline } from "@/pages/costs/Sparkline";
import { PluginStatusBanner } from "./PluginStatusBanner";
import { TopUsersTable } from "./TopUsersTable";

// Fixed window — this tab is a glanceable summary, not the deep-dive
// analytics view (mcp.x's AnalyticsTab has an interactive range picker).
const OVERVIEW_RANGE = "7d" as const;

export function MCPOverviewTab({
  toolset,
}: {
  toolset: Toolset;
}): React.JSX.Element {
  const client = useGramContext();
  const [expandedChart, setExpandedChart] = useState<string | null>(null);

  const { from, to, timeRangeMs } = useMemo(() => {
    const range = getPresetRange(OVERVIEW_RANGE);
    return {
      from: range.from,
      to: range.to,
      timeRangeMs: range.to.getTime() - range.from.getTime(),
    };
  }, []);

  const { data, isLoading, isLogsDisabled } = useLogsEnabledErrorCheck(
    useQuery<GetObservabilityOverviewResult>({
      queryKey: [
        "mcp-detail-overview",
        toolset.slug,
        from.toISOString(),
        to.toISOString(),
      ],
      queryFn: () =>
        unwrapAsync(
          telemetryGetObservabilityOverview(client, {
            getObservabilityOverviewPayload: {
              from,
              to,
              toolsetSlug: toolset.slug,
              includeTimeSeries: true,
            },
          }),
        ),
      placeholderData: keepPreviousData,
      throwOnError: false,
    }),
  );

  const timeSeries = useMemo(() => data?.timeSeries ?? [], [data]);
  const totalToolCalls = data?.summary?.totalToolCalls ?? 0;

  return (
    <Stack gap={6} className="mb-4">
      <PluginStatusBanner toolset={toolset} />

      {isLogsDisabled ? (
        <div className="flex flex-col items-center justify-center rounded-lg border p-12 text-center">
          <Type muted className="mb-1 block">
            Observability is not enabled
          </Type>
          <Type muted small>
            Enable logs for this organization to see usage for this MCP server.
          </Type>
        </div>
      ) : (
        <>
          <div className="flex items-center justify-between gap-4 rounded-lg border p-5">
            <div className="flex flex-col gap-1">
              <Type variant="small" muted>
                Tool calls, last 7 days
              </Type>
              <Heading variant="h3">
                {isLoading ? "—" : totalToolCalls.toLocaleString()}
              </Heading>
            </div>
            <Sparkline
              values={timeSeries.map((bucket) => bucket.totalToolCalls)}
              width={160}
              height={40}
            />
          </div>

          <ToolCallsTimeSeriesChart
            title="Tool calls over time"
            chartId="mcp-detail-overview-tool-calls"
            timeSeries={timeSeries}
            timeRangeMs={timeRangeMs}
            expandedChart={expandedChart}
            onExpand={setExpandedChart}
          />

          <TopUsersTable toolsetSlug={toolset.slug} from={from} to={to} />
        </>
      )}
    </Stack>
  );
}
