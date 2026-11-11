import { useGetToolUsageTargetToolBreakdown } from "@gram/client/react-query/getToolUsageTargetToolBreakdown.js";
import { useMemo, useState } from "react";

/** Calls and failures for one tool over the health window. */
export interface ToolHealth {
  calls: number;
  failureRate: number;
}

/** The window every health figure on Inspect covers. */
export const HEALTH_WINDOW_LABEL = "last 7 days";
const HEALTH_WINDOW_MS = 7 * 24 * 60 * 60 * 1000;

/** A tool is worth flagging once this share of its calls fail. */
export const FAILING_RATE = 0.02;
/** Too few calls make a failure rate noise. */
export const MIN_CALLS_FOR_RATE = 10;

export function isFailing(health: ToolHealth | undefined): boolean {
  return (
    !!health &&
    health.calls >= MIN_CALLS_FOR_RATE &&
    health.failureRate >= FAILING_RATE
  );
}

/**
 * Per-tool usage for one MCP server or gateway over the last 7 days. Telemetry
 * may record a gateway member's tool with or without its server prefix, so
 * lookups try the exact name first and then the bare one.
 */
export function useToolHealth(target: {
  mcpServerId?: string;
  metaMcpServerId?: string;
}): {
  healthFor: (toolName: string) => ToolHealth | undefined;
  isLoading: boolean;
} {
  // Fixed at mount so the query key stays stable across renders.
  const [window] = useState(() => {
    const to = new Date();
    return { from: new Date(to.getTime() - HEALTH_WINDOW_MS), to };
  });
  const enabled = !!(target.mcpServerId || target.metaMcpServerId);

  const query = useGetToolUsageTargetToolBreakdown(
    {
      getToolUsageSummaryPayload: {
        from: window.from,
        to: window.to,
        ...(target.metaMcpServerId
          ? { metaMcpServerIds: [target.metaMcpServerId] }
          : { mcpServerIds: target.mcpServerId ? [target.mcpServerId] : [] }),
      },
    },
    undefined,
    { enabled, throwOnError: false, retry: false },
  );

  const byName = useMemo(() => {
    const map = new Map<string, ToolHealth>();
    for (const row of query.data?.targetToolBreakdown ?? []) {
      const existing = map.get(row.toolName);
      const calls = (existing?.calls ?? 0) + row.eventCount;
      const failures =
        (existing ? existing.failureRate * existing.calls : 0) +
        row.failureCount;
      map.set(row.toolName, {
        calls,
        failureRate: calls > 0 ? failures / calls : 0,
      });
    }
    return map;
  }, [query.data]);

  return {
    healthFor: (toolName) =>
      byName.get(toolName) ?? byName.get(bareToolName(toolName)),
    isLoading: enabled && query.isLoading,
  };
}

/**
 * A gateway member tool's own name: `linear--create_issue` (the gateway's
 * qualified form) or `linear__create_issue` → `create_issue`.
 */
export function bareToolName(name: string): string {
  const match = /--|__/.exec(name);
  return match ? name.slice(match.index + 2) : name;
}
