import { dateTimeFormatters } from "@/lib/dates";
import { TelemetryLogRecord } from "@gram/client/models/components/telemetrylogrecord.js";

/**
 * Format Unix nanoseconds to a readable timestamp
 */
export function formatNanoTimestamp(nanos: string): string {
  const ms = Number(BigInt(nanos) / 1_000_000n);
  return dateTimeFormatters.logTimestamp.format(new Date(ms)).replace(",", "");
}

/**
 * Get severity color class
 */
export function getSeverityColorClass(severity?: string): string {
  switch (severity?.toUpperCase()) {
    case "ERROR":
    case "FATAL":
      return "text-destructive";
    case "WARN":
      return "text-warning";
    case "DEBUG":
      return "text-muted-foreground";
    case "INFO":
    case undefined:
    default:
      return "text-foreground";
  }
}

/**
 * Format a log record body for display
 */
export function formatLogBody(log: TelemetryLogRecord): string {
  return log.body || "(no message)";
}
