import { useCallback, useMemo, useRef, type RefObject } from "react";
import type { Chart as ChartJS, ChartType, DefaultDataPoint } from "chart.js";
import type { ZoomPluginOptions } from "chartjs-plugin-zoom/types/options";

type UseChartZoomResult<TType extends ChartType, TData, TLabel> = {
  chartRef: RefObject<ChartJS<TType, TData, TLabel> | null>;
  zoomPluginOptions: ZoomPluginOptions;
  resetZoom: () => void;
};

export function useChartZoom<
  TType extends ChartType = "line",
  TData = DefaultDataPoint<TType>,
  TLabel = unknown,
>({
  onRangeSelect,
  resolveRange,
}: {
  onRangeSelect?: (from: Date, to: Date) => void;
  resolveRange?: (min: number, max: number) => { from: Date; to: Date } | null;
}): UseChartZoomResult<TType, TData, TLabel> {
  const chartRef = useRef<ChartJS<TType, TData, TLabel>>(null);
  const isResettingRef = useRef(false);

  const resetZoom = useCallback(() => {
    const chart = chartRef.current;
    if (!chart) return;

    isResettingRef.current = true;
    chart.resetZoom();
    queueMicrotask(() => {
      isResettingRef.current = false;
    });
  }, []);

  const zoomPluginOptions = useMemo<ZoomPluginOptions>(
    () => ({
      zoom: {
        drag: {
          enabled: !!onRangeSelect,
          backgroundColor: "rgba(59, 130, 246, 0.15)",
          borderColor: "rgba(59, 130, 246, 0.4)",
          borderWidth: 1,
        },
        mode: "x",
        onZoomComplete({ chart }) {
          if (isResettingRef.current) return;
          if (!onRangeSelect || !chart.scales.x) return;
          const { min, max } = chart.scales.x;
          if (typeof min !== "number" || typeof max !== "number") return;
          const range = resolveRange
            ? resolveRange(min, max)
            : {
                from: new Date(min),
                to: new Date(max),
              };
          if (!range) return;
          onRangeSelect(range.from, range.to);
        },
      },
    }),
    [onRangeSelect, resolveRange],
  );

  return { chartRef, zoomPluginOptions, resetZoom };
}
