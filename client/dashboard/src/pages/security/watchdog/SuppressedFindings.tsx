import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Icon } from "@/components/ui/Icon";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useRowSelection, type RowSelection } from "@/hooks/useRowSelection";
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

  const { restore } = useDismissFinding();
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

  const results = useMemo(
    () => listQuery.data?.results ?? [],
    [listQuery.data],
  );
  // Rule suppressions carry no restore action, so they stay out of the
  // selection entirely — including out of select-all, which would otherwise
  // report a count the bulk action can't act on.
  const selectable = useMemo(() => results.filter(isRestorable), [results]);
  const selection = useRowSelection(selectable, resultId);

  const total = listQuery.data?.totalCount ?? 0;
  const nextCursor = listQuery.data?.nextCursor;

  const restoreIds = (ids: string[]): Promise<boolean> => {
    selection.clear();
    // Restoring shifts every row after it, which invalidates the cursors held
    // for the pages ahead. Snapping back to the first page is both correct and
    // where the newest suppressions are.
    setCursors([undefined]);
    setPageIndex(0);
    return restore(ids);
  };

  const goToNextPage = () => {
    if (!nextCursor) return;
    setCursors((prev) => [...prev.slice(0, pageIndex + 1), nextCursor]);
    setPageIndex((prev) => prev + 1);
  };

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
            onRestoreSelected={() =>
              void restoreIds([...selection.selectedIds])
            }
          />
          <SuppressedRows
            results={results}
            isLoading={listQuery.isFetching && results.length === 0}
            exclusionsById={exclusionsById}
            selection={selection}
            onRestore={(id) => void restoreIds([id])}
            onViewRule={viewRule}
            onOpen={setOpenFinding}
          />
          <SuppressedPagination
            rangeStart={pageIndex * PAGE_SIZE + 1}
            rangeEnd={pageIndex * PAGE_SIZE + results.length}
            total={total}
            canGoBack={pageIndex > 0}
            canGoForward={nextCursor !== undefined}
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
  onRestoreSelected,
}: {
  selection: RowSelection<RiskResult>;
  hasSelectable: boolean;
  onRestoreSelected: () => void;
}): JSX.Element | null {
  if (!hasSelectable) return null;
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="flex items-center gap-3 px-3">
        <Checkbox
          checked={selection.allState}
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
          <Button variant="secondary" size="sm" onClick={onRestoreSelected}>
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

function SuppressedRows({
  results,
  isLoading,
  exclusionsById,
  selection,
  onRestore,
  onViewRule,
  onOpen,
}: {
  results: RiskResult[];
  isLoading: boolean;
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
    <div className="border-border divide-border divide-y border">
      {results.map((result) => (
        <SuppressedRow
          key={result.id}
          result={result}
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
  exclusion,
  selection,
  onRestore,
  onViewRule,
  onOpen,
}: {
  result: RiskResult;
  exclusion: RiskExclusion | undefined;
  selection: RowSelection<RiskResult>;
  onRestore: (id: string) => void;
  onViewRule: () => void;
  onOpen: (result: RiskResult) => void;
}): JSX.Element {
  const restorable = isRestorable(result);
  const detail = suppressionDetail(result, exclusion);
  return (
    // Clicking the row opens the detail drawer, but the checkbox and the
    // action button are sibling controls rather than descendants of the
    // role="button" region: nesting them inside a button would flatten them
    // out of the accessibility tree (button descendants are presentational).
    // The outer click handler covers the rest of the row — the cell padding
    // around those controls — and skips any click that landed on one of them.
    <div
      className="bg-card hover:bg-muted/40 divide-border flex w-full cursor-pointer items-stretch divide-x transition-colors"
      onClick={(e) => {
        if ((e.target as HTMLElement).closest("button, a, [role='checkbox']")) {
          return;
        }
        onOpen(result);
      }}
    >
      <div className="flex w-12 shrink-0 items-center justify-center px-3">
        {restorable ? (
          <Checkbox
            checked={selection.isSelected(result.id)}
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
        onClick={() => onOpen(result)}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            onOpen(result);
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
            onClick={() => onRestore(result.id)}
          >
            <Button.Text>Restore</Button.Text>
          </Button>
        ) : (
          // A rule suppression is undone by editing the exclusion, not per
          // finding, so the row points at the Exclusion rules tab instead.
          <Button variant="tertiary" size="sm" onClick={onViewRule}>
            <Button.Text>View rule</Button.Text>
          </Button>
        )}
      </div>
    </div>
  );
}

function SuppressedPagination({
  rangeStart,
  rangeEnd,
  total,
  canGoBack,
  canGoForward,
  onBack,
  onForward,
}: {
  rangeStart: number;
  rangeEnd: number;
  total: number;
  canGoBack: boolean;
  canGoForward: boolean;
  onBack: () => void;
  onForward: () => void;
}): JSX.Element {
  return (
    <div className="flex items-center justify-between">
      <Text small muted>
        {rangeStart}–{rangeEnd} of {total} suppressed
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
