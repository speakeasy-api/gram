import { ACCENT_RED, TOOLTIP } from "@/components/chart/palette";
import { Chart as ChartJS, type ChartOptions, type Scale } from "chart.js";

export const CHART_COLORS = {
  label: "#737373",
  labelFaded: "#A3A3A3",
  gridLine: "#e5e5e5",
} as const;

// Failure stacks: the one brand-red accent leads, the neutral series steps
// recede behind it so severity reads at a glance. Slice off the palette's
// trailing entry — green has no place in a failure ramp.
export function failureColors(seriesColors: string[]): string[] {
  return [ACCENT_RED, ...seriesColors.slice(0, -1)];
}

export const COLLAPSED_BAR_CHART_MAX_ROWS = 6;
export const BAR_THICKNESS = { collapsed: 18, expanded: 24 };
export const BAR_ROW_HEIGHT = { collapsed: 18, expanded: 24 };
export const BAR_ROW_SPACER = { collapsed: 8, expanded: 12 };
export const BAR_BORDER_RADIUS = 0;
export const LINE_CHART_HEIGHT = { collapsed: 250, expanded: 600 };

type _BarLegend = Exclude<
  NonNullable<ChartOptions<"bar">["plugins"]>["legend"],
  false
>;
type _BarTooltip = NonNullable<ChartOptions<"bar">["plugins"]>["tooltip"];
type _BarScales = NonNullable<ChartOptions<"bar">["scales"]>;

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

// Category-axis labels are mostly emails. Keep the domain — it's how you tell
// internal from external users — and spend the character budget on the local
// part instead. Full label is still available in the tooltip title.
const MAX_AXIS_LABEL_CHARS = 26;

export function truncateAxisLabel(label: string): string {
  if (label.length <= MAX_AXIS_LABEL_CHARS) return label;

  const at = label.lastIndexOf("@");
  if (at > 0) {
    const domain = label.slice(at);
    if (domain.length <= MAX_AXIS_LABEL_CHARS - 4) {
      return `${label.slice(0, MAX_AXIS_LABEL_CHARS - 1 - domain.length)}…${domain}`;
    }
  }

  return `${label.slice(0, MAX_AXIS_LABEL_CHARS - 1)}…`;
}

export const SHARED_BAR_SCALES = {
  x: {
    stacked: true,
    grid: { color: CHART_COLORS.gridLine },
    ticks: { color: CHART_COLORS.labelFaded, precision: 0 },
    afterFit(scale: Scale) {
      scale.paddingRight = 30;
    },
  },
  y: {
    stacked: true,
    grid: { display: false },
    ticks: {
      color: CHART_COLORS.labelFaded,
      crossAlign: "far" as const,
      padding: 2,
      font: { size: 12 },
      callback(value) {
        return truncateAxisLabel(this.getLabelForValue(value as number));
      },
    },
  },
} satisfies _BarScales;
