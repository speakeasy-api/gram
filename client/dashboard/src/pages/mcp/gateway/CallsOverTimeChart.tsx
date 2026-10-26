import { ChartCard } from "@/components/chart/ChartCard";
import { StackedTimeBarChart } from "@/components/chart/StackedTimeBarChart";
import { WidgetEmptyState } from "@/components/chart/WidgetEmptyState";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { useIsDarkTheme } from "@/lib/theme";
import type { MetaMcpCallPhases } from "@gram/client/models/components/metamcpcallphases.js";
import { useMemo, useState } from "react";
import {
  GATEWAY_MODE_DESCRIPTIONS,
  phaseTimeSeries,
  type GatewayMode,
  type ModeShare,
} from "./gatewayActivity";
import { gatewayModeColor, gatewayPhaseColors } from "./gatewayActivityColors";
import { SeriesLegend } from "./SeriesLegend";

const CHART_ID = "gateway-calls-over-time";

// Segment labels are short so every mode fits one track at narrow widths.
const SEGMENT_LABELS: Record<GatewayMode, string> = {
  progressive: "Progressive",
  code_mode: "Code Mode",
  direct: "Direct",
  unrecorded: "Unrecorded",
};

/**
 * Lookups and tool calls per bucket, stacked, for every mode or one. Code
 * runs hold calls of both kinds, so they are counted in the tooltip only.
 */
export function CallsOverTimeChart({
  phases,
  modes,
  from,
  to,
  titleHref,
  loading,
  expandedChart,
  onExpand,
  onRangeSelect,
  isZoomed,
  onResetZoom,
}: {
  phases: MetaMcpCallPhases | undefined;
  /** The modes with calls in the range, which the filter offers. */
  modes: ModeShare[];
  from: Date;
  to: Date;
  titleHref?: string;
  loading: boolean;
  expandedChart: string | null;
  onExpand: (id: string | null) => void;
  onRangeSelect: (from: Date, to: Date) => void;
  isZoomed: boolean;
  onResetZoom: () => void;
}): JSX.Element {
  const isDark = useIsDarkTheme();
  const colors = gatewayPhaseColors(isDark);
  const isExpanded = expandedChart === CHART_ID;
  const [selected, setSelected] = useState<GatewayMode | "all">("all");
  // A mode the new range no longer has falls back to every mode.
  const mode =
    selected !== "all" && modes.some((m) => m.mode === selected)
      ? selected
      : "all";

  const series = useMemo(
    () => (phases ? phaseTimeSeries(phases, from, to, mode) : undefined),
    [phases, from, to, mode],
  );
  const hasData =
    series !== undefined &&
    series.discoveryCalls.some((n, i) => n + series.executionCalls[i]! > 0);

  const datasets = useMemo(
    () =>
      series
        ? [
            {
              label: "Discovery",
              data: series.discoveryCalls,
              backgroundColor: colors.discovery,
              hoverBackgroundColor: colors.discovery,
            },
            {
              label: "Tool calls",
              data: series.executionCalls,
              backgroundColor: colors.execution,
              hoverBackgroundColor: colors.execution,
            },
          ]
        : [],
    [series, colors.discovery, colors.execution],
  );

  const runsLine = (index: number): string[] => {
    const runs = series?.executeRuns[index] ?? 0;
    return runs > 0 ? [`Code runs: ${runs.toLocaleString()}`] : [];
  };

  return (
    <ChartCard
      title="Calls over time"
      titleHref={titleHref}
      chartId={CHART_ID}
      expandedChart={expandedChart}
      onExpand={onExpand}
      hasData={hasData}
      loading={loading}
      isZoomed={isZoomed}
      onResetZoom={onResetZoom}
    >
      <div className="flex flex-col gap-3">
        {modes.length > 1 && (
          <div className="max-w-full overflow-x-auto">
            <SegmentedControl
              ariaLabel="Discovery mode"
              value={mode}
              onChange={setSelected}
              options={[
                {
                  value: "all" as const,
                  label: "All",
                  tooltip: "Every mode together",
                },
                ...modes.map((m) => ({
                  value: m.mode,
                  label: (
                    <span className="inline-flex items-center gap-1.5 whitespace-nowrap">
                      <span
                        aria-hidden="true"
                        className="inline-block size-2 shrink-0"
                        style={{
                          backgroundColor: gatewayModeColor(m.mode, isDark),
                        }}
                      />
                      {SEGMENT_LABELS[m.mode]}
                    </span>
                  ),
                  tooltip: GATEWAY_MODE_DESCRIPTIONS[m.mode],
                })),
              ]}
            />
          </div>
        )}
        {hasData && series ? (
          <>
            <StackedTimeBarChart
              labels={series.labels}
              timestamps={series.timestamps}
              bucketMs={series.bucketMs}
              tooltipLabels={series.tooltipLabels}
              datasets={datasets}
              tooltipAfterBody={runsLine}
              onRangeSelect={onRangeSelect}
              height={isExpanded ? 420 : 240}
              expanded={isExpanded}
            />
            <SeriesLegend
              items={[
                { label: "Discovery", color: colors.discovery },
                { label: "Tool calls", color: colors.execution },
              ]}
            />
          </>
        ) : (
          <WidgetEmptyState
            message="No discovery or tool calls in the selected range"
            className="h-[240px]"
          />
        )}
      </div>
    </ChartCard>
  );
}
