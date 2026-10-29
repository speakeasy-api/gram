import { describe, expect, it } from "vitest";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { MemberRow } from "./memberRows";
import type { MetaMcpCallPhases } from "@gram/client/models/components/metamcpcallphases.js";
import type { MetaMcpModePhases } from "@gram/client/models/components/metamcpmodephases.js";
import {
  executionShare,
  memberUsageRows,
  modeShares,
  phaseTimeSeries,
  positionLines,
  totalCalls,
} from "./gatewayActivity";

function row(
  mcpServerId: string,
  server: Partial<McpServer> | undefined,
  memberName?: string,
): MemberRow {
  return {
    member: {
      id: `member-${mcpServerId}`,
      mcpServerId,
      mcpServerName: memberName,
      sortOrder: 0,
    },
    server: server ? ({ id: mcpServerId, ...server } as McpServer) : undefined,
    classification: server ? "hosted" : "unknown",
  };
}

describe("memberUsageRows", () => {
  it("labels members by server name, then member name, then slug, then id", () => {
    const rows = memberUsageRows(
      [
        { mcpServerId: "a", toolCalls: 2, errorCount: 1 },
        { mcpServerId: "b", toolCalls: 1, errorCount: 0 },
        { mcpServerId: "c", toolCalls: 1, errorCount: 0 },
        { mcpServerId: "gone", toolCalls: 5, errorCount: 5 },
      ],
      [
        row("a", { name: "Alpha", slug: "alpha" }),
        row("b", { slug: "beta" }, "Beta member"),
        row("c", { slug: "gamma" }),
      ],
    );
    expect(rows.map((r) => r.label)).toEqual([
      "Alpha",
      "Beta member",
      "gamma",
      "gone",
    ]);
  });

  it("computes the error rate and keeps a zero-call member at 0%", () => {
    const rows = memberUsageRows(
      [
        { mcpServerId: "a", toolCalls: 4, errorCount: 1 },
        { mcpServerId: "b", toolCalls: 0, errorCount: 0 },
      ],
      [],
    );
    expect(rows[0]?.errorRate).toBe(25);
    expect(rows[1]?.errorRate).toBe(0);
  });
});

function phases(overrides: Partial<MetaMcpCallPhases> = {}): MetaMcpCallPhases {
  return {
    modes: [],
    positions: [],
    intervalSeconds: 3600,
    series: [],
    callsWithoutSession: 0,
    ...overrides,
  };
}

function modeRow(
  mode: string,
  discoveryCalls: number,
  executionCalls: number,
  previous: [number, number] = [0, 0],
  executeRuns = 0,
): MetaMcpModePhases {
  return {
    // An open enum: values a newer server may send are still strings.
    mode: mode as MetaMcpModePhases["mode"],
    discoveryCalls,
    executionCalls,
    executeRuns,
    previousDiscoveryCalls: previous[0],
    previousExecutionCalls: previous[1],
    previousExecuteRuns: 0,
  };
}

describe("executionShare", () => {
  it("divides tool calls by every call, and has no share without calls", () => {
    expect(executionShare(3, 1)).toBe(0.25);
    expect(executionShare(0, 0)).toBeNull();
  });
});

describe("modeShares", () => {
  it("orders modes from most discovery to least and keeps the previous share", () => {
    const shares = modeShares(
      phases({
        modes: [
          modeRow("direct", 0, 8),
          modeRow("progressive", 6, 2, [8, 2]),
          modeRow("code_mode", 2, 2, [0, 0], 3),
        ],
      }),
    );
    expect(shares.map((s) => s.mode)).toEqual([
      "progressive",
      "code_mode",
      "direct",
    ]);
    expect(shares[0]).toMatchObject({ share: 0.25, previousShare: 0.2 });
    expect(shares[1]).toMatchObject({
      share: 0.5,
      previousShare: null,
      executeRuns: 3,
    });
    expect(shares[2]).toMatchObject({ share: 1, label: "Direct" });
  });

  it("drops a mode with calls only in the previous period and folds unknown modes into unrecorded", () => {
    const shares = modeShares(
      phases({
        modes: [
          modeRow("progressive", 0, 0, [4, 4]),
          modeRow("future_mode", 0, 2),
          modeRow("unrecorded", 0, 1),
        ],
      }),
    );
    expect(shares).toHaveLength(1);
    expect(shares[0]).toMatchObject({
      mode: "unrecorded",
      label: "Mode not recorded",
      executionCalls: 3,
    });
  });
});

describe("positionLines", () => {
  it("lays each mode's share on the position axis, with gaps where no call landed", () => {
    const lines = positionLines(
      phases({
        positions: [
          {
            mode: "direct",
            position: "1",
            discoveryCalls: 0,
            executionCalls: 4,
          },
          {
            mode: "progressive",
            position: "1",
            discoveryCalls: 4,
            executionCalls: 0,
          },
          {
            mode: "progressive",
            position: "3-5",
            discoveryCalls: 1,
            executionCalls: 3,
          },
        ],
      }),
    );
    expect(lines.map((l) => l.mode)).toEqual(["progressive", "direct"]);
    expect(lines[0]?.shares).toEqual([0, null, 0.75, null, null, null]);
    expect(lines[0]?.executionCalls).toEqual([0, 0, 3, 0, 0, 0]);
    expect(lines[1]?.shares).toEqual([1, null, null, null, null, null]);
  });
});

describe("phaseTimeSeries", () => {
  const from = new Date("2026-01-01T00:00:00Z");
  const to = new Date("2026-01-01T03:00:00Z");
  const hour = (h: number) =>
    String(BigInt(from.getTime() + h * 3_600_000) * 1_000_000n);
  const data = phases({
    series: [
      {
        bucketTimeUnixNano: hour(0),
        mode: "progressive",
        discoveryCalls: 3,
        executionCalls: 1,
        executeRuns: 0,
      },
      {
        bucketTimeUnixNano: hour(0),
        mode: "code_mode",
        discoveryCalls: 1,
        executionCalls: 2,
        executeRuns: 2,
      },
      {
        bucketTimeUnixNano: hour(2),
        mode: "direct",
        discoveryCalls: 0,
        executionCalls: 5,
        executeRuns: 0,
      },
    ],
  });

  it("zero-fills every bucket from the range start and sums the modes", () => {
    const series = phaseTimeSeries(data, from, to, "all");
    expect(series.timestamps).toEqual([
      from.getTime(),
      from.getTime() + 3_600_000,
      from.getTime() + 7_200_000,
    ]);
    expect(series.discoveryCalls).toEqual([4, 0, 0]);
    expect(series.executionCalls).toEqual([3, 0, 5]);
    expect(series.executeRuns).toEqual([2, 0, 0]);
  });

  it("keeps one mode's calls when filtered to it", () => {
    const series = phaseTimeSeries(data, from, to, "code_mode");
    expect(series.discoveryCalls).toEqual([1, 0, 0]);
    expect(series.executionCalls).toEqual([2, 0, 0]);
  });

  it("gives a partial last bucket its own slot", () => {
    const series = phaseTimeSeries(
      data,
      from,
      new Date(to.getTime() + 60_000),
      "all",
    );
    expect(series.timestamps).toHaveLength(4);
  });
});

describe("totalCalls", () => {
  it("counts lookups, tool calls, and code runs", () => {
    expect(
      totalCalls(phases({ modes: [modeRow("code_mode", 1, 2, [9, 9], 3)] })),
    ).toBe(6);
  });
});
