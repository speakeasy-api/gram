// The single source of Chart.js styling for the consolidated chart system in
// this directory. Every chart component here (Timeseries, StackedBarChart, …)
// pulls its palette, tooltip, grid, and tick styling from this module instead
// of hand-rolling a local `CHART_COLORS` object — the pattern duplicated
// verbatim across src/components/observe/InsightsAgents.tsx,
// src/components/billing/breakdown-options.ts,
// src/components/observe/InsightsTools.tsx (USER_SOURCE_COLORS), and
// src/pages/security/SecurityOverview.tsx.
//
// Design language: the language palette (the --color-lang-* tokens in
// src/components/ui/moonshine/base.css) IS the series palette — colors
// identify series, never surface fills. Tooltips render as a fixed dark
// "ink" surface with "bone" text regardless of the app's light/dark theme,
// matching the existing CHART_COLORS.tooltipBg/tooltipTitle/tooltipBody
// convention. Grid lines are a faint neutral; axis labels are small mono
// type; corners are always square (no rounded()) per the brand's --radius:0
// rule.
import {
  BarController,
  BarElement,
  CategoryScale,
  Chart as ChartJS,
  Filler,
  Legend,
  LinearScale,
  LineController,
  LineElement,
  PointElement,
  Tooltip,
  type ChartOptions,
  type TooltipItem,
} from "chart.js";
import ZoomPlugin from "chartjs-plugin-zoom";

// Extraction vehicle for the tooltip/scale option shapes, mirroring the
// `_BarTooltip` / `_BarScales` convention already used in
// src/components/observe/InsightsTools.tsx. Tooltip/grid/tick options don't
// vary structurally by chart type (only `callbacks` does, and that's left to
// the caller), so instantiating against "bar" is safe to spread into any
// chart type's options.
/**
 * Plain-value tooltip styling shape. Deliberately NOT derived from
 * Chart.js's TooltipOptions: those carry scriptable-callback unions whose
 * chart-type generics are invariant, so a "bar"-derived object can't
 * spread into a "line" chart's options. Concrete values spread into any
 * chart type.
 */
interface SharedTooltipStyle {
  enabled: boolean;
  backgroundColor: string;
  titleColor: string;
  bodyColor: string;
  borderColor: string;
  borderWidth: number;
  cornerRadius: number;
  padding: number;
  boxPadding: number;
  boxWidth: number;
  boxHeight: number;
  displayColors: boolean;
  titleFont: { family: string; size: number };
  bodyFont: { family: string; size: number };
}
type _SharedScale = NonNullable<
  NonNullable<ChartOptions<"bar">["scales"]>["x"]
>;

let registered = false;

/**
 * Registers every Chart.js controller/element/plugin the app's charts need,
 * including the zoom plugin. Idempotent — safe to call from every chart
 * component's module scope without double-registering. (Existing chart
 * components rely on `Bar`/`Line` from react-chartjs-2 having already
 * self-registered their controllers as an import side effect, which makes
 * registration order-dependent across files; this registers controllers
 * explicitly so components in this directory don't depend on that.)
 */
export function registerChartJs(): void {
  if (registered) return;
  ChartJS.register(
    CategoryScale,
    LinearScale,
    BarElement,
    BarController,
    LineElement,
    LineController,
    PointElement,
    Filler,
    Tooltip,
    Legend,
    ZoomPlugin,
  );
  registered = true;
}

function resolveCssVar(name: string, fallback: string): string {
  if (typeof document === "undefined") return fallback;
  const value = getComputedStyle(document.documentElement)
    .getPropertyValue(name)
    .trim();
  return value.length > 0 ? value : fallback;
}

// The nine language-identity tokens from base.css, in the order they're
// declared there. These are identifiers, not a sequential ramp — callers
// should not re-sort or re-derive shades from them.
const LANGUAGE_TOKENS: ReadonlyArray<{ name: string; fallback: string }> = [
  { name: "--color-lang-typescript", fallback: "hsl(220, 100%, 12%)" },
  { name: "--color-lang-javascript", fallback: "hsl(215, 69%, 50%)" },
  { name: "--color-lang-python", fallback: "hsl(216, 100%, 80%)" },
  { name: "--color-lang-go", fallback: "hsl(154, 100%, 7%)" },
  { name: "--color-lang-ruby", fallback: "hsl(108, 24%, 41%)" },
  { name: "--color-lang-php", fallback: "hsl(68, 52%, 72%)" },
  { name: "--color-lang-java", fallback: "hsl(334, 54%, 13%)" },
  { name: "--color-lang-csharp", fallback: "hsl(4, 67%, 47%)" },
  { name: "--color-lang-rust", fallback: "hsl(23, 96%, 62%)" },
];

/**
 * The series palette: the language-identity CSS tokens resolved to concrete
 * colors at call time (via `getComputedStyle`) so theme/token changes apply
 * without a rebuild. Falls back to the literal `hsl()` values mirroring
 * base.css when `document` is unavailable (SSR, non-DOM tests).
 */
export function seriesPalette(): string[] {
  return LANGUAGE_TOKENS.map(({ name, fallback }) =>
    resolveCssVar(name, fallback),
  );
}

/**
 * Mixes an alpha channel into a resolved color using CSS relative-color
 * syntax (already used elsewhere in base.css, e.g. `--color-neutral-900-56`),
 * so it works for both `hsl(...)` strings and `var(--token)` references
 * without needing to parse the input color ourselves.
 */
export function withAlpha(color: string, alpha: number): string {
  return `hsl(from ${color} h s l / ${alpha})`;
}

// A fixed dark "ink" surface with "bone" text — the tooltip look holds
// regardless of the app's light/dark theme, matching the existing
// tooltipBg/tooltipTitle/tooltipBody convention in InsightsTools.tsx and
// SecurityOverview.tsx. Sourced from the "fixed dark"/"fixed light" token
// pairs in base.css, which exist for exactly this "always dark chrome"
// case.
const TOOLTIP_BG_VAR = "--bg-surface-secondary-fixed-dark";
const TOOLTIP_BG_FALLBACK = "hsl(23, 36%, 4%)"; // --color-neutral-900 ("ink")
const TOOLTIP_TITLE_VAR = "--text-highlight-fixed-light";
const TOOLTIP_TITLE_FALLBACK = "hsl(0, 0%, 100%)"; // --color-base-white
const TOOLTIP_BODY_VAR = "--text-muted-fixed-light";
const TOOLTIP_BODY_FALLBACK = "hsla(0, 0%, 98%, 0.72)"; // muted "bone"
const TOOLTIP_HAIRLINE = "rgba(255, 255, 255, 0.12)";

const MONO_FONT_FALLBACK =
  "'Diatype Mono', SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', 'Courier New', monospace";

/**
 * The app's mono font stack (`--font-mono` → `--font-diatype-mono`),
 * resolved at call time so canvas text matches the DOM's font without a
 * second hardcoded copy of the stack.
 */
export function monoFontStack(): string {
  return resolveCssVar("--font-mono", MONO_FONT_FALLBACK);
}

/**
 * Shared tooltip styling: dark ink surface, bone title/body text, hairline
 * border, squared corners (no border radius), 12px mono type. Does not
 * include `callbacks` — spread this into a chart's own tooltip options and
 * add callbacks (e.g. `timeTitleFormatter`) alongside it.
 */
export function chartTooltip(): SharedTooltipStyle {
  return {
    enabled: true,
    backgroundColor: resolveCssVar(TOOLTIP_BG_VAR, TOOLTIP_BG_FALLBACK),
    titleColor: resolveCssVar(TOOLTIP_TITLE_VAR, TOOLTIP_TITLE_FALLBACK),
    bodyColor: resolveCssVar(TOOLTIP_BODY_VAR, TOOLTIP_BODY_FALLBACK),
    borderColor: TOOLTIP_HAIRLINE,
    borderWidth: 1,
    cornerRadius: 0,
    padding: 10,
    boxPadding: 4,
    boxWidth: 8,
    boxHeight: 8,
    displayColors: true,
    titleFont: { family: monoFontStack(), size: 12 },
    bodyFont: { family: monoFontStack(), size: 12 },
  };
}

// Faint neutral grid lines: the "softest" border token (already tuned per
// light/dark theme for a subtle hairline) rather than a fixed grey literal.
const GRID_VAR = "--border-neutral-softest";
const GRID_FALLBACK = "hsl(20, 6%, 92%)"; // --color-neutral-200 (light mode)
// Faint tick labels: the "muted" text token, which is already a
// theme-appropriate translucent ink/bone rather than a fixed grey literal.
const TICK_VAR = "--text-muted";
const TICK_FALLBACK = "hsla(23, 36%, 4%, 0.64)"; // --color-neutral-900-64 (light mode)

/** Faint neutral grid lines shared by every axis in the chart system. */
export function chartGrid(): _SharedScale["grid"] {
  return {
    color: resolveCssVar(GRID_VAR, GRID_FALLBACK),
    drawTicks: false,
  };
}

/** The resolved axis tick / canvas-label color, standalone for canvas plugins
 * (e.g. a stack-total label) that draw text outside of `ticks`. */
export function tickColor(): string {
  return resolveCssVar(TICK_VAR, TICK_FALLBACK);
}

/** 11px mono axis tick styling shared by every axis in the chart system. */
export function chartTicks(): _SharedScale["ticks"] {
  return {
    color: tickColor(),
    font: { family: monoFontStack(), size: 11 },
  };
}

/**
 * Tooltip title callback shared by every timeseries chart: formats the
 * hovered point's x value (expected to be a millisecond timestamp on a
 * linear/time x scale) as "Jan 5, 2:00 PM", mirroring the
 * `formatChartZoomRangeLabel` date format already used for the zoom-range
 * label in chartUtils.ts. Typed against `"line"` (rather than the broad
 * `ChartType` union) because Chart.js's `TooltipItem<T>["parsed"]` collapses
 * to an intersection — not a union — when `T` is a multi-member union,
 * which breaks assignability into a concrete `ChartOptions<"line">` tooltip
 * config. `<Timeseries>` always renders via `<Chart type="line" .../>`
 * (bar/stacked-bar datasets override their own `type` per-dataset), so
 * `"line"` covers every real call site.
 */
export function timeTitleFormatter(
  items: readonly TooltipItem<"line">[],
): string {
  const first = items[0];
  if (!first) return "";
  // Chart.js's parsed type collapses to an intersection under a generic
  // chart type; the x member is only statically visible per concrete type.
  const { x } = first.parsed as { x?: unknown };
  if (typeof x !== "number" || !Number.isFinite(x)) return "";
  return new Date(x).toLocaleString([], {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

/**
 * Accent color for derived/aggregate series that aren't part of the
 * identity palette (e.g. the smoothed trend line in "bar-with-trend" mode).
 * Distinct from `seriesPalette()` on purpose — a trend line isn't a series
 * identity, it's an overlay.
 */
export function trendLineColor(): string {
  return resolveCssVar("--color-brand-blue-500", "hsl(214, 69%, 50%)");
}
