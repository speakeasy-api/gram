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
import { useCommandPalette } from "@/contexts/CommandPalette";
import { useProject } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRBAC } from "@/hooks/useRBAC";
import {
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { requestAskAi } from "./askAiBridge";
import { useLauncherCandidates } from "./candidates";
import type { LauncherCandidate } from "./candidates/types";
import type { ProjectResourceIndexMetrics } from "./candidates/useProjectResourceIndexCandidates";
import {
  captureCommandPalettePerformance,
  CommandPalettePerformanceTracker,
} from "./performance";
import { prefilter, rank } from "./ranker";
import { useRecentsUserId } from "./recentlyVisited";

// Speakeasy brand spectrum — the same brand-language gradient the Project
// Assistant uses. Rendered as a thin hairline at the top of the palette so the
// surface reads as Speakeasy without leaning on display type or heavy chrome.
const BRAND_GRADIENT =
  "linear-gradient(90deg, #320F1E 0%, #C83228 12.5%, #FB873F 25%, #D2DC91 37.5%, #5A8250 50%, #002314 62%, #00143C 74%, #2873D7 86%, #9BC3FF 100%)";

const KBD_CLASS =
  "border-neutral-softest bg-muted text-muted-foreground pointer-events-none inline-flex h-5 min-w-5 items-center justify-center gap-1 border px-1.5 font-mono text-[10px] font-medium select-none";

const ASK_AI_VALUE = "__ask_ai__";

/** A candidate displayed by the palette. */
interface DisplayRow {
  candidate: LauncherCandidate;
}

const EMPTY_ROWS: DisplayRow[] = [];

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
  return { candidate };
}

/**
 * The zero-state list: recents first, then the organization's projects, then
 * registered page actions by group. Search ranking only runs while the query
 * is non-empty. Projects are listed only at the org level (see
 * `CommandPalette`); the hook keeps them in their source order (by slug).
 */
function idleRows(candidates: LauncherCandidate[]): {
  recents: DisplayRow[];
  projects: DisplayRow[];
  actions: DisplayRow[];
} {
  const recents = candidates.filter((c) => c.kind === "recent").map(openRow);
  const projects = candidates.filter((c) => c.kind === "project").map(openRow);
  const actions = candidates
    .filter((c) => c.kind === "page")
    .map(openRow)
    .sort((a, b) => compareGroups(a.candidate.group, b.candidate.group));
  return { recents, projects, actions };
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

function PaletteRow({
  row,
  onSelect,
}: {
  row: DisplayRow;
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
          <span className="truncate">{candidate.title}</span>
          {candidate.stage && (
            <ReleaseStageBadge stage={candidate.stage} noTooltip />
          )}
        </span>
        <span className="text-muted-foreground truncate text-xs">
          {candidate.detail}
        </span>
      </div>
    </CommandItem>
  );
}

function RowGroups({
  rows,
  onSelect,
}: {
  rows: DisplayRow[];
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
  onSelect,
}: {
  rows: DisplayRow[];
  onSelect: (row: DisplayRow) => void;
}): JSX.Element {
  return (
    <>
      {rows.map((row) => (
        <PaletteRow
          key={row.candidate.id}
          row={row}
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

export function CommandPalette(): JSX.Element {
  const { isOpen, close, contextBadge } = useCommandPalette();
  const { orgSlug, projectSlug } = useSlugs();
  const project = useProject();
  const { hasScope } = useRBAC();
  const telemetry = useTelemetry();
  const [query, setQuery] = useState("");
  const performanceTracker = useRef<CommandPalettePerformanceTracker | null>(
    null,
  );
  if (performanceTracker.current === null) {
    performanceTracker.current = new CommandPalettePerformanceTracker();
  }
  const tracker = performanceTracker.current;
  const wasOpen = useRef(false);
  const recordedRanking = useRef<object | null>(null);
  const indexMetrics = useRef<ProjectResourceIndexMetrics | null>(null);
  const recordIndexMetrics = useCallback(
    (metrics: ProjectResourceIndexMetrics) => {
      indexMetrics.current = metrics;
      tracker.index(performance.now(), metrics);
    },
    [tracker],
  );

  // Project Assistant and resource search are project-scoped. At the org level
  // (no project in the URL) the palette still works for navigating org pages.
  const inProject = Boolean(projectSlug);
  const canAskAssistant =
    inProject && hasScope("assistant:read", undefined, project.id);

  // Recents are scoped per user so a shared browser profile doesn't leak
  // history. Gate the session lookup on `isOpen` so we don't poll auth.info on
  // every page (it 401s when unauthenticated).
  const recentsUserId = useRecentsUserId(isOpen);
  const trimmedQuery = query.trim();
  const candidates = useLauncherCandidates({
    enabled: isOpen,
    inProject,
    recentsUserId: recentsUserId ?? null,
    orgSlug,
    projectSlug,
    query: trimmedQuery,
    onIndexMetrics: recordIndexMetrics,
  });
  const hasQuery = trimmedQuery.length > 0;

  const ranked = useMemo(() => {
    if (!isOpen || !hasQuery) return null;

    const startedAt = performance.now();
    const rows = rank(prefilter(trimmedQuery, candidates));
    return { rows, duration: performance.now() - startedAt };
  }, [candidates, hasQuery, isOpen, trimmedQuery]);
  const rows = ranked?.rows ?? EMPTY_ROWS;
  const idle = useMemo(() => idleRows(candidates), [candidates]);

  useEffect(() => {
    const now = performance.now();
    const pages = candidates.filter(
      (candidate) => candidate.kind === "page",
    ).length;
    const resources = candidates.filter(
      (candidate) => candidate.kind !== "page" && candidate.kind !== "recent",
    ).length;

    if (isOpen && !wasOpen.current) {
      tracker.open(now, {
        total: candidates.length,
        pages,
        resources,
      });
      if (indexMetrics.current) tracker.index(now, indexMetrics.current);
    }
    if (isOpen) {
      tracker.candidates(now, candidates.length, pages, resources);
    } else if (wasOpen.current) {
      const payload = tracker.close(
        now,
        inProject ? "project" : "organization",
      );
      if (payload) captureCommandPalettePerformance(telemetry, payload);
    }
    wasOpen.current = isOpen;
  }, [candidates, inProject, isOpen, telemetry, tracker]);

  useEffect(() => {
    if (!isOpen || ranked === null || recordedRanking.current === ranked)
      return;
    recordedRanking.current = ranked;
    tracker.ranking(ranked.duration);
  }, [isOpen, ranked, tracker]);

  // cmdk only reselects the first item when its search changes. Controlling
  // the value pins it to the top row whenever the DOM order
  // changes — unless the user had moved off the top, in which case their row
  // keeps the highlight (see `resolveSelection`).
  const [selectedFor, setSelectedFor] = useState<Selection | null>(null);
  // Set by a navigation key or a pointer move over a row, consumed by the next
  // selection change cmdk reports: that change was the user's. Any other
  // change is cmdk reselecting the top row after the list or query changed.
  const userInput = useRef(false);
  // At the org level, picking a project is the palette's main job, so the
  // projects head the idle list after recents. Inside a project they are a
  // switcher: they wait for a query rather than heading an idle palette with
  // the projects you aren't in, and the ranked list already includes them.
  const idleProjects = useMemo(
    () => (inProject ? [] : idle.projects),
    [inProject, idle.projects],
  );
  const domOrder = useMemo((): string[] => {
    const ask = canAskAssistant ? [ASK_AI_VALUE] : [];
    if (!hasQuery) {
      return [
        ...idle.recents.map((r) => r.candidate.id),
        ...ask,
        ...idleProjects.map((r) => r.candidate.id),
        ...idle.actions.map((r) => r.candidate.id),
      ];
    }
    return [...rows.map((r) => r.candidate.id), ...ask];
  }, [canAskAssistant, hasQuery, idle, idleProjects, rows]);
  const rowsKey = domOrder.join("\n");
  const selectedValue = resolveSelection(
    selectedFor,
    trimmedQuery,
    rowsKey,
    domOrder,
  );

  const closeAndReset = () => {
    setQuery("");
    setSelectedFor(null);
    close();
  };

  const selectRow = (row: DisplayRow) => {
    void Promise.resolve(row.candidate.run("open")).catch(() => {});
    closeAndReset();
  };

  // Radix listens for Escape on the document in the capture phase, so this is
  // the only place that can clear the query before the dialog closes.
  const handleEscape = (e: KeyboardEvent) => {
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
  const askAiGroup = canAskAssistant ? (
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

  const rowGroupProps = { onSelect: selectRow };

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
      <CommandInput
        placeholder={
          inProject
            ? "Ask AI or search resources and pages…"
            : "Search projects and pages…"
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
      <CommandList
        className="transition-opacity"
        // Capture phase: cmdk selects the row under the pointer from the
        // row's own handler, so the flag must be set before it fires.
        onPointerMoveCapture={(e: ReactPointerEvent<HTMLDivElement>) => {
          if ((e.target as Element).closest("[cmdk-item]")) {
            userInput.current = true;
          }
        }}
      >
        {!hasQuery && (
          <>
            {/* Recents are a zero-state affordance only: once the user types
                they compete on their own merits (AGE-2808). */}
            <RowGroups rows={idle.recents} {...rowGroupProps} />
            {askAiGroup}
            <RowGroups rows={idleProjects} {...rowGroupProps} />
            <RowGroups rows={idle.actions} {...rowGroupProps} />
          </>
        )}

        {hasQuery && (
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
      </div>
    </CommandDialog>
  );
}
