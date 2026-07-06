import { act, renderHook } from "@testing-library/react";
import type { Chart } from "chart.js";
import type { ZoomPluginOptions } from "chartjs-plugin-zoom/types/options";
import { describe, expect, it, vi } from "vitest";
import { useChartZoom } from "./useChartZoom";

function fireZoomComplete(
  zoomPluginOptions: ZoomPluginOptions,
  min: number,
  max: number,
) {
  const onZoomComplete = zoomPluginOptions.zoom?.onZoomComplete;
  if (!onZoomComplete) {
    throw new Error("Expected zoom complete callback to be configured");
  }

  onZoomComplete({
    chart: {
      scales: {
        x: { min, max },
      },
    } as unknown as Chart,
  });
}

describe("useChartZoom", () => {
  it("maps the selected x-axis range to dates", () => {
    const onRangeSelect = vi.fn();
    const { result } = renderHook(() =>
      useChartZoom({
        onRangeSelect: (from, to) => {
          onRangeSelect(from, to);
        },
      }),
    );
    const from = Date.UTC(2026, 0, 1, 12, 0);
    const to = Date.UTC(2026, 0, 2, 12, 0);

    act(() => {
      fireZoomComplete(result.current.zoomPluginOptions, from, to);
    });

    expect(onRangeSelect).toHaveBeenCalledWith(new Date(from), new Date(to));
  });

  it("does not configure in-progress range updates", () => {
    const onRangeSelect = vi.fn();
    const { result } = renderHook(() =>
      useChartZoom({
        onRangeSelect: (from, to) => {
          onRangeSelect(from, to);
        },
      }),
    );

    expect(result.current.zoomPluginOptions.zoom?.onZoom).toBeUndefined();
    expect(onRangeSelect).not.toHaveBeenCalled();
  });

  it("ignores zoom completion events caused by resetZoom", () => {
    const onRangeSelect = vi.fn();
    const { result } = renderHook(() =>
      useChartZoom({
        onRangeSelect: (from, to) => {
          onRangeSelect(from, to);
        },
      }),
    );
    const chart = {
      scales: {
        x: {
          min: Date.UTC(2026, 0, 1, 12, 0),
          max: Date.UTC(2026, 0, 2, 12, 0),
        },
      },
      resetZoom: vi.fn(() => {
        result.current.zoomPluginOptions.zoom?.onZoomComplete?.({
          chart: chart as unknown as Chart,
        });
      }),
    };
    (result.current.chartRef as unknown as { current: Chart | null }).current =
      chart as unknown as Chart;

    act(() => {
      result.current.resetZoom();
    });

    expect(chart.resetZoom).toHaveBeenCalled();
    expect(onRangeSelect).not.toHaveBeenCalled();
  });

  it("does not configure drag selection when no range callback is provided", () => {
    const { result } = renderHook(() => useChartZoom({}));

    expect(result.current.zoomPluginOptions.zoom?.drag?.enabled).toBe(false);
  });

  it("ignores non-numeric chart scale ranges", () => {
    const onRangeSelect = vi.fn();
    const { result } = renderHook(() =>
      useChartZoom({
        onRangeSelect: (from, to) => {
          onRangeSelect(from, to);
        },
      }),
    );
    const onZoomComplete =
      result.current.zoomPluginOptions.zoom?.onZoomComplete;
    if (!onZoomComplete) {
      throw new Error("Expected zoom complete callback to be configured");
    }

    act(() => {
      onZoomComplete({
        chart: {
          scales: {
            x: { min: "start", max: Date.UTC(2026, 0, 2) },
          },
        } as unknown as Chart,
      });
    });

    expect(onRangeSelect).not.toHaveBeenCalled();
  });

  it("uses a custom resolver for non-date scale ranges", () => {
    const onRangeSelect = vi.fn();
    const first = new Date(Date.UTC(2026, 0, 1, 12, 0));
    const second = new Date(Date.UTC(2026, 0, 1, 13, 0));
    const { result } = renderHook(() =>
      useChartZoom({
        onRangeSelect: (from, to) => {
          onRangeSelect(from, to);
        },
        resolveRange: (min, max) => {
          expect(min).toBe(0);
          expect(max).toBe(1);
          return { from: first, to: second };
        },
      }),
    );

    act(() => {
      fireZoomComplete(result.current.zoomPluginOptions, 0, 1);
    });

    expect(onRangeSelect).toHaveBeenCalledWith(first, second);
  });
});
