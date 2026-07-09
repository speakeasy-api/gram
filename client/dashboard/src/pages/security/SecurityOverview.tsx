import { MetricCard } from "@/components/chart/MetricCard";
import { ChartCard } from "@/components/chart/ChartCard";
import {
  formatChartLabel,
  formatChartZoomRangeLabel,
} from "@/components/chart/chartUtils";
import { useChartZoom } from "@/components/chart/useChartZoom";
import { InsightsConfig } from "@/components/insights-dock";
import { INSIGHTS_SUGGESTIONS } from "@/lib/insights-suggestions";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { DashboardCard } from "@/components/ui/dashboard-card";
import { Skeleton } from "@/components/ui/skeleton";
import { Button } from "@/components/ui/moonshine";
import { type DateRangePreset } from "@gram-ai/elements";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { useRiskOverview } from "@gram/client/react-query/riskOverview.js";
import { keepPreviousData } from "@tanstack/react-query";
import { ArrowRight, CircleAlert, Shield } from "lucide-react";
import { useCallback, useEffect, useMemo, type ReactNode } from "react";
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
import ZoomPlugin from "chartjs-plugin-zoom";
import { Line } from "react-chartjs-2";
import { Type } from "@/components/ui/type";
import { buildRiskTrendChartData, type TrendPoint } from "./riskTrendChartData";

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Filler,
  Tooltip,
  Legend,
  ZoomPlugin,
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

type BarDatum = {
  key: string;
  label: string;
  value: number;
  href?: string;
};

export default function SecurityOverview(): JSX.Element {
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

export function RiskOverviewRoot(): JSX.Element {
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
              <ArrowRight />
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
  const handleChartRangeSelect = useCallback(
    (from: Date, to: Date) => {
      setCustomRangeParam(from, to, formatChartZoomRangeLabel(from, to));
    },
    [setCustomRangeParam],
  );

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
    const riskEventsHref = routes.riskEvents.href();
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
  }, [overview?.topRules, routes.riskEvents, location.search]);

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
            <CircleAlert className="text-muted-foreground size-6" />
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

  return (
    <>
      {insightsContext && (
        <InsightsConfig
          contextInfo={insightsContext}
          suggestions={INSIGHTS_SUGGESTIONS["risk-overview"]}
          title="Risk insights"
          subtitle="Ask about policies, findings, and shadow MCP activity. Match content is redacted before it reaches the assistant."
        />
      )}
      <RiskOverviewShell rangeLabel={rangeLabel} controls={controls}>
        {policiesDisabledWithHistory && (
          <div className="bg-muted/30 flex items-start gap-3 rounded-lg border border-dashed px-4 py-3">
            <CircleAlert className="text-muted-foreground mt-0.5 size-4 shrink-0" />
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
                  <ArrowRight />
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
            onExpand={() => {
              void null;
            }}
            hasData={
              hasFindings &&
              overview.timeSeriesFindings.some((point) => point.findings > 0)
            }
            isZoomed={customRange !== null}
            onResetZoom={clearCustomRange}
          >
            <RiskTrendChart
              points={overview.timeSeriesFindings}
              from={overview.from}
              to={overview.to}
              height={250}
              onRangeSelect={handleChartRangeSelect}
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
  const agentsHref = `${routes.agentSessions.href()}?${agentsParams.toString()}`;

  const riskEventsHref = carriedRangeParams.toString()
    ? `${routes.riskEvents.href()}?${carriedRangeParams.toString()}`
    : routes.riskEvents.href();

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
                <ArrowRight />
              </Button.RightIcon>
            </Link>
          </Button>
          <Button variant="secondary" asChild>
            <Link to={riskEventsHref}>
              <Button.Text>View All Events</Button.Text>
              <Button.RightIcon>
                <ArrowRight />
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
      <ArrowRight className="size-3.5" />
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
  onRangeSelect,
}: {
  points: TrendPoint[];
  from: Date;
  to: Date;
  height: number;
  onRangeSelect?: (from: Date, to: Date) => void;
}) {
  const chartData = useMemo(
    () => buildRiskTrendChartData(points, from, to),
    [points, from, to],
  );
  const timeRangeMs = to.getTime() - from.getTime();
  const { chartRef, zoomPluginOptions, resetZoom } = useChartZoom({
    onRangeSelect,
  });

  useEffect(() => {
    resetZoom();
  }, [points, resetZoom]);

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
          title: (items) => {
            const x = items[0]?.parsed.x;
            if (x == null)
              return chartData.tooltipLabels[items[0]?.dataIndex ?? 0] ?? "";
            return new Date(x).toLocaleString([], {
              month: "short",
              day: "numeric",
              hour: "numeric",
              minute: "2-digit",
            });
          },
          label: (item) => {
            if ((item.parsed.y ?? 0) === 0) return undefined;
            return item.formattedValue
              ? `${item.dataset.label}: ${item.formattedValue}`
              : "";
          },
        },
      },
      zoom: zoomPluginOptions,
    },
    scales: {
      x: {
        type: "linear",
        grid: {
          display: true,
          color: CHART_COLORS.gridLineFaint,
          lineWidth: 1,
        },
        ticks: {
          maxTicksLimit: 8,
          callback: (value) =>
            formatChartLabel(new Date(value as number), timeRangeMs),
        },
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
      <Line
        ref={chartRef}
        data={{ datasets: chartData.datasets }}
        options={options}
      />
    </div>
  );
}
