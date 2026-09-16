import { TOOLTIP } from "@/components/chart/palette";
import { Chart as ChartJS, type ChartOptions } from "chart.js";

export const CHART_COLORS = {
  label: "#737373",
  labelFaded: "#A3A3A3",
  gridLine: "#e5e5e5",
} as const;

type _BarLegend = Exclude<
  NonNullable<ChartOptions<"bar">["plugins"]>["legend"],
  false
>;
type _BarTooltip = NonNullable<ChartOptions<"bar">["plugins"]>["tooltip"];

export const SHARED_RESIZE_TRANSITION = {
  resize: { animation: { duration: 0 } },
} as const;

export const SHARED_LEGEND = {
  display: false,
} satisfies NonNullable<_BarLegend>;

// Expanded charts have room for a legend: mono uppercase micro-labels with
// square swatches (the eyebrow idiom, rendered on canvas).
export const EXPANDED_LEGEND = {
  display: true,
  position: "bottom",
  align: "start",
  labels: {
    boxWidth: 8,
    boxHeight: 8,
    usePointStyle: false,
    padding: 16,
    color: CHART_COLORS.label,
    font: { family: "monospace", size: 11 },
    generateLabels: (chart: ChartJS) =>
      ChartJS.defaults.plugins.legend.labels
        .generateLabels(chart)
        .map((item) => ({ ...item, text: item.text.toUpperCase() })),
  },
} satisfies NonNullable<_BarLegend>;

export const SHARED_TOOLTIP = {
  ...TOOLTIP,
  cornerRadius: 0,
  boxWidth: 8,
  boxHeight: 8,
} satisfies _BarTooltip;
