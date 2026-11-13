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
    tracker.candidates(180, 7, 4, 3);
    tracker.ranking(2.5);
    tracker.ranking(1.5);

    expect(tracker.close(600, "project")).toEqual({
      schema_version: 4,
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
      inventory_call_baseline: 17,
      searched_resource_kind_count: 4,
      net_eliminated_inventory_call_baseline: 3,
      remaining_inventory_call_baseline: 14,
      search_request_count: 0,
      modeled_no_fallback_call_count: 14,
      search_candidate_count: 0,
      search_complete: false,
      search_truncated: false,
      search_payload_byte_bucket: null,
      time_to_search_ready_ms: null,
      parse_search_duration_ms: null,
      fallback_used: false,
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

  it("measures page actions that become available after open", () => {
    const tracker = new CommandPalettePerformanceTracker();
    tracker.open(10, { total: 0, pages: 0, resources: 0 });
    tracker.candidates(35, 2, 2, 0);
    expect(tracker.close(40, "project")?.time_to_page_actions_ms).toBe(25);
  });

  it("records complete search semantics without high-cardinality values", () => {
    const tracker = new CommandPalettePerformanceTracker();
    tracker.open(100, { total: 2, pages: 2, resources: 0 });
    tracker.search(125, {
      state: "ready",
      searchRequestCount: 1,
      candidateCount: 418,
      complete: true,
      truncated: false,
      payloadByteBucket: "50k_to_250k",
      parseSearchDurationMs: 3.5,
    });
    const payload = tracker.close(150, "project");
    expect(payload).toMatchObject({
      inventory_call_baseline: 17,
      searched_resource_kind_count: 4,
      net_eliminated_inventory_call_baseline: 3,
      remaining_inventory_call_baseline: 14,
      search_request_count: 1,
      modeled_no_fallback_call_count: 15,
      search_candidate_count: 418,
      search_complete: true,
      search_truncated: false,
      search_payload_byte_bucket: "50k_to_250k",
      time_to_search_ready_ms: 25,
      parse_search_duration_ms: 3.5,
      fallback_used: false,
    });
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
    const keys = Object.keys(payload);
    for (const forbidden of ["query", "candidate", "name", "id", "route"]) {
      expect(keys).not.toContain(forbidden);
    }
    expect(["organization", "project"]).toContain(payload.scope);
    expect(["cold", "warm"]).toContain(payload.cache_state);
    for (const [key, value] of Object.entries(payload)) {
      if (key === "scope" || key === "cache_state" || value === null) continue;
      expect(["number", "boolean"], key).toContain(typeof value);
    }
  });
});
