import { describe, expect, it } from "vitest";
import {
  formatCadence,
  runtimeOrDefault,
} from "./use-device-integration-schedules";

describe("formatCadence", () => {
  it("renders whole hours as hour labels", () => {
    expect(formatCadence(60)).toBe("Every hour");
    expect(formatCadence(120)).toBe("Every 2h");
    expect(formatCadence(1440)).toBe("Every 24h");
  });

  it("renders sub-hour cadences in minutes", () => {
    expect(formatCadence(30)).toBe("Every 30m");
    expect(formatCadence(90)).toBe("Every 90m");
  });
});

describe("runtimeOrDefault", () => {
  it("falls back to an enabled pending runtime for unknown schedules", () => {
    const runtime = runtimeOrDefault({}, "never_synced");
    expect(runtime.enabled).toBe(true);
    expect(runtime.status).toBe("pending");
    expect(runtime.lastSyncedAt).toBeNull();
  });

  it("returns the live runtime when present", () => {
    const live = {
      enabled: false,
      status: "failed" as const,
      lastSyncedAt: null,
      error: "boom",
      isMutating: false,
    };
    expect(runtimeOrDefault({ jamf_inventory: live }, "jamf_inventory")).toBe(
      live,
    );
  });
});
