import { Page } from "@/components/page-layout";
import { MetricCard } from "@/components/chart/MetricCard";
import { formatChartLabel } from "@/components/chart/chartUtils";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useRoutes } from "@/routes";
import { cn } from "@/lib/utils";
import { MessageSquare } from "lucide-react";
import { useMemo } from "react";
import {
  Chart as ChartJS,
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  ArcElement,
  Filler,
  Tooltip,
  Legend,
} from "chart.js";
import { Line, Doughnut } from "react-chartjs-2";
import {
  generateMockEvents,
  generateTimeSeries,
  computeUserRisks,
  computeCategoryBreakdown,
  type DlpCategory,
  type EventType,
  type Severity,
} from "./mock-data";

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  ArcElement,
  Filler,
  Tooltip,
  Legend,
);

const CATEGORY_LABELS: Record<DlpCategory, string> = {
  secret: "Secrets",
  financial: "Financial",
  government_id: "Government IDs",
  healthcare: "Healthcare",
  contact_info: "Contact Info",
  prompt_attack: "Prompt Attack",
  prompt_injection: "Prompt Injection",
  off_policy: "Off-Policy",
};

const CATEGORY_COLORS: Record<DlpCategory, string> = {
  secret: "rgb(239, 68, 68)",
  financial: "rgb(99, 102, 241)",
  government_id: "rgb(245, 158, 11)",
  healthcare: "rgb(168, 85, 247)",
  contact_info: "rgb(34, 197, 94)",
  prompt_attack: "rgb(220, 38, 38)",
  prompt_injection: "rgb(249, 115, 22)",
  off_policy: "rgb(14, 165, 233)",
};

const EVENT_TYPE_LABELS: Record<EventType, string> = {
  tool_argument: "Tool Argument",
  tool_output: "Tool Output",
  user_message: "User Message",
  assistant_message: "Assistant Message",
};

const SEVERITY_VARIANT: Record<
  Severity,
  "destructive" | "warning" | "secondary" | "outline"
> = {
  critical: "destructive",
  high: "warning",
  medium: "secondary",
  low: "outline",
};

function SeverityBadge({ severity }: { severity: Severity }) {
  return (
    <Badge variant={SEVERITY_VARIANT[severity]} className="capitalize">
      {severity}
    </Badge>
  );
}

function CategoryBadge({ category }: { category: DlpCategory }) {
  return (
    <Badge variant="outline" className="gap-1.5">
      <span
        className="size-2 rounded-full"
        style={{ backgroundColor: CATEGORY_COLORS[category] }}
      />
      {CATEGORY_LABELS[category]}
    </Badge>
  );
}

export default function DlpOverview() {
  const routes = useRoutes();
  const events = useMemo(() => generateMockEvents(200), []);
  const timeSeries = useMemo(() => generateTimeSeries(events), [events]);
  const userRisks = useMemo(() => computeUserRisks(events), [events]);
  const categoryBreakdown = useMemo(
    () => computeCategoryBreakdown(events),
    [events],
  );

  const totalProcessed = useMemo(
    () => timeSeries.reduce((sum, d) => sum + d.total, 0),
    [timeSeries],
  );
  const totalFlagged = events.length;
  const flagRate =
    totalProcessed > 0 ? (totalFlagged / totalProcessed) * 100 : 0;

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        {/* Metric Cards */}
        <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
          <MetricCard
            title="Events Processed"
            value={totalProcessed}
            icon="activity"
            accentColor="blue"
          />
          <MetricCard
            title="Flagged Events"
            value={totalFlagged}
            icon="shield-alert"
            accentColor="red"
            previousValue={Math.round(totalFlagged * 0.85)}
            comparisonLabel="vs previous period"
          />
          <MetricCard
            title="Flag Rate"
            value={flagRate}
            format="percent"
            icon="percent"
            accentColor="orange"
            invertDelta
          />
        </div>

        {/* Charts Row */}
        <div className="mt-6 grid grid-cols-1 gap-6 lg:grid-cols-3">
          {/* Time Series Chart */}
          <div className="bg-card rounded-lg border p-5 lg:col-span-2">
            <h3 className="mb-4 text-sm font-semibold">
              Flagged vs Total Events (Last 30 Days)
            </h3>
            <div className="h-[280px]">
              <TimeSeriesChart timeSeries={timeSeries} />
            </div>
          </div>

          {/* Category Breakdown */}
          <div className="bg-card rounded-lg border p-5">
            <h3 className="mb-4 text-sm font-semibold">Flagged by Category</h3>
            <div className="flex h-[280px] items-center justify-center">
              <CategoryChart breakdown={categoryBreakdown} />
            </div>
          </div>
        </div>

        {/* Users at Risk */}
        <div className="bg-card mt-6 rounded-lg border p-5">
          <h3 className="mb-4 text-sm font-semibold">Top Users by Risk</h3>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>User</TableHead>
                <TableHead className="text-right">Flagged Events</TableHead>
                <TableHead>Top Category</TableHead>
                <TableHead>Last Flagged</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {userRisks.slice(0, 10).map((user) => (
                <TableRow key={user.userId}>
                  <TableCell className="font-medium">{user.userName}</TableCell>
                  <TableCell className="text-right tabular-nums">
                    {user.flaggedCount}
                  </TableCell>
                  <TableCell>
                    <CategoryBadge category={user.topCategory} />
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {user.lastFlagged.toLocaleDateString()}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>

        {/* Recent Flagged Events */}
        <div className="bg-card mt-6 rounded-lg border p-5">
          <h3 className="mb-4 text-sm font-semibold">Recent Flagged Events</h3>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Timestamp</TableHead>
                <TableHead>User</TableHead>
                <TableHead>Event Type</TableHead>
                <TableHead>Category</TableHead>
                <TableHead>Rule</TableHead>
                <TableHead>Severity</TableHead>
                <TableHead>Content</TableHead>
                <TableHead className="w-[100px]" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {events.slice(0, 20).map((event) => (
                <TableRow key={event.id}>
                  <TableCell className="text-muted-foreground tabular-nums">
                    {event.timestamp.toLocaleString()}
                  </TableCell>
                  <TableCell className="font-medium">
                    {event.userName}
                  </TableCell>
                  <TableCell>
                    <Badge variant="secondary">
                      {EVENT_TYPE_LABELS[event.eventType]}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    <CategoryBadge category={event.category} />
                  </TableCell>
                  <TableCell className="text-muted-foreground">
                    {event.ruleName}
                  </TableCell>
                  <TableCell>
                    <SeverityBadge severity={event.severity} />
                  </TableCell>
                  <TableCell
                    className={cn(
                      "text-muted-foreground max-w-[200px] truncate font-mono text-xs",
                    )}
                  >
                    {event.contentPreview}
                  </TableCell>
                  <TableCell>
                    <routes.chatSessions.Link className="no-underline hover:no-underline">
                      <Button variant="ghost" size="sm" className="gap-1.5">
                        <MessageSquare className="size-3.5" />
                        View Chat
                      </Button>
                    </routes.chatSessions.Link>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </Page.Body>
    </Page>
  );
}

function TimeSeriesChart({
  timeSeries,
}: {
  timeSeries: Array<{ date: string; flagged: number; total: number }>;
}) {
  const data = {
    labels: timeSeries.map((d) => formatChartLabel(new Date(d.date), "30d")),
    datasets: [
      {
        label: "Total Events",
        data: timeSeries.map((d) => d.total),
        borderColor: "rgb(99, 102, 241)",
        backgroundColor: "rgba(99, 102, 241, 0.05)",
        fill: true,
        tension: 0.3,
        pointRadius: 0,
        pointHoverRadius: 4,
        borderWidth: 2,
      },
      {
        label: "Flagged Events",
        data: timeSeries.map((d) => d.flagged),
        borderColor: "rgb(239, 68, 68)",
        backgroundColor: "rgba(239, 68, 68, 0.1)",
        fill: true,
        tension: 0.3,
        pointRadius: 0,
        pointHoverRadius: 4,
        borderWidth: 2,
      },
    ],
  };

  const options = {
    responsive: true,
    maintainAspectRatio: false,
    interaction: {
      mode: "index" as const,
      intersect: false,
    },
    plugins: {
      legend: {
        position: "top" as const,
        labels: {
          usePointStyle: true,
          pointStyle: "circle",
          boxWidth: 6,
          padding: 16,
          font: { size: 12 },
        },
      },
      tooltip: {
        backgroundColor: "rgba(0, 0, 0, 0.8)",
        padding: 12,
        titleFont: { size: 12 },
        bodyFont: { size: 12 },
      },
    },
    scales: {
      x: {
        grid: { display: false },
        ticks: {
          maxTicksLimit: 10,
          font: { size: 11 },
        },
      },
      y: {
        beginAtZero: true,
        grid: {
          color: "rgba(0, 0, 0, 0.04)",
        },
        ticks: {
          font: { size: 11 },
        },
      },
    },
  };

  return <Line data={data} options={options} />;
}

function CategoryChart({
  breakdown,
}: {
  breakdown: Array<{ category: DlpCategory; count: number }>;
}) {
  const data = {
    labels: breakdown.map((b) => CATEGORY_LABELS[b.category]),
    datasets: [
      {
        data: breakdown.map((b) => b.count),
        backgroundColor: breakdown.map((b) => CATEGORY_COLORS[b.category]),
        borderWidth: 0,
        hoverOffset: 4,
      },
    ],
  };

  const options = {
    responsive: true,
    maintainAspectRatio: false,
    cutout: "60%",
    plugins: {
      legend: {
        position: "bottom" as const,
        labels: {
          usePointStyle: true,
          pointStyle: "circle",
          boxWidth: 6,
          padding: 12,
          font: { size: 12 },
        },
      },
      tooltip: {
        backgroundColor: "rgba(0, 0, 0, 0.8)",
        padding: 10,
        bodyFont: { size: 12 },
      },
    },
  };

  return <Doughnut data={data} options={options} />;
}
