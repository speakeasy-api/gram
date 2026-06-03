import { MetricCard } from "@/components/chart/MetricCard";
import { ChartCard } from "@/components/chart/ChartCard";
import { formatChartLabel } from "@/components/chart/chartUtils";
import { InsightsConfig } from "@/components/insights-sidebar";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { DashboardCard } from "@/components/ui/dashboard-card";
import { Skeleton } from "@/components/ui/skeleton";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { type DateRangePreset } from "@gram-ai/elements";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { useRiskOverview } from "@gram/client/react-query/index.js";
import { keepPreviousData } from "@tanstack/react-query";
import { Shield } from "lucide-react";
import { useMemo, type ReactNode } from "react";
import { Link, Outlet, useLocation } from "react-router";
import { useRoutes } from "@/routes";
import {
  formatDateRangeLabel,
  useDateRangeFilter,
} from "@/components/observe/useDateRangeFilter";
import { RULE_CATEGORY_META, type RuleCategory } from "./policy-data";
import { getRuleTitleFallback } from "./risk-utils";
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
import { Line } from "react-chartjs-2";
import { Type } from "@/components/ui/type";

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Filler,
  Tooltip,
  Legend,
);

const RISK_TREND_CHART_ID = "risk-events-trend";
const RISK_OVERVIEW_PRESETS: DateRangePreset[] = [
  "15m",
  "1h",
  "4h",
  "1d",
  "2d",
  "3d",
  "7d",
  "15d",
  "30d",
];

const CHART_COLORS = {
  gridLine: "rgba(128, 128, 128, 0.2)",
  gridLineFaint: "rgba(128, 128, 128, 0.1)",
  tooltipBg: "#171717",
  tooltipTitle: "#fafafa",
  tooltipBody: "#d4d4d4",
  tooltipBorder: "#262626",
} as const;

const RISK_CATEGORY_CHART_COLORS = [
  { category: "secrets", color: "#60a5fa" },
  { category: "financial", color: "#34d399" },
  { category: "pii", color: "#f87171" },
  { category: "government_ids", color: "#a78bfa" },
  { category: "healthcare", color: "#facc15" },
  { category: "prompt_injection", color: "#22d3ee" },
  { category: "off_policy", color: "#f472b6" },
  { category: "shadow_mcp", color: "#a3e635" },
  { category: "destructive_tool", color: "#818cf8" },
  { category: "cli_destructive", color: "#fb7185" },
  { category: "custom", color: "#94a3b8" },
] satisfies ReadonlyArray<{ category: RuleCategory; color: string }>;

const RISK_CATEGORY_CHART_COLOR_BY_CATEGORY = new Map<RuleCategory, string>(
  RISK_CATEGORY_CHART_COLORS.map(({ category, color }) => [category, color]),
);

const RISK_CATEGORY_CHART_ORDER = new Map<RuleCategory, number>(
  RISK_CATEGORY_CHART_COLORS.map(({ category }, index) => [category, index]),
);

type BarDatum = {
  key: string;
  label: string;
  value: number;
  href?: string;
};

type TrendPoint = {
  category: string;
  bucketStart: Date;
  findings: number;
};

export default function SecurityOverview() {
  return (
    <RequireScope scope="org:admin" level="page">
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <SecurityOverviewContent />
        </Page.Body>
      </Page>
    </RequireScope>
  );
}

export function RiskOverviewRoot() {
  return <Outlet />;
}

function RiskOverviewShell({
  children,
  rangeLabel,
  controls,
}: {
  children: ReactNode;
  rangeLabel?: string;
  controls?: ReactNode;
}) {
  return (
    <Page.Section>
      <Page.Section.Title stage="beta">Risk Overview</Page.Section.Title>
      <Page.Section.Description>
        Risk analysis summary for policy findings
        {rangeLabel && ` across ${rangeLabel}.`}
      </Page.Section.Description>
      <Page.Section.CTA>{controls ?? null}</Page.Section.CTA>
      <Page.Section.Body>
        <div className="space-y-8">{children}</div>
      </Page.Section.Body>
    </Page.Section>
  );
}

function NoPoliciesEmptyState() {
  const routes = useRoutes();
  return (
    <RiskOverviewShell>
      <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
        <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
          <Shield className="text-muted-foreground size-6" />
        </div>
        <Type variant="subheading" className="mb-1">
          Risk Analysis
        </Type>
        <Type small muted className="mb-4 max-w-md text-center">
          Create a risk policy to begin scanning chat messages for leaked
          secrets, sensitive data, and policy flags.
        </Type>
        <Button variant="primary" asChild>
          <Link to={routes.policyCenter.href()}>
            <Button.Text>Manage Policies</Button.Text>
            <Button.RightIcon>
              <Icon name="arrow-right" />
            </Button.RightIcon>
          </Link>
        </Button>
      </div>
    </RiskOverviewShell>
  );
}

function SecurityOverviewContent() {
  const routes = useRoutes();
  const location = useLocation();
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
  const rangeLabel = useMemo(
    () => formatDateRangeLabel(dateRange, customRangeLabel),
    [dateRange, customRangeLabel],
  );
  const controls = (
    <TimeRangePicker
      preset={customRange ? null : dateRange}
      customRange={customRange}
      customRangeLabel={customRangeLabel}
      availablePresets={RISK_OVERVIEW_PRESETS}
      onPresetChange={setDateRangeParam}
      onCustomRangeChange={setCustomRangeParam}
      onClearCustomRange={clearCustomRange}
    />
  );
  const overviewQuery = useRiskOverview({ from, to }, undefined, {
    placeholderData: keepPreviousData,
  });
  const overview = overviewQuery.data;
  const isOverviewLoading = overviewQuery.isLoading;

  const categoriesIndexHref = useMemo(() => {
    const r = (
      routes.riskOverview as unknown as {
        categoriesIndex?: { href: () => string };
      }
    ).categoriesIndex;
    return r ? `${r.href()}${location.search}` : "";
  }, [routes.riskOverview, location.search]);

  const usersIndexHref = useMemo(() => {
    const r = (
      routes.riskOverview as unknown as {
        usersIndex?: { href: () => string };
      }
    ).usersIndex;
    return r ? `${r.href()}${location.search}` : "";
  }, [routes.riskOverview, location.search]);

  const rulesIndexHref = useMemo(() => {
    const r = (
      routes.riskOverview as unknown as {
        rulesIndex?: { href: () => string };
      }
    ).rulesIndex;
    return r ? `${r.href()}${location.search}` : "";
  }, [routes.riskOverview, location.search]);

  const topCategories = useMemo<BarDatum[]>(() => {
    const categoryDetailRoute = (
      routes.riskOverview as unknown as {
        categoryDetail?: { href: (...params: string[]) => string };
      }
    ).categoryDetail;
    return (overview?.topCategories ?? []).map((category) => {
      const key = category.category;
      const meta = RULE_CATEGORY_META[key as RuleCategory];
      const href = categoryDetailRoute
        ? `${categoryDetailRoute.href(encodeURIComponent(key))}${location.search}`
        : undefined;
      return {
        key,
        label: meta?.label ?? key,
        value: category.findings,
        href,
      };
    });
  }, [overview?.topCategories, routes.riskOverview, location.search]);

  const topRules = useMemo<BarDatum[]>(() => {
    const riskEventsHref = routes.logs.riskEvents.href();
    return (overview?.topRules ?? []).map((r) => {
      const label = r.ruleId ? getRuleTitleFallback(r.ruleId) : "(no rule_id)";
      const ruleParams = new URLSearchParams();
      if (r.ruleId) ruleParams.set("rule_id", r.ruleId);
      const search = location.search
        ? `${location.search}&${ruleParams.toString()}`
        : ruleParams.toString()
          ? `?${ruleParams.toString()}`
          : "";
      const href = r.ruleId ? `${riskEventsHref}${search}` : undefined;
      return {
        key: r.ruleId || "__none",
        label,
        value: Number(r.findings),
        href,
      };
    });
  }, [overview?.topRules, routes.logs.riskEvents, location.search]);

  const topUsers = useMemo<BarDatum[]>(() => {
    const userDetailRoute = (
      routes.riskOverview as unknown as {
        userDetail?: { href: (...params: string[]) => string };
      }
    ).userDetail;
    return (overview?.topUsers ?? []).map((user) => {
      const href =
        user.externalUserId && userDetailRoute
          ? `${userDetailRoute.href(
              encodeURIComponent(user.externalUserId),
            )}${location.search}`
          : undefined;
      return {
        key: user.externalUserId || user.email,
        label: user.email,
        value: user.findings,
        href,
      };
    });
  }, [overview?.topUsers, routes.riskOverview, location.search]);

  if (overviewQuery.error) {
    return (
      <RiskOverviewShell rangeLabel={rangeLabel} controls={controls}>
        <div className="bg-muted/20 flex flex-col items-center justify-center rounded-lg border border-dashed px-8 py-16 text-center">
          <div className="bg-muted/50 mb-4 flex size-12 items-center justify-center rounded-full">
            <Icon
              name="circle-alert"
              className="text-muted-foreground size-6"
            />
          </div>
          <p className="text-foreground text-sm font-medium">
            Error loading risk overview
          </p>
          <p className="text-muted-foreground mt-1 max-w-md text-sm">
            {overviewQuery.error.message}
          </p>
        </div>
      </RiskOverviewShell>
    );
  }

  const hasHistoricActivity =
    (overview?.messagesScanned ?? 0) > 0 || (overview?.findings ?? 0) > 0;

  // Only collapse to the empty state once data has actually arrived —
  // during the first fetch we render the full shell with skeletons so the
  // layout never blinks between "Loading…" and the real page. Also keep the
  // full overview visible whenever the selected range contains historic
  // activity, so disabling every policy doesn't hide prior scans and
  // findings.
  if (overview && overview.activePolicies === 0 && !hasHistoricActivity) {
    return <NoPoliciesEmptyState />;
  }

  const hasFindings = (overview?.findings ?? 0) > 0;
  const policiesDisabledWithHistory =
    !!overview && overview.activePolicies === 0 && hasHistoricActivity;

  // Brief security-flavoured context for the AI Insights sidebar. Numbers are
  // pulled from the current risk overview query so the assistant can reason
  // about "this period" without re-fetching, but it must still call the risk
  // tools for anything that isn't a top-line metric. Only mount once `overview`
  // is populated so the contextInfo never embeds stale or undefined counts.
  const insightsContext = overview
    ? [
        "Page: Security Overview.",
        `Selected date range: ${rangeLabel}.`,
        `Active risk policies: ${overview.activePolicies}.`,
        `Findings in current range: ${overview.findings}.`,
        `Messages scanned: ${overview.messagesScanned}.`,
        `Flagged sessions: ${overview.flaggedSessions}.`,
        "Available risk tools: listRiskResultsForAgent (finding-level, match is redacted to <redacted len=N sha=XXXXXXXX>), listRiskResultsByChat (chat-level rollups), listRiskPolicies, getRiskPolicyStatus, listShadowMCPApprovals.",
        "Never echo match_redacted values verbatim. Refer to findings by rule_id and source.",
      ].join(" ")
    : null;

  const insightsSuggestions = [
    {
      title: "Top rules this week",
      label: "which rule_ids fired most",
      prompt:
        "Use listRiskResultsForAgent to find the top 5 rule_ids by finding count over the last 7 days. Report by source family and rule_id only — never quote any match_redacted value.",
    },
    {
      title: "Shadow MCP servers",
      label: "unapproved MCPs in use",
      prompt:
        "List all shadow_mcp findings across the project. For each, name the MCP server identifier (match), the chat_id, and when it was first observed. These match values are server URLs/commands and are safe to name.",
    },
    {
      title: "Unique leaked secrets",
      label: "dedupe by fingerprint",
      prompt:
        "Use listRiskResultsForAgent to count distinct leaked secrets by their match_redacted fingerprint (since identical secrets share a sha prefix). Group by rule_id and report counts. Do not print match_redacted values back to me.",
    },
    {
      title: "Analysis backlog",
      label: "pending messages per policy",
      prompt:
        "For each active policy, call getRiskPolicyStatus and report pending vs analyzed message counts and workflow state. Flag any policy whose pending count is non-zero.",
    },
  ];

  return (
    <>
      {insightsContext && (
        <InsightsConfig
          contextInfo={insightsContext}
          suggestions={insightsSuggestions}
          title="Risk insights"
          subtitle="Ask about policies, findings, and shadow MCP activity. Match content is redacted before it reaches the assistant."
        />
      )}
      <RiskOverviewShell rangeLabel={rangeLabel} controls={controls}>
        {policiesDisabledWithHistory && (
          <div className="bg-muted/30 flex items-start gap-3 rounded-lg border border-dashed px-4 py-3">
            <Icon
              name="circle-alert"
              className="text-muted-foreground mt-0.5 size-4 shrink-0"
            />
            <div className="min-w-0 flex-1">
              <Type small className="font-medium">
                All risk policies are disabled
              </Type>
              <Type small muted>
                Showing historic findings only — new chat messages will not be
                scanned until a policy is re-enabled.
              </Type>
            </div>
            <Button variant="secondary" size="sm" asChild>
              <Link to={routes.policyCenter.href()}>
                <Button.Text>Manage Policies</Button.Text>
                <Button.RightIcon>
                  <Icon name="arrow-right" />
                </Button.RightIcon>
              </Link>
            </Button>
          </div>
        )}
        <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
          {isOverviewLoading ? (
            <Skeleton className="h-[100px] rounded-lg" />
          ) : (
            <MetricCard
              title="Events Scanned"
              value={overview?.messagesScanned ?? 0}
              format="compact"
              icon="scan-search"
            />
          )}
          {isOverviewLoading ? (
            <Skeleton className="h-[100px] rounded-lg" />
          ) : (
            <MetricCard
              title="Findings"
              value={overview?.findings ?? 0}
              format="compact"
              icon="flag"
            />
          )}
          {isOverviewLoading ? (
            <Skeleton className="h-[100px] rounded-lg" />
          ) : (
            <MetricCard
              title="Flagged Sessions"
              value={overview?.flaggedSessions ?? 0}
              format="compact"
              icon="message-square"
            />
          )}
          {isOverviewLoading ? (
            <Skeleton className="h-[100px] rounded-lg" />
          ) : (
            <MetricCard
              title="Active Policies"
              value={overview?.activePolicies ?? 0}
              format="compact"
              icon="shield-check"
            />
          )}
        </div>
      </RiskOverviewShell>

      <RiskActivitySection>
        <div className="grid grid-cols-1 gap-6 md:grid-cols-2 xl:grid-cols-3">
          <DashboardChartCard
            title="Top Risk Events by Category"
            loading={isOverviewLoading}
            empty={!hasFindings || topCategories.length === 0}
            action={
              <ViewAllLink
                href={categoriesIndexHref}
                label="View all categories"
              />
            }
          >
            <RankedBarList items={topCategories} />
          </DashboardChartCard>
          <DashboardChartCard
            title="Top Risk Events by Rule"
            loading={isOverviewLoading}
            empty={!hasFindings || topRules.length === 0}
            action={
              <ViewAllLink href={rulesIndexHref} label="View all rules" />
            }
          >
            <RankedBarList items={topRules} />
          </DashboardChartCard>
          <DashboardChartCard
            title="Users with Most Findings"
            loading={isOverviewLoading}
            empty={!hasFindings || topUsers.length === 0}
            action={
              <ViewAllLink href={usersIndexHref} label="View all users" />
            }
          >
            <RankedBarList items={topUsers} />
          </DashboardChartCard>
        </div>

        {isOverviewLoading || !overview ? (
          <Skeleton className="h-[250px] w-full rounded-lg" />
        ) : (
          <ChartCard
            title="Risk Events over Time"
            chartId={RISK_TREND_CHART_ID}
            expandedChart={null}
            onExpand={() => null}
            hasData={
              hasFindings &&
              overview.timeSeriesFindings.some((point) => point.findings > 0)
            }
          >
            <RiskTrendChart
              points={overview.timeSeriesFindings}
              from={overview.from}
              to={overview.to}
              height={250}
            />
          </ChartCard>
        )}
      </RiskActivitySection>
    </>
  );
}

function RiskActivitySection({ children }: { children: ReactNode }) {
  const routes = useRoutes();
  const location = useLocation();

  const carriedRangeParams = useMemo(() => {
    const incoming = new URLSearchParams(location.search);
    const next = new URLSearchParams();
    for (const key of ["range", "from", "to"]) {
      const value = incoming.get(key);
      if (value) next.set(key, value);
    }
    return next;
  }, [location.search]);

  const agentsParams = new URLSearchParams(carriedRangeParams);
  agentsParams.set("has_risk", "true");
  const agentsHref = `${routes.logs.agents.href()}?${agentsParams.toString()}`;

  const riskEventsHref = carriedRangeParams.toString()
    ? `${routes.logs.riskEvents.href()}?${carriedRangeParams.toString()}`
    : routes.logs.riskEvents.href();

  return (
    <Page.Section>
      <Page.Section.Title>Policy Activity</Page.Section.Title>
      <Page.Section.Description>
        Review where policy findings are concentrated and how risk activity
        changes over time.
      </Page.Section.Description>
      <Page.Section.CTA>
        <div className="flex items-center gap-2">
          <Button variant="secondary" asChild>
            <Link to={agentsHref}>
              <Button.Text>View Sessions with Risk</Button.Text>
              <Button.RightIcon>
                <Icon name="arrow-right" />
              </Button.RightIcon>
            </Link>
          </Button>
          <Button variant="secondary" asChild>
            <Link to={riskEventsHref}>
              <Button.Text>View All Events</Button.Text>
              <Button.RightIcon>
                <Icon name="arrow-right" />
              </Button.RightIcon>
            </Link>
          </Button>
        </div>
      </Page.Section.CTA>
      <Page.Section.Body>
        <div className="space-y-8">{children}</div>
      </Page.Section.Body>
    </Page.Section>
  );
}

function DashboardChartCard({
  title,
  empty,
  loading,
  children,
  action,
}: {
  title: string;
  empty: boolean;
  loading?: boolean;
  children: ReactNode;
  action?: ReactNode;
}) {
  return (
    <DashboardCard title={title} action={action}>
      {loading ? <SkeletonList /> : empty ? <ChartEmptyState /> : children}
    </DashboardCard>
  );
}

function SkeletonList() {
  return (
    <div className="space-y-2">
      {Array.from({ length: 5 }).map((_, i) => (
        <Skeleton key={i} className="h-6 w-full" />
      ))}
    </div>
  );
}

function ChartEmptyState() {
  return <p className="text-muted-foreground text-sm">No findings recorded</p>;
}

function ViewAllLink({ href, label }: { href: string; label: string }) {
  if (!href) return null;
  return (
    <Link
      to={href}
      className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-xs"
      aria-label={label}
    >
      <span>{label}</span>
      <Icon name="arrow-right" className="size-3.5" />
    </Link>
  );
}

function RankedBarList({ items }: { items: BarDatum[] }) {
  const max = items[0]?.value || 1;

  return (
    <ul className="my-1 space-y-3">
      {items.map((item, i) => {
        const body = (
          <div className="min-w-0 flex-1">
            <div className="mb-1 flex items-center justify-between">
              <span className="truncate text-sm">{item.label}</span>
              <span className="text-muted-foreground ml-2 shrink-0 text-xs">
                {item.value.toLocaleString()}
              </span>
            </div>
            <div className="bg-muted h-1 w-full rounded-full">
              <div
                className="h-1 rounded-full bg-blue-700 dark:bg-blue-500"
                style={{ width: `${(item.value / max) * 100}%` }}
              />
            </div>
          </div>
        );
        return (
          <li key={item.key} className="flex items-center gap-3">
            <span className="text-muted-foreground w-4 shrink-0 text-right text-xs">
              {i + 1}
            </span>
            {item.href ? (
              <Link
                to={item.href}
                className="hover:bg-muted/40 -mx-2 flex min-w-0 flex-1 items-center rounded px-2 py-1 transition-colors"
              >
                {body}
              </Link>
            ) : (
              body
            )}
          </li>
        );
      })}
    </ul>
  );
}

function RiskTrendChart({
  points,
  from,
  to,
  height,
}: {
  points: TrendPoint[];
  from: Date;
  to: Date;
  height: number;
}) {
  const chartData = useMemo(
    () => buildRiskTrendChartData(points, from, to),
    [points, from, to],
  );

  if (chartData.labels.length === 0) {
    return <ChartEmptyState />;
  }

  const options: ChartOptions<"line"> = {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: "index", intersect: false },
    plugins: {
      legend: { display: false },
      tooltip: {
        backgroundColor: CHART_COLORS.tooltipBg,
        titleColor: CHART_COLORS.tooltipTitle,
        bodyColor: CHART_COLORS.tooltipBody,
        borderColor: CHART_COLORS.tooltipBorder,
        borderWidth: 1,
        padding: 12,
        boxPadding: 4,
        callbacks: {
          title: (items) =>
            chartData.tooltipLabels[items[0]?.dataIndex ?? 0] ?? "",
          label: (item) => {
            if ((item.parsed.y ?? 0) === 0) return undefined;
            return item.formattedValue
              ? `${item.dataset.label}: ${item.formattedValue}`
              : "";
          },
        },
      },
    },
    scales: {
      x: {
        grid: {
          display: true,
          color: CHART_COLORS.gridLineFaint,
          lineWidth: 1,
        },
        ticks: { maxTicksLimit: 8 },
      },
      y: {
        beginAtZero: true,
        grid: { color: CHART_COLORS.gridLine },
        ticks: { precision: 0 },
      },
    },
    transitions: {
      resize: { animation: { duration: 0 } },
    },
  };

  return (
    <div
      className="relative transition-all duration-200 ease-in-out"
      style={{ height }}
    >
      <Line data={chartData} options={options} />
    </div>
  );
}

function getRiskCategoryChartColor(category: string) {
  return RISK_CATEGORY_CHART_COLOR_BY_CATEGORY.get(category as RuleCategory);
}

function buildRiskTrendChartData(points: TrendPoint[], from: Date, to: Date) {
  if (points.length === 0) {
    return { labels: [], tooltipLabels: [], datasets: [] };
  }

  const timeRangeMs = to.getTime() - from.getTime();
  const dateMap = new Map<number, Date>();
  const seriesMap = new Map<string, Map<number, number>>();

  for (const point of points) {
    const timestamp = point.bucketStart.getTime();
    dateMap.set(timestamp, point.bucketStart);
    const series = seriesMap.get(point.category) ?? new Map<number, number>();
    series.set(timestamp, point.findings);
    seriesMap.set(point.category, series);
  }

  const timestamps = Array.from(dateMap.keys()).sort((a, b) => a - b);
  const labels = timestamps.map((timestamp) =>
    formatChartLabel(dateMap.get(timestamp)!, timeRangeMs),
  );
  const tooltipLabels = timestamps.map((timestamp) =>
    dateMap.get(timestamp)!.toLocaleString([], {
      month: "short",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
    }),
  );

  const datasets = Array.from(seriesMap.entries())
    .sort(([left], [right]) => {
      const leftOrder =
        RISK_CATEGORY_CHART_ORDER.get(left as RuleCategory) ??
        Number.MAX_SAFE_INTEGER;
      const rightOrder =
        RISK_CATEGORY_CHART_ORDER.get(right as RuleCategory) ??
        Number.MAX_SAFE_INTEGER;

      return leftOrder - rightOrder || left.localeCompare(right);
    })
    .map(([category, series], index) => {
      const color =
        getRiskCategoryChartColor(category) ??
        RISK_CATEGORY_CHART_COLORS[index % RISK_CATEGORY_CHART_COLORS.length]
          .color;
      const meta = RULE_CATEGORY_META[category as RuleCategory];
      return {
        label: meta?.label ?? category,
        data: timestamps.map((timestamp) => series.get(timestamp) ?? 0),
        borderColor: color,
        backgroundColor: `${color}1a`,
        pointBackgroundColor: color,
        fill: false,
        tension: 0.45,
        borderWidth: 1.5,
        pointRadius: 0,
        pointHoverRadius: 4,
      };
    });

  return {
    labels,
    tooltipLabels,
    datasets,
  };
}
