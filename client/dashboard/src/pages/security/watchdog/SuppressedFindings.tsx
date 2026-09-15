import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Icon } from "@/components/ui/Icon";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useRowSelection, type RowSelection } from "@/hooks/useRowSelection";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import type { RiskExclusion } from "@gram/client/models/components/riskexclusion.js";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { useRiskListDismissedResults } from "@gram/client/react-query/riskListDismissedResults.js";
import { useRiskListExclusions } from "@gram/client/react-query/riskListExclusions.js";
import { keepPreviousData } from "@tanstack/react-query";
import { format } from "date-fns";
import { useCallback, useMemo, useState } from "react";
import { useNavigate } from "react-router";
import { useChatDetailSheet } from "@/pages/chatLogs/useChatDetailSheet";
import { CategoryLabel } from "../risk-ui";
import { getRuleTitleFallback } from "../risk-utils";
import { useDismissFinding } from "../useDismissFinding";
import { SuppressedFindingDrawer } from "./SuppressedFindingDrawer";
import {
  isRestorable,
  suppressionDetail,
  suppressionReason,
  SUPPRESSION_REASON_LABEL,
} from "./suppressed-helpers";

const PAGE_SIZE = 10;

const resultId = (result: RiskResult): string => result.id;

/**
 * Suppressed findings, below the active signal list: everything the signals
 * above deliberately leave out — findings hidden by an exclusion rule, a manual
 * dismissal, or the automated false-positive sweep. Collapsed by default, since
 * this is the "what am I not being shown" audit trail rather than the working
 * surface, and the header spells out that none of it feeds the risk score or
 * the finding counts.
 *
 * Renders nothing at all when there is nothing suppressed — an empty section
 * here would read as a broken feature rather than a clean slate.
 */
export function SuppressedFindings(): JSX.Element | null {
  const [expanded, setExpanded] = useState(false);
  // The listing cursor is forward-only, so paging back replays the cursor that
  // opened each already-visited page. Index 0 is the first page's absent cursor.
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined]);
  const [pageIndex, setPageIndex] = useState(0);
  const [openFinding, setOpenFinding] = useState<RiskResult | null>(null);

  const { restore, optimisticallyRestoredIds } = useDismissFinding();
  const routes = useRoutes();
  const navigate = useNavigate();
  // Opening the session hands off to the same chat sheet the Risk Events log
  // uses, rather than a second copy of the transcript UI.
  const chat = useChatDetailSheet();

  const viewRule = useCallback(() => {
    void navigate(`${routes.policyCenter.href()}?tab=exclusions`);
  }, [navigate, routes]);

  const listQuery = useRiskListDismissedResults(
    { cursor: cursors[pageIndex], limit: PAGE_SIZE },
    undefined,
    { placeholderData: keepPreviousData, throwOnError: false },
  );
  // Only the expanded section joins findings to their exclusion rule, so the
  // collapsed header costs one request, not two.
  const exclusionsQuery = useRiskListExclusions(undefined, undefined, {
    enabled: expanded,
    throwOnError: false,
  });

  const exclusionsById = useMemo(() => {
    const byId = new Map<string, RiskExclusion>();
    for (const exclusion of exclusionsQuery.data?.exclusions ?? []) {
      byId.set(exclusion.id, exclusion);
    }
    return byId;
  }, [exclusionsQuery.data]);

  // True while the rows on screen belong to a different page than the one
  // pageIndex and the range label now describe — keepPreviousData holds the old
  // page up until the requested one lands.
  const showingStalePage = listQuery.isPlaceholderData;

  const fetchedResults = useMemo(
    () => listQuery.data?.results ?? [],
    [listQuery.data],
  );
  const results = useMemo(
    () =>
      fetchedResults.filter(
        (result) => !optimisticallyRestoredIds.has(result.id),
      ),
    [fetchedResults, optimisticallyRestoredIds],
  );
  // Rule suppressions carry no restore action, so they stay out of the
  // selection entirely — including out of select-all, which would otherwise
  // report a count the bulk action can't act on.
  const selectable = useMemo(() => results.filter(isRestorable), [results]);
  const selection = useRowSelection(selectable, resultId);

  const nextCursor = listQuery.data?.nextCursor;
  // The server's count still includes rows we are hiding, so discount them.
  // Every held id counts, not just the ones on this page: paging away from a
  // restored row would otherwise let the total jump back up, which reads as
  // the restore having failed. The cost is the opposite skew — once the mirror
  // catches up the server total already excludes them while the hold has not
  // yet expired, so the count can sit low by that many for the rest of the
  // window. A count that only ever settles downward is the less alarming of
  // the two. Display only: the cursor stack and pageIndex stay server-side.
  const serverTotal = listQuery.data?.totalCount ?? 0;
  const total = Math.max(
    serverTotal - Math.min(optimisticallyRestoredIds.size, serverTotal),
    0,
  );

  const restoreIds = (ids: string[]): Promise<boolean> => {
    selection.clear();
    // The rows themselves disappear via the optimistic filter above, which
    // outlives the refetch; this only puts the pager back somewhere valid,
    // since a restore shifts every page boundary after it.
    setCursors([undefined]);
    setPageIndex(0);
    return restore(ids);
  };

  const goToNextPage = () => {
    // While the previous page is still on screen, nextCursor belongs to it —
    // a second click would push that same cursor again and desync pageIndex
    // from the cursor stack. The disabled Next button covers the usual case;
    // this covers the click that lands in the same frame as the first.
    if (!nextCursor || showingStalePage || listQuery.isFetching) return;
    if (cursors[pageIndex + 1] === nextCursor) {
      setPageIndex((prev) => prev + 1);
      return;
    }
    setCursors((prev) => [...prev.slice(0, pageIndex + 1), nextCursor]);
    setPageIndex((prev) => prev + 1);
  };

  if (listQuery.isError) {
    return <SuppressedError onRetry={() => void listQuery.refetch()} />;
  }

  if (listQuery.data === undefined || total === 0) return null;

  return (
    <div className="border-border border-t pt-6">
      <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2">
        <button
          type="button"
          aria-expanded={expanded}
          onClick={() => setExpanded((prev) => !prev)}
          className="text-foreground hover:text-muted-foreground flex cursor-pointer items-center gap-2 transition-colors"
        >
          <Icon
            name={expanded ? "chevron-down" : "chevron-right"}
            className="size-4"
          />
          <span className="text-eyebrow text-foreground">
            Suppressed · {total}
          </span>
        </button>
        <span className="text-eyebrow">Not in score · Not in counts</span>
      </div>
      {expanded && (
        <div className="mt-4 space-y-3">
          <SuppressedToolbar
            selection={selection}
            hasSelectable={selectable.length > 0}
            disabled={showingStalePage}
            onRestoreSelected={() =>
              void restoreIds([...selection.selectedIds])
            }
          />
          <SuppressedRows
            results={results}
            isLoading={listQuery.isFetching && results.length === 0}
            disabled={showingStalePage}
            exclusionsById={exclusionsById}
            selection={selection}
            onRestore={(id) => void restoreIds([id])}
            onViewRule={viewRule}
            onOpen={setOpenFinding}
          />
          <SuppressedPagination
            label={rangeLabel({
              showingStalePage,
              pageStart: pageIndex * PAGE_SIZE,
              visible: results.length,
              total,
            })}
            canGoBack={pageIndex > 0 && !showingStalePage}
            canGoForward={nextCursor !== undefined && !showingStalePage}
            onBack={() => setPageIndex((prev) => prev - 1)}
            onForward={goToNextPage}
          />
        </div>
      )}
      <SuppressedFindingDrawer
        finding={openFinding}
        exclusion={
          openFinding?.exclusionId
            ? exclusionsById.get(openFinding.exclusionId)
            : undefined
        }
        onClose={() => setOpenFinding(null)}
        onRestore={(id) => {
          void restoreIds([id]).then((restored) => {
            // Only close once the finding is actually back: a failed restore
            // leaves the drawer open on the row it failed to change, next to
            // the error toast that says so.
            if (restored) setOpenFinding(null);
          });
        }}
        onViewRule={viewRule}
        onViewSession={(chatId) => {
          // Hand off rather than stack: the transcript sheet replaces this
          // drawer instead of layering a second sheet over it.
          setOpenFinding(null);
          chat.openChat(chatId);
        }}
      />
      {chat.sheet}
    </div>
  );
}

function SuppressedToolbar({
  selection,
  hasSelectable,
  disabled,
  onRestoreSelected,
}: {
  selection: RowSelection<RiskResult>;
  hasSelectable: boolean;
  disabled: boolean;
  onRestoreSelected: () => void;
}): JSX.Element | null {
  if (!hasSelectable) return null;
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="flex items-center gap-3 px-3">
        <Checkbox
          checked={selection.allState}
          disabled={disabled}
          onCheckedChange={selection.toggleAll}
          aria-label="Select all restorable findings"
        />
        <Text small muted>
          {selection.selectedCount > 0
            ? `${selection.selectedCount} selected`
            : "Select all"}
        </Text>
      </span>
      {selection.selectedCount > 0 && (
        <span className="flex items-center gap-2">
          <Button
            variant="secondary"
            size="sm"
            disabled={disabled}
            onClick={onRestoreSelected}
          >
            <Button.Text>Restore</Button.Text>
          </Button>
          <Button variant="secondary" size="sm" onClick={selection.clear}>
            <Button.Text>Clear</Button.Text>
          </Button>
        </span>
      )}
    </div>
  );
}

/** The listing failed outright. Rendering nothing here would be indistinguishable
 * from "nothing is suppressed", which is the one reading that must not be
 * guessed at — so the section announces itself and offers a retry. */
function SuppressedError({ onRetry }: { onRetry: () => void }): JSX.Element {
  return (
    <div className="border-border border-t pt-6">
      <span className="text-eyebrow text-foreground">Suppressed</span>
      <div className="border-border mt-3 flex flex-wrap items-center justify-between gap-3 border p-4">
        <Text small muted>
          Couldn't load suppressed findings.
        </Text>
        <Button variant="secondary" size="sm" onClick={onRetry}>
          <Button.Text>Retry</Button.Text>
        </Button>
      </div>
    </div>
  );
}

function SuppressedRows({
  results,
  isLoading,
  disabled,
  exclusionsById,
  selection,
  onRestore,
  onViewRule,
  onOpen,
}: {
  results: RiskResult[];
  isLoading: boolean;
  /** Rows belong to a page the pager has already moved past — show them, but
   * don't let them be acted on until the requested page lands. */
  disabled: boolean;
  exclusionsById: Map<string, RiskExclusion>;
  selection: RowSelection<RiskResult>;
  onRestore: (id: string) => void;
  onViewRule: () => void;
  onOpen: (result: RiskResult) => void;
}): JSX.Element {
  if (isLoading) {
    return (
      <div className="space-y-2">
        <Skeleton className="h-16" />
        <Skeleton className="h-16" />
        <Skeleton className="h-16" />
      </div>
    );
  }
  return (
    <div
      aria-busy={disabled}
      className={cn(
        "border-border divide-border divide-y border transition-opacity",
        disabled && "opacity-50",
      )}
    >
      {results.map((result) => (
        <SuppressedRow
          key={result.id}
          result={result}
          disabled={disabled}
          exclusion={
            result.exclusionId
              ? exclusionsById.get(result.exclusionId)
              : undefined
          }
          selection={selection}
          onRestore={onRestore}
          onViewRule={onViewRule}
          onOpen={onOpen}
        />
      ))}
    </div>
  );
}

function SuppressedRow({
  result,
  disabled,
  exclusion,
  selection,
  onRestore,
  onViewRule,
  onOpen,
}: {
  result: RiskResult;
  disabled: boolean;
  exclusion: RiskExclusion | undefined;
  selection: RowSelection<RiskResult>;
  onRestore: (id: string) => void;
  onViewRule: () => void;
  onOpen: (result: RiskResult) => void;
}): JSX.Element {
  const restorable = isRestorable(result);
  const detail = suppressionDetail(result, exclusion);
  const open = () => {
    if (disabled) return;
    onOpen(result);
  };
  return (
    // Clicking the row opens the detail drawer, but the checkbox and the
    // action button are sibling controls rather than descendants of the
    // role="button" region: nesting them inside a button would flatten them
    // out of the accessibility tree (button descendants are presentational).
    // The outer click handler covers the rest of the row — the cell padding
    // around those controls — and skips any click that landed on one of them.
    <div
      className={cn(
        "bg-card divide-border flex w-full items-stretch divide-x transition-colors",
        disabled ? "cursor-default" : "hover:bg-muted/40 cursor-pointer",
      )}
      onClick={(e) => {
        if ((e.target as HTMLElement).closest("button, a, [role='checkbox']")) {
          return;
        }
        open();
      }}
    >
      <div className="flex w-12 shrink-0 items-center justify-center px-3">
        {restorable ? (
          <Checkbox
            checked={selection.isSelected(result.id)}
            disabled={disabled}
            onCheckedChange={() => selection.toggle(result.id)}
            aria-label="Select suppressed finding"
          />
        ) : (
          // Rule-suppressed rows cannot be restored individually, so their
          // checkbox is present for visual alignment but permanently disabled.
          <Checkbox
            checked={false}
            disabled
            aria-label="Rule-suppressed findings cannot be selected"
          />
        )}
      </div>
      <div
        role="button"
        tabIndex={0}
        aria-label={`View suppressed finding ${getRuleTitleFallback(result.ruleId)}`}
        onClick={open}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            open();
          }
        }}
        className="divide-border flex min-w-0 flex-1 items-stretch divide-x text-left"
      >
        <div className="flex min-w-0 flex-1 flex-col justify-center gap-1 px-4 py-3">
          <Text className="truncate font-semibold">
            {getRuleTitleFallback(result.ruleId)}
          </Text>
          <div className="flex min-w-0 flex-wrap items-center gap-3">
            <CategoryLabel source={result.source} ruleId={result.ruleId} />
            {result.chatTitle && (
              <span
                className="text-muted-foreground min-w-0 truncate font-mono text-xs"
                title={result.chatTitle}
              >
                {result.chatTitle}
              </span>
            )}
          </div>
        </div>
        <div className="flex w-72 shrink-0 flex-col justify-center gap-1 px-4 py-3">
          {detail && (
            <Text small className="truncate" title={detail}>
              {detail}
            </Text>
          )}
          <span className="text-muted-foreground font-mono text-xs">
            {SUPPRESSION_REASON_LABEL[suppressionReason(result)]}
            {result.suppressedAt &&
              ` · ${format(result.suppressedAt, "MMM d")}`}
          </span>
        </div>
      </div>
      <div className="flex w-32 shrink-0 items-center justify-start px-4 py-3">
        {restorable ? (
          <Button
            variant="tertiary"
            size="sm"
            disabled={disabled}
            onClick={() => onRestore(result.id)}
          >
            <Button.Text>Restore</Button.Text>
          </Button>
        ) : (
          // A rule suppression is undone by editing the exclusion, not per
          // finding, so the row points at the Exclusion rules tab instead.
          <Button
            variant="tertiary"
            size="sm"
            disabled={disabled}
            onClick={onViewRule}
          >
            <Button.Text>View rule</Button.Text>
          </Button>
        )}
      </div>
    </div>
  );
}

/**
 * Which rows the reader is looking at. While a stale page is on screen the
 * positions are unknowable — pageIndex has moved on but the rows have not — so
 * the label says so rather than naming a range that belongs to neither page.
 */
function rangeLabel({
  showingStalePage,
  pageStart,
  visible,
  total,
}: {
  showingStalePage: boolean;
  /** Zero-based index of this page's first row within the whole listing. */
  pageStart: number;
  visible: number;
  total: number;
}): string {
  if (showingStalePage) return "Loading…";
  // Every row on the page is optimistically hidden — there is no range to name,
  // but the total still describes what is left elsewhere.
  if (visible === 0) return `0 of ${total} suppressed`;
  const start = Math.min(pageStart + 1, total);
  const end = Math.min(pageStart + visible, total);
  return `${start}–${end} of ${total} suppressed`;
}

function SuppressedPagination({
  label,
  canGoBack,
  canGoForward,
  onBack,
  onForward,
}: {
  label: string;
  canGoBack: boolean;
  canGoForward: boolean;
  onBack: () => void;
  onForward: () => void;
}): JSX.Element {
  return (
    <div className="flex items-center justify-between">
      <Text small muted>
        {label}
      </Text>
      <div className="flex items-center gap-1">
        <Button
          variant="tertiary"
          size="sm"
          disabled={!canGoBack}
          onClick={onBack}
        >
          <Button.Text>Previous</Button.Text>
        </Button>
        <Button
          variant="tertiary"
          size="sm"
          disabled={!canGoForward}
          onClick={onForward}
        >
          <Button.Text>Next</Button.Text>
        </Button>
      </div>
    </div>
  );
}
