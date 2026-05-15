import { Icon } from "@speakeasy-api/moonshine";
import { ChartCard } from "@/components/chart/ChartCard";
import { formatChartLabel } from "@/components/chart/chartUtils";
import { MetricCard } from "@/components/chart/MetricCard";
import { InsightsConfig } from "@/components/insights-sidebar";
import { useInsightsState } from "@/components/insights-context";
import { ErrorAlert } from "@/components/ui/alert";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Skeleton } from "@/components/ui/skeleton";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { cn } from "@/lib/utils";
import { telemetryGetObservabilityOverview } from "@gram/client/funcs/telemetryGetObservabilityOverview";
import { telemetryGetUserMetricsSummary } from "@gram/client/funcs/telemetryGetUserMetricsSummary";
import { telemetrySearchUsers } from "@gram/client/funcs/telemetrySearchUsers";
import type {
  GetObservabilityOverviewResult,
  ProjectSummary,
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
import { slugify } from "@/lib/constants";
import { useMemo, useState } from "react";
import { Line } from "react-chartjs-2";
import { useParams } from "react-router";
import { useQuery } from "@tanstack/react-query";

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Filler,
  Tooltip,
  Legend,
);

const CHART_COLOR = "#60a5fa";
const LOOKBACK_DAYS = 30;

export function InsightsEmployeeDetailContent() {
  const { userSlug } = useParams<{ userSlug: string }>();
  const client = useGramContext();
  const { isExpanded: isInsightsOpen } = useInsightsState();
  const mcpConfig = useObservabilityMcpConfig({
    toolsToInclude: ["gram_search_users", "gram_list_organization_users"],
  });
  const {
    data: membersData,
    isLoading: membersLoading,
    error: membersError,
  } = useMembers();
  const routeUser = useMemo(
    () => (userSlug ? decodeURIComponent(userSlug) : ""),
    [userSlug],
  );
  const members = useMemo(() => membersData?.members ?? [], [membersData]);
  const member = useMemo(
    () =>
      members.find(
        (m) => slugify(m.name) === userSlug || m.email === routeUser,
      ),
    [members, routeUser, userSlug],
  );

  const { from, to, timeRangeMs } = useMemo(() => {
    const end = new Date();
    const start = new Date(end);
    start.setDate(start.getDate() - LOOKBACK_DAYS);
    return { from: start, to: end, timeRangeMs: LOOKBACK_DAYS * 86_400_000 };
  }, []);

  const fallbackUserQuery = useQuery({
    queryKey: [
      "insights",
      "employee-detail",
      "fallback-user",
      routeUser,
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: () => fetchMatchingUserSummary(client, from, to, routeUser),
    enabled: member == null && routeUser !== "",
    throwOnError: false,
  });
  const resolvedUserId = member?.id ?? fallbackUserQuery.data?.userId;

  const summaryQuery = useQuery({
    queryKey: [
      "insights",
      "employee-detail",
      "summary",
      resolvedUserId,
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: () => fetchUserSummary(client, from, to, resolvedUserId!),
    enabled: resolvedUserId != null,
    throwOnError: false,
  });

  const metricsQuery = useQuery({
    queryKey: [
      "insights",
      "employee-detail",
      "metrics",
      resolvedUserId,
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: () => fetchUserMetrics(client, from, to, resolvedUserId!),
    enabled: resolvedUserId != null,
    throwOnError: false,
  });

  const overviewQuery = useQuery({
    queryKey: [
      "insights",
      "employee-detail",
      "overview",
      resolvedUserId,
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: () => fetchUserOverview(client, from, to, resolvedUserId!),
    enabled: resolvedUserId != null,
    throwOnError: false,
  });

  const summary = summaryQuery.data ?? fallbackUserQuery.data ?? null;
  const metrics = metricsQuery.data;
  const overview = overviewQuery.data;
  const timeSeries = overview?.timeSeries ?? [];
  const [expandedChart, setExpandedChart] = useState<string | null>(null);

  const displayName =
    member?.name ?? fallbackUserQuery.data?.userId ?? routeUser ?? "Employee";
  const displayEmail =
    member?.email ??
    (resolvedUserId?.includes("@") ? resolvedUserId : "Unknown email");

  const totalTokens = getTotalTokens(summary);
  const isLoading =
    membersLoading ||
    (member == null && fallbackUserQuery.isLoading) ||
    (resolvedUserId != null && summaryQuery.isLoading);
  const error = summaryQuery.error ?? fallbackUserQuery.error ?? membersError;

  return (
    <>
      <InsightsConfig
        mcpConfig={mcpConfig}
        title={`What would you like to know about ${displayName}?`}
        subtitle="Ask about token usage, tool activity, and platform breakdown"
        suggestions={[
          {
            title: "Usage Summary",
            label: "Summarize usage",
            prompt: `Summarize the token and tool usage for ${displayName} (${displayEmail}) over the last ${LOOKBACK_DAYS} days.`,
          },
          {
            title: "Platform Breakdown",
            label: "Show platforms",
            prompt: `What platforms has ${displayName} been using?`,
          },
        ]}
      />
      <div className="min-h-0 w-full flex-1 overflow-y-auto p-8 pb-24">
        <div className="mx-auto flex max-w-7xl flex-col gap-6">
          <div className="flex items-center gap-3">
            <Avatar className="size-12">
              {member?.photoUrl && (
                <AvatarImage src={member.photoUrl} alt={displayName} />
              )}
              <AvatarFallback className="text-base font-semibold">
                {getInitials(displayName)}
              </AvatarFallback>
            </Avatar>
            <div>
              <h1 className="text-xl font-semibold">{displayName}</h1>
              <p className="text-muted-foreground text-sm">{displayEmail}</p>
            </div>
          </div>

          {error ? (
            <ErrorAlert
              title="Unable to load employee usage data"
              error={error}
            />
          ) : isLoading ? (
            <DetailLoadingState isInsightsOpen={isInsightsOpen} />
          ) : (
            <>
              <section
                className={cn(
                  "grid gap-4 transition-all duration-300",
                  isInsightsOpen
                    ? "grid-cols-1 md:grid-cols-2"
                    : "grid-cols-1 md:grid-cols-3",
                )}
              >
                <FirstActivityCard
                  firstSeenUnixNano={summary?.firstSeenUnixNano}
                />
                <MetricCard
                  title="Total Tokens"
                  value={totalTokens}
                  icon="gauge"
                />
                <MetricCard
                  title="Tool Calls"
                  value={summary?.totalToolCalls ?? 0}
                  icon="wrench"
                  subtext={`${(summary?.toolCallSuccess ?? 0).toLocaleString()} succeeded / ${(summary?.toolCallFailure ?? 0).toLocaleString()} failed`}
                />
              </section>

              <section
                className={cn(
                  "grid gap-4 transition-all duration-300",
                  isInsightsOpen ? "grid-cols-1" : "grid-cols-1 lg:grid-cols-2",
                )}
              >
                <BreakdownCard
                  title="Platform Breakdown"
                  rows={(summary?.hookSources ?? []).map((source) => ({
                    label: formatPlatform(source.source),
                    value: source.eventCount,
                    valueLabel: `${source.eventCount.toLocaleString()} events`,
                  }))}
                  emptyLabel="No platform data"
                />
                <BreakdownCard
                  title="Top Used Tools"
                  rows={(summary?.tools ?? [])
                    .slice()
                    .sort((a, b) => b.count - a.count)
                    .slice(0, 8)
                    .map((tool) => ({
                      label: formatToolUrn(tool.urn),
                      value: tool.count,
                      valueLabel: `${tool.count.toLocaleString()} calls (${tool.successCount.toLocaleString()} ok / ${tool.failureCount.toLocaleString()} blocked)`,
                    }))}
                  emptyLabel="No tool calls"
                />
              </section>

              {metricsQuery.error ? (
                <ErrorAlert
                  title="Unable to load model metrics"
                  error={metricsQuery.error}
                />
              ) : metricsQuery.isLoading ? (
                <Skeleton className="h-40 rounded-lg" />
              ) : (
                <BreakdownCard
                  title="Model Usage"
                  rows={(metrics?.models ?? [])
                    .slice()
                    .sort((a, b) => b.count - a.count)
                    .map((model) => ({
                      label: model.name,
                      value: model.count,
                      valueLabel: `${model.count.toLocaleString()} requests`,
                    }))}
                  emptyLabel="No model usage"
                />
              )}

              {overviewQuery.error ? (
                <ErrorAlert
                  title="Unable to load time series"
                  error={overviewQuery.error}
                />
              ) : overviewQuery.isLoading ? (
                <Skeleton className="h-72 rounded-lg" />
              ) : (
                <TokenTimeSeriesChart
                  title="Token Use Over Time"
                  chartId="user-tokens-over-time"
                  timeSeries={timeSeries}
                  timeRangeMs={timeRangeMs}
                  hasData={timeSeries.some(
                    (point) => getTotalTokens(point) > 0,
                  )}
                  expandedChart={expandedChart}
                  onExpand={setExpandedChart}
                />
              )}
            </>
          )}
        </div>
      </div>
    </>
  );
}

function FirstActivityCard({
  firstSeenUnixNano,
}: {
  firstSeenUnixNano?: string | null;
}) {
  const hasActivity =
    firstSeenUnixNano != null &&
    firstSeenUnixNano !== "" &&
    firstSeenUnixNano !== "0";
  const date = hasActivity ? unixNanoToDate(firstSeenUnixNano) : null;
  const primary = date
    ? date.toLocaleDateString([], {
        month: "short",
        day: "numeric",
        year: "numeric",
      })
    : "No activity";
  const subtext = date ? `${daysSince(date).toLocaleString()} days ago` : null;

  return (
    <div className="bg-card border-border rounded-lg border p-5">
      <div className="mb-3 flex items-center justify-between">
        <span className="text-sm font-semibold">First Activity</span>
        <div className="bg-muted/50 rounded-lg p-2">
          <Icon name="calendar" className="text-muted-foreground size-4" />
        </div>
      </div>
      <span className="block text-3xl font-semibold tracking-tight">
        {primary}
      </span>
      {subtext && (
        <span className="text-muted-foreground mt-1 block text-xs">
          {subtext}
        </span>
      )}
    </div>
  );
}

function daysSince(date: Date) {
  const ms = Date.now() - date.getTime();
  return Math.max(0, Math.floor(ms / 86_400_000));
}

function DetailLoadingState({ isInsightsOpen }: { isInsightsOpen: boolean }) {
  return (
    <>
      <section
        className={cn(
          "grid gap-4 transition-all duration-300",
          isInsightsOpen
            ? "grid-cols-1 md:grid-cols-2"
            : "grid-cols-1 md:grid-cols-3",
        )}
      >
        {Array.from({ length: 3 }).map((_, index) => (
          <div key={index} className="bg-card rounded-lg border p-5">
            <Skeleton className="mb-4 h-4 w-28" />
            <Skeleton className="h-9 w-20" />
            <Skeleton className="mt-3 h-3 w-36" />
          </div>
        ))}
      </section>
      <section className="grid gap-4 lg:grid-cols-2">
        <Skeleton className="h-48 rounded-lg" />
        <Skeleton className="h-48 rounded-lg" />
      </section>
      <Skeleton className="h-40 rounded-lg" />
      <Skeleton className="h-72 rounded-lg" />
    </>
  );
}

function BreakdownCard({
  title,
  rows,
  emptyLabel,
}: {
  title: string;
  rows: Array<{ label: string; value: number; valueLabel: string }>;
  emptyLabel: string;
}) {
  const total = rows.reduce((sum, row) => sum + row.value, 0);

  return (
    <section className="rounded-lg border p-4">
      <h3 className="font-semibold">{title}</h3>
      <div className="mt-4 space-y-3">
        {rows.length > 0 ? (
          rows.map((row) => (
            <div key={row.label} className="space-y-1.5">
              <div className="flex items-center justify-between gap-3 text-sm">
                <span className="truncate">{row.label}</span>
                <span className="text-muted-foreground shrink-0">
                  {row.valueLabel}
                </span>
              </div>
              <div className="bg-muted h-2 overflow-hidden rounded-full">
                <div
                  className="bg-primary h-full rounded-full"
                  style={{
                    width: `${total > 0 ? Math.max((row.value / total) * 100, 4) : 0}%`,
                  }}
                />
              </div>
            </div>
          ))
        ) : (
          <p className="text-muted-foreground text-sm">{emptyLabel}</p>
        )}
      </div>
    </section>
  );
}

function TokenTimeSeriesChart({
  title,
  chartId,
  timeSeries,
  timeRangeMs,
  hasData,
  expandedChart,
  onExpand,
}: {
  title: string;
  chartId: string;
  timeSeries: TimeSeriesBucket[];
  timeRangeMs: number;
  hasData: boolean;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}) {
  const isExpanded = expandedChart === chartId;
  const height = isExpanded ? 420 : 220;

  const chartData = useMemo(() => {
    const points = timeSeries.map((point) => {
      const date = unixNanoToDate(point.bucketTimeUnixNano);
      return {
        label: formatChartLabel(date, timeRangeMs),
        tooltipLabel: date.toLocaleString([], {
          month: "short",
          day: "numeric",
          hour: "numeric",
          minute: "2-digit",
        }),
        value: getTotalTokens(point),
      };
    });

    return {
      labels: points.map((p) => p.label),
      tooltipLabels: points.map((p) => p.tooltipLabel),
      values: points.map((p) => p.value),
    };
  }, [timeSeries, timeRangeMs]);

  const options = useMemo<ChartOptions<"line">>(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: "index", intersect: false },
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: {
            title: (items) =>
              chartData.tooltipLabels[items[0]?.dataIndex ?? 0] ?? "",
            label: (item) =>
              `Tokens: ${Number(item.parsed.y ?? 0).toLocaleString()}`,
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
    }),
    [chartData.tooltipLabels],
  );

  return (
    <ChartCard
      title={title}
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={hasData}
    >
      {!hasData ? (
        <div className="text-muted-foreground flex h-[220px] items-center justify-center text-sm">
          No data for selected time range
        </div>
      ) : (
        <div style={{ height }}>
          <Line
            data={{
              labels: chartData.labels,
              datasets: [
                {
                  label: "Tokens",
                  data: chartData.values,
                  borderColor: CHART_COLOR,
                  backgroundColor: `${CHART_COLOR}1a`,
                  pointBackgroundColor: CHART_COLOR,
                  fill: true,
                  tension: 0.45,
                  borderWidth: 1.5,
                  pointRadius: 0,
                  pointHoverRadius: 4,
                },
              ],
            }}
            options={options}
          />
        </div>
      )}
    </ChartCard>
  );
}

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

function getInitials(name: string) {
  return name
    .split(" ")
    .map((part) => part[0])
    .join("")
    .slice(0, 2)
    .toUpperCase();
}

async function fetchMatchingUserSummary(
  client: Parameters<typeof telemetrySearchUsers>[0],
  from: Date,
  to: Date,
  identifier: string,
): Promise<UserSummary | null> {
  let cursor: string | undefined;
  do {
    const result = await unwrapAsync(
      telemetrySearchUsers(client, {
        searchUsersPayload: {
          cursor,
          filter: {
            from,
            to,
          },
          limit: 1000,
          sort: "desc",
          userType: "internal",
        },
      }),
    );

    const match = result.users.find(
      (user) =>
        user.userId === identifier || slugify(user.userId) === identifier,
    );
    if (match) return match;
    cursor = result.nextCursor;
  } while (cursor);

  return null;
}

async function fetchUserSummary(
  client: Parameters<typeof telemetrySearchUsers>[0],
  from: Date,
  to: Date,
  userId: string,
): Promise<UserSummary | null> {
  const result = await unwrapAsync(
    telemetrySearchUsers(client, {
      searchUsersPayload: {
        filter: {
          from,
          to,
          userIds: [userId],
        },
        limit: 1,
        sort: "desc",
        userType: "internal",
      },
    }),
  );

  return result.users[0] ?? null;
}

async function fetchUserMetrics(
  client: Parameters<typeof telemetryGetUserMetricsSummary>[0],
  from: Date,
  to: Date,
  userId: string,
): Promise<ProjectSummary> {
  const result = await unwrapAsync(
    telemetryGetUserMetricsSummary(client, {
      getUserMetricsSummaryPayload: {
        from,
        to,
        userId,
      },
    }),
  );

  return result.metrics;
}

async function fetchUserOverview(
  client: Parameters<typeof telemetryGetObservabilityOverview>[0],
  from: Date,
  to: Date,
  userId: string,
): Promise<GetObservabilityOverviewResult> {
  return unwrapAsync(
    telemetryGetObservabilityOverview(client, {
      getObservabilityOverviewPayload: {
        from,
        to,
        includeTimeSeries: true,
        userId,
      },
    }),
  );
}

function unixNanoToDate(value: string) {
  const nanos = BigInt(value);
  const millis = Number(nanos / 1_000_000n);
  return new Date(millis);
}

function formatPlatform(value: string) {
  return value
    .split(/[-_]/)
    .filter(Boolean)
    .map((part) => part[0]!.toUpperCase() + part.slice(1))
    .join(" ");
}

function formatToolUrn(value: string) {
  const parts = value.split(/[/:]/).filter(Boolean);
  return parts[parts.length - 1] ?? value;
}
