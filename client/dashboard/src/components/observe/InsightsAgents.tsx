import { ChartCard } from "@/components/chart/ChartCard";
import { formatChartLabel } from "@/components/chart/chartUtils";
import { MetricCard } from "@/components/chart/MetricCard";
import { ErrorAlert } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";
import { dateTimeFormatters } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { telemetryGetObservabilityOverview } from "@gram/client/funcs/telemetryGetObservabilityOverview";
import { telemetrySearchUsers } from "@gram/client/funcs/telemetrySearchUsers";
import type {
  AccessMember,
  GetObservabilityOverviewResult,
  TimeSeriesBucket,
  UserSummary,
} from "@gram/client/models/components";
import { useGramContext, useMembers } from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import {
  CategoryScale,
  Chart as ChartJS,
  Filler,
  Legend,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip,
  type ChartOptions,
} from "chart.js";
import { useCallback, useMemo, useState } from "react";
import { Line } from "react-chartjs-2";
import { useQuery } from "@tanstack/react-query";
import {
  InsightsOverviewShell,
  type InsightsContentProps,
} from "./InsightsMCP";

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Filler,
  Tooltip,
  Legend,
);

const USER_SOURCE_COLORS = [
  "#60a5fa",
  "#fb923c",
  "#34d399",
  "#f87171",
  "#a78bfa",
  "#facc15",
  "#22d3ee",
  "#f472b6",
  "#a3e635",
];
const LINE_CHART_HEIGHT = { collapsed: 250, expanded: 600 };
const MAX_CHART_USERS = USER_SOURCE_COLORS.length;

export function InsightsAgentsContent() {
  const {
    data: membersData,
    isLoading: membersLoading,
    error: membersError,
  } = useMembers();
  const members = useMemo(() => membersData?.members ?? [], [membersData]);
  const memberById = useMemo(
    () => new Map(members.map((member) => [member.id, member])),
    [members],
  );
  const mapFilterOptions = useCallback(
    (
      options: Array<{ id: string; label: string; count: number }>,
      dimension: string,
    ) => {
      if (dimension !== "user") return options;

      return options.map((option) => {
        const member = memberById.get(option.id);
        return member
          ? { ...option, label: member.name || member.email || option.label }
          : option;
      });
    },
    [memberById],
  );

  return (
    <InsightsOverviewShell
      noDataKind="agent_sessions"
      showMcpFilter={false}
      filterDimensions={["all", "user", "agent"]}
      userFilterType="internal"
      fixedEventSource="hook"
      mapFilterOptions={mapFilterOptions}
      showSetupRequiredModal={false}
      title="Agent Session Usage Overview"
      subtitle="Monitor token consumption and tool usage across your team's agent sessions"
    >
      {(props) => (
        <InsightsAgentsInner
          {...props}
          members={members}
          membersLoading={membersLoading}
          membersError={membersError}
        />
      )}
    </InsightsOverviewShell>
  );
}

function InsightsAgentsInner({
  summary,
  comparison,
  comparisonLabel,
  isInsightsOpen,
  effectiveFrom,
  effectiveTo,
  timeRangeMs,
  filterDimension,
  selectedFilterValue,
  members,
}: InsightsContentProps & {
  members: AccessMember[];
  membersLoading: boolean;
  membersError: unknown;
}) {
  const client = useGramContext();
  const [expandedChart, setExpandedChart] = useState<string | null>(null);
  const selectedTopLevelUserId =
    filterDimension === "user" ? selectedFilterValue : null;
  const selectedAgent =
    filterDimension === "agent" ? selectedFilterValue : null;
  const usersQuery = useQuery({
    queryKey: [
      "insights",
      "agents",
      "users",
      effectiveFrom.toISOString(),
      effectiveTo.toISOString(),
      selectedTopLevelUserId,
      selectedAgent,
    ],
    queryFn: () =>
      fetchUserSummaries(client, effectiveFrom, effectiveTo, {
        userId: selectedTopLevelUserId,
        hookSource: selectedAgent,
      }),
    throwOnError: false,
  });
  const userRows = useMemo(
    () => buildUserRows(usersQuery.data ?? [], members),
    [usersQuery.data, members],
  );
  const chartUsers = useMemo(() => selectChartUsers(userRows), [userRows]);
  const userTimeSeriesQuery = useQuery({
    queryKey: [
      "insights",
      "agents",
      "user-time-series",
      effectiveFrom.toISOString(),
      effectiveTo.toISOString(),
      chartUsers.map((user) => user.id),
      selectedAgent,
    ],
    queryFn: () =>
      fetchUserTimeSeries(
        client,
        effectiveFrom,
        effectiveTo,
        chartUsers,
        selectedAgent,
      ),
    enabled: chartUsers.length > 0,
    throwOnError: false,
  });
  const userTimeSeries = userTimeSeriesQuery.data ?? [];
  const isUserTimeSeriesLoading =
    usersQuery.isLoading || userTimeSeriesQuery.isLoading;

  return (
    <>
      <section className="space-y-4">
        <h2 className="text-lg font-semibold">Usage Summary</h2>
        <div
          className={cn(
            "grid gap-4 transition-all duration-300",
            isInsightsOpen
              ? "grid-cols-1 md:grid-cols-2"
              : "grid-cols-1 md:grid-cols-3",
          )}
        >
          <MetricCard
            title="Total Sessions"
            value={summary?.totalChats ?? 0}
            previousValue={comparison?.totalChats ?? 0}
            icon="message-circle"
            comparisonLabel={comparisonLabel}
          />
          <MetricCard
            title="Total Tokens"
            value={getTotalTokens(summary)}
            previousValue={getTotalTokens(comparison)}
            icon="gauge"
            comparisonLabel={comparisonLabel}
          />
          <MetricCard
            title="Total Tool Calls"
            value={summary?.totalToolCalls ?? 0}
            previousValue={comparison?.totalToolCalls ?? 0}
            icon="wrench"
            comparisonLabel={comparisonLabel}
          />
        </div>
      </section>

      <div
        className={cn(
          "grid gap-4",
          expandedChart || isInsightsOpen
            ? "grid-cols-1"
            : "grid-cols-1 lg:grid-cols-2",
        )}
      >
        <UserBreakdownTimeSeriesChart
          title="Token Use Over Time"
          chartId="tokens-over-time"
          userTimeSeries={userTimeSeries}
          users={chartUsers}
          timeRangeMs={timeRangeMs}
          valueKey="totalTokens"
          valueLabel="Tokens"
          isLoading={isUserTimeSeriesLoading}
          error={userTimeSeriesQuery.error}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
        />
        <UserBreakdownTimeSeriesChart
          title="Tool Calls Over Time"
          chartId="tool-calls-over-time"
          userTimeSeries={userTimeSeries}
          users={chartUsers}
          timeRangeMs={timeRangeMs}
          valueKey="totalToolCalls"
          valueLabel="Tool calls"
          isLoading={isUserTimeSeriesLoading}
          error={userTimeSeriesQuery.error}
          expandedChart={expandedChart}
          onExpand={setExpandedChart}
        />
      </div>
    </>
  );
}

type UserUsageRow = {
  id: string;
  name: string;
  email: string;
  platforms: string[];
  totalTokens: number;
  totalToolCalls: number;
  toolCallSuccess: number;
  toolCallFailure: number;
  lastActivity: string;
  firstActivity: string;
  summary: UserSummary;
};

type UserTimeSeries = {
  userId: string;
  timeSeries: TimeSeriesBucket[];
};

type TimeSeriesDataset = {
  label: string;
  data: number[];
  borderColor: string;
  backgroundColor: string;
  pointBackgroundColor: string;
  fill: boolean;
  tension: number;
  borderWidth: number;
  pointRadius: number;
  pointHoverRadius: number;
};

type TokenUsageTotals = {
  totalTokens?: number | null;
  totalInputTokens?: number | null;
  totalOutputTokens?: number | null;
};

function getTotalTokens(metrics: TokenUsageTotals | null | undefined) {
  if (!metrics) return 0;
  const totalTokens = metrics.totalTokens ?? 0;
  if (totalTokens > 0) return totalTokens;
  return (metrics.totalInputTokens ?? 0) + (metrics.totalOutputTokens ?? 0);
}

function getMetricValue(
  point: TimeSeriesBucket,
  valueKey: "totalTokens" | "totalToolCalls",
) {
  return valueKey === "totalTokens"
    ? getTotalTokens(point)
    : point.totalToolCalls;
}

function UserBreakdownTimeSeriesChart({
  title,
  chartId,
  userTimeSeries,
  users,
  timeRangeMs,
  valueKey,
  valueLabel,
  isLoading,
  error,
  expandedChart,
  onExpand,
}: {
  title: string;
  chartId: string;
  userTimeSeries: UserTimeSeries[];
  users: UserUsageRow[];
  timeRangeMs: number;
  valueKey: "totalTokens" | "totalToolCalls";
  valueLabel: string;
  isLoading: boolean;
  error: Error | null;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}) {
  const isExpanded = expandedChart === chartId;
  const userLabels = useMemo(
    () => new Map(users.map((user) => [user.id, user.name || user.email])),
    [users],
  );
  const chartData = useMemo(
    () =>
      buildUserTimeSeriesChartData(
        userTimeSeries,
        userLabels,
        timeRangeMs,
        valueKey,
      ),
    [userTimeSeries, userLabels, timeRangeMs, valueKey],
  );
  const hasData = chartData.datasets.length > 0;

  return (
    <ChartCard
      title={title}
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={hasData || isLoading}
    >
      {error ? (
        <ErrorAlert
          title={`Unable to load ${title.toLowerCase()}`}
          error={error}
        />
      ) : isLoading ? (
        <Skeleton
          className="rounded-lg"
          style={{
            height: isExpanded
              ? LINE_CHART_HEIGHT.expanded
              : LINE_CHART_HEIGHT.collapsed,
          }}
        />
      ) : (
        <MultiLineChart
          labels={chartData.labels}
          tooltipLabels={chartData.tooltipLabels}
          datasets={chartData.datasets}
          valueLabel={valueLabel}
          height={
            isExpanded
              ? LINE_CHART_HEIGHT.expanded
              : LINE_CHART_HEIGHT.collapsed
          }
        />
      )}
    </ChartCard>
  );
}

function MultiLineChart({
  labels,
  tooltipLabels,
  datasets,
  valueLabel,
  height = LINE_CHART_HEIGHT.collapsed,
}: {
  labels: string[];
  tooltipLabels: string[];
  datasets: TimeSeriesDataset[];
  valueLabel: string;
  height?: number;
}) {
  const options = useMemo<ChartOptions<"line">>(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: "index", intersect: false },
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: {
            title: (items) => tooltipLabels[items[0]?.dataIndex ?? 0] ?? "",
            label: (item) => {
              if ((item.parsed.y ?? 0) === 0) return undefined;
              return `${item.dataset.label}: ${Number(
                item.parsed.y ?? 0,
              ).toLocaleString()} ${valueLabel.toLowerCase()}`;
            },
          },
        },
      },
      scales: {
        x: {
          grid: { display: true, color: "rgba(128, 128, 128, 0.1)" },
          ticks: { maxTicksLimit: 8 },
        },
        y: {
          beginAtZero: true,
          grid: { color: "rgba(128, 128, 128, 0.2)" },
          ticks: { precision: 0 },
        },
      },
      transitions: {
        resize: { animation: { duration: 0 } },
      },
    }),
    [tooltipLabels, valueLabel],
  );

  if (labels.length === 0 || datasets.length === 0) {
    return (
      <div className="text-muted-foreground flex h-[220px] items-center justify-center text-sm">
        No data for selected time range
      </div>
    );
  }

  return (
    <div style={{ height }}>
      <Line data={{ labels, datasets }} options={options} />
    </div>
  );
}

function selectChartUsers(users: UserUsageRow[]): UserUsageRow[] {
  const selected = new Map<string, UserUsageRow>();
  const byTokens = users.slice().sort((a, b) => b.totalTokens - a.totalTokens);
  const byToolCalls = users
    .slice()
    .sort((a, b) => b.totalToolCalls - a.totalToolCalls);

  for (const user of byTokens.slice(0, 5)) {
    if (user.totalTokens > 0) selected.set(user.id, user);
  }

  for (const user of byToolCalls) {
    if (selected.size >= MAX_CHART_USERS) break;
    if (user.totalToolCalls > 0) selected.set(user.id, user);
  }

  for (const user of byTokens) {
    if (selected.size >= MAX_CHART_USERS) break;
    if (user.totalTokens > 0 || user.totalToolCalls > 0) {
      selected.set(user.id, user);
    }
  }

  return Array.from(selected.values());
}

function buildUserTimeSeriesChartData(
  userTimeSeries: UserTimeSeries[],
  userLabels: Map<string, string>,
  timeRangeMs: number,
  valueKey: "totalTokens" | "totalToolCalls",
) {
  if (userTimeSeries.length === 0) {
    return { labels: [], tooltipLabels: [], datasets: [] };
  }

  const allTimestamps = new Set<number>();
  const valuesByUser = new Map<string, Map<number, number>>();

  for (const series of userTimeSeries) {
    const values = new Map<number, number>();
    for (const point of series.timeSeries) {
      const date = unixNanoToDate(point.bucketTimeUnixNano);
      const timestamp = date.getTime();
      allTimestamps.add(timestamp);
      values.set(
        timestamp,
        (values.get(timestamp) ?? 0) + getMetricValue(point, valueKey),
      );
    }
    valuesByUser.set(series.userId, values);
  }

  const sortedTimestamps = Array.from(allTimestamps).sort((a, b) => a - b);
  const labels = sortedTimestamps.map((timestamp) =>
    formatChartLabel(new Date(timestamp), timeRangeMs),
  );
  const tooltipLabels = sortedTimestamps.map((timestamp) =>
    new Date(timestamp).toLocaleString([], {
      month: "short",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
    }),
  );

  const datasets = userTimeSeries
    .map((series, index) => {
      const values = valuesByUser.get(series.userId) ?? new Map();
      const data = sortedTimestamps.map(
        (timestamp) => values.get(timestamp) ?? 0,
      );
      const total = data.reduce((sum, value) => sum + value, 0);
      if (total === 0) return null;

      const color = USER_SOURCE_COLORS[index % USER_SOURCE_COLORS.length]!;
      return {
        label: userLabels.get(series.userId) ?? series.userId,
        data,
        borderColor: color,
        backgroundColor: `${color}1a`,
        pointBackgroundColor: color,
        fill: false,
        tension: 0.45,
        borderWidth: 1.5,
        pointRadius: 0,
        pointHoverRadius: 4,
      };
    })
    .filter((dataset): dataset is TimeSeriesDataset => dataset != null);

  return { labels, tooltipLabels, datasets };
}

async function fetchUserSummaries(
  client: Parameters<typeof telemetrySearchUsers>[0],
  from: Date,
  to: Date,
  filters: { userId: string | null; hookSource: string | null },
): Promise<UserSummary[]> {
  const users: UserSummary[] = [];
  let cursor: string | undefined;

  do {
    const result = await unwrapAsync(
      telemetrySearchUsers(client, {
        searchUsersPayload: {
          cursor,
          filter: {
            from,
            to,
            eventSource: "hook",
            hookSource: filters.hookSource ?? undefined,
            userIds: filters.userId ? [filters.userId] : undefined,
          },
          limit: 1000,
          sort: "desc",
          userType: "internal",
        },
      }),
    );

    users.push(...result.users);
    cursor = result.nextCursor;
  } while (cursor);

  return users;
}

async function fetchUserTimeSeries(
  client: Parameters<typeof telemetryGetObservabilityOverview>[0],
  from: Date,
  to: Date,
  users: UserUsageRow[],
  hookSource: string | null,
): Promise<UserTimeSeries[]> {
  const result: UserTimeSeries[] = [];

  for (const user of users) {
    const overview = await fetchUserOverview(
      client,
      from,
      to,
      user.id,
      hookSource,
    );
    result.push({
      userId: user.id,
      timeSeries: overview.timeSeries,
    });
  }

  return result;
}

async function fetchUserOverview(
  client: Parameters<typeof telemetryGetObservabilityOverview>[0],
  from: Date,
  to: Date,
  userId: string,
  hookSource: string | null = null,
): Promise<GetObservabilityOverviewResult> {
  return unwrapAsync(
    telemetryGetObservabilityOverview(client, {
      getObservabilityOverviewPayload: {
        from,
        to,
        includeTimeSeries: true,
        userId,
        eventSource: "hook",
        hookSource: hookSource ?? undefined,
      },
    }),
  );
}

function buildUserRows(
  summaries: UserSummary[],
  members: AccessMember[],
): UserUsageRow[] {
  const memberById = new Map(members.map((member) => [member.id, member]));

  return summaries
    .map((summary) => {
      const member = memberById.get(summary.userId);
      const displayId = summary.userId;

      return {
        id: displayId,
        name: member?.name ?? displayId,
        email: member?.email ?? "Unknown email",
        platforms: summary.hookSources.map((source) => source.source),
        totalTokens: getTotalTokens(summary),
        totalToolCalls: summary.totalToolCalls,
        toolCallSuccess: summary.toolCallSuccess,
        toolCallFailure: summary.toolCallFailure,
        firstActivity: formatUnixNano(summary.firstSeenUnixNano),
        lastActivity: formatUnixNano(summary.lastSeenUnixNano),
        summary,
      };
    })
    .sort((a, b) => b.totalTokens - a.totalTokens);
}

function formatUnixNano(value: string) {
  return dateTimeFormatters.humanize(unixNanoToDate(value));
}

function unixNanoToDate(value: string) {
  const nanos = BigInt(value);
  const millis = Number(nanos / 1_000_000n);

  return new Date(millis);
}
