/**
 * A short, in-memory log of what the user just did and saw in the dashboard:
 * pages opened, API actions and their outcome, error toasts, and signs of
 * struggle. The Project Assistant uses it to work out what the user is trying
 * to do, so a user who does not know what to ask still gets specific help.
 *
 * Nothing here leaves the browser except as Project Assistant context, which
 * goes to Speakeasy's own server. It is not sent to analytics. Action entries
 * name the API call only, never its arguments, so secrets typed into forms
 * cannot reach the log.
 */

export type ActivityKind = "page" | "action" | "error" | "signal";

export interface ActivityEntry {
  /** ISO timestamp. */
  at: string;
  kind: ActivityKind;
  detail: string;
}

const MAX_ENTRIES = 40;
const MAX_DETAIL_LENGTH = 300;
/** A repeat of the last entry inside this window updates it instead of adding a line. */
const REPEAT_MERGE_MS = 60_000;
/** The same action failing this often inside the window is a sign of struggle. */
const STRUGGLE_FAILURES = 3;
const STRUGGLE_WINDOW_MS = 5 * 60_000;
/** Upper bound on the rendered context block, in characters. */
const MAX_CONTEXT_LENGTH = 3000;

/**
 * API calls that are the assistant's own plumbing, not something the user
 * did. Logging them would make every assistant open look like activity.
 */
const IGNORED_ACTIONS = new Set([
  "assistants.inferIntent",
  "assistants.sendMessage",
  "assistants.interruptTurn",
  "assistants.ensureManaged",
]);

interface StoredEntry extends ActivityEntry {
  repeats: number;
  atMs: number;
}

let entries: StoredEntry[] = [];
let revision = 0;
const recentFailures = new Map<string, number[]>();

function clip(text: string): string {
  const flat = text.replace(/\s+/g, " ").trim();
  return flat.length > MAX_DETAIL_LENGTH
    ? `${flat.slice(0, MAX_DETAIL_LENGTH - 1)}…`
    : flat;
}

export function recordActivity(
  kind: ActivityKind,
  detail: string,
  now: number = Date.now(),
): void {
  const text = clip(detail);
  if (!text) return;

  const last = entries[entries.length - 1];
  if (
    last &&
    last.kind === kind &&
    last.detail === text &&
    now - last.atMs < REPEAT_MERGE_MS
  ) {
    last.repeats += 1;
    last.atMs = now;
    last.at = new Date(now).toISOString();
    revision += 1;
    return;
  }

  entries.push({
    kind,
    detail: text,
    at: new Date(now).toISOString(),
    atMs: now,
    repeats: 1,
  });
  if (entries.length > MAX_ENTRIES) {
    entries = entries.slice(entries.length - MAX_ENTRIES);
  }
  revision += 1;
}

/** Records the outcome of an API call, and a struggle signal when it keeps failing. */
export function recordActionOutcome(
  action: string,
  failed: boolean,
  now: number = Date.now(),
): void {
  if (IGNORED_ACTIONS.has(action)) return;
  recordActivity("action", `${action}: ${failed ? "failed" : "ok"}`, now);

  if (!failed) {
    recentFailures.delete(action);
    return;
  }
  const failures = (recentFailures.get(action) ?? []).filter(
    (t) => now - t < STRUGGLE_WINDOW_MS,
  );
  failures.push(now);
  recentFailures.set(action, failures);
  if (failures.length === STRUGGLE_FAILURES) {
    recordActivity(
      "signal",
      `${action} failed ${STRUGGLE_FAILURES} times in a row`,
      now,
    );
  }
}

/** The log, oldest first. Merged repeats read "(x3)". */
export function getActivity(): ActivityEntry[] {
  return entries.map(({ at, kind, detail, repeats }) => ({
    at,
    kind,
    detail: repeats > 1 ? `${detail} (x${repeats})` : detail,
  }));
}

/** Changes whenever the log changes, so callers can skip repeat work. */
export function getActivityRevision(): number {
  return revision;
}

export function clearActivity(): void {
  entries = [];
  recentFailures.clear();
  revision += 1;
}

/**
 * Names an SDK mutation by its key: `["@gram/client", "toolsets",
 * "updateBySlug"]` becomes `toolsets.updateBySlug`. Returns null for keys
 * that are not SDK calls.
 */
export function describeMutationKey(
  key: readonly unknown[] | undefined,
): string | null {
  if (!key || key[0] !== "@gram/client") return null;
  const parts = key
    .slice(1)
    .filter((part): part is string => typeof part === "string");
  return parts.length > 0 ? parts.join(".") : null;
}

/** Strips the organization and project prefix from a dashboard path. */
export function projectRelativePath(pathname: string): string {
  const match = /^\/[^/]+\/projects\/[^/]+(\/.*)?$/.exec(pathname);
  if (match) return match[1] ?? "/";
  const org = /^\/[^/]+(\/.*)?$/.exec(pathname);
  return org?.[1] ?? pathname;
}

function escapeContext(text: string): string {
  return text
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}

export interface InferredGoal {
  goal?: string | undefined;
  blocker?: string | undefined;
}

/**
 * Renders the log as the `<user_activity>` section of the Project Assistant's
 * dashboard context. Text is escaped so nothing in it can close the block.
 */
export function formatActivityContext(
  currentPath: string,
  activity: ActivityEntry[],
  inferred?: InferredGoal | null,
  maxEntries = 20,
): string {
  const lines = [`Current page: ${clip(escapeContext(currentPath))}`];
  if (inferred?.goal) {
    lines.push(`Likely goal: ${clip(escapeContext(inferred.goal))}`);
  }
  if (inferred?.blocker) {
    lines.push(`Likely blocker: ${clip(escapeContext(inferred.blocker))}`);
  }
  const render = (recent: string[]) => {
    const body =
      recent.length > 0
        ? [...lines, "Recent activity, oldest first:", ...recent]
        : lines;
    return `<user_activity>\n${body.join("\n")}\n</user_activity>`;
  };
  // The block rides inside the user's message, which the server caps, so the
  // oldest entries give way first. Every header line is clipped, so the
  // header alone always fits.
  let recent = activity.slice(-maxEntries).map((entry) => {
    const time = entry.at.slice(11, 19);
    return `${time} ${entry.kind}: ${escapeContext(entry.detail)}`;
  });
  let block = render(recent);
  while (recent.length > 0 && block.length > MAX_CONTEXT_LENGTH) {
    recent = recent.slice(1);
    block = render(recent);
  }
  return block;
}

/** Test-only: resets module state between tests. */
export function resetActivityForTests(): void {
  clearActivity();
  revision = 0;
}
