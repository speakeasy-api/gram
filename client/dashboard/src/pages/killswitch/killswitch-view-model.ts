import type { KillswitchSchedule } from "@gram/client/models/components/killswitchschedule.js";
import type { KillswitchScope } from "@gram/client/models/components/killswitchscope.js";

const NOTE_TRIM_CHARACTERS = new Set(
  Array.from(
    "\t\n\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000",
  ),
);

function hasProhibitedControl(value: string): boolean {
  return Array.from(value).some((character) => {
    const code = character.codePointAt(0) ?? 0;
    return (
      (code <= 31 && code !== 9 && code !== 10 && code !== 13) ||
      (code >= 127 && code <= 159)
    );
  });
}

function trimNoteEdges(value: string): string {
  const characters = Array.from(value);
  let start = 0;
  let end = characters.length;
  while (start < end && NOTE_TRIM_CHARACTERS.has(characters[start]!)) start++;
  while (end > start && NOTE_TRIM_CHARACTERS.has(characters[end - 1]!)) end--;
  return characters.slice(start, end).join("");
}

export type EditorDraft = {
  userId: string;
  capabilityKey: "" | "mcp_tool_calls";
  scopeType: "" | "all_servers" | "selected_servers";
  serverIds: string[];
  startType: "now" | "scheduled";
  startsAt: string;
  endType: "until_lifted" | "bounded";
  endsAt: string;
  externalNote: string;
  internalNote: string;
};

export type DraftErrors = Partial<Record<keyof EditorDraft, string>>;

export function unicodeLength(value: string): number {
  return Array.from(value).length;
}

export function validateDraft(
  draft: EditorDraft,
  now = new Date(),
): DraftErrors {
  const errors: DraftErrors = {};
  if (!draft.userId) errors.userId = "Choose one team member.";
  if (!draft.capabilityKey) errors.capabilityKey = "Choose one capability.";
  if (!draft.scopeType) errors.scopeType = "Choose an MCP server scope.";
  if (draft.scopeType === "selected_servers" && draft.serverIds.length === 0) {
    errors.serverIds = "Choose at least one MCP server.";
  }

  const startsAt = draft.startsAt ? new Date(draft.startsAt) : null;
  if (draft.startType === "scheduled") {
    if (!startsAt || Number.isNaN(startsAt.getTime())) {
      errors.startsAt = "Choose a start date and time.";
    } else if (startsAt.getTime() <= now.getTime()) {
      errors.startsAt = "Scheduled starts must be in the future.";
    }
  }

  const effectiveStart = draft.startType === "scheduled" ? startsAt : now;
  if (draft.endType === "bounded") {
    const endsAt = draft.endsAt ? new Date(draft.endsAt) : null;
    if (!endsAt || Number.isNaN(endsAt.getTime())) {
      errors.endsAt = "Choose an end date and time.";
    } else if (effectiveStart && endsAt.getTime() <= effectiveStart.getTime()) {
      errors.endsAt = "The end must be after the start.";
    }
  }

  validateNote(draft.externalNote, "externalNote", 500, errors);
  validateNote(draft.internalNote, "internalNote", 4000, errors);
  return errors;
}

function validateNote(
  value: string,
  field: "externalNote" | "internalNote",
  max: number,
  errors: DraftErrors,
): void {
  if (hasProhibitedControl(value)) {
    errors[field] = "Remove unsupported control characters.";
    return;
  }

  const normalized = trimNoteEdges(value);
  if (!normalized) {
    errors[field] = "This note is required.";
  } else if (unicodeLength(value) > max) {
    errors[field] = `Use ${max} characters or fewer.`;
  }
}

export function draftToScope(draft: EditorDraft): KillswitchScope {
  return draft.scopeType === "all_servers"
    ? { type: "all_servers" }
    : {
        type: "selected_servers",
        serverIds: [...new Set(draft.serverIds)].sort(),
      };
}

export function draftToSchedule(draft: EditorDraft): KillswitchSchedule {
  if (draft.startType === "scheduled") {
    const startsAt = new Date(draft.startsAt);
    return draft.endType === "bounded"
      ? {
          start: "scheduled",
          startsAt,
          end: "bounded",
          endsAt: new Date(draft.endsAt),
        }
      : { start: "scheduled", startsAt, end: "until_lifted" };
  }
  return draft.endType === "bounded"
    ? { start: "now", end: "bounded", endsAt: new Date(draft.endsAt) }
    : { start: "now", end: "until_lifted" };
}

export function scheduleLabel(schedule: KillswitchSchedule): string {
  const start =
    schedule.start === "scheduled"
      ? `Starts ${schedule.startsAt.toLocaleString()}`
      : "Starts now";
  const end =
    schedule.end === "bounded"
      ? `ends ${schedule.endsAt.toLocaleString()}`
      : "until lifted";
  return `${start}; ${end}`;
}

export function scopeLabel(
  scope: KillswitchScope,
  serverNames: ReadonlyMap<string, string>,
): string {
  if (scope.type === "all_servers") return "All MCP servers";
  if (scope.serverIds.length === 0) return "No MCP servers";
  const names = scope.serverIds
    .slice(0, 2)
    .map((id) => serverNames.get(id) ?? "Deleted MCP server");
  return scope.serverIds.length <= 2
    ? names.join(", ")
    : `${names.join(", ")} +${scope.serverIds.length - 2}`;
}

export function serverDiff(
  before: KillswitchScope,
  after: KillswitchScope,
  allServerIds?: readonly string[],
): { added: string[]; unchanged: string[]; removed: string[] } | null {
  if (after.type !== "selected_servers") return null;

  const beforeIds =
    before.type === "all_servers"
      ? allServerIds === undefined
        ? null
        : [...allServerIds]
      : before.serverIds;
  if (beforeIds === null) return null;

  const previous = new Set(beforeIds);
  const next = new Set(after.serverIds);
  return {
    added: after.serverIds.filter((id) => !previous.has(id)),
    unchanged: after.serverIds.filter((id) => previous.has(id)),
    removed: beforeIds.filter((id) => !next.has(id)),
  };
}

export function nextScheduleBoundaryDelay(
  schedules: readonly KillswitchSchedule[],
  now = Date.now(),
): number | undefined {
  const boundaries = schedules.flatMap((schedule) => {
    const values: number[] = [];
    if (schedule.start === "scheduled")
      values.push(schedule.startsAt.getTime());
    if (schedule.end === "bounded") values.push(schedule.endsAt.getTime());
    return values;
  });
  const next = boundaries
    .filter((value) => value > now)
    .sort((a, b) => a - b)[0];
  return next == null ? undefined : Math.min(next - now + 100, 2_147_000_000);
}

export type KillswitchConflictName = "operation_conflict" | "version_conflict";

export function conflictName(
  error: unknown,
): KillswitchConflictName | undefined {
  if (!error || typeof error !== "object") return undefined;
  const name = (error as { data$?: { name?: unknown } }).data$?.name;
  return name === "operation_conflict" || name === "version_conflict"
    ? name
    : undefined;
}

export function newOperationId(): string {
  return crypto.randomUUID();
}
