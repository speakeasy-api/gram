import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  useDeviceScheduleRuntimes,
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

const query = vi.hoisted(() => ({ useSchedules: vi.fn() }));
vi.mock("@gram/client/react-query/deviceIntegrationSchedules.js", () => ({
  useDeviceIntegrationSchedules: query.useSchedules,
}));
vi.mock("@gram/client/react-query/retryDeviceIntegrationSchedule.js", () => ({
  useRetryDeviceIntegrationScheduleMutation: () => ({ mutate: vi.fn() }),
}));
vi.mock(
  "@gram/client/react-query/setDeviceIntegrationScheduleEnabled.js",
  () => ({
    useSetDeviceIntegrationScheduleEnabledMutation: () => ({ mutate: vi.fn() }),
  }),
);
vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({}) }));
afterEach(cleanup);

describe("schedule status compatibility", () => {
  it("keeps future statuses unknown while known schedules remain usable", () => {
    query.useSchedules.mockReturnValue({
      data: {
        schedules: [
          { schedule: "future", status: "future_status", enabled: true },
          { schedule: "current", status: "success", enabled: true },
        ],
      },
      isLoading: false,
    });
    const { result } = renderHook(() =>
      useDeviceScheduleRuntimes("test_provider"),
    );
    expect(result.current.runtimes.future?.status).toBe("unknown");
    expect(result.current.runtimes.current?.status).toBe("success");
  });
});
