import { format } from "date-fns";
import type { TelemetryLogRecord } from "@gram/client/models/components/telemetrylogrecord.js";

// Raw OTEL streams are persisted for analytics but are too noisy for the chat
// detail panel; derived rows (e.g. claude-code:usage:metrics) stay visible.
const RAW_OTEL_URNS = new Set([
  "claude-code:otel:logs",
  "codex:otel:logs",
  "codex:otel:metrics",
]);

export function formatLogTimestamp(nanos: string): string {
  const ms = Number(BigInt(nanos) / 1_000_000n);
  return format(new Date(ms), "HH:mm:ss.SSS");
}

export function filterToolLogs(
  logs: TelemetryLogRecord[],
): TelemetryLogRecord[] {
  return logs.filter((log) => {
    const body = log.body.toLowerCase();
    const hasToolKeyword =
      body.includes("tool") ||
      body.includes("function") ||
      body.includes("mcp");
    const attrs = log.attributes || {};
    const hasToolAttr =
      attrs.tool_name || attrs.function_name || attrs.gram_urn;
    return Boolean(hasToolKeyword || hasToolAttr);
  });
}

export function filterPanelTelemetryLogs(
  logs: TelemetryLogRecord[],
): TelemetryLogRecord[] {
  return logs.filter(
    (log) => !RAW_OTEL_URNS.has(String(log.attributes?.gram_urn ?? "")),
  );
}
