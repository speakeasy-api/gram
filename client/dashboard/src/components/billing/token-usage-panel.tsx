import { type TumDetailsPoint } from "@gram/client/models/components/tumdetailspoint.js";
import { type ReactNode, useMemo } from "react";
import { unixNanoToMs } from "@/components/chart/chartUtils";
import { type TimeSeriesStack } from "@/components/stacked-time-series";
import { StackedTimeSeriesPanel } from "@/components/stacked-time-series-panel";
import { type StackMode } from "./breakdown-options";

// The billing page's tokens-under-management breakdown: the shared stacked
// time-series panel fed with daily token buckets, stacked by a chosen
// dimension or by token type. Everything renders from the billing details
// response (points + per-dimension rows, both scoped server-side to the
// observed agent traffic, cache reads excluded) — plus, for the headline
// total, the billed per-day series the usage endpoint already returns. On a
// cycle finalized before a billing-definition change, that sealed series is
// the invoiced record and can differ from the live details data, so switching
// from the total view to a token-type or dimension stacking can change the
// displayed sum — deliberate: the total view shows what was billed, the
// breakdowns show the current population's shape.

// The tokens-under-management token types: input + output + cache writes
// sum to the total. Cache READS are excluded from the population entirely
// (a cache read re-observes already-counted prompt content), so they are
// not a series here.
const TOKEN_TYPES: {
  label: string;
  value: (p: TumDetailsPoint) => number;
}[] = [
  { label: "Input", value: (p) => p.inputTokens },
  { label: "Output", value: (p) => p.outputTokens },
  { label: "Cache write", value: (p) => p.cacheCreationTokens },
];

const compactTokens = new Intl.NumberFormat("en-US", {
  notation: "compact",
  maximumFractionDigits: 1,
});

// Module-scope so the panel receives stable identities — its chartOptions
// memo keys on the formatter props.
function formatTokens(value: number): string {
  return `${value.toLocaleString()} tokens`;
}
function formatTokensAxis(value: number): string {
  return compactTokens.format(value);
}

// A daily series from the details response's dimension rows, aligned to the
// points grid by index.
export type GroupSeries = { label: string; series: number[] };

// The header info copy for the panel.
const HEADER_HINT =
  "Tokens under management — the agent traffic the platform observes from your users' sessions (including Claude Code, Cowork, Cursor, and Codex): input, output, and cache-write tokens. Cache reads are excluded (re-read cached content isn't new traffic), and so is inference the platform itself runs (risk-policy analysis, hosted chat).";

// The daily stack series for the selected mode, aligned to the points grid.
// The panel drops all-zero stacks, so modes whose series don't apply (e.g.
// cache writes on billed completions) fall out naturally.
function dailyStacks(
  mode: StackMode,
  points: TumDetailsPoint[],
  groups: GroupSeries[],
  billedSeries: number[] | null,
): TimeSeriesStack[] {
  switch (mode) {
    case "total":
      // Prefers the BILLED per-day series (the exact numbers on the usage
      // card) when the caller has it; the details totals stand in otherwise.
      return [
        {
          label: "Total tokens",
          series: points.map((p, i) =>
            billedSeries ? (billedSeries[i] ?? 0) : p.totalTokens,
          ),
        },
      ];
    case "tokenType":
      return TOKEN_TYPES.map((t) => ({
        label: t.label,
        series: points.map(t.value),
      }));
    case "group":
      // The server already ranks rows and rolls the remainder into "Other",
      // so the rows map straight to stacks.
      return groups;
  }
}

export function TokenUsagePanel({
  points,
  groups,
  billedSeries,
  stackBy,
  breakdownPicker,
  loading,
  onSelectRange,
}: {
  // Gap-filled daily buckets from the billing details response — the axis
  // grid plus the total/token-type measures.
  points: TumDetailsPoint[];
  // The selected dimension's rows, with daily series aligned to points.
  groups: GroupSeries[];
  // The billed per-day token series aligned to points, when the caller has
  // it (org scope with known cycle days); the total view plots it so the
  // chart matches the usage card exactly.
  billedSeries: number[] | null;
  // How the bars stack — controlled by the caller's breakdown picker.
  stackBy: StackMode;
  // The unified breakdown selector (dimensions + token type), rendered at
  // the head of the control row.
  breakdownPicker: ReactNode;
  loading: boolean;
  // Called when a bar is clicked with the bucket's time range — the caller
  // narrows the page's period to it (drill-down). Bars aren't clickable
  // without it.
  onSelectRange?: (start: Date, end: Date) => void;
}): JSX.Element {
  const bucketsMs = useMemo(
    () => points.map((p) => unixNanoToMs(p.bucketTimeUnixNano)),
    [points],
  );
  const stacks = useMemo(
    () => dailyStacks(stackBy, points, groups, billedSeries),
    [stackBy, points, groups, billedSeries],
  );

  return (
    <StackedTimeSeriesPanel
      title="Token Usage Time Series"
      headerHint={HEADER_HINT}
      bucketsMs={bucketsMs}
      stacks={stacks}
      headerControls={breakdownPicker}
      formatValue={formatTokens}
      formatAxisValue={formatTokensAxis}
      emptyMessage="No token usage in this range."
      loading={loading}
      onSelectRange={onSelectRange}
    />
  );
}
