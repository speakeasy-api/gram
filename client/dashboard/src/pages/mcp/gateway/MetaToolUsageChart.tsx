import { ChartCard } from "@/components/chart/ChartCard";
import { AXIS, TOOLTIP, withAlpha } from "@/components/chart/palette";
import {
  useIsDarkTheme,
  useSeriesColors,
} from "@/components/chart/useSeriesColors";
import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { formatCompact } from "@/lib/format";
import {
  BarElement,
  CategoryScale,
  Chart as ChartJS,
  LinearScale,
  Tooltip,
  type ChartOptions,
} from "chart.js";
import { useMemo } from "react";
import { Bar } from "react-chartjs-2";
import type { MetaToolUsageItem } from "./gatewayActivity";

ChartJS.register(CategoryScale, LinearScale, BarElement, Tooltip);

// Horizontal bars, one per gateway tool, in the order an agent reaches for
// them. The tooltip carries each tool's share of all gateway calls.
export function MetaToolUsageChart({
  items,
  chartId,
  expandedChart,
  onExpand,
  loading,
  error,
}: {
  items: MetaToolUsageItem[];
  chartId: string;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
  loading?: boolean;
  error?: boolean;
}): JSX.Element {
  const isExpanded = expandedChart === chartId;
  const isDark = useIsDarkTheme();
  const colors = useSeriesColors();
  const total = items.reduce((sum, item) => sum + item.value, 0);
  const hasData = total > 0;

  const data = useMemo(
    () => ({
      labels: items.map((item) => item.label),
      datasets: [
        {
          label: "Calls",
          data: items.map((item) => item.value),
          backgroundColor: items.map((_, i) =>
            withAlpha(colors[i % colors.length]!, 0.85),
          ),
          hoverBackgroundColor: items.map((_, i) => colors[i % colors.length]!),
          borderRadius: 2,
          borderSkipped: false,
          barThickness: isExpanded ? 28 : 18,
        },
      ],
    }),
    [items, colors, isExpanded],
  );

  const options = useMemo<ChartOptions<"bar">>(() => {
    const textColor = isDark ? AXIS.faded : AXIS.label;
    return {
      indexAxis: "y",
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        legend: { display: false },
        tooltip: {
          ...TOOLTIP,
          padding: 12,
          displayColors: false,
          callbacks: {
            label: (item) => {
              const value = Number(item.parsed.x ?? 0);
              const share = total > 0 ? (value / total) * 100 : 0;
              return ` ${formatCompact(value)} calls · ${share.toFixed(0)}% of gateway calls`;
            },
          },
        },
      },
      scales: {
        x: {
          beginAtZero: true,
          grid: { color: isDark ? AXIS.gridDark : AXIS.grid },
          ticks: {
            color: textColor,
            maxTicksLimit: 6,
            callback: (value) => formatCompact(Number(value)),
          },
        },
        y: {
          grid: { display: false },
          ticks: {
            color: textColor,
            font: { family: "ui-monospace, SFMono-Regular, Menlo, monospace" },
          },
        },
      },
    };
  }, [isDark, total]);

  return (
    <ChartCard
      title="Gateway tool usage"
      chartId={chartId}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={hasData}
      loading={loading}
      error={error}
    >
      {!hasData ? (
        <WidgetEmptyState
          message="No gateway tool calls in the selected range"
          className="h-[220px]"
        />
      ) : (
        <div style={{ height: isExpanded ? 360 : 220 }}>
          <Bar data={data} options={options} />
        </div>
      )}
    </ChartCard>
  );
}
