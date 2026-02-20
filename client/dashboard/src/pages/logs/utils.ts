import {
  ToolCallSummary,
  TelemetryLogRecord,
} from "@gram/client/models/components";
import { dateTimeFormatters } from "@/lib/dates";
import {
  FileCode,
  PencilRuler,
  SquareFunction,
  LucideIcon,
} from "lucide-react";

/**
 * Format Unix nanoseconds to a readable timestamp
 */
export function formatNanoTimestamp(nanos: number): string {
  const ms = nanos / 1_000_000;
  return dateTimeFormatters.logTimestamp.format(new Date(ms)).replace(",", "");
}

/**
 * Format Unix nanoseconds to human-readable relative time
 */
export function formatRelativeTime(nanos: number): string {
  const ms = nanos / 1_000_000;
  return dateTimeFormatters.humanize(new Date(ms));
}

/**
 * Get status indicator for a tool call
 */
export function getStatusInfo(toolCall: ToolCallSummary): {
  isSuccess: boolean;
  statusText: string;
} {
  if (toolCall.httpStatusCode) {
    const isSuccess =
      toolCall.httpStatusCode >= 200 && toolCall.httpStatusCode < 400;
    return {
      isSuccess,
      statusText: String(toolCall.httpStatusCode),
    };
  }
  return { isSuccess: true, statusText: "OK" };
}

/**
 * Get severity color class
 */
export function getSeverityColorClass(severity?: string): string {
  switch (severity?.toUpperCase()) {
    case "ERROR":
    case "FATAL":
      return "text-destructive-default";
    case "WARN":
      return "text-warning-default";
    case "DEBUG":
      return "text-muted-foreground";
    case "INFO":
    default:
      return "text-foreground";
  }
}

/**
 * Parse the tool/source name from a gram URN
 * Format: tools:{kind}:{source}:{name}
 */
export function parseGramUrn(urn: string): {
  kind: string;
  source: string;
  name: string;
} {
  const parts = urn.split(":");
  return {
    kind: parts[1] || "",
    source: parts[2] || "",
    name: parts[3] || urn,
  };
}

/**
 * Get the source name from a gram URN
 */
export function getSourceFromUrn(urn: string): string {
  const { source } = parseGramUrn(urn);
  return source || urn;
}

/**
 * Get the tool name from a gram URN
 */
export function getToolNameFromUrn(urn: string): string {
  const { name } = parseGramUrn(urn);
  return name || urn;
}

/**
 * Get the appropriate icon for a tool based on its URN
 */
export function getToolIcon(urn: string): LucideIcon {
  const { kind } = parseGramUrn(urn);
  if (kind === "http") {
    return FileCode;
  }
  if (kind === "prompt") {
    return PencilRuler;
  }
  // Otherwise it's a function tool
  return SquareFunction;
}

/**
 * Format a log record body for display
 */
export function formatLogBody(log: TelemetryLogRecord): string {
  return log.body || "(no message)";
}

/**
 * Get attributes as a formatted string for preview
 */
export function formatAttributesPreview(attributes: unknown): string {
  if (!attributes || typeof attributes !== "object") return "";
  const entries = Object.entries(attributes as Record<string, unknown>);
  if (entries.length === 0) return "";
  return entries
    .slice(0, 3)
    .map(([key, value]) => `${key}=${JSON.stringify(value)}`)
    .join(", ");
}
