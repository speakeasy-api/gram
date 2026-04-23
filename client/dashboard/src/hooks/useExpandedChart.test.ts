import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { useExpandedChart } from "./useExpandedChart";

describe("useExpandedChart", () => {
  it("initializes with no chart expanded", () => {
    const { result } = renderHook(() => useExpandedChart());
    expect(result.current.expandedChart).toBeNull();
  });

  it("expands a chart when setExpandedChart is called", () => {
    const { result } = renderHook(() => useExpandedChart());
    act(() => {
      result.current.setExpandedChart("server-usage");
    });
    expect(result.current.expandedChart).toBe("server-usage");
  });

  it("collapses the chart when Escape is pressed", () => {
    const { result } = renderHook(() => useExpandedChart());
    act(() => {
      result.current.setExpandedChart("server-usage");
    });
    act(() => {
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    });
    expect(result.current.expandedChart).toBeNull();
  });

  it("does not collapse when the Escape event was already handled (e.g. a Radix dialog)", () => {
    const { result } = renderHook(() => useExpandedChart());
    act(() => {
      result.current.setExpandedChart("server-usage");
    });
    act(() => {
      const event = new KeyboardEvent("keydown", {
        key: "Escape",
        cancelable: true,
      });
      event.preventDefault();
      window.dispatchEvent(event);
    });
    expect(result.current.expandedChart).toBe("server-usage");
  });

  it("ignores Escape when no chart is expanded", () => {
    const { result } = renderHook(() => useExpandedChart());
    act(() => {
      window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape" }));
    });
    expect(result.current.expandedChart).toBeNull();
  });
});
