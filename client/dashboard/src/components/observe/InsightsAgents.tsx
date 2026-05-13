import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { ChartCard } from "@/components/chart/ChartCard";
import { formatChartLabel, smoothData } from "@/components/chart/chartUtils";
import { MetricCard } from "@/components/chart/MetricCard";
import { InsightsConfig } from "@/components/insights-sidebar";
import { useInsightsState } from "@/components/insights-context";
import { ErrorAlert } from "@/components/ui/alert";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { cn } from "@/lib/utils";
import { telemetryGetObservabilityOverview } from "@gram/client/funcs/telemetryGetObservabilityOverview";
import { telemetryGetProjectMetricsSummary } from "@gram/client/funcs/telemetryGetProjectMetricsSummary";
import { telemetrySearchUsers } from "@gram/client/funcs/telemetrySearchUsers";
import type {
  GetObservabilityOverviewResult,
  ModelUsage,
  ProjectSummary,
  TimeSeriesBucket,
  UserSummary,
} from "@gram/client/models/components";
import { useGramContext, useMembers } from "@gram/client/react-query";
import { unwrapAsync } from "@gram/client/types/fp";
import {
  TimeRangePicker,
  type DateRangePreset,
  getPresetRange,
} from "@gram-ai/elements";
import { useQuery } from "@tanstack/react-query";
import {
  BarElement,
  CategoryScale,
  Chart as ChartJS,
  Filler,
  Legend,
  LineElement,
  LinearScale,
  PointElement,
  Tooltip,
  type ChartOptions,
} from "chart.js";
import { useMemo, useState } from "react";
import { Bar } from "react-chartjs-2";

ChartJS.register(
  CategoryScale,
  LinearScale,
  BarElement,
  LineElement,
  PointElement,
  Filler,
  Tooltip,
  Legend,
);

type ValueMode = "tokens" | "cost";

const PRESET_RANGE_LABELS: Record<DateRangePreset, string> = {
  "15m": "the last 15 minutes",
  "1h": "the last hour",
  "4h": "the last 4 hours",
  "1d": "the last day",
  "2d": "the last 2 days",
  "3d": "the last 3 days",
  "7d": "the last 7 days",
  "15d": "the last 15 days",
  "30d": "the last 30 days",
  "90d": "the last 90 days",
};

const CHART_COLORS = [
  "#60a5fa", // blue
  "#34d399", // emerald
  "#f97316", // orange
  "#a78bfa", // violet
  "#fb7185", // rose
  "#facc15", // yellow
  "#38bdf8", // sky
  "#c084fc", // purple
  "#4ade80", // green
  "#f472b6", // pink
];

function formatCost(value: number): string {
  if (value >= 1) return `$${value.toFixed(2)}`;
  if (value >= 0.01) return `$${value.toFixed(3)}`;
  if (value > 0) return `$${value.toFixed(4)}`;
  return "$0.00";
}

function formatTokens(value: number): string {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`;
  return value.toLocaleString();
}

function formatValue(value: number, mode: ValueMode): string {
  return mode === "cost" ? formatCost(value) : formatTokens(value);
}

function formatPlatform(value: string): string {
  return value
    .split(/[-_]/)
    .filter(Boolean)
    .map((part) => part[0]!.toUpperCase() + part.slice(1))
    .join(" ");
}

function initials(name: string): string {
  const parts = name.split(/[\s-]+/).filter(Boolean);
  if (parts.length >= 2)
    return (parts[0]![0]! + parts[parts.length - 1]![0]!).toUpperCase();
  return (name[0] ?? "?").toUpperCase();
}

function unixNanoToDate(value: string): Date {
  const nanos = BigInt(value);
  const millis = Number(nanos / 1_000_000n);
  return new Date(millis);
}

function ValueModeToggle({
  mode,
  onChange,
}: {
  mode: ValueMode;
  onChange: (mode: ValueMode) => void;
}) {
  return (
    <div className="border-border flex h-[34px] items-center rounded-md border p-0.5">
      {(["tokens", "cost"] as const).map((option) => (
        <button
          key={option}
          onClick={() => onChange(option)}
          className={cn(
            "h-7 rounded px-3 text-xs font-medium transition-all duration-150",
            mode === option
              ? "text-foreground bg-white shadow-sm dark:bg-gray-900"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          {option === "tokens" ? "Tokens" : "Cost ($)"}
        </button>
      ))}
    </div>
  );
}

export function InsightsAgentsContent() {
  const client = useGramContext();
  const { isExpanded: isInsightsOpen } = useInsightsState();
  const mcpConfig = useObservabilityMcpConfig({
    toolsToInclude: ["gram_search_users", "gram_list_organization_users"],
  });

  const [dateRange, setDateRange] = useState<DateRangePreset>("30d");
  const [customRange, setCustomRange] = useState<{
    from: Date;
    to: Date;
  } | null>(null);
  const [customRangeLabel, setCustomRangeLabel] = useState<string | null>(null);
  const [valueMode, setValueMode] = useState<ValueMode>("tokens");
  const [expandedChart, setExpandedChart] = useState<string | null>(null);
  const [clientFilter, setClientFilter] = useState<string>("all");

  const { from, to, timeRangeMs } = useMemo(() => {
    const range = customRange ?? getPresetRange(dateRange);
    return {
      from: range.from,
      to: range.to,
      timeRangeMs: range.to.getTime() - range.from.getTime(),
    };
  }, [customRange, dateRange]);

  const rangeLabel = useMemo(() => {
    if (customRange) return customRangeLabel ?? "the selected range";
    return PRESET_RANGE_LABELS[dateRange] ?? "the selected range";
  }, [customRange, customRangeLabel, dateRange]);

  const { data: membersData, isLoading: membersLoading } = useMembers();
  const memberMap = useMemo(
    () => new Map((membersData?.members ?? []).map((m) => [m.id, m])),
    [membersData],
  );

  const usersQuery = useQuery({
    queryKey: [
      "insights",
      "agents",
      "users",
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: () => fetchAllUsers(client, from, to),
    throwOnError: false,
  });

  const projectQuery = useQuery({
    queryKey: [
      "insights",
      "agents",
      "project",
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: () => fetchProjectMetrics(client, from, to),
    throwOnError: false,
  });

  const overviewQuery = useQuery({
    queryKey: [
      "insights",
      "agents",
      "overview",
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: () => fetchOverview(client, from, to),
    throwOnError: false,
  });

  const users = useMemo(() => usersQuery.data ?? [], [usersQuery.data]);
  const projectMetrics = projectQuery.data ?? null;
  const timeSeries = overviewQuery.data?.timeSeries ?? [];

  const totalTokens = users.reduce((s, u) => s + u.totalTokens, 0);
  const totalCost = users.reduce((s, u) => s + u.totalCost, 0);
  const totalSessions = users.reduce((s, u) => s + u.totalChats, 0);
  const activeUsers = users.filter((u) => u.totalTokens > 0).length;

  const clientBreakdown = useMemo(() => {
    const map = new Map<
      string,
      { tokens: number; cost: number; users: Set<string> }
    >();
    for (const user of users) {
      const userTotalEvents = user.hookSources.reduce(
        (s, hs) => s + hs.eventCount,
        0,
      );
      for (const hs of user.hookSources) {
        const entry = map.get(hs.source) ?? {
          tokens: 0,
          cost: 0,
          users: new Set<string>(),
        };
        entry.tokens += hs.eventCount;
        // Distribute user cost proportionally across hook sources
        if (userTotalEvents > 0) {
          entry.cost += user.totalCost * (hs.eventCount / userTotalEvents);
        }
        entry.users.add(user.userId);
        map.set(hs.source, entry);
      }
    }
    return Array.from(map.entries())
      .map(([source, data]) => ({
        source,
        label: formatPlatform(source),
        tokens: data.tokens,
        cost: data.cost,
        userCount: data.users.size,
      }))
      .sort((a, b) => b.tokens - a.tokens);
  }, [users]);

  const modelBreakdown = useMemo<ModelUsage[]>(
    () =>
      (projectMetrics?.models ?? []).slice().sort((a, b) => b.count - a.count),
    [projectMetrics],
  );

  const userRows = useMemo(
    () =>
      users
        .slice()
        .sort((a, b) =>
          valueMode === "cost"
            ? b.totalCost - a.totalCost
            : b.totalTokens - a.totalTokens,
        )
        .map((u) => {
          const member = memberMap.get(u.userId);
          return {
            ...u,
            displayName: member?.name ?? u.userId,
            email: member?.email ?? "",
            photoUrl: member?.photoUrl ?? null,
            costPerSession: u.totalChats > 0 ? u.totalCost / u.totalChats : 0,
            costShare: totalCost > 0 ? (u.totalCost / totalCost) * 100 : 0,
            tokenShare:
              totalTokens > 0 ? (u.totalTokens / totalTokens) * 100 : 0,
            clients:
              u.hookSources.length > 0
                ? u.hookSources
                    .slice()
                    .sort((a, b) => b.eventCount - a.eventCount)
                    .map((hs) => formatPlatform(hs.source))
                : [],
          };
        }),
    [users, memberMap, valueMode, totalCost, totalTokens],
  );

  // Unique client sources for filter dropdown
  const availableClients = useMemo(() => {
    const sources = new Set<string>();
    for (const u of users) {
      for (const hs of u.hookSources) sources.add(hs.source);
    }
    return Array.from(sources)
      .sort()
      .map((s) => ({ value: s, label: formatPlatform(s) }));
  }, [users]);

  // Filtered rows: when a client is selected, proportionally attribute cost/tokens
  const filteredUserRows = useMemo(() => {
    if (clientFilter === "all") return userRows;

    return userRows
      .filter((u) => u.hookSources.some((hs) => hs.source === clientFilter))
      .map((u) => {
        const totalEvents = u.hookSources.reduce(
          (s, hs) => s + hs.eventCount,
          0,
        );
        const clientEvents =
          u.hookSources.find((hs) => hs.source === clientFilter)?.eventCount ??
          0;
        const ratio = totalEvents > 0 ? clientEvents / totalEvents : 0;
        const adjTokens = Math.round(u.totalTokens * ratio);
        const adjInput = Math.round(u.totalInputTokens * ratio);
        const adjOutput = Math.round(u.totalOutputTokens * ratio);
        const adjCost = u.totalCost * ratio;
        const adjSessions = Math.round(u.totalChats * ratio);
        return {
          ...u,
          totalTokens: adjTokens,
          totalInputTokens: adjInput,
          totalOutputTokens: adjOutput,
          totalCost: adjCost,
          totalChats: adjSessions,
          costPerSession: adjSessions > 0 ? adjCost / adjSessions : 0,
          costShare: totalCost > 0 ? (adjCost / totalCost) * 100 : 0,
          tokenShare: totalTokens > 0 ? (adjTokens / totalTokens) * 100 : 0,
        };
      })
      .sort((a, b) =>
        valueMode === "cost"
          ? b.totalCost - a.totalCost
          : b.totalTokens - a.totalTokens,
      );
  }, [userRows, clientFilter, valueMode, totalCost, totalTokens]);

  const isLoading =
    membersLoading || usersQuery.isLoading || projectQuery.isLoading;
  const error = usersQuery.error ?? projectQuery.error;

  const handlePresetChange = (preset: DateRangePreset) => {
    setDateRange(preset);
    setCustomRange(null);
    setCustomRangeLabel(null);
  };
  const handleCustomRangeChange = (
    rangeFrom: Date,
    rangeTo: Date,
    label?: string,
  ) => {
    setCustomRange({ from: rangeFrom, to: rangeTo });
    setCustomRangeLabel(label ?? null);
  };
  const handleClearCustomRange = () => {
    setCustomRange(null);
    setCustomRangeLabel(null);
  };

  return (
    <>
      <InsightsConfig
        mcpConfig={mcpConfig}
        title="What would you like to know about AI agent costs?"
        subtitle="Ask about token spend, model costs, and usage by team or client"
        contextInfo={`Agents tab: ${activeUsers} active users, ${formatTokens(totalTokens)} tokens, ${formatCost(totalCost)} total cost in ${rangeLabel}. ${clientBreakdown.length} client types, ${modelBreakdown.length} models.`}
        suggestions={[
          {
            title: "Cost Summary",
            label: "Summarize costs",
            prompt:
              "Summarize AI agent costs across all users, broken down by client type and model.",
          },
          {
            title: "Top Spenders",
            label: "Who spends most?",
            prompt:
              "Which users have the highest token usage and cost? Show a breakdown.",
          },
          {
            title: "Model Costs",
            label: "Cost by model",
            prompt:
              "Break down token usage and cost by model. Which models are most expensive?",
          },
          {
            title: "Client Comparison",
            label: "Compare clients",
            prompt:
              "Compare usage across different AI coding clients (Claude Code, Cursor, etc). Which is most popular?",
          },
        ]}
      />
      <div className="min-h-0 w-full flex-1 overflow-y-auto p-8 pb-24">
        <div className="mx-auto flex max-w-7xl flex-col gap-6">
          <div
            className={cn(
              "flex gap-4 transition-all duration-300",
              isInsightsOpen
                ? "flex-col items-stretch"
                : "flex-row items-center justify-between",
            )}
          >
            <div className="flex min-w-0 flex-col gap-1">
              <h1 className="text-xl font-semibold">AI Agent Costs</h1>
              <p className="text-muted-foreground text-sm">
                Track token consumption and costs across users, clients, and
                models over {rangeLabel}.
              </p>
            </div>
            <div
              className={cn(
                "flex flex-wrap items-center gap-3",
                isInsightsOpen ? "justify-start" : "shrink-0",
              )}
            >
              <ValueModeToggle mode={valueMode} onChange={setValueMode} />
              <TimeRangePicker
                preset={customRange ? null : dateRange}
                customRange={customRange}
                customRangeLabel={customRangeLabel}
                onPresetChange={handlePresetChange}
                onCustomRangeChange={handleCustomRangeChange}
                onClearCustomRange={handleClearCustomRange}
                disabled={isLoading}
              />
            </div>
          </div>

          {error ? (
            <ErrorAlert title="Unable to load agent usage data" error={error} />
          ) : isLoading ? (
            <AgentsLoadingState isInsightsOpen={isInsightsOpen} />
          ) : (
            <>
              <section
                className={cn(
                  "grid gap-4 transition-all duration-300",
                  isInsightsOpen
                    ? "grid-cols-1 md:grid-cols-2"
                    : "grid-cols-1 md:grid-cols-2 lg:grid-cols-4",
                )}
              >
                <MetricCard
                  title="Total Tokens"
                  value={totalTokens}
                  icon="gauge"
                  accentColor="blue"
                  subtext={`${formatTokens(totalTokens)} across ${totalSessions.toLocaleString()} sessions`}
                />
                <MetricCard
                  title="Total Cost"
                  value={totalCost}
                  format="number"
                  icon="credit-card"
                  accentColor="purple"
                  subtext={
                    totalCost > 0
                      ? formatCost(totalCost)
                      : "No cost data reported"
                  }
                />
                <MetricCard
                  title="Active Users"
                  value={activeUsers}
                  icon="user"
                  accentColor="green"
                  subtext={`of ${(membersData?.members ?? []).length} org members`}
                />
                <MetricCard
                  title="AI Clients"
                  value={clientBreakdown.length}
                  icon="terminal"
                  accentColor="orange"
                  subtext={
                    clientBreakdown.length > 0
                      ? clientBreakdown.map((c) => c.label).join(", ")
                      : "No client data"
                  }
                />
              </section>

              <section
                className={cn(
                  "grid gap-4 transition-all duration-300",
                  isInsightsOpen || expandedChart
                    ? "grid-cols-1"
                    : "grid-cols-1 lg:grid-cols-2",
                )}
              >
                <TokenTimeSeriesChart
                  title={
                    valueMode === "cost" ? "Cost Over Time" : "Tokens Over Time"
                  }
                  chartId="tokens-over-time"
                  timeSeries={timeSeries}
                  timeRangeMs={timeRangeMs}
                  valueMode={valueMode}
                  expandedChart={expandedChart}
                  onExpand={setExpandedChart}
                />
                <ClientBreakdownChart
                  title="Usage by Client"
                  chartId="client-breakdown"
                  data={clientBreakdown}
                  valueMode={valueMode}
                  expandedChart={expandedChart}
                  onExpand={setExpandedChart}
                />
              </section>

              <ModelBreakdownCard
                models={modelBreakdown}
                valueMode={valueMode}
              />

              <EmployeeCostTable
                users={filteredUserRows}
                valueMode={valueMode}
                clientFilter={clientFilter}
                availableClients={availableClients}
                onClientFilterChange={setClientFilter}
              />

              <CostDisclaimer />
            </>
          )}
        </div>
      </div>
    </>
  );
}

function TokenTimeSeriesChart({
  title,
  chartId,
  timeSeries,
  timeRangeMs,
  valueMode,
  expandedChart,
  onExpand,
}: {
  title: string;
  chartId: string;
  timeSeries: TimeSeriesBucket[];
  timeRangeMs: number;
  valueMode: ValueMode;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}) {
  const isExpanded = expandedChart === chartId;
  const height = isExpanded ? 420 : 260;
  const hasData = timeSeries.some(
    (b) => (valueMode === "cost" ? b.totalCost : b.totalTokens) > 0,
  );

  const chartData = useMemo(() => {
    const labels = timeSeries.map((b) =>
      formatChartLabel(unixNanoToDate(b.bucketTimeUnixNano), timeRangeMs),
    );

    // Raw bar datasets
    const barDatasets =
      valueMode === "cost"
        ? [
            {
              label: "Cost",
              data: timeSeries.map((b) => b.totalCost),
              backgroundColor: "rgba(96, 165, 250, 0.35)",
              stack: "stack",
              order: 2,
            },
          ]
        : [
            {
              label: "Input Tokens",
              data: timeSeries.map((b) => b.totalInputTokens),
              backgroundColor: "rgba(96, 165, 250, 0.35)",
              stack: "stack",
              order: 2,
            },
            {
              label: "Output Tokens",
              data: timeSeries.map((b) => b.totalOutputTokens),
              backgroundColor: "rgba(52, 211, 153, 0.35)",
              stack: "stack",
              order: 2,
            },
            {
              label: "Cache Read",
              data: timeSeries.map((b) => b.cacheReadInputTokens),
              backgroundColor: "rgba(167, 139, 250, 0.35)",
              stack: "stack",
              order: 2,
            },
          ];

    // Smoothed trend line (total across all stacked values)
    const rawTotal = timeSeries.map((b) =>
      valueMode === "cost"
        ? b.totalCost
        : b.totalInputTokens + b.totalOutputTokens + b.cacheReadInputTokens,
    );
    const trendData = smoothData(rawTotal);

    const trendDataset = {
      label: valueMode === "cost" ? "Cost Trend" : "Token Trend",
      data: trendData,
      type: "line" as const,
      borderColor: valueMode === "cost" ? "#818cf8" : "#3b82f6",
      backgroundColor: "transparent",
      pointRadius: 0,
      pointHoverRadius: 4,
      borderWidth: 2,
      tension: 0.4,
      fill: false,
      order: 1,
    };

    return {
      labels,
      datasets: [...barDatasets, trendDataset],
    };
  }, [timeSeries, timeRangeMs, valueMode]);

  const options = useMemo<ChartOptions<"bar">>(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: "index", intersect: false },
      plugins: {
        legend: {
          position: "bottom",
          labels: {
            boxWidth: 12,
            usePointStyle: true,
            padding: 16,
            font: { size: 11 },
          },
        },
        tooltip: {
          backgroundColor: "rgba(0, 0, 0, 0.85)",
          titleColor: "#fff",
          bodyColor: "#e5e7eb",
          borderColor: "rgba(255, 255, 255, 0.1)",
          borderWidth: 1,
          padding: 12,
          boxPadding: 4,
          usePointStyle: true,
          callbacks: {
            label: (item) => {
              const val = Number(item.parsed.y ?? 0);
              return ` ${item.dataset.label}: ${formatValue(val, valueMode)}`;
            },
          },
        },
      },
      scales: {
        x: {
          stacked: true,
          grid: { display: true, color: "rgba(128, 128, 128, 0.08)" },
          ticks: { maxTicksLimit: 8 },
        },
        y: {
          stacked: true,
          beginAtZero: true,
          grid: { color: "rgba(128, 128, 128, 0.15)" },
          ticks: {
            callback: (value) => formatValue(Number(value), valueMode),
          },
        },
      },
    }),
    [valueMode],
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
        <div className="text-muted-foreground flex h-[260px] items-center justify-center text-sm">
          No data for selected time range
        </div>
      ) : (
        <div style={{ height }}>
          <Bar data={chartData} options={options} />
        </div>
      )}
    </ChartCard>
  );
}

function ClientBreakdownChart({
  title,
  chartId,
  data,
  valueMode,
  expandedChart,
  onExpand,
}: {
  title: string;
  chartId: string;
  data: Array<{
    label: string;
    tokens: number;
    cost: number;
    userCount: number;
  }>;
  valueMode: ValueMode;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}) {
  const isExpanded = expandedChart === chartId;
  const height = isExpanded ? 420 : 260;
  const hasData = data.length > 0;

  const chartData = useMemo(
    () => ({
      labels: data.map((d) => d.label),
      datasets: [
        {
          label: valueMode === "cost" ? "Cost" : "Events",
          data: data.map((d) => (valueMode === "cost" ? d.cost : d.tokens)),
          backgroundColor: data.map(
            (_, i) => CHART_COLORS[i % CHART_COLORS.length]!,
          ),
        },
      ],
    }),
    [data, valueMode],
  );

  const options = useMemo<ChartOptions<"bar">>(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      indexAxis: "y",
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: {
            afterLabel: (item) => {
              const entry = data[item.dataIndex];
              return entry
                ? `${entry.userCount} user${entry.userCount !== 1 ? "s" : ""}`
                : "";
            },
          },
        },
      },
      scales: {
        x: {
          beginAtZero: true,
          grid: { color: "rgba(128, 128, 128, 0.15)" },
          ticks: {
            callback: (value) => formatValue(Number(value), valueMode),
          },
        },
        y: { grid: { display: false } },
      },
    }),
    [data, valueMode],
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
        <div className="text-muted-foreground flex h-[260px] items-center justify-center text-sm">
          No client data available
        </div>
      ) : (
        <div style={{ height }}>
          <Bar data={chartData} options={options} />
        </div>
      )}
    </ChartCard>
  );
}

function ModelBreakdownCard({
  models,
  valueMode,
}: {
  models: ModelUsage[];
  valueMode: ValueMode;
}) {
  const total = models.reduce((s, m) => s + m.count, 0);

  return (
    <section className="rounded-lg border p-4">
      <h3 className="font-semibold">
        {valueMode === "cost" ? "Requests by Model" : "Requests by Model"}
      </h3>
      <div className="mt-4 space-y-3">
        {models.length > 0 ? (
          models.map((model) => (
            <div key={model.name} className="space-y-1.5">
              <div className="flex items-center justify-between gap-3 text-sm">
                <span className="truncate font-mono text-xs">{model.name}</span>
                <span className="text-muted-foreground shrink-0">
                  {model.count.toLocaleString()} requests (
                  {total > 0 ? ((model.count / total) * 100).toFixed(1) : 0}%)
                </span>
              </div>
              <div className="bg-muted h-2 overflow-hidden rounded-full">
                <div
                  className="bg-primary h-full rounded-full"
                  style={{
                    width: `${total > 0 ? Math.max((model.count / total) * 100, 4) : 0}%`,
                  }}
                />
              </div>
            </div>
          ))
        ) : (
          <p className="text-muted-foreground text-sm">No model usage data</p>
        )}
      </div>
    </section>
  );
}

type EmployeeRow = UserSummary & {
  displayName: string;
  email: string;
  photoUrl: string | null;
  clients: string[];
  costPerSession: number;
  costShare: number;
  tokenShare: number;
};

function EmployeeCostTable({
  users,
  valueMode,
  clientFilter,
  availableClients,
  onClientFilterChange,
}: {
  users: EmployeeRow[];
  valueMode: ValueMode;
  clientFilter: string;
  availableClients: Array<{ value: string; label: string }>;
  onClientFilterChange: (value: string) => void;
}) {
  const PAGE_SIZE = 10;
  const [page, setPage] = useState(0);
  const totalPages = Math.ceil(users.length / PAGE_SIZE);
  const safePage = Math.min(page, Math.max(totalPages - 1, 0));
  const pageUsers = users.slice(
    safePage * PAGE_SIZE,
    (safePage + 1) * PAGE_SIZE,
  );
  const isCost = valueMode === "cost";

  return (
    <section className="bg-card flex flex-col gap-4 rounded-xl border p-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="font-semibold">
            {isCost ? "Cost" : "Usage"} by Employee
          </h3>
          <p className="text-muted-foreground text-xs">
            {isCost
              ? "Sorted by total cost (highest first)"
              : "Sorted by total tokens (highest first)"}
            {clientFilter !== "all" &&
              ` · filtered to ${formatPlatform(clientFilter)}`}
          </p>
        </div>
        <div className="flex items-center gap-3">
          <select
            value={clientFilter}
            onChange={(e) => onClientFilterChange(e.target.value)}
            className="border-border bg-background text-foreground rounded-md border px-2.5 py-1.5 text-xs"
          >
            <option value="all">All Clients</option>
            {availableClients.map((c) => (
              <option key={c.value} value={c.value}>
                {c.label}
              </option>
            ))}
          </select>
          <span className="text-muted-foreground text-xs">
            {users.length} employee{users.length !== 1 ? "s" : ""}
          </span>
        </div>
      </div>
      <div className="overflow-x-auto rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-6">Employee</TableHead>
              <TableHead>Input</TableHead>
              <TableHead>Output</TableHead>
              <TableHead>Total Tokens</TableHead>
              <TableHead>Cost</TableHead>
              <TableHead>$/Session</TableHead>
              <TableHead>Sessions</TableHead>
              <TableHead className="pr-6">Share</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {pageUsers.length > 0 ? (
              pageUsers.map((user) => (
                <TableRow key={user.userId}>
                  <TableCell className="pl-6">
                    <div className="flex min-w-[200px] items-center gap-3">
                      <Avatar className="size-8 shrink-0">
                        {user.photoUrl ? (
                          <AvatarImage
                            src={user.photoUrl}
                            alt={user.displayName}
                          />
                        ) : null}
                        <AvatarFallback className="text-xs">
                          {initials(user.displayName)}
                        </AvatarFallback>
                      </Avatar>
                      <div className="min-w-0">
                        <p className="truncate text-sm font-medium">
                          {user.displayName}
                        </p>
                        {user.email ? (
                          <p className="text-muted-foreground truncate text-xs">
                            {user.email}
                          </p>
                        ) : null}
                        {clientFilter === "all" && user.clients.length > 0 && (
                          <p className="text-muted-foreground/70 mt-0.5 text-[10px]">
                            {user.clients.join(", ")}
                          </p>
                        )}
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className="font-mono text-xs tabular-nums">
                    {formatTokens(user.totalInputTokens)}
                  </TableCell>
                  <TableCell className="font-mono text-xs tabular-nums">
                    {formatTokens(user.totalOutputTokens)}
                  </TableCell>
                  <TableCell>
                    <span
                      className={cn(
                        "font-mono text-sm tabular-nums",
                        !isCost && "font-semibold",
                      )}
                    >
                      {formatTokens(user.totalTokens)}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span
                      className={cn(
                        "font-mono text-sm tabular-nums",
                        isCost && "font-semibold",
                      )}
                    >
                      {formatCost(user.totalCost)}
                    </span>
                  </TableCell>
                  <TableCell className="text-muted-foreground font-mono text-xs tabular-nums">
                    {formatCost(user.costPerSession)}
                  </TableCell>
                  <TableCell className="font-mono text-sm tabular-nums">
                    {user.totalChats.toLocaleString()}
                  </TableCell>
                  <TableCell className="pr-6">
                    <div className="flex items-center gap-2">
                      <div className="bg-muted h-1.5 w-12 overflow-hidden rounded-full">
                        <div
                          className="bg-primary h-full rounded-full"
                          style={{
                            width: `${Math.max(isCost ? user.costShare : user.tokenShare, 3)}%`,
                          }}
                        />
                      </div>
                      <span className="text-muted-foreground font-mono text-xs tabular-nums">
                        {(isCost ? user.costShare : user.tokenShare).toFixed(1)}
                        %
                      </span>
                    </div>
                  </TableCell>
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell
                  colSpan={8}
                  className="text-muted-foreground py-10 text-center text-sm"
                >
                  No employee activity found for this time range.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
        {totalPages > 1 && (
          <div className="flex items-center justify-between border-t px-4 py-3">
            <p className="text-muted-foreground text-sm">
              {safePage * PAGE_SIZE + 1}–
              {Math.min((safePage + 1) * PAGE_SIZE, users.length)} of{" "}
              {users.length}
            </p>
            <div className="flex items-center gap-1">
              <button
                className="hover:bg-muted rounded p-1 text-sm disabled:opacity-40"
                onClick={() => setPage((p) => p - 1)}
                disabled={safePage === 0}
              >
                Prev
              </button>
              <button
                className="hover:bg-muted rounded p-1 text-sm disabled:opacity-40"
                onClick={() => setPage((p) => p + 1)}
                disabled={safePage >= totalPages - 1}
              >
                Next
              </button>
            </div>
          </div>
        )}
      </div>
    </section>
  );
}

function CostDisclaimer() {
  return (
    <section className="bg-muted/40 border-border rounded-xl border p-5">
      <div className="max-w-3xl space-y-1">
        <h2 className="text-sm font-semibold">About cost data</h2>
        <p className="text-muted-foreground text-sm">
          Dollar costs are reported by the AI provider. Currently only Anthropic
          (Claude) reports cost data. For other providers, use token counts to
          estimate costs. Token counts are always available regardless of
          provider.
        </p>
      </div>
    </section>
  );
}

function AgentsLoadingState({ isInsightsOpen }: { isInsightsOpen: boolean }) {
  return (
    <>
      <section
        className={cn(
          "grid gap-4 transition-all duration-300",
          isInsightsOpen
            ? "grid-cols-1 md:grid-cols-2"
            : "grid-cols-1 md:grid-cols-2 lg:grid-cols-4",
        )}
      >
        {Array.from({ length: 4 }).map((_, i) => (
          <div key={i} className="bg-card rounded-lg border p-5">
            <Skeleton className="mb-4 h-4 w-28" />
            <Skeleton className="h-9 w-20" />
            <Skeleton className="mt-3 h-3 w-36" />
          </div>
        ))}
      </section>
      <section className="grid gap-4 lg:grid-cols-2">
        <Skeleton className="h-72 rounded-lg" />
        <Skeleton className="h-72 rounded-lg" />
      </section>
      <Skeleton className="h-40 rounded-lg" />
      <Skeleton className="h-64 rounded-lg" />
    </>
  );
}

async function fetchAllUsers(
  client: Parameters<typeof telemetrySearchUsers>[0],
  from: Date,
  to: Date,
): Promise<UserSummary[]> {
  const users: UserSummary[] = [];
  let cursor: string | undefined;
  do {
    const result = await unwrapAsync(
      telemetrySearchUsers(client, {
        searchUsersPayload: {
          cursor,
          filter: { from, to, eventSource: "hook" },
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

async function fetchProjectMetrics(
  client: Parameters<typeof telemetryGetProjectMetricsSummary>[0],
  from: Date,
  to: Date,
): Promise<ProjectSummary> {
  const result = await unwrapAsync(
    telemetryGetProjectMetricsSummary(client, {
      getProjectMetricsSummaryPayload: { from, to },
    }),
  );
  return result.metrics;
}

async function fetchOverview(
  client: Parameters<typeof telemetryGetObservabilityOverview>[0],
  from: Date,
  to: Date,
): Promise<GetObservabilityOverviewResult> {
  return unwrapAsync(
    telemetryGetObservabilityOverview(client, {
      getObservabilityOverviewPayload: {
        from,
        to,
        includeTimeSeries: true,
        eventSource: "hook",
      },
    }),
  );
}
