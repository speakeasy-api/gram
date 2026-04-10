import { render, screen, waitFor } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";

import type {
  SearchLogsData,
  ObservabilityOverviewData,
} from "@/hooks/useObservability";
import type { TelemetryLogRecord } from "@gram/client/models/components/telemetrylogrecord";
import type { GetObservabilityOverviewResult } from "@gram/client/models/components/getobservabilityoverviewresult";

// Mock the hooks module -- tests control what the hooks return
vi.mock("@/hooks/useObservability", () => ({
  useSearchLogs: vi.fn(),
  useObservabilityOverview: vi.fn(),
}));

// Lazy import so the mocks are installed before module evaluation
const { useSearchLogs, useObservabilityOverview } =
  await import("@/hooks/useObservability");

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeFakeLog(
  overrides: Partial<TelemetryLogRecord> & { id: string },
): TelemetryLogRecord {
  return {
    attributes: {},
    body: overrides.body ?? "search query",
    id: overrides.id,
    observedTimeUnixNano:
      overrides.observedTimeUnixNano ?? "1712000000000000000",
    resourceAttributes: {},
    service: { name: "gram-server" },
    timeUnixNano: overrides.timeUnixNano ?? "1712000000000000000",
    ...overrides,
  };
}

function makeFakeOverview(
  overrides?: Partial<GetObservabilityOverviewResult>,
): GetObservabilityOverviewResult {
  return {
    comparison: {
      avgLatencyMs: 30,
      avgResolutionTimeMs: 500,
      avgSessionDurationMs: 12000,
      failedChats: 1,
      failedToolCalls: 2,
      resolvedChats: 8,
      totalChats: 10,
      totalToolCalls: 50,
    },
    intervalSeconds: 3600,
    summary: {
      avgLatencyMs: 42,
      avgResolutionTimeMs: 600,
      avgSessionDurationMs: 15000,
      failedChats: 2,
      failedToolCalls: 5,
      resolvedChats: 18,
      totalChats: 25,
      totalToolCalls: 120,
    },
    timeSeries: [],
    topToolsByCount: [],
    topToolsByFailureRate: [],
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// Dynamically import the component under test *after* mocks are established
const { ObservabilityTab } = await import("./ObservabilityTab");

function renderObservability() {
  return render(<ObservabilityTab />);
}

describe("ObservabilityTab", () => {
  test("renders search logs table from API", async () => {
    const logs = [
      makeFakeLog({ id: "log-1", body: "system architecture overview" }),
      makeFakeLog({ id: "log-2", body: "competitor comparison DataStream" }),
      makeFakeLog({ id: "log-3", body: "incident response SEV1 procedure" }),
    ];

    vi.mocked(useSearchLogs).mockReturnValue({
      logs,
      nextCursor: undefined,
      isLoading: false,
      error: null,
      search: vi.fn(),
    } satisfies SearchLogsData);

    vi.mocked(useObservabilityOverview).mockReturnValue({
      overview: makeFakeOverview(),
      isLoading: false,
      error: null,
    } satisfies ObservabilityOverviewData);

    renderObservability();

    // Each log body should appear as a row in the table
    await waitFor(() => {
      expect(screen.getByText("system architecture overview")).toBeTruthy();
      expect(screen.getByText("competitor comparison DataStream")).toBeTruthy();
      expect(screen.getByText("incident response SEV1 procedure")).toBeTruthy();
    });
  });

  test("renders latency stats", async () => {
    vi.mocked(useSearchLogs).mockReturnValue({
      logs: [],
      nextCursor: undefined,
      isLoading: false,
      error: null,
      search: vi.fn(),
    } satisfies SearchLogsData);

    const overview = makeFakeOverview({
      summary: {
        avgLatencyMs: 42,
        avgResolutionTimeMs: 600,
        avgSessionDurationMs: 15000,
        failedChats: 2,
        failedToolCalls: 5,
        resolvedChats: 18,
        totalChats: 25,
        totalToolCalls: 120,
      },
    });

    vi.mocked(useObservabilityOverview).mockReturnValue({
      overview,
      isLoading: false,
      error: null,
    } satisfies ObservabilityOverviewData);

    renderObservability();

    // The summary stats cards should display latency information
    await waitFor(() => {
      // Average latency from the overview summary
      expect(screen.getByText("42ms")).toBeTruthy();
      // Total tool calls
      expect(screen.getByText("120")).toBeTruthy();
      // Total chats
      expect(screen.getByText("25")).toBeTruthy();
    });
  });
});
