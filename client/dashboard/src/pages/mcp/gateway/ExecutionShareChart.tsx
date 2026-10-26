import { ChartCard } from "@/components/chart/ChartCard";
import { chartColors, SHARED_TOOLTIP } from "@/components/chart/chartTheme";
import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { Text } from "@/components/ui/Text";
import { useIsDarkTheme } from "@/lib/theme";
import {
  CategoryScale,
  Chart as ChartJS,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip,
  type ChartData,
  type ChartOptions,
  type PointStyle,
} from "chart.js";
import { useMemo } from "react";
import { Line } from "react-chartjs-2";
import {
  CALL_POSITION_LABELS,
  CALL_POSITIONS,
  type GatewayMode,
  type PositionLine,
} from "./gatewayActivity";
import { gatewayModeColor } from "./gatewayActivityColors";
import { SeriesLegend } from "./SeriesLegend";

ChartJS.register(
  CategoryScale,
  LinearScale,
  PointElement,
  LineElement,
  Tooltip,
);

const CHART_ID = "gateway-execution-share";

// Each mode also has its own marker, so lines that meet (every mode starts
// at a lookup or a call) stay apart without relying on color.
const MODE_POINT_STYLES: Record<GatewayMode, PointStyle> = {
  progressive: "circle",
  code_mode: "rect",
  direct: "triangle",
  unrecorded: "crossRot",
};

function positionTitle(position: string): string {
  const label = CALL_POSITION_LABELS[position]!;
  return position.includes("-") || position.endsWith("+")
    ? `Calls ${label} in a session`
    : `Call ${label} in a session`;
}

/**
 * How the share of calls that ran a tool grows with each call in a session,
 * one line per discovery mode. A mode that discovers less sits higher sooner.
 */
export function ExecutionShareChart({
  lines,
  callsWithoutSession,
  hasCalls,
  loading,
  expandedChart,
  onExpand,
}: {
  lines: PositionLine[];
  /** Calls in the range that carried no session, which the chart leaves out. */
  callsWithoutSession: number;
  /** Whether the range has any calls at all, with or without a session. */
  hasCalls: boolean;
  loading: boolean;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
}): JSX.Element {
  const isDark = useIsDarkTheme();
  const colors = useMemo(() => chartColors(isDark), [isDark]);
  const isExpanded = expandedChart === CHART_ID;
  const hasData = lines.length > 0;
  // Positions past the longest session's last call are left off the axis.
  const positions = useMemo(() => {
    const reached = Math.max(
      0,
      ...lines.map((line) => line.shares.findLastIndex((s) => s !== null) + 1),
    );
    return CALL_POSITIONS.slice(0, reached);
  }, [lines]);

  const data = useMemo<ChartData<"line", (number | null)[], string>>(
    () => ({
      labels: positions.map((position) => CALL_POSITION_LABELS[position]!),
      datasets: lines.map((line) => {
        const color = gatewayModeColor(line.mode, isDark);
        return {
          label: line.label,
          data: line.shares
            .slice(0, positions.length)
            .map((share) =>
              share === null ? null : Math.round(share * 1000) / 10,
            ),
          borderColor: color,
          backgroundColor: color,
          borderWidth: 2,
          pointStyle: MODE_POINT_STYLES[line.mode],
          pointRadius: 5,
          pointHoverRadius: 7,
          // Points on 100% sit on the chart's edge; keep them whole.
          clip: false,
          // A position no call reached is a gap, not a zero.
          spanGaps: false,
          tension: 0,
        };
      }),
    }),
    [lines, positions, isDark],
  );

  const options = useMemo<ChartOptions<"line">>(
    () => ({
      responsive: true,
      maintainAspectRatio: false,
      interaction: { mode: "index", intersect: false },
      layout: { padding: { top: 8 } },
      plugins: {
        legend: { display: false },
        tooltip: {
          ...SHARED_TOOLTIP,
          callbacks: {
            title: (items) =>
              positionTitle(positions[items[0]?.dataIndex ?? 0] ?? "1"),
            label: (item) => {
              const line = lines[item.datasetIndex];
              if (!line) return "";
              const ran = line.executionCalls[item.dataIndex] ?? 0;
              const total = ran + (line.discoveryCalls[item.dataIndex] ?? 0);
              return `${line.label}: ${item.formattedValue}% ran a tool (${ran.toLocaleString()} of ${total.toLocaleString()})`;
            },
          },
        },
      },
      scales: {
        x: {
          grid: { display: false },
          border: { display: true, color: colors.gridLine },
          ticks: { color: colors.labelFaded, maxRotation: 0 },
          title: {
            display: true,
            text: "Call number in session",
            color: colors.labelFaded,
          },
        },
        y: {
          min: 0,
          max: 100,
          grid: { color: colors.gridLine },
          ticks: {
            stepSize: 25,
            color: colors.labelFaded,
            callback: (value) => `${value}%`,
          },
        },
      },
    }),
    [lines, positions, colors],
  );

  const height = isExpanded ? 420 : 260;
  let body: JSX.Element;
  if (hasData) {
    body = (
      <div className="flex flex-col gap-3">
        <div style={{ height }}>
          <Line data={data} options={options} />
        </div>
        {/* One line is named by the tile above; several need a key. */}
        {lines.length > 1 && (
          <SeriesLegend
            items={lines.map((line) => ({
              label: line.label,
              color: gatewayModeColor(line.mode, isDark),
            }))}
          />
        )}
        {callsWithoutSession > 0 && (
          <Text muted small>
            Excludes {callsWithoutSession.toLocaleString()}{" "}
            {callsWithoutSession === 1 ? "call" : "calls"} with no session.
          </Text>
        )}
      </div>
    );
  } else {
    // Calls without a session cannot be put in order; Calls over time still
    // splits them.
    body = (
      <WidgetEmptyState
        message={
          hasCalls
            ? "Agents' clients sent no session with these calls, so they can't be ordered. Calls over time still shows how they split."
            : "No calls in the selected range"
        }
        className="h-[260px]"
      />
    );
  }

  return (
    <ChartCard
      title="Execution share as sessions progress"
      chartId={CHART_ID}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={hasData}
      loading={loading}
    >
      {body}
    </ChartCard>
  );
}
