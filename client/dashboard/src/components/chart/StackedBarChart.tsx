import {
  BAR_BORDER_RADIUS,
  BAR_ROW_HEIGHT,
  BAR_ROW_SPACER,
  BAR_THICKNESS,
  CHART_COLORS,
  EXPANDED_LEGEND,
  SHARED_BAR_SCALES,
  SHARED_LEGEND,
  SHARED_RESIZE_TRANSITION,
  SHARED_TOOLTIP,
} from "@/components/chart/chartTheme";
import { Button } from "@/components/ui/Button";
import {
  Chart as ChartJS,
  type ChartOptions,
  type TooltipItem,
} from "chart.js";
import { useMemo } from "react";
import { Bar } from "react-chartjs-2";

export type StackedBarDataset = {
  label: string;
  data: Array<number | null>;
  backgroundColor: string;
  borderColor?: string;
  borderWidth?: number;
  barThickness: number;
  borderRadius?: number;
  borderSkipped?: string | boolean;
  hoverBackgroundColor?: string;
  hoverBorderColor?: string;
};

export function hideZeroBarSegments(data: Array<number | null>) {
  return data.map((value) => (value === 0 ? null : value));
}

const stackTotalPlugin = {
  id: "stackTotal",
  afterDatasetsDraw(chart: ChartJS) {
    const { ctx, data } = chart;
    ctx.save();
    ctx.font = "12px sans-serif";
    ctx.fillStyle = CHART_COLORS.label;
    ctx.textAlign = "left";
    ctx.textBaseline = "middle";
    for (let i = 0; i < (data.labels?.length ?? 0); i++) {
      let total = 0;
      let labelX: number | null = null;
      let labelY: number | null = null;

      data.datasets.forEach((dataset, datasetIndex) => {
        const value = dataset.data[i];
        if (typeof value !== "number" || value === 0) return;

        total += value;
        const bar = chart.getDatasetMeta(datasetIndex).data[i];
        if (!bar) return;

        if (labelX === null || bar.x > labelX) {
          labelX = bar.x;
          labelY = bar.y;
        }
      });

      if (total > 0 && labelX !== null && labelY !== null) {
        ctx.fillText(String(total), labelX + 4, labelY);
      }
    }
    ctx.restore();
  },
};

const STACKED_BAR_PLUGINS = [stackTotalPlugin];

export function StackedBarChart({
  labels,
  datasets,
  handleFilter,
  tooltipLabelFn,
  expanded = false,
  maxRows,
  onShowAll,
}: {
  labels: string[];
  datasets: StackedBarDataset[];
  handleFilter?: (datasetLabel: string, rowLabel: string) => void;
  tooltipLabelFn?: (item: TooltipItem<"bar">) => string | string[] | undefined;
  expanded?: boolean;
  maxRows?: number;
  onShowAll?: () => void;
}) {
  const thickness = expanded ? BAR_THICKNESS.expanded : BAR_THICKNESS.collapsed;
  const hiddenCount =
    !expanded && maxRows && labels.length > maxRows
      ? labels.length - maxRows
      : 0;
  const visibleLabels = hiddenCount > 0 ? labels.slice(0, maxRows) : labels;
  const visibleDatasets = (
    hiddenCount > 0
      ? datasets.map((ds) => ({
          ...ds,
          data: ds.data.slice(0, maxRows),
        }))
      : datasets
  ).map((ds) => ({
    ...ds,
    data: hideZeroBarSegments(ds.data),
    barThickness: thickness,
    borderRadius: BAR_BORDER_RADIUS,
    borderSkipped: false,
  }));

  const rowH = expanded ? BAR_ROW_HEIGHT.expanded : BAR_ROW_HEIGHT.collapsed;
  const rowS = expanded ? BAR_ROW_SPACER.expanded : BAR_ROW_SPACER.collapsed;
  const containerHeight = Math.max(
    120,
    visibleLabels.length * (rowH + rowS) + 60,
  );

  const options = useMemo<ChartOptions<"bar">>(
    () => ({
      indexAxis: "y",
      responsive: true,
      maintainAspectRatio: false,
      onClick(_, elements) {
        if (!elements.length || !handleFilter) return;
        const { datasetIndex, index } = elements[0]!;
        const datasetLabel = datasets[datasetIndex]?.label;
        const rowLabel = visibleLabels[index];
        if (datasetLabel && rowLabel) handleFilter(datasetLabel, rowLabel);
      },
      onHover(event, elements) {
        const el = event.native?.target as HTMLElement | null;
        if (el) el.style.cursor = elements.length ? "pointer" : "default";
      },
      scales: SHARED_BAR_SCALES,
      transitions: SHARED_RESIZE_TRANSITION,
      plugins: {
        legend: expanded ? EXPANDED_LEGEND : SHARED_LEGEND,
        tooltip: {
          ...SHARED_TOOLTIP,
          callbacks: {
            label:
              tooltipLabelFn ??
              ((item: TooltipItem<"bar">) =>
                ` ${item.dataset.label}: ${item.parsed.x}`),
          },
        },
      },
    }),
    [datasets, visibleLabels, handleFilter, tooltipLabelFn, expanded],
  );

  if (visibleLabels.length === 0) return null;

  return (
    <>
      <div
        className="transition-all duration-200 ease-in-out"
        style={{ height: containerHeight }}
      >
        <Bar
          plugins={STACKED_BAR_PLUGINS}
          data={{ labels: visibleLabels, datasets: visibleDatasets }}
          options={options}
        />
      </div>
      {hiddenCount > 0 && onShowAll && (
        <div className="mt-2 flex w-full">
          <Button
            variant="tertiary"
            size="sm"
            icon="chevron-down"
            iconAfter={true}
            onClick={onShowAll}
          >
            Show {hiddenCount} more
          </Button>
        </div>
      )}
    </>
  );
}
