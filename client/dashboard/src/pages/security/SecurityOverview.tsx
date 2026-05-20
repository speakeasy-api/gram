import { MetricCard } from "@/components/chart/MetricCard";
import { ChartCard } from "@/components/chart/ChartCard";
import { formatChartLabel } from "@/components/chart/chartUtils";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { DashboardCard } from "@/components/ui/dashboard-card";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { useRiskOverview } from "@gram/client/react-query/index.js";
import { Shield } from "lucide-react";
import { useMemo, type ReactNode } from "react";
import { Link } from "react-router";
import { useRoutes } from "@/routes";
import { RULE_CATEGORY_META, type RuleCategory } from "./policy-data";
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

function RiskOverviewShell({
  children,
  days = 7,
}: {
  children: ReactNode;
  days?: number;
}) {
  return (
    <Page.Section>
      <Page.Section.Title stage="beta">Risk Overview</Page.Section.Title>
      <Page.Section.Description>
        Risk analysis summary for policy findings{" "}
        {days > 0 ? ` across the last ${days.toLocaleString()} days` : ""}
      </Page.Section.Description>
      <Page.Section.CTA> {/* tbd */}</Page.Section.CTA>
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
  const overviewQuery = useRiskOverview();
  const overview = overviewQuery.data;

  const topCategories = useMemo<BarDatum[]>(() => {
    return (overview?.topCategories ?? []).map((category) => {
      const key = category.category;
      const meta = RULE_CATEGORY_META[key as RuleCategory];
      return {
        key,
        label: meta?.label ?? key,
        value: category.findings,
      };
    });
  }, [overview?.topCategories]);

  const topUsers = useMemo<BarDatum[]>(() => {
    return (overview?.topUsers ?? []).map((user) => ({
      key: user.email,
      label: user.email,
      value: user.findings,
    }));
  }, [overview?.topUsers]);

  if (overviewQuery.isLoading) {
    return (
      <RiskOverviewShell>
        <div className="flex items-center justify-center py-20">
          <p className="text-muted-foreground text-sm">Loading...</p>
        </div>
      </RiskOverviewShell>
    );
  }

  if (overviewQuery.error) {
    return (
      <RiskOverviewShell>
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

  if (!overview) {
    return null;
  }

  if (overview.activePolicies === 0) {
    return <NoPoliciesEmptyState />;
  }

  const hasFindings = overview.findings > 0;

  return (
    <>
      <RiskOverviewShell>
        <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
          <MetricCard
            title="Messages Scanned"
            value={overview.messagesScanned}
            format="number"
            icon="scan-search"
          />
          <MetricCard
            title="Findings"
            value={overview.findings}
            format="number"
            icon="flag"
          />
          <MetricCard
            title="Flagged Sessions"
            value={overview.flaggedSessions}
            format="number"
            icon="message-square"
          />
          <MetricCard
            title="Active Policies"
            value={overview.activePolicies}
            format="number"
            icon="shield-check"
          />
        </div>
      </RiskOverviewShell>

      {overview.activePolicies > 0 && (
        <RiskActivitySection>
          <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
            <DashboardChartCard
              title="Top Risk Events by Category"
              empty={!hasFindings || topCategories.length === 0}
            >
              <RankedBarList items={topCategories} />
            </DashboardChartCard>
            <DashboardChartCard
              title="Users with Most Findings"
              empty={!hasFindings || topUsers.length === 0}
            >
              <RankedBarList items={topUsers} />
            </DashboardChartCard>
          </div>

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
        </RiskActivitySection>
      )}
    </>
  );
}

function RiskActivitySection({ children }: { children: ReactNode }) {
  const routes = useRoutes();

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
            <Link to={routes.logs.agents.href()}>
              <Button.Text>View All Sessions</Button.Text>
              <Button.RightIcon>
                <Icon name="arrow-right" />
              </Button.RightIcon>
            </Link>
          </Button>
          <Button variant="secondary" asChild>
            <Link to={routes.logs.riskEvents.href()}>
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
  children,
}: {
  title: string;
  empty: boolean;
  children: ReactNode;
}) {
  return (
    <DashboardCard title={title}>
      {empty ? <ChartEmptyState /> : children}
    </DashboardCard>
  );
}

function ChartEmptyState() {
  return <p className="text-muted-foreground text-sm">No findings recorded</p>;
}

function RankedBarList({ items }: { items: BarDatum[] }) {
  const max = items[0]?.value || 1;

  return (
    <ul className="my-1 space-y-3">
      {items.map((item, i) => (
        <li key={item.key} className="flex items-center gap-3">
          <span className="text-muted-foreground w-4 shrink-0 text-right text-xs">
            {i + 1}
          </span>
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
        </li>
      ))}
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
