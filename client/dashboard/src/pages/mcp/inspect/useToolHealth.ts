import type { ToolUsageTargetToolBreakdownRow } from "@gram/client/models/components/toolusagetargettoolbreakdownrow.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildGetToolUsageTargetToolBreakdownQuery } from "@gram/client/react-query/getToolUsageTargetToolBreakdown.js";
import { useQuery } from "@tanstack/react-query";
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

  const client = useGramContext();
  const request = {
    getToolUsageSummaryPayload: {
      from: window.from,
      to: window.to,
      ...(target.metaMcpServerId
        ? { metaMcpServerIds: [target.metaMcpServerId] }
        : { mcpServerIds: target.mcpServerId ? [target.mcpServerId] : [] }),
    },
  };
  const built = buildGetToolUsageTargetToolBreakdownQuery(client, request);
  // The generated key leaves the payload out, so two servers would share one
  // cache entry; scope it to the target and the window.
  const query = useQuery({
    ...built,
    queryKey: [
      ...built.queryKey,
      target.metaMcpServerId ?? null,
      target.mcpServerId ?? null,
      window.from.toISOString(),
      window.to.toISOString(),
    ],
    enabled,
    throwOnError: false,
    retry: false,
  });

  const byName = useMemo(
    () => aggregateToolHealth(query.data?.targetToolBreakdown ?? []),
    [query.data],
  );

  return {
    healthFor: (toolName) =>
      byName.get(toolName) ?? byName.get(bareToolName(toolName)),
    isLoading: enabled && query.isLoading,
  };
}

/**
 * Sums usage rows per tool name. The failure rate covers completed calls
 * only (successes plus failures), so calls still in flight don't dilute it.
 */
export function aggregateToolHealth(
  rows: Pick<
    ToolUsageTargetToolBreakdownRow,
    "toolName" | "eventCount" | "successCount" | "failureCount"
  >[],
): Map<string, ToolHealth> {
  const totals = new Map<
    string,
    { calls: number; completed: number; failures: number }
  >();
  for (const row of rows) {
    const total = totals.get(row.toolName) ?? {
      calls: 0,
      completed: 0,
      failures: 0,
    };
    total.calls += row.eventCount;
    total.completed += row.successCount + row.failureCount;
    total.failures += row.failureCount;
    totals.set(row.toolName, total);
  }
  const map = new Map<string, ToolHealth>();
  for (const [name, total] of totals) {
    map.set(name, {
      calls: total.calls,
      failureRate: total.completed > 0 ? total.failures / total.completed : 0,
    });
  }
  return map;
}

/**
 * A gateway member tool's own name: `linear--create_issue` (the gateway's
 * qualified form) or `linear__create_issue` → `create_issue`.
 */
export function bareToolName(name: string): string {
  const match = /--|__/.exec(name);
  return match ? name.slice(match.index + 2) : name;
}
