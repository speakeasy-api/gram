import { ChartNoData } from "@/components/chart/ChartNoData";
import {
  formatChartLabel,
  formatChartZoomRangeLabel,
} from "@/components/chart/chartUtils";
import { StackedTimeBarChart } from "@/components/chart/StackedTimeBarChart";
import { useMemo } from "react";
import { signalBucketStarts } from "./signals-helpers";

const DEFAULT_HEIGHT = 160;

/**
 * Findings per time bucket for one signal across the page's window, so the
 * reader can tell a new burst from a steady background rate. Plots the same
 * sparkline the signal row draws, with real time on the axis.
 */
export function SignalTimeline({
  sparkline,
  from,
  to,
  color,
  height = DEFAULT_HEIGHT,
}: {
  sparkline: number[];
  from: Date;
  to: Date;
  color: string;
  height?: number;
}): JSX.Element {
  const chart = useMemo(() => {
    const { starts, bucketMs } = signalBucketStarts(from, to, sparkline.length);
    const windowMs = to.getTime() - from.getTime();
    return {
      bucketMs,
      timestamps: starts,
      labels: starts.map((start) =>
        formatChartLabel(new Date(start), windowMs),
      ),
      tooltipLabels: starts.map((start) =>
        formatChartZoomRangeLabel(new Date(start), new Date(start + bucketMs)),
      ),
      datasets: [
        {
          label: "Findings",
          data: sparkline,
          backgroundColor: color,
          hoverBackgroundColor: color,
        },
      ],
    };
  }, [sparkline, from, to, color]);

  if (sparkline.every((count) => count === 0)) {
    return (
      <div
        className="flex items-center justify-center"
        style={{ height: height }}
      >
        <ChartNoData message="No findings in this period" />
      </div>
    );
  }

  return (
    <StackedTimeBarChart
      labels={chart.labels}
      timestamps={chart.timestamps}
      bucketMs={chart.bucketMs}
      tooltipLabels={chart.tooltipLabels}
      datasets={chart.datasets}
      height={height}
    />
  );
}
