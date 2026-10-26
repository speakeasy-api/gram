import { formatChartLabel, unixNanoToMs } from "@/components/chart/chartUtils";
import type { MetaMcpCallPhases } from "@gram/client/models/components/metamcpcallphases.js";
import type { MetaMcpMemberUsage } from "@gram/client/models/components/metamcpmemberusage.js";
import type { MetaMcpModePhases } from "@gram/client/models/components/metamcpmodephases.js";
import type { MemberRow } from "./memberRows";

// The modes a gateway connection can be served in, from the most discovery to
// the least, then calls recorded before connections recorded their mode. A
// mode keeps its slot, and so its color, whichever modes a range contains.
export const GATEWAY_MODES = [
  "progressive",
  "code_mode",
  "direct",
  "unrecorded",
] as const;
export type GatewayMode = (typeof GATEWAY_MODES)[number];

export const GATEWAY_MODE_LABELS: Record<GatewayMode, string> = {
  progressive: "Progressive",
  code_mode: "Code Mode",
  direct: "Direct",
  unrecorded: "Mode not recorded",
};

export const GATEWAY_MODE_DESCRIPTIONS: Record<GatewayMode, string> = {
  progressive:
    "Agents list servers and read tool schemas through the gateway before calling a tool.",
  code_mode:
    "Agents write code that searches for tools and calls them. Each code run is counted apart from the calls inside it.",
  direct: "Agents see every tool up front and call them without a lookup.",
  unrecorded:
    "Tool calls made before the gateway recorded each connection's mode.",
};

function gatewayMode(value: string): GatewayMode {
  return (GATEWAY_MODES as readonly string[]).includes(value)
    ? (value as GatewayMode)
    : "unrecorded";
}

// Where a call falls in its session, counting the session's lookups and tool
// calls from its first.
export const CALL_POSITIONS = ["1", "2", "3-5", "6-10", "11-20", "21+"];
export const CALL_POSITION_LABELS: Record<string, string> = {
  "1": "1",
  "2": "2",
  "3-5": "3–5",
  "6-10": "6–10",
  "11-20": "11–20",
  "21+": "21+",
};

/** The share of calls that were tool calls, or null with no calls to divide. */
export function executionShare(
  discoveryCalls: number,
  executionCalls: number,
): number | null {
  const total = discoveryCalls + executionCalls;
  return total > 0 ? executionCalls / total : null;
}

export interface ModeShare {
  mode: GatewayMode;
  label: string;
  discoveryCalls: number;
  executionCalls: number;
  executeRuns: number;
  /** Tool calls over lookups plus tool calls, 0–1; null with neither. */
  share: number | null;
  /** The same share for the window of equal length before; null with no calls then. */
  previousShare: number | null;
}

/** One entry per mode with calls in the range, in mode order. */
export function modeShares(phases: MetaMcpCallPhases): ModeShare[] {
  const byMode = new Map<GatewayMode, MetaMcpModePhases>();
  for (const row of phases.modes) {
    const mode = gatewayMode(row.mode);
    const entry = byMode.get(mode);
    if (!entry) {
      byMode.set(mode, { ...row, mode });
      continue;
    }
    entry.discoveryCalls += row.discoveryCalls;
    entry.executionCalls += row.executionCalls;
    entry.executeRuns += row.executeRuns;
    entry.previousDiscoveryCalls += row.previousDiscoveryCalls;
    entry.previousExecutionCalls += row.previousExecutionCalls;
  }
  return GATEWAY_MODES.flatMap((mode) => {
    const row = byMode.get(mode);
    if (
      !row ||
      row.discoveryCalls + row.executionCalls + row.executeRuns === 0
    ) {
      return [];
    }
    return [
      {
        mode,
        label: GATEWAY_MODE_LABELS[mode],
        discoveryCalls: row.discoveryCalls,
        executionCalls: row.executionCalls,
        executeRuns: row.executeRuns,
        share: executionShare(row.discoveryCalls, row.executionCalls),
        previousShare: executionShare(
          row.previousDiscoveryCalls,
          row.previousExecutionCalls,
        ),
      },
    ];
  });
}

export interface PositionLine {
  mode: GatewayMode;
  label: string;
  /** Tool-call share at each of CALL_POSITIONS, null where no calls landed. */
  shares: (number | null)[];
  discoveryCalls: number[];
  executionCalls: number[];
}

/** One line per mode with session-ordered calls, in mode order. */
export function positionLines(phases: MetaMcpCallPhases): PositionLine[] {
  const lines = new Map<GatewayMode, PositionLine>();
  for (const row of phases.positions) {
    const index = CALL_POSITIONS.indexOf(row.position);
    if (index < 0) continue;
    const mode = gatewayMode(row.mode);
    const line = lines.get(mode) ?? {
      mode,
      label: GATEWAY_MODE_LABELS[mode],
      shares: [],
      discoveryCalls: CALL_POSITIONS.map(() => 0),
      executionCalls: CALL_POSITIONS.map(() => 0),
    };
    line.discoveryCalls[index]! += row.discoveryCalls;
    line.executionCalls[index]! += row.executionCalls;
    lines.set(mode, line);
  }
  return GATEWAY_MODES.flatMap((mode) => {
    const line = lines.get(mode);
    if (!line) return [];
    line.shares = CALL_POSITIONS.map((_, i) =>
      executionShare(line.discoveryCalls[i]!, line.executionCalls[i]!),
    );
    return [line];
  });
}

const DAY_MS = 24 * 60 * 60 * 1000;

export interface PhaseTimeSeries {
  bucketMs: number;
  timestamps: number[];
  labels: string[];
  tooltipLabels: string[];
  discoveryCalls: number[];
  executionCalls: number[];
  executeRuns: number[];
}

/**
 * Lookups and tool calls per bucket across the whole range, zero-filled, for
 * one mode or all of them. Buckets start at `from` and step by the interval
 * the server chose, the last one holding the range's end.
 */
export function phaseTimeSeries(
  phases: MetaMcpCallPhases,
  from: Date,
  to: Date,
  mode: GatewayMode | "all",
): PhaseTimeSeries {
  const bucketMs = Math.max(phases.intervalSeconds, 1) * 1000;
  const fromMs = from.getTime();
  const rangeMs = Math.max(to.getTime() - fromMs, 0);
  const count = Math.floor(Math.max(rangeMs - 1, 0) / bucketMs) + 1;
  const timestamps = Array.from(
    { length: count },
    (_, i) => fromMs + i * bucketMs,
  );
  const discoveryCalls = timestamps.map(() => 0);
  const executionCalls = timestamps.map(() => 0);
  const executeRuns = timestamps.map(() => 0);
  for (const bucket of phases.series) {
    if (mode !== "all" && gatewayMode(bucket.mode) !== mode) continue;
    const index = Math.round(
      (unixNanoToMs(bucket.bucketTimeUnixNano) - fromMs) / bucketMs,
    );
    if (index < 0 || index >= count) continue;
    discoveryCalls[index]! += bucket.discoveryCalls;
    executionCalls[index]! += bucket.executionCalls;
    executeRuns[index]! += bucket.executeRuns;
  }
  // Buckets start at the range start, not at midnight, so a tooltip names
  // both ends rather than a day the bucket only partly covers.
  const format = (t: number) =>
    new Date(t).toLocaleString([], {
      month: "short",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
    });
  return {
    bucketMs,
    timestamps,
    // A day-wide bucket starting mid-afternoon is mostly the next day, so a
    // day bucket is labeled by its middle.
    labels: timestamps.map((t) =>
      formatChartLabel(
        new Date(bucketMs >= DAY_MS ? t + bucketMs / 2 : t),
        rangeMs,
      ),
    ),
    tooltipLabels: timestamps.map(
      (t) => `${format(t)} – ${format(Math.min(t + bucketMs, to.getTime()))}`,
    ),
    discoveryCalls,
    executionCalls,
    executeRuns,
  };
}

/** Every lookup, tool call, and code run in the range, across modes. */
export function totalCalls(phases: MetaMcpCallPhases): number {
  return phases.modes.reduce(
    (sum, row) =>
      sum + row.discoveryCalls + row.executionCalls + row.executeRuns,
    0,
  );
}

export interface MemberUsageRow {
  mcpServerId: string;
  label: string;
  toolCalls: number;
  errorCount: number;
  errorRate: number;
  lastCalledAt: Date | undefined;
}

// Usage rows keyed by the member's mcp_servers id, labeled from the gateway's
// member list; the id fallback covers a member list that has not loaded yet.
export function memberUsageRows(
  usage: MetaMcpMemberUsage[],
  members: MemberRow[],
): MemberUsageRow[] {
  const labels = new Map<string, string>();
  for (const row of members) {
    labels.set(
      row.member.mcpServerId,
      row.server?.name ||
        row.member.mcpServerName ||
        row.server?.slug ||
        row.member.mcpServerSlug ||
        row.member.mcpServerId,
    );
  }
  return usage.map((entry) => ({
    mcpServerId: entry.mcpServerId,
    label: labels.get(entry.mcpServerId) ?? entry.mcpServerId,
    toolCalls: entry.toolCalls,
    errorCount: entry.errorCount,
    errorRate:
      entry.toolCalls > 0 ? (entry.errorCount / entry.toolCalls) * 100 : 0,
    lastCalledAt: entry.lastCalledAt,
  }));
}
