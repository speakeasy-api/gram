import type { AuditLog } from "@gram/client/models/components/auditlog.js";

export type TimestampMode = "utc" | "local";

export function formatTimeOnly(date: Date, mode: TimestampMode): string {
  return new Intl.DateTimeFormat(undefined, {
    ...(mode === "utc" ? { timeZone: "UTC" } : {}),
    hour: "numeric",
    minute: "2-digit",
    hour12: true,
  }).format(date);
}

export function formatDateHeader(date: Date, mode: TimestampMode): string {
  return new Intl.DateTimeFormat(undefined, {
    ...(mode === "utc" ? { timeZone: "UTC" } : {}),
    year: "numeric",
    month: "long",
    day: "numeric",
  }).format(date);
}

function getDateKey(date: Date, mode: TimestampMode): string {
  if (mode === "utc") {
    return date.toISOString().slice(0, 10);
  }
  const y = date.getFullYear();
  const m = String(date.getMonth() + 1).padStart(2, "0");
  const d = String(date.getDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}

export type DateGroup = {
  key: string;
  date: Date;
  logs: AuditLog[];
};

export function groupLogsByDate(
  logs: AuditLog[],
  mode: TimestampMode,
): DateGroup[] {
  const groups: DateGroup[] = [];
  const keyMap = new Map<string, DateGroup>();

  for (const log of logs) {
    const key = getDateKey(log.createdAt, mode);
    let group = keyMap.get(key);
    if (!group) {
      group = { key, date: log.createdAt, logs: [] };
      groups.push(group);
      keyMap.set(key, group);
    }
    group.logs.push(log);
  }

  return groups;
}

export type FacetOption = {
  count?: number;
  displayName: string;
  value: string;
};
