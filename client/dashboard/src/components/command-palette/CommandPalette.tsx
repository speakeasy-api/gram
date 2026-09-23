import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { Badge } from "@/components/ui/Badge";
import {
  CommandDialog,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/Command";
import { Icon } from "@/components/ui/Icon";
import { Spinner } from "@/components/ui/Spinner";
import { useCommandPalette } from "@/contexts/CommandPalette";
import { useSlugs } from "@/contexts/Sdk";
import { cn } from "@/lib/utils";
import {
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useLocation } from "react-router";
import { requestAskAi } from "./askAiBridge";
import { useLauncherCandidates } from "./candidates";
import {
  MUTATING_VERBS,
  type LauncherCandidate,
  type Verb,
} from "./candidates/types";
import { isReady, prefilter, rank } from "./ranker";
import { useRecentsUserId } from "./recentlyVisited";
import { useLauncherJudge } from "./useLauncherJudge";

// Speakeasy brand spectrum — the same brand-language gradient the Project
// Assistant uses. Rendered as a thin hairline at the top of the palette so the
// surface reads as Gram without leaning on display type or heavy chrome.
const BRAND_GRADIENT =
  "linear-gradient(90deg, #320F1E 0%, #C83228 12.5%, #FB873F 25%, #D2DC91 37.5%, #5A8250 50%, #002314 62%, #00143C 74%, #2873D7 86%, #9BC3FF 100%)";

const KBD_CLASS =
  "border-neutral-softest bg-muted text-muted-foreground pointer-events-none inline-flex h-5 min-w-5 items-center justify-center gap-1 border px-1.5 font-mono text-[10px] font-medium select-none";

const READY_KBD_CLASS =
  "text-default-success border-success-softest bg-success-softest";

const ASK_AI_VALUE = "__ask_ai__";

const VERB_LABEL: Record<Verb, string> = {
  open: "Open",
  enable: "Enable",
  disable: "Disable",
  publish: "Publish",
};

/** A candidate plus the verb Jev (or the fuzzy fallback) resolved for it. */
interface DisplayRow {
  candidate: LauncherCandidate;
  verb: Verb;
}

/**
 * The confirm flow (see the design spec, "Mutation confirm flow"):
 * list ─Enter on verb≠open─▶ confirm ─Enter─▶ running ─▶ closed, with Esc
 * returning from confirm to list and a rejected run returning from running.
 */
type PaletteMode =
  | { mode: "list" }
  | { mode: "confirm"; row: DisplayRow }
  | { mode: "running"; row: DisplayRow };

const LIST: PaletteMode = { mode: "list" };

// Idle order for page actions: contextual Tool Actions first, then project
// Pages before Organization pages, then anything else alphabetically.
function groupPriority(name: string): number {
  switch (name) {
    case "Tool Actions":
      return 0;
    case "Pages":
      return 1;
    case "Organization":
      return 2;
    default:
      return 3;
  }
}

function compareGroups(a: string, b: string): number {
  const byPriority = groupPriority(a) - groupPriority(b);
  return byPriority !== 0 ? byPriority : a.localeCompare(b);
}

function openRow(candidate: LauncherCandidate): DisplayRow {
  return { candidate, verb: "open" };
}

/**
 * The zero-state list: recents first, then registered page actions by group.
 * No prefilter and no Jev call while the query is empty.
 */
function idleRows(candidates: LauncherCandidate[]): {
  recents: DisplayRow[];
  actions: DisplayRow[];
} {
  const recents = candidates.filter((c) => c.kind === "recent").map(openRow);
  const actions = candidates
    .filter((c) => c.kind === "page")
    .map(openRow)
    .sort((a, b) => compareGroups(a.candidate.group, b.candidate.group));
  return { recents, actions };
}

/**
 * Buckets the idle rows by their display group in order of first appearance.
 * Only the idle list is grouped: ranked rows render flat (see `RankedRows`),
 * because merging a group's rows into the position of its first row would
 * pull later rows ahead of better-ranked rows from other groups.
 */
function groupRows(rows: DisplayRow[]): Array<{
  heading: string;
  rows: DisplayRow[];
}> {
  const groups: Array<{ heading: string; rows: DisplayRow[] }> = [];
  const byHeading = new Map<string, DisplayRow[]>();
  for (const row of rows) {
    const heading = row.candidate.group;
    let bucket = byHeading.get(heading);
    if (!bucket) {
      bucket = [];
      byHeading.set(heading, bucket);
      groups.push({ heading, rows: bucket });
    }
    bucket.push(row);
  }
  return groups;
}

function rowTitle({ candidate, verb }: DisplayRow): string {
  return verb === "open"
    ? candidate.title
    : `${VERB_LABEL[verb]} · ${candidate.title}`;
}

function PaletteRow({
  row,
  ready,
  running,
  onSelect,
}: {
  row: DisplayRow;
  ready: boolean;
  running: boolean;
  onSelect: () => void;
}): JSX.Element {
  const { candidate } = row;
  return (
    <CommandItem
      value={candidate.id}
      onSelect={onSelect}
      className="flex items-center gap-2"
    >
      {candidate.icon && (
        <Icon name={candidate.icon} className="size-4 shrink-0" />
      )}
      <div className="flex min-w-0 flex-1 flex-col">
        <span className="flex items-center gap-2">
          <span className="truncate">{rowTitle(row)}</span>
          {candidate.stage && (
            <ReleaseStageBadge stage={candidate.stage} noTooltip />
          )}
        </span>
        <span className="text-muted-foreground truncate text-xs">
          {candidate.detail}
        </span>
      </div>
      {running && <Spinner className="mr-0 size-4 shrink-0" />}
      {ready && !running && (
        <kbd aria-label="Ready" className={cn(KBD_CLASS, READY_KBD_CLASS)}>
          ↵
        </kbd>
      )}
    </CommandItem>
  );
}

function RowGroups({
  rows,
  readyValue,
  runningValue,
  onSelect,
}: {
  rows: DisplayRow[];
  readyValue: string | null;
  runningValue: string | null;
  onSelect: (row: DisplayRow) => void;
}): JSX.Element {
  return (
    <>
      {groupRows(rows).map((group) => (
        <CommandGroup key={group.heading} heading={group.heading}>
          {group.rows.map((row) => (
            <PaletteRow
              key={row.candidate.id}
              row={row}
              ready={readyValue === row.candidate.id}
              running={runningValue === row.candidate.id}
              onSelect={() => onSelect(row)}
            />
          ))}
        </CommandGroup>
      ))}
    </>
  );
}

/**
 * Ranked rows, one flat list in ranked order: cmdk moves ↑/↓ through the DOM,
 * so the DOM order must be the ranked order for ↓ from the top to reach rank
 * 2. Headings are not needed here — the detail column already names the kind.
 */
function RankedRows({
  rows,
  readyValue,
  runningValue,
  onSelect,
}: {
  rows: DisplayRow[];
  readyValue: string | null;
  runningValue: string | null;
  onSelect: (row: DisplayRow) => void;
}): JSX.Element {
  return (
    <>
      {rows.map((row) => (
        <PaletteRow
          key={row.candidate.id}
          row={row}
          ready={readyValue === row.candidate.id}
          running={runningValue === row.candidate.id}
          onSelect={() => onSelect(row)}
        />
      ))}
    </>
  );
}

/** What cmdk last reported as selected, and the order it was selected in. */
interface Selection {
  /** The query the selection was made under. */
  query: string;
  /** The DOM order (see `domOrder`) the selection was made in. */
  key: string;
  value: string;
  /**
   * Whether the user moved the highlight (↑/↓, pointer) since the query last
   * changed. Tracked explicitly rather than inferred from the value: a user
   * who arrows away and back is on the top row by choice, and comparing the
   * value to the top would read that as "never moved".
   */
  navigated: boolean;
}

/**
 * The value to hand cmdk for the current order. A selection made in this
 * very order stands. When the order changed under it (a judgment landing
 * asynchronously reorders the rows), a row the user had navigated to stays
 * highlighted as long as it is still listed and the query has not changed,
 * so an Enter pressed after ↓ runs the row they were looking at; otherwise
 * the highlight snaps to the new top row.
 */
function resolveSelection(
  selection: Selection | null,
  query: string,
  key: string,
  order: string[],
): string {
  const top = order[0] ?? "";
  if (!selection) return top;
  if (selection.key === key) return selection.value;
  const kept =
    selection.navigated &&
    selection.query === query &&
    order.includes(selection.value);
  return kept ? selection.value : top;
}

/** The keys cmdk moves the highlight on: ↑/↓, Home/End and its vim bindings. */
function isNavigationKey(e: ReactKeyboardEvent): boolean {
  switch (e.key) {
    case "ArrowDown":
    case "ArrowUp":
    case "Home":
    case "End":
      return true;
    case "n":
    case "j":
    case "p":
    case "k":
      return e.ctrlKey;
    default:
      return false;
  }
}

/**
 * Replaces the input while a mutating verb awaits its second Enter. It holds
 * focus so Enter lands here rather than on cmdk, and swallows it while the
 * run is in flight. Escape is handled by the dialog (see `handleEscape`).
 */
function ConfirmBar({
  row,
  running,
  onConfirm,
}: {
  row: DisplayRow;
  running: boolean;
  onConfirm: () => void;
}): JSX.Element {
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    ref.current?.focus();
  }, []);

  return (
    <div
      ref={ref}
      tabIndex={0}
      role="group"
      aria-label="Confirm action"
      className="flex h-14 items-center gap-3 border-b px-3 text-sm outline-hidden"
      onKeyDown={(e) => {
        if (e.key !== "Enter") return;
        e.preventDefault();
        e.stopPropagation();
        if (!running) onConfirm();
      }}
    >
      <span className="flex-1 truncate font-medium">
        {VERB_LABEL[row.verb]} {row.candidate.title}?
      </span>
      <span className="text-muted-foreground flex items-center gap-1.5 text-xs">
        <kbd className={KBD_CLASS}>↵</kbd>
        confirm
        <span aria-hidden>·</span>
        <kbd className={KBD_CLASS}>esc</kbd>
        back
      </span>
    </div>
  );
}

export function CommandPalette(): JSX.Element {
  const { isOpen, close, contextBadge } = useCommandPalette();
  const { orgSlug, projectSlug } = useSlugs();
  const { pathname } = useLocation();
  const [query, setQuery] = useState("");
  const [mode, setMode] = useState<PaletteMode>(LIST);
  const inputRef = useRef<HTMLInputElement>(null);

  // Project Assistant and resource search are project-scoped. At the org level
  // (no project in the URL) the palette still works for navigating org pages.
  const inProject = Boolean(projectSlug);

  // Recents are scoped per user so a shared browser profile doesn't leak
  // history. Gate the session lookup on `isOpen` so we don't poll auth.info on
  // every page (it 401s when unauthenticated).
  const recentsUserId = useRecentsUserId(isOpen);

  const candidates = useLauncherCandidates({
    enabled: isOpen,
    inProject,
    recentsUserId: recentsUserId ?? null,
    orgSlug,
    projectSlug,
  });
  // The intent service is keyed per organization and project, so the "no
  // service" latch is too: moving to a tenant that has one asks again.
  const { state, judge, reset } = useLauncherJudge(
    `${orgSlug ?? ""}/${projectSlug ?? ""}`,
  );

  const trimmedQuery = query.trim();
  const hasQuery = trimmedQuery.length > 0;

  const pre = useMemo(
    () => prefilter(trimmedQuery, candidates),
    [trimmedQuery, candidates],
  );
  const rows = useMemo(() => rank(pre, state.judgment), [pre, state.judgment]);
  const idle = useMemo(() => idleRows(candidates), [candidates]);

  // Ask Jev on every keystroke while open. The hook aborts the previous
  // request itself; an empty query clears the judgment without a call.
  //
  // Keyed on what would be sent rather than on the array's identity: a
  // background refetch of any candidate query rebuilds the array with the
  // same content, and re-asking then would abort a good request in flight.
  const sendableKey = useMemo(
    () =>
      pre.sendable
        .map((c) => [c.id, c.title, c.detail, c.verbs.join(",")].join("\u0001"))
        .join("\n"),
    [pre.sendable],
  );
  const sendableRef = useRef(pre.sendable);
  useEffect(() => {
    sendableRef.current = pre.sendable;
  }, [pre.sendable]);
  useEffect(() => {
    if (!isOpen || state.disabled) return;
    judge(trimmedQuery, sendableRef.current, pathname);
  }, [isOpen, state.disabled, trimmedQuery, sendableKey, pathname, judge]);

  useEffect(() => {
    if (!isOpen) reset();
  }, [isOpen, reset]);

  // cmdk only reselects the first item when its search changes, so when a
  // judgment reorders the rows the highlight would stay on a now-demoted row.
  // Controlling the value pins it to the top row whenever the DOM order
  // changes — unless the user had moved off the top, in which case their row
  // keeps the highlight (see `resolveSelection`).
  const [selectedFor, setSelectedFor] = useState<Selection | null>(null);
  // Set by a navigation key or a pointer move over a row, consumed by the next
  // selection change cmdk reports: that change was the user's. Any other
  // change is cmdk reselecting the top row after the list or query changed.
  const userInput = useRef(false);
  const domOrder = useMemo((): string[] => {
    if (mode.mode !== "list") return [mode.row.candidate.id];
    const ask = inProject ? [ASK_AI_VALUE] : [];
    if (!hasQuery) {
      return [
        ...idle.recents.map((r) => r.candidate.id),
        ...ask,
        ...idle.actions.map((r) => r.candidate.id),
      ];
    }
    return [...rows.map((r) => r.candidate.id), ...ask];
  }, [mode, inProject, hasQuery, idle, rows]);
  const rowsKey = domOrder.join("\n");
  const selectedValue = resolveSelection(
    selectedFor,
    trimmedQuery,
    rowsKey,
    domOrder,
  );

  // The green ↵ is purely visual: Enter always runs the highlighted row.
  const topRow = rows[0];
  const readyValue =
    mode.mode === "list" &&
    hasQuery &&
    topRow &&
    selectedValue === topRow.candidate.id &&
    // Only a fresh judgment may light the ↵: a dimmed one belongs to the
    // previous keystroke and says nothing about the current query.
    state.fresh &&
    isReady(rows, state.judgment)
      ? topRow.candidate.id
      : null;

  // Focus follows the mode: the confirm bar focuses itself on mount, and the
  // input takes focus back when the bar goes away.
  useEffect(() => {
    if (mode.mode === "list") inputRef.current?.focus();
  }, [mode.mode]);

  const closeAndReset = () => {
    setQuery("");
    setMode(LIST);
    setSelectedFor(null);
    close();
  };

  const selectRow = (row: DisplayRow) => {
    if (mode.mode !== "list") return;
    if (MUTATING_VERBS.has(row.verb)) {
      setMode({ mode: "confirm", row });
      return;
    }
    void row.candidate.run(row.verb);
    closeAndReset();
  };

  // Second Enter. The candidate's `run` toasts on its own and rejects on
  // failure, so a rejection just returns to the list with the query intact.
  const runConfirmed = () => {
    if (mode.mode !== "confirm") return;
    const { row } = mode;
    setMode({ mode: "running", row });
    Promise.resolve()
      .then(() => row.candidate.run(row.verb))
      .then(
        () => closeAndReset(),
        () => setMode(LIST),
      );
  };

  // Radix listens for Escape on the document in the capture phase, so this is
  // the only place that can stop it closing the dialog. Escape steps back one
  // level at a time: running ignores it, confirm returns to the list with the
  // query intact, a query is cleared, and only an empty list closes.
  const handleEscape = (e: KeyboardEvent) => {
    if (mode.mode === "running") {
      e.preventDefault();
      return;
    }
    if (mode.mode === "confirm") {
      e.preventDefault();
      setMode(LIST);
      return;
    }
    if (hasQuery) {
      e.preventDefault();
      setQuery("");
    }
  };

  const handleAskAi = () => {
    requestAskAi(trimmedQuery);
    closeAndReset();
  };

  // Free-form AI escape hatch — forceMounted so the typed query can always be
  // sent to the assistant (the group too, since cmdk hides a group whose id
  // isn't in `filtered.groups` while a search is active). Project Assistant
  // is project-scoped, so only at the project level. Rendered near the top
  // when the palette is idle (discoverable) but pushed below the results
  // while searching: cmdk auto-selects the first item in DOM order, so keeping
  // this row above the matches would steal the highlight from the closest
  // result and force an extra ↓ keypress to reach it (AGE-2807).
  const askAiGroup = inProject ? (
    <CommandGroup forceMount heading="Assistant">
      <CommandItem
        forceMount
        value={ASK_AI_VALUE}
        onSelect={handleAskAi}
        className="flex items-center gap-2"
      >
        <Icon name="sparkles" className="text-primary size-4 shrink-0" />
        <div className="flex min-w-0 flex-col">
          <span className="shimmer truncate">
            {hasQuery ? "Ask Project Assistant" : "Ask the Project Assistant…"}
          </span>
          {hasQuery && (
            <span className="text-muted-foreground truncate text-xs">
              &ldquo;{trimmedQuery}&rdquo;
            </span>
          )}
        </div>
      </CommandItem>
    </CommandGroup>
  ) : null;

  const rowGroupProps = {
    readyValue,
    runningValue: mode.mode === "running" ? mode.row.candidate.id : null,
    onSelect: mode.mode === "list" ? selectRow : runConfirmed,
  };

  return (
    <CommandDialog
      open={isOpen}
      onOpenChange={(open) => {
        if (!open) closeAndReset();
      }}
      shouldFilter={false}
      value={selectedValue}
      onValueChange={(value) => {
        const navigated =
          userInput.current ||
          (selectedFor !== null &&
            selectedFor.query === trimmedQuery &&
            selectedFor.navigated);
        userInput.current = false;
        setSelectedFor({ query: trimmedQuery, key: rowsKey, value, navigated });
      }}
      onEscapeKeyDown={handleEscape}
    >
      {/* Speakeasy brand hairline */}
      <div
        aria-hidden
        className="h-0.5 w-full shrink-0"
        style={{ background: BRAND_GRADIENT }}
      />
      {contextBadge && (
        <div className="px-3 pt-3 pb-2">
          <Badge variant="neutral">
            <Badge.Text>{contextBadge}</Badge.Text>
          </Badge>
        </div>
      )}
      {mode.mode === "list" ? (
        <CommandInput
          ref={inputRef}
          placeholder={
            inProject
              ? "Ask AI or search resources and pages…"
              : "Search pages…"
          }
          value={query}
          onValueChange={(value) => {
            // A keystroke that changes the query starts over: a navigation
            // key pressed before it must not mark the next selection as
            // the user's.
            userInput.current = false;
            setQuery(value);
          }}
          onKeyDownCapture={(e) => {
            if (isNavigationKey(e)) userInput.current = true;
          }}
        />
      ) : (
        <ConfirmBar
          row={mode.row}
          running={mode.mode === "running"}
          onConfirm={runConfirmed}
        />
      )}
      <CommandList
        className={cn(
          "transition-opacity",
          // A newer request is in flight: keep the last order, but dimmed.
          state.judgment && !state.fresh && "opacity-60",
        )}
        // Capture phase: cmdk selects the row under the pointer from the
        // row's own handler, so the flag must be set before it fires.
        onPointerMoveCapture={(e: ReactPointerEvent<HTMLDivElement>) => {
          if ((e.target as Element).closest("[cmdk-item]")) {
            userInput.current = true;
          }
        }}
      >
        {mode.mode !== "list" && (
          <RankedRows rows={[mode.row]} {...rowGroupProps} />
        )}

        {mode.mode === "list" && !hasQuery && (
          <>
            {/* Recents are a zero-state affordance only: once the user types
                they compete on their own merits (AGE-2808). */}
            <RowGroups rows={idle.recents} {...rowGroupProps} />
            {askAiGroup}
            <RowGroups rows={idle.actions} {...rowGroupProps} />
          </>
        )}

        {mode.mode === "list" && hasQuery && (
          <>
            {/* The Ask row is always mounted inside a project, so an unmatched
                query there is an offer to ask the assistant, not a dead end.
                cmdk's own empty state depends on its filter, which is off. */}
            {rows.length === 0 && !inProject && (
              <div className="py-6 text-center text-sm">No results found.</div>
            )}
            <RankedRows rows={rows} {...rowGroupProps} />
            {askAiGroup}
          </>
        )}
      </CommandList>

      {/* Keyboard navigation hints */}
      <div className="text-muted-foreground flex items-center gap-3 border-t px-3 py-2 text-xs">
        <span className="flex items-center gap-1.5">
          <kbd className={KBD_CLASS}>↑</kbd>
          <kbd className={KBD_CLASS}>↓</kbd>
          to navigate
        </span>
        <span className="flex items-center gap-1.5">
          <kbd className={KBD_CLASS}>↵</kbd>
          to select
        </span>
        <span className="flex items-center gap-1.5">
          <kbd className={KBD_CLASS}>esc</kbd>
          to close
        </span>
        {state.fresh && state.latencyMs != null && (
          <span className="ml-auto tabular-nums">{state.latencyMs} ms</span>
        )}
      </div>
    </CommandDialog>
  );
}
