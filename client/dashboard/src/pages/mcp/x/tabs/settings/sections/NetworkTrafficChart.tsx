import { ChartCard } from "@/components/chart/ChartCard";
import { AXIS, SERIES, TOOLTIP } from "@/components/chart/palette";
import { formatCompact } from "@/lib/format";
import {
  CategoryScale,
  Chart as ChartJS,
  LineElement,
  LinearScale,
  PointElement,
  Tooltip,
  type ChartData,
  type ChartOptions,
} from "chart.js";
import { useMemo } from "react";
import { Line } from "react-chartjs-2";

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
);

export type NetworkTrafficPoint = {
  bucketStart: string;
  publicRequests: number;
  privateRequests: number;
};

export type NetworkTrafficChartProps = {
  points: NetworkTrafficPoint[];
  loading: boolean;
  error: boolean;
  windowStart?: string;
  windowEnd?: string;
  scopeLabel?: string;
  lastPublicAt?: Date;
  lastPrivateAt?: Date;
};

const DISCLAIMER =
  "Observed requests only; absence of activity is not proof clients have migrated.";

function formatLastSeen(
  points: NetworkTrafficPoint[],
  route: "public" | "private",
  exact?: Date,
) {
  const lastSeen =
    exact ??
    [...points].reverse().find((point) => point[`${route}Requests`] > 0)
      ?.bucketStart;

  if (!lastSeen) return "No observed requests";

  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(lastSeen));
}

export function NetworkTrafficChart({
  points,
  loading,
  error,
  windowStart,
  windowEnd,
  scopeLabel = "",
  lastPublicAt,
  lastPrivateAt,
}: NetworkTrafficChartProps): JSX.Element {
  const publicTotal = points.reduce(
    (total, point) => total + point.publicRequests,
    0,
  );
  const privateTotal = points.reduce(
    (total, point) => total + point.privateRequests,
    0,
  );
  const hasData = publicTotal + privateTotal > 0;

  const data = useMemo<ChartData<"line", number[], string>>(
    () => ({
      labels: points.map((point) =>
        new Intl.DateTimeFormat(undefined, {
          month: "short",
          day: "numeric",
          hour: "numeric",
        }).format(new Date(point.bucketStart)),
      ),
      datasets: [
        {
          label: "Public route",
          data: points.map((point) => point.publicRequests),
          borderColor: SERIES[0]!,
          backgroundColor: SERIES[0]!,
          pointRadius: 2,
          pointHoverRadius: 4,
          borderWidth: 2,
          tension: 0.25,
        },
        {
          label: "Private route",
          data: points.map((point) => point.privateRequests),
          borderColor: SERIES[1]!,
          backgroundColor: SERIES[1]!,
          pointRadius: 2,
          pointHoverRadius: 4,
          borderWidth: 2,
          tension: 0.25,
        },
      ],
    }),
    [points],
  );

  const options = useMemo<ChartOptions<"line">>(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: "index", intersect: false },
      plugins: {
        legend: {
          position: "bottom",
          labels: { usePointStyle: true, boxWidth: 8, padding: 16 },
        },
        tooltip: {
          ...TOOLTIP,
          callbacks: {
            label: (item) =>
              ` ${item.dataset.label}: ${formatCompact(Number(item.parsed.y ?? 0))}`,
          },
        },
      },
      scales: {
        x: {
          grid: { color: AXIS.grid },
          ticks: { color: AXIS.label, maxTicksLimit: 8, maxRotation: 0 },
        },
        y: {
          beginAtZero: true,
          grid: { color: AXIS.grid },
          ticks: { color: AXIS.label, precision: 0 },
        },
      },
    }),
    [],
  );

  const windowLabel =
    windowStart && windowEnd
      ? `${new Date(windowStart).toLocaleDateString()} – ${new Date(windowEnd).toLocaleDateString()}`
      : scopeLabel;

  return (
    <div className="space-y-3">
      <ChartCard
        title="Network route traffic"
        chartId="mcp-network-traffic"
        hasData={hasData}
        expandable={false}
        loading={loading}
        error={error}
        expandedChart={null}
        onExpand={() => undefined}
      >
        {hasData ? (
          <div className="h-[240px] w-full">
            <Line
              data={data}
              options={options}
              aria-label="Public and private route requests over time"
              role="img"
            />
          </div>
        ) : (
          <div className="text-muted-foreground flex h-[240px] items-center justify-center text-sm">
            No observed requests in this window
          </div>
        )}
      </ChartCard>

      {!loading && !error && (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <RouteSummary
            route="Public route"
            total={publicTotal}
            lastSeen={formatLastSeen(points, "public", lastPublicAt)}
          />
          <RouteSummary
            route="Private route"
            total={privateTotal}
            lastSeen={formatLastSeen(points, "private", lastPrivateAt)}
          />
        </div>
      )}
      <p className="text-muted-foreground text-xs">
        {windowLabel && <span>{windowLabel}. </span>}
        {DISCLAIMER}
      </p>
    </div>
  );
}

function RouteSummary({
  route,
  total,
  lastSeen,
}: {
  route: string;
  total: number;
  lastSeen: string;
}): JSX.Element {
  return (
    <div className="border-border bg-card space-y-1 border p-3">
      <div className="text-eyebrow">{route}</div>
      <div className="text-sm font-medium">{formatCompact(total)} requests</div>
      <div className="text-muted-foreground text-xs">Last seen: {lastSeen}</div>
    </div>
  );
}
