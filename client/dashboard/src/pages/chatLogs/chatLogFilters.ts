import { format } from "date-fns";
import type { TelemetryLogRecord } from "@gram/client/models/components";

const CLAUDE_OTEL_LOG_URN = "claude-code:otel:logs";

export function formatLogTimestamp(nanos: string): string {
  const ms = Number(BigInt(nanos) / 1_000_000n);
  return format(new Date(ms), "HH:mm:ss.SSS");
}

export function getSeverityBadgeVariant(
  severity: string | undefined,
): "destructive" | "warning" | "neutral" {
  switch (severity?.toUpperCase()) {
    case "ERROR":
    case "FATAL":
      return "destructive";
    case "WARN":
      return "warning";
    case undefined:
    default:
      return "neutral";
  }
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
  return logs.filter((log) => log.attributes?.gram_urn !== CLAUDE_OTEL_LOG_URN);
}
