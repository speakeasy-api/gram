import { dateTimeFormatters } from "@/lib/dates";

/**
 * Format Unix nanoseconds to a readable timestamp
 */
export function formatNanoTimestamp(nanos: number): string {
  const ms = nanos / 1_000_000;
  return dateTimeFormatters.logTimestamp.format(new Date(ms));
}

/**
 * Format Unix nanoseconds to a Date object
 */
export function nanoToDate(nanos: number): Date {
  return new Date(nanos / 1_000_000);
}

/**
 * Format duration in seconds to human-readable string
 */
export function formatDuration(seconds: number): string {
  if (seconds < 1) {
    return `${Math.round(seconds * 1000)}ms`;
  }
  if (seconds < 60) {
    return `${Math.round(seconds)}s`;
  }
  const mins = Math.floor(seconds / 60);
  const secs = Math.round(seconds % 60);
  if (secs === 0) {
    return `${mins}m`;
  }
  return `${mins}m ${secs}s`;
}

/**
 * Format token count to compact display
 */
export function formatTokenCount(n: number): string {
  if (n < 1000) {
    return String(n);
  }
  if (n < 1_000_000) {
    return `${(n / 1000).toFixed(1)}k`;
  }
  return `${(n / 1_000_000).toFixed(1)}M`;
}
