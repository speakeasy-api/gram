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

export type ActorRun = {
  key: string;
  actorId: string;
  actorType: string;
  logs: AuditLog[];
};

/**
 * Splits a day's entries into runs of consecutive entries by the same actor.
 * An agent's runtime writes a security entry on every start and teardown, so
 * a run reads as one process at work rather than a column of repeats.
 */
export function groupConsecutiveByActor(logs: AuditLog[]): ActorRun[] {
  const runs: ActorRun[] = [];
  for (const log of logs) {
    const last = runs[runs.length - 1];
    if (
      last &&
      last.actorId === log.actorId &&
      last.actorType === log.actorType
    ) {
      last.logs.push(log);
      continue;
    }
    runs.push({
      key: log.id,
      actorId: log.actorId,
      actorType: log.actorType,
      logs: [log],
    });
  }
  return runs;
}
