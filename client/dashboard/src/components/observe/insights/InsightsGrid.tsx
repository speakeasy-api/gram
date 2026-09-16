import { StackedTimeBarChart } from "@/components/chart/StackedTimeBarChart";
import {
  CARD_COLORS,
  INSIGHT_OTHER_COLOR,
  INSIGHT_SERIES_COLORS,
} from "@/components/observe/insights/insightsPalette";
import {
  InsightCard,
  MetricSpark,
} from "@/components/observe/insights/InsightCard";
import {
  RankedList,
  type RankedRow,
} from "@/components/observe/insights/RankedList";
import {
  useObserveLogsLink,
  type ObserveDeepLinkScope,
} from "@/components/observe/observeDeepLink";
import { buildToolUsageTimeSeries } from "@/components/observe/toolUsageTimeSeriesChartData";
import type { useServerNameMappings } from "@/hooks/useServerNameMappings";
import type { ToolUsageClientSummary } from "@gram/client/models/components/toolusageclientsummary.js";
import type { ToolUsageTargetSummary } from "@gram/client/models/components/toolusagetargetsummary.js";
import type { ToolUsageTargetTimeSeriesPoint } from "@gram/client/models/components/toolusagetargettimeseriespoint.js";
import type { ToolUsageTargetToolBreakdownRow } from "@gram/client/models/components/toolusagetargettoolbreakdownrow.js";
import type { ToolUsageTotals } from "@gram/client/models/components/toolusagetotals.js";
import type { ToolUsageUserSummary } from "@gram/client/models/components/toolusageusersummary.js";
import { useEffect, useMemo, useState } from "react";

type SectionState = { pending: boolean; error: boolean };

const SERVER_TARGET_TYPES = new Set([
  "hosted_mcp_server",
  "tunneled_mcp_server",
  "meta_mcp_server",
  "shadow_mcp_server",
]);

function percent(rate: number): string {
  return `${(rate * 100).toFixed(1)}%`;
}

function targetScope(target: ToolUsageTargetSummary): ObserveDeepLinkScope {
  switch (target.targetType) {
    case "hosted_mcp_server":
      return { target: { type: "hosted", id: target.targetId } };
    case "meta_mcp_server":
      return { target: { type: "gateway", id: target.targetId } };
    case "shadow_mcp_server":
      return { target: { type: "shadow", id: target.targetId } };
    case "tunneled_mcp_server":
      return { target: { type: "shadow", id: target.targetId } };
    case "skill":
      return { targetTypes: ["skill"] };
    case "local_tool":
    default:
      return { targetTypes: ["local_tool"] };
  }
}

/**
 * The insights board: one question per card, each answered by a number or a
 * ranked list, and each card a way into the rows behind it.
 *
 * It replaces six full-width stacked bar charts that all looked alike and
 * answered "who used what" six slightly different ways. The questions here are
 * the ones the admin journey actually asks — what ran, what failed, which
 * servers, tools, skills, clients and people are behind it.
 */
export function InsightsGrid({
  totals,
  targets,
  users,
  clients,
  targetToolBreakdown,
  timeSeries,
  from,
  to,
  serverNameMappings,
  status,
  onRangeSelect,
}: {
  totals: ToolUsageTotals | undefined;
  targets: ToolUsageTargetSummary[];
  users: ToolUsageUserSummary[];
  clients: ToolUsageClientSummary[];
  targetToolBreakdown: ToolUsageTargetToolBreakdownRow[];
  timeSeries: ToolUsageTargetTimeSeriesPoint[];
  from: Date;
  to: Date;
  serverNameMappings: ReturnType<typeof useServerNameMappings>;
  status: {
    totals: SectionState;
    targets: SectionState;
    users: SectionState;
    clients: SectionState;
    targetToolBreakdown: SectionState;
    targetTimeSeries: SectionState;
  };
  onRangeSelect?: (from: Date, to: Date) => void;
}): JSX.Element {
  const logsLink = useObserveLogsLink();
  const [highlightedServer, setHighlightedServer] = useState<string | null>(
    null,
  );

  const displayLabel = (target: ToolUsageTargetSummary) =>
    target.targetType === "shadow_mcp_server"
      ? (serverNameMappings.rawToDisplay.get(target.targetLabel) ??
        target.targetLabel)
      : target.targetLabel;

  const chartData = useMemo(
    () =>
      buildToolUsageTimeSeries(
        timeSeries,
        (point) =>
          point.targetType === "shadow_mcp_server"
            ? (serverNameMappings.rawToDisplay.get(point.targetLabel) ??
              point.targetLabel)
            : point.targetLabel,
        from,
        to,
        undefined,
        INSIGHT_SERIES_COLORS,
        INSIGHT_OTHER_COLOR,
      ),
    [timeSeries, from, to, serverNameMappings],
  );

  // A server keeps one colour everywhere it appears: its stack segment, its
  // row in "most used", its row in "most errors". The chart builder is the one
  // that decides those colours — it ranks the series and folds everything past
  // the palette into a single "Other" — so the lists read their colours back
  // off it rather than ranking a second time and disagreeing at the fold.
  const seriesColors = useMemo(() => {
    const colors = new Map<string, string>();
    for (const dataset of chartData.datasets) {
      if (dataset.label && typeof dataset.backgroundColor === "string") {
        colors.set(dataset.label, dataset.backgroundColor);
      }
    }
    return colors;
  }, [chartData.datasets]);

  const colorForServerRow = (row: RankedRow) =>
    seriesColors.get(row.label) ?? INSIGHT_OTHER_COLOR;

  // Hovering a server row isolates it: the others fade rather than disappear,
  // so the bar heights stay put and the eye can still see the share it takes
  // out of each day.
  const datasets = useMemo(
    () =>
      chartData.datasets.map((dataset) => {
        const color =
          seriesColors.get(dataset.label ?? "") ?? INSIGHT_OTHER_COLOR;
        const dimmed =
          highlightedServer !== null && dataset.label !== highlightedServer;
        return {
          ...dataset,
          backgroundColor: dimmed ? `${color}1f` : color,
          hoverBackgroundColor: color,
        };
      }),
    [chartData.datasets, seriesColors, highlightedServer],
  );

  // Hovering a row the chart does not draw — a skill, a local tool, a server
  // folded into "Other" — would dim every series and highlight nothing.
  const highlightRow = (row: RankedRow | null) =>
    setHighlightedServer(row && seriesColors.has(row.label) ? row.label : null);

  // The headline trend: total calls per bucket, for the sparkline silhouettes.
  // A range or filter change can drop the hovered row out from under the
  // pointer, which would otherwise leave the chart dimmed with nothing lit.
  useEffect(() => {
    setHighlightedServer(null);
  }, [timeSeries, targets]);

  const callSeries = useMemo(() => {
    const buckets = new Map<string, number>();
    for (const point of timeSeries) {
      buckets.set(
        point.bucketStartNs,
        (buckets.get(point.bucketStartNs) ?? 0) + Number(point.eventCount),
      );
    }
    return [...buckets.entries()]
      .sort(([a], [b]) => (a < b ? -1 : 1))
      .map(([, count]) => count);
  }, [timeSeries]);

  const failureSeries = useMemo(() => {
    const buckets = new Map<string, number>();
    for (const point of timeSeries) {
      buckets.set(
        point.bucketStartNs,
        (buckets.get(point.bucketStartNs) ?? 0) + Number(point.failureCount),
      );
    }
    return [...buckets.entries()]
      .sort(([a], [b]) => (a < b ? -1 : 1))
      .map(([, count]) => count);
  }, [timeSeries]);

  const servers = targets.filter((target) =>
    SERVER_TARGET_TYPES.has(target.targetType),
  );
  const skills = targets.filter((target) => target.targetType === "skill");

  const serverRows: RankedRow[] = servers.map((target) => ({
    id: `${target.targetType}:${target.targetId}`,
    label: displayLabel(target),
    value: Number(target.eventCount),
    href: logsLink(targetScope(target)),
  }));

  const erroringRows: RankedRow[] = [...targets]
    .filter((target) => Number(target.failureCount) > 0)
    .sort((a, b) => Number(b.failureCount) - Number(a.failureCount))
    .map((target) => ({
      id: `err:${target.targetType}:${target.targetId}`,
      label: displayLabel(target),
      value: Number(target.failureCount),
      secondary: percent(target.failureRate),
      href: logsLink({
        ...targetScope(target),
        // A skill's identity is its tool name; targetScope only narrows to the
        // type, which would open every failing skill.
        ...(target.targetType === "skill" ? { toolName: target.targetId } : {}),
        statuses: ["error"],
      }),
    }));

  const skillRows: RankedRow[] = skills.map((target) => ({
    id: `skill:${target.targetId}`,
    label: target.targetLabel,
    value: Number(target.eventCount),
    href: logsLink({ targetTypes: ["skill"], toolName: target.targetId }),
  }));

  const clientRows: RankedRow[] = clients.map((client) => ({
    id: `client:${client.clientKey}`,
    label: client.clientLabel,
    value: Number(client.eventCount),
    href: logsLink({ clientKey: client.clientKey }),
  }));

  const userRows: RankedRow[] = users.map((user) => ({
    id: `user:${user.userKey}`,
    label: user.userLabel,
    value: Number(user.eventCount),
    href: logsLink({
      userEmail: user.userKind === "email" ? user.userKey : undefined,
    }),
  }));

  // Tools are ranked across every target: "which tool is busiest" is a question
  // about the catalog, not about one server's slice of it.
  const toolRows: RankedRow[] = useMemo(() => {
    const totalsByTool = new Map<string, number>();
    for (const row of targetToolBreakdown) {
      totalsByTool.set(
        row.toolName,
        (totalsByTool.get(row.toolName) ?? 0) + Number(row.eventCount),
      );
    }
    return [...totalsByTool.entries()]
      .sort(([, a], [, b]) => b - a)
      .map(([toolName, count]) => ({
        id: `tool:${toolName}`,
        label: toolName,
        value: count,
        href: logsLink({ toolName }),
      }));
  }, [targetToolBreakdown, logsLink]);

  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <InsightCard
          title="Tool calls"
          href={logsLink()}
          loading={status.totals.pending}
          error={status.totals.error}
        >
          <MetricSpark
            value={(Number(totals?.eventCount) || 0).toLocaleString()}
            series={callSeries}
          />
        </InsightCard>

        <InsightCard
          title="Failures"
          href={logsLink({ statuses: ["error"] })}
          loading={status.totals.pending}
          error={status.totals.error}
        >
          <MetricSpark
            value={(Number(totals?.failureCount) || 0).toLocaleString()}
            series={failureSeries}
            tone={Number(totals?.failureCount) > 0 ? "destructive" : "default"}
            caption={
              totals ? `${percent(totals.failureRate)} of calls` : undefined
            }
          />
        </InsightCard>

        <InsightCard
          title="Tools used"
          href={logsLink()}
          loading={status.totals.pending}
          error={status.totals.error}
        >
          <MetricSpark
            value={(Number(totals?.uniqueTools) || 0).toLocaleString()}
            series={[]}
            caption={`across ${Number(totals?.uniqueTargets) || 0} sources`}
          />
        </InsightCard>

        <InsightCard
          title="People"
          href={logsLink()}
          loading={status.totals.pending}
          error={status.totals.error}
        >
          <MetricSpark
            value={(Number(totals?.uniqueUsers) || 0).toLocaleString()}
            series={[]}
            caption="active in this window"
          />
        </InsightCard>
      </div>

      <InsightCard
        title="Calls over time"
        href={logsLink()}
        loading={status.targetTimeSeries.pending}
        error={status.targetTimeSeries.error}
      >
        <StackedTimeBarChart
          labels={chartData.labels}
          timestamps={chartData.timestamps}
          bucketMs={chartData.bucketMs}
          tooltipLabels={chartData.tooltipLabels}
          datasets={datasets}
          onRangeSelect={onRangeSelect}
        />
      </InsightCard>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <InsightCard
          title="Most used MCP servers"
          href={logsLink()}
          loading={status.targets.pending}
          error={status.targets.error}
        >
          <RankedList
            color={colorForServerRow}
            onRowHover={highlightRow}
            rows={serverRows}
          />
        </InsightCard>

        <InsightCard
          title="Most used tools"
          href={logsLink()}
          loading={status.targetToolBreakdown.pending}
          error={status.targetToolBreakdown.error}
        >
          <RankedList color={CARD_COLORS.tools} rows={toolRows} />
        </InsightCard>

        <InsightCard
          title="Most used clients"
          href={logsLink()}
          loading={status.clients.pending}
          error={status.clients.error}
        >
          <RankedList
            color={CARD_COLORS.clients}
            rows={clientRows}
            emptyMessage="No client reported a name in this window"
          />
        </InsightCard>

        <InsightCard
          title="Most errors"
          href={logsLink({ statuses: ["error"] })}
          loading={status.targets.pending}
          error={status.targets.error}
        >
          <RankedList
            color={colorForServerRow}
            onRowHover={highlightRow}
            rows={erroringRows}
            emptyMessage="Nothing failed"
          />
        </InsightCard>

        <InsightCard
          title="Most used skills"
          href={logsLink({ targetTypes: ["skill"] })}
          loading={status.targets.pending}
          error={status.targets.error}
        >
          <RankedList
            color={CARD_COLORS.skills}
            rows={skillRows}
            emptyMessage="No skills invoked"
          />
        </InsightCard>

        <InsightCard
          title="Busiest people"
          href={logsLink()}
          loading={status.users.pending}
          error={status.users.error}
        >
          <RankedList color={CARD_COLORS.people} rows={userRows} />
        </InsightCard>
      </div>
    </div>
  );
}
