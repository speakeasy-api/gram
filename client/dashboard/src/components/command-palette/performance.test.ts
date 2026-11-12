import { describe, expect, it, vi } from "vitest";
import {
  captureCommandPalettePerformance,
  CommandPalettePerformanceTracker,
  COMMAND_PALETTE_PERFORMANCE_EVENT,
} from "./performance";

describe("CommandPalettePerformanceTracker", () => {
  it("emits exactly one payload for an open and classifies cold timings", () => {
    const tracker = new CommandPalettePerformanceTracker();
    tracker.open(100, { total: 4, pages: 4, resources: 0 });
    tracker.open(101, { total: 4, pages: 4, resources: 0 });
    tracker.candidates(180, 7, 3);
    tracker.ranking(2.5);
    tracker.ranking(1.5);

    expect(tracker.close(600, "project")).toEqual({
      schema_version: 1,
      scope: "project",
      cache_state: "cold",
      candidate_count_at_open: 4,
      candidate_count_final: 7,
      page_action_count_at_open: 4,
      time_to_page_actions_ms: 0,
      time_to_first_resource_ms: 80,
      time_to_resource_quiet_ms: 380,
      resource_quiet_before_close: true,
      open_duration_ms: 500,
      ranking_invocation_count: 2,
      ranking_duration_ms: 4,
      ranking_max_duration_ms: 2.5,
    });
    expect(tracker.close(700, "project")).toBeNull();
  });

  it("classifies synchronously cached resources as warm", () => {
    const tracker = new CommandPalettePerformanceTracker();
    tracker.open(10, { total: 9, pages: 3, resources: 6 });
    const payload = tracker.close(20, "organization");
    expect(payload?.cache_state).toBe("warm");
    expect(payload?.time_to_first_resource_ms).toBe(0);
    expect(payload?.resource_quiet_before_close).toBe(false);
  });

  it("captures a fixed privacy-safe payload without query or candidate fields", () => {
    const capture = vi.fn();
    const tracker = new CommandPalettePerformanceTracker();
    tracker.open(0, { total: 1, pages: 1, resources: 0 });
    const payload = tracker.close(1, "project");
    if (!payload) throw new Error("expected payload");
    captureCommandPalettePerformance({ capture } as never, payload);
    expect(capture).toHaveBeenCalledOnce();
    expect(capture).toHaveBeenCalledWith(
      COMMAND_PALETTE_PERFORMANCE_EVENT,
      payload,
    );
    expect(Object.keys(payload)).not.toEqual(
      expect.arrayContaining(["query", "candidate", "name", "id", "route"]),
    );
  });
});
