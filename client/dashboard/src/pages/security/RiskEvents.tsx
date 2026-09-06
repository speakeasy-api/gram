import { IdentityLink } from "@/components/identity-link";
import { identityRefForUserKey } from "@/lib/identity-urn";
import { LogWorkbench } from "@/components/log-workbench";
import {
  defineFilters,
  useFilterState,
  type FilterValue,
} from "@/components/filters";
import { Page } from "@/components/page-layout";
import { BulkActionBar } from "@/components/ui/bulk-action-bar";
import { Checkbox } from "@/components/ui/Checkbox";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { useSdkClient } from "@/contexts/Sdk";
import { useRowSelection, type RowSelection } from "@/hooks/useRowSelection";
import { useMeasuredHeight } from "@/hooks/useMeasuredHeight";
import { cn } from "@/lib/utils";
import { ChatDetailSheet } from "@/pages/chatLogs/ChatDetailPanel";
import { getPresetRange } from "@/elements";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { useAssistantsList } from "@gram/client/react-query/assistantsList.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useRiskOverview } from "@gram/client/react-query/riskOverview.js";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useVirtualizer } from "@tanstack/react-virtual";
import { History } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, type RefObject } from "react";
import { useSearchParams } from "react-router";
import { toast } from "sonner";
import { useDismissFinding } from "./useDismissFinding";
import { useSetupExclusionRule } from "./useSetupExclusionRule";
import {
  CategoryLabel,
  EventMatchDialog,
  MaskedMatch,
  RevealAllProvider,
  RevealAllToggle,
  RuleLabel,
} from "./risk-ui";
import {
  isJudgeSource,
  isShadowMcpSource,
  scoreToRating,
  SEVERITY_RATING_LABEL,
  type SeverityRating,
} from "./risk-utils";

// Signal-list layout (Risk Watchdog idiom): the severity score leads each row
// as a big serif numeral, so it sits directly after the checkbox.
//
// Category and rule share one column, badge over rule title, matching the
// findings table on the risk overview category drill-down. Judge findings own a
// single rule whose name only restates the category, so they render the badge
// alone rather than an empty Rule cell.
//
// Evidence gets the widest track: for judge findings it holds a sentence or two
// of rationale, where every other column holds a label.
const RISK_EVENTS_GRID =
  "grid grid-cols-[28px_88px_172px_minmax(0,1.3fr)_minmax(0,0.85fr)_minmax(0,0.85fr)_minmax(0,2.4fr)_minmax(0,0.9fr)_110px] gap-3";

// Signal severity palette: band → text / row-edge classes. Colors are
// token-derived — brand red hsl(4,67%,47%) (--color-brand-red-500) is reserved
// for critical, the feedback-orange ramp covers high/medium, low stays neutral
// ink. Applied to the score numeral, the severity word, and the row's 2px
// left edge.
const SEVERITY_TEXT: Record<SeverityRating, string> = {
  critical: "text-[var(--color-brand-red-500)]",
  high: "text-[var(--color-feedback-orange-600)]",
  medium: "text-[var(--color-feedback-orange-400)]",
  low: "text-foreground",
};

const SEVERITY_EDGE: Record<SeverityRating, string> = {
  critical: "border-l-[var(--color-brand-red-500)]",
  high: "border-l-[var(--color-feedback-orange-600)]",
  medium: "border-l-[var(--color-feedback-orange-400)]",
  low: "border-l-border",
};

// Ratings key off the rounded value we display, so a score sitting just below
// a band boundary (e.g. 3.96 → shown as "4.0") never renders in a color that
// disagrees with the band its displayed value falls in.
function displayedScoreRating(score: number): {
  displayed: number;
  rating: SeverityRating;
} {
  const displayed = Math.round(score * 10) / 10;
  return { displayed, rating: scoreToRating(displayed) };
}

// The signal-list score block: thin display-serif numeral over a mono
// uppercase severity word, both colored by band. The numeral carries the exact
// policy score while the word makes the band scannable.
function SignalScore({ score }: { score: number | undefined }): JSX.Element {
  if (score == null) {
    return <span className="text-muted-foreground text-sm">-</span>;
  }
  const { displayed, rating } = displayedScoreRating(score);
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <span
        className={cn(
          "font-display text-3xl leading-none font-thin tabular-nums",
          SEVERITY_TEXT[rating],
        )}
      >
        {displayed.toFixed(1)}
      </span>
      <span
        className={cn(
          "font-mono text-2xs tracking-widest uppercase",
          SEVERITY_TEXT[rating],
        )}
      >
        {SEVERITY_RATING_LABEL[rating]}
      </span>
    </div>
  );
}

// Strongly-typed filter schema for Risk Events. `policy_id` and the date range
// are pinned (always visible in the bar); the rest live behind "More filters".
// `listRiskResults` already accepts from/to, so the date range needs no backend
// change. (Source isn't a list param, so it's intentionally omitted here.)
const RISK_FILTERS = defineFilters([
  { id: "policy_id", label: "Policy", kind: "select", pinned: true },
  { id: "date", label: "Date range", kind: "daterange", pinned: true },
  {
    id: "rule_id",
    label: "Rule ID",
    kind: "text",
    placeholder: "Rule ID contains...",
  },
  {
    id: "user_id",
    label: "User",
    kind: "text",
    placeholder: "User contains...",
  },
  {
    id: "unique",
    label: "Unique matches only",
    kind: "boolean",
    description: "Keep the latest row per policy, rule and matched value.",
  },
  { id: "assistant", label: "Assistant", kind: "select" },
]);

// Sentinel option value for the assistant filter meaning "chats with no
// assistant link" — maps to the API's non_assistant flag rather than an
// assistant_id. Assistant ids are UUIDs, so this can't collide.
const NO_ASSISTANT = "none";

export default function RiskEvents(): JSX.Element {
  const client = useSdkClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedChatId = searchParams.get("chat_id");
  const containerRef = useRef<HTMLDivElement>(null);
  const headerMeasure = useMeasuredHeight<HTMLDivElement>();

  const { values, setValue, clearValue, clearAll } =
    useFilterState(RISK_FILTERS);
  const policyFilter = values.policy_id ?? "";
  const ruleFilter = values.rule_id;
  const userFilter = values.user_id;
  const uniqueOnly = values.unique;
  // "No assistant" pre-selects the non-assistant events (the API's
  // non_assistant flag); any other value scopes to that assistant's chats.
  const assistantFilter = values.assistant ?? "";
  const nonAssistantOnly = assistantFilter === NO_ASSISTANT;
  const assistantId =
    assistantFilter && assistantFilter !== NO_ASSISTANT
      ? assistantFilter
      : undefined;

  // The date range maps to the endpoint's from/to. A null preset with no custom
  // range means "all time" (no from/to sent) — Risk Events' previous behavior.
  const { from, to } = useMemo(() => {
    const d = values.date;
    if (d.customRange) return d.customRange;
    if (d.preset) return getPresetRange(d.preset);
    return { from: undefined, to: undefined };
  }, [values.date]);

  const setSelectedChatId = useCallback(
    (chatId: string | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (chatId) {
            next.set("chat_id", chatId);
          } else {
            next.delete("chat_id");
          }
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const { data: policiesData, isLoading: policiesLoading } =
    useRiskListPolicies();
  const policies = useMemo(
    () => policiesData?.policies ?? [],
    [policiesData?.policies],
  );

  // Powers the rule_id filter autocomplete: surface only rules that actually
  // have findings in this project's recent window.
  const { data: overviewData } = useRiskOverview({}, undefined, {
    throwOnError: false,
  });
  const ruleSuggestions = useMemo(
    () => (overviewData?.topRules ?? []).map((r) => r.ruleId).filter(Boolean),
    [overviewData?.topRules],
  );
  const policyNameById = useMemo(() => {
    const m = new Map<string, string>();
    for (const policy of policies) {
      if (policy.name && policy.name.trim() !== "") {
        m.set(policy.id, policy.name);
      }
    }
    return m;
  }, [policies]);

  // Findings surface their owning policy's severity score, resolved by
  // risk_policy_id at read time (score is a policy attribute, not stored on the
  // finding).
  const policyScoreById = useMemo(() => {
    const m = new Map<string, number>();
    for (const policy of policies) {
      m.set(policy.id, policy.score);
    }
    return m;
  }, [policies]);

  // The policy currently selected in the filter, if any. When it's disabled the
  // list still returns its historical findings (the backend drops the
  // enabled-only filter for explicit policy selections), so we surface a notice
  // that the user is viewing data for an inactive policy.
  const selectedPolicy = useMemo(
    () => policies.find((p) => p.id === policyFilter),
    [policies, policyFilter],
  );
  const viewingInactivePolicy =
    selectedPolicy != null && selectedPolicy.enabled === false;

  // Powers the assistant filter options; "No assistant" is always offered so
  // findings missing user attribution can be surfaced even before any
  // assistant exists in the project.
  const { data: assistantsData } = useAssistantsList(undefined, undefined, {
    throwOnError: false,
  });
  const assistants = useMemo(
    () => assistantsData?.assistants ?? [],
    [assistantsData?.assistants],
  );

  // Page-supplied option lists for the schema's select/text dimensions.
  // Disabled policies stay selectable — they hold historical findings — but are
  // labelled "(inactive)" so the distinction is clear in the dropdown.
  const filterOptions = useMemo(
    () => ({
      policy_id: policies.map((p) => ({
        label: p.enabled === false ? `${p.name} (inactive)` : p.name,
        value: p.id,
      })),
      rule_id: ruleSuggestions.map((r) => ({ label: r, value: r })),
      assistant: [
        { label: "No assistant", value: NO_ASSISTANT },
        ...assistants.map((a) => ({ label: a.name, value: a.id })),
      ],
    }),
    [policies, ruleSuggestions, assistants],
  );

  const fromIso = from?.toISOString();
  const toIso = to?.toISOString();

  // Reset the virtualized list to the top whenever a filter changes, so users
  // don't stay at a stale offset and miss the newly filtered results.
  useEffect(() => {
    containerRef.current?.scrollTo({ top: 0 });
  }, [
    policyFilter,
    ruleFilter,
    userFilter,
    uniqueOnly,
    assistantFilter,
    fromIso,
    toIso,
  ]);

  const resultsQuery = useInfiniteQuery({
    queryKey: [
      "risk",
      "results",
      "list",
      policyFilter,
      ruleFilter,
      userFilter,
      uniqueOnly,
      assistantFilter,
      fromIso,
      toIso,
    ],
    queryFn: async ({ pageParam }) => {
      return client.risk.results.list({
        cursor: pageParam,
        limit: 50,
        policyId: policyFilter || undefined,
        ruleId: ruleFilter || undefined,
        userId: userFilter || undefined,
        uniqueMatch: uniqueOnly || undefined,
        nonAssistant: nonAssistantOnly || undefined,
        assistantId,
        from,
        to,
      });
    },
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
  });

  const results = useMemo(
    () => resultsQuery.data?.pages.flatMap((p) => p.results) ?? [],
    [resultsQuery.data],
  );
  const totalCount = resultsQuery.data?.pages[0]?.totalCount ?? results.length;
  const isInitialLoading = policiesLoading || resultsQuery.isLoading;

  const { dismiss, isOptimisticallyDismissed } = useDismissFinding();
  const visibleResults = useMemo(
    () => results.filter((r) => !isOptimisticallyDismissed(r.id)),
    [results, isOptimisticallyDismissed],
  );
  const selection = useRowSelection(visibleResults, (r) => r.id);
  const handleDismissSelected = useCallback(() => {
    const toDismiss = selection.selectedItems;
    if (toDismiss.length === 0) return;
    dismiss(toDismiss);
    selection.clear();
  }, [selection, dismiss]);

  const exclusionRule = useSetupExclusionRule();
  const handleSetupExclusionSelected = useCallback(() => {
    const selected = selection.selectedItems;
    if (selected.length === 0) return;
    // Deliberately doesn't clear the selection (unlike handleDismissSelected)
    // so retrying still works if the sheet is canceled.
    exclusionRule.open(selected);
  }, [selection, exclusionRule]);

  const handleScroll = useCallback(
    (e: React.UIEvent<HTMLDivElement>) => {
      const container = e.currentTarget;
      const distanceFromBottom =
        container.scrollHeight - (container.scrollTop + container.clientHeight);

      if (resultsQuery.isFetchingNextPage || resultsQuery.isFetching) return;
      if (!resultsQuery.hasNextPage) return;

      if (distanceFromBottom < 200) {
        void resultsQuery.fetchNextPage();
      }
    },
    [resultsQuery],
  );

  return (
    <RevealAllProvider>
      <LogWorkbench
        eyebrow="Secure"
        title="Risk Events"
        stage="beta"
        description="Review policy findings across recent analyzed chats."
        actions={
          <RevealAllToggle
            // Match the "More filters"/"Refresh" toolbar triggers (hairline
            // secondary button, h-10, mono label) instead of the component's
            // hand-rolled default.
            className="border-neutral-default text-btn-secondary hover:bg-btn-secondary hover:text-btn-secondary-hover inline-flex h-10 items-center gap-2 border bg-transparent px-4 font-mono text-sm tracking-[-0.01em] transition-all"
          />
        }
        filters={
          <Page.Toolbar>
            <Page.Toolbar.Filters
              schema={RISK_FILTERS}
              values={values}
              optionsById={filterOptions}
              onChange={setValue as (id: string, value: FilterValue) => void}
              onClear={clearValue as (id: string) => void}
              onClearAll={clearAll}
            />
            <Page.Toolbar.Refresh
              onRefresh={() => void resultsQuery.refetch()}
              isRefreshing={resultsQuery.isFetching}
            />
          </Page.Toolbar>
        }
        status={
          <>
            {viewingInactivePolicy ? (
              <InactivePolicyNotice policyName={selectedPolicy?.name} />
            ) : null}
            {exclusionRule.sheet}
            {resultsQuery.isFetching && results.length > 0 ? (
              <div className="bg-primary/20 h-1 shrink-0">
                <div className="bg-primary h-full animate-pulse" />
              </div>
            ) : null}
          </>
        }
        header={
          <div className="relative min-w-[1200px]">
            <BulkActionBar
              selectedCount={selection.selectedCount}
              actions={[
                {
                  label: "Suppress Once",
                  onClick: handleDismissSelected,
                },
                {
                  label: "Create Rule",
                  onClick: handleSetupExclusionSelected,
                },
              ]}
              leftOffsetPx={28}
              heightPx={headerMeasure.height}
            />
            <RiskEventsHeader
              selection={selection}
              headerRef={headerMeasure.ref}
            />
          </div>
        }
        footer={
          results.length > 0 ? (
            <RiskEventsFooter
              count={results.length}
              totalCount={totalCount}
              hasNextPage={resultsQuery.hasNextPage}
              isFetchingNextPage={resultsQuery.isFetchingNextPage}
              onLoadMore={() => {
                void resultsQuery.fetchNextPage();
              }}
            />
          ) : null
        }
        detail={
          <ChatDetailSheet
            chatId={selectedChatId}
            onClose={() => setSelectedChatId(null)}
            onDelete={() => setSelectedChatId(null)}
            riskFocus
          />
        }
        scrollRef={containerRef}
        onScroll={handleScroll}
        surfaceClassName="overflow-x-auto"
        contentClassName="min-w-[1200px]"
      >
        <RiskEventsRows
          error={resultsQuery.error}
          isLoading={isInitialLoading}
          results={visibleResults}
          policyNameById={policyNameById}
          policyScoreById={policyScoreById}
          scrollRef={containerRef}
          onSelectChat={setSelectedChatId}
          selection={selection}
          onDismiss={(r) => dismiss([r])}
          onSetupExclusion={(r) => exclusionRule.open([r])}
        />
      </LogWorkbench>
    </RevealAllProvider>
  );
}

// Shown when the active policy filter points at a disabled ("turned off")
// policy. Those policies no longer produce new findings, so the list is purely
// historical — make that explicit rather than leaving users to assume the data
// is current.
function InactivePolicyNotice({
  policyName,
}: {
  policyName: string | undefined;
}) {
  return (
    <div className="text-warning flex shrink-0 items-center gap-2 border-b px-5 py-2 text-sm">
      <History className="size-4 shrink-0" />
      <span>
        {policyName ? (
          <>
            <span className="font-medium">{policyName}</span> is no longer
            active.
          </>
        ) : (
          "This policy is no longer active."
        )}{" "}
        You're viewing historical findings from when it was enabled.
      </span>
    </div>
  );
}

function RiskEventsHeader({
  selection,
  headerRef,
}: {
  selection: RowSelection<RiskResult>;
  headerRef: (node: HTMLDivElement | null) => void;
}) {
  return (
    <div
      ref={headerRef}
      className={cn(
        RISK_EVENTS_GRID,
        // whitespace-nowrap keeps two-word labels ("Session Name") on one
        // line when their fr track compresses.
        "text-eyebrow bg-muted/30 shrink-0 items-center border-b px-5 py-2.5 whitespace-nowrap",
      )}
    >
      <div className="min-w-0">
        <Checkbox
          checked={selection.allState}
          onCheckedChange={() => selection.toggleAll()}
          aria-label="Select all loaded findings"
        />
      </div>
      <div className="min-w-0">Severity</div>
      <div className="min-w-0">Timestamp</div>
      <div className="min-w-0">Category / Rule</div>
      <div className="min-w-0 whitespace-nowrap">Session Name</div>
      <div className="min-w-0">User</div>
      <div className="min-w-0">Evidence</div>
      <div className="min-w-0">Policy</div>
      <div className="flex min-w-0 justify-center">Actions</div>
    </div>
  );
}

function RiskEventsRows({
  error,
  isLoading,
  results,
  policyNameById,
  policyScoreById,
  scrollRef,
  onSelectChat,
  selection,
  onDismiss,
  onSetupExclusion,
}: {
  error: Error | null;
  isLoading: boolean;
  results: RiskResult[];
  policyNameById: Map<string, string>;
  policyScoreById: Map<string, number>;
  scrollRef: RefObject<HTMLDivElement | null>;
  onSelectChat: (chatId: string | null) => void;
  selection: RowSelection<RiskResult>;
  onDismiss: (result: RiskResult) => void;
  onSetupExclusion: (result: RiskResult) => void;
}) {
  const rowVirtualizer = useVirtualizer({
    count: results.length,
    getScrollElement: () => scrollRef.current,
    // Rows stack category over rule and can wrap two lines of rationale;
    // measureElement corrects each row, this is just the pre-measure estimate.
    estimateSize: () => 68,
    overscan: 12,
  });

  if (error) {
    return (
      <div className="flex flex-col items-center gap-3 py-12">
        <div className="bg-destructive/10 flex size-12 items-center justify-center">
          <Icon name="circle-alert" className="text-destructive size-6" />
        </div>
        <span className="text-foreground font-medium">
          Error loading risk events
        </span>
        <span className="text-muted-foreground max-w-sm text-center text-sm">
          {error.message}
        </span>
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
        <Icon name="loader-circle" className="size-5 animate-spin" />
        <span>Loading risk events...</span>
      </div>
    );
  }

  if (results.length === 0) {
    return (
      <div className="flex flex-col items-center gap-3 py-12 text-center">
        <div className="bg-muted flex size-12 items-center justify-center">
          <Icon name="inbox" className="text-muted-foreground size-6" />
        </div>
        <span className="text-foreground font-medium">
          No risk events found
        </span>
        <span className="text-muted-foreground max-w-sm text-sm">
          Findings will appear here as messages are analyzed.
        </span>
      </div>
    );
  }

  return (
    <div
      className="relative w-full"
      style={{ height: `${rowVirtualizer.getTotalSize()}px` }}
    >
      {rowVirtualizer.getVirtualItems().map((virtualRow) => {
        const result = results[virtualRow.index];
        if (!result) return null;

        return (
          <div
            key={result.id}
            ref={rowVirtualizer.measureElement}
            data-index={virtualRow.index}
            className="absolute top-0 left-0 w-full"
            style={{ transform: `translateY(${virtualRow.start}px)` }}
          >
            <RiskEventsRow
              result={result}
              policyName={policyNameById.get(result.policyId)}
              policyScore={policyScoreById.get(result.policyId)}
              onSelectChat={onSelectChat}
              selection={selection}
              onDismiss={onDismiss}
              onSetupExclusion={onSetupExclusion}
            />
          </div>
        );
      })}
    </div>
  );
}

function RiskEventsRow({
  result,
  policyName,
  policyScore,
  onSelectChat,
  selection,
  onDismiss,
  onSetupExclusion,
}: {
  result: RiskResult;
  policyName: string | undefined;
  policyScore: number | undefined;
  onSelectChat: (chatId: string | null) => void;
  selection: RowSelection<RiskResult>;
  onDismiss: (result: RiskResult) => void;
  onSetupExclusion: (result: RiskResult) => void;
}) {
  const isShadowMCP = isShadowMcpSource(result.source);
  const isEventSource = isJudgeSource(result.source);
  // The 2px left edge carries the severity band color; rows whose policy
  // hasn't loaded a score keep a transparent edge so the grid stays aligned.
  const edgeRating =
    policyScore != null ? displayedScoreRating(policyScore).rating : null;

  // A row click opens the chat only when the gesture both starts and ends inside
  // the row. This rejects the stray click Radix's outside-dismiss sends here:
  // closing the View-event dialog by clicking its overlay fires pointerdown on
  // the (portaled) overlay, which unmounts, so the trailing click lands on a row
  // cell and would otherwise open the chat.
  const pointerDownInsideRef = useRef(false);

  const handleShare = useCallback(async () => {
    if (!result.chatId) return;
    const url = new URL(window.location.href);
    url.searchParams.set("chat_id", result.chatId);
    try {
      await navigator.clipboard.writeText(url.toString());
      toast.success("Link copied to clipboard");
    } catch {
      toast.error("Failed to copy link");
    }
  }, [result.chatId]);

  const rowActions: Action[] = [
    ...(result.chatId
      ? [{ label: "Copy link", onClick: () => void handleShare() }]
      : []),
    { label: "Suppress Once", onClick: () => onDismiss(result) },
    { label: "Create Rule", onClick: () => onSetupExclusion(result) },
  ];

  return (
    <div
      role={result.chatId ? "button" : undefined}
      tabIndex={result.chatId ? 0 : undefined}
      className={cn(
        RISK_EVENTS_GRID,
        "hover:bg-muted/30 w-full items-center border-b border-l-2 px-5 py-3 text-left text-sm transition-colors",
        edgeRating ? SEVERITY_EDGE[edgeRating] : "border-l-transparent",
        !result.chatId && "cursor-default",
      )}
      onPointerDown={(e) => {
        pointerDownInsideRef.current = e.currentTarget.contains(
          e.target as Node,
        );
      }}
      onClick={(e) => {
        const startedInside = pointerDownInsideRef.current;
        pointerDownInsideRef.current = false;
        // Ignore the trailing click from an outside-dismiss (pointerdown never
        // landed on the row).
        if (!startedInside) return;
        // The row wraps its own interactive controls (match reveal, the
        // View-event dialog trigger, copy-link); a click on one of those must
        // not also open the chat. stopPropagation on those children doesn't
        // reliably stop this handler under React's event delegation, so guard on
        // the real target too.
        if ((e.target as HTMLElement).closest("button, a")) return;
        if (result.chatId) {
          onSelectChat(result.chatId);
        }
      }}
      onKeyDown={(e) => {
        // Only the row itself activates on Enter/Space. Key events bubbling up
        // from a focused child control (match reveal, the event dialog trigger,
        // copy-link) must reach that control instead — preventing them here
        // would swallow the control's own activation and wrongly open the chat.
        if (e.target !== e.currentTarget) return;
        if (!result.chatId) return;
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onSelectChat(result.chatId);
        }
      }}
    >
      <div className="min-w-0">
        <Checkbox
          checked={selection.isSelected(result.id)}
          onCheckedChange={() => selection.toggle(result.id)}
          aria-label="Select finding"
        />
      </div>
      <div className="min-w-0">
        <SignalScore score={policyScore} />
      </div>
      <div className="text-muted-foreground min-w-0 font-mono text-xs">
        {result.createdAt ? new Date(result.createdAt).toLocaleString() : "-"}
      </div>
      <div className="flex min-w-0 flex-col gap-0.5">
        <CategoryLabel source={result.source} ruleId={result.ruleId} />
        <RuleLabel source={result.source} ruleId={result.ruleId} />
      </div>
      <div className="text-muted-foreground min-w-0 truncate font-mono text-xs">
        {result.chatTitle ?? "Untitled"}
      </div>
      <div className="text-muted-foreground min-w-0 truncate font-mono text-xs">
        <IdentityLink identifier={identityRefForUserKey(result.userId)}>
          {result.userId ?? "-"}
        </IdentityLink>
      </div>
      {/* Judge rationale wraps to two lines, so this cell can't clip to one. */}
      <div className={cn("min-w-0", !isEventSource && "truncate")}>
        {isShadowMCP && result.matchRedacted ? (
          // Square hairline mono tag — the fingerprint reads as a chip, not
          // prose.
          <span
            className="border-border inline-block max-w-full truncate border px-1.5 py-0.5 font-mono text-xs"
            title={result.matchRedacted}
          >
            {result.matchRedacted}
          </span>
        ) : isEventSource ? (
          <EventMatchDialog
            resultId={result.id}
            matchRedacted={result.matchRedacted}
            rationale={result.description}
          />
        ) : (
          <MaskedMatch
            resultId={result.id}
            matchRedacted={result.matchRedacted}
          />
        )}
      </div>
      <div
        className="text-muted-foreground min-w-0 truncate font-mono text-xs"
        title={policyName}
      >
        {policyName ?? "-"}
      </div>
      <div
        className="flex min-w-0 justify-center"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => e.stopPropagation()}
      >
        <MoreActions actions={rowActions} />
      </div>
    </div>
  );
}

function RiskEventsFooter({
  count,
  totalCount,
  hasNextPage,
  isFetchingNextPage,
  onLoadMore,
}: {
  count: number;
  totalCount: number;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  onLoadMore: () => void;
}) {
  return (
    <div className="bg-muted/30 text-muted-foreground flex shrink-0 items-center justify-between gap-4 border-t px-5 py-3 text-sm">
      <span>
        Showing {count.toLocaleString()} of {totalCount.toLocaleString()}{" "}
        {totalCount === 1 ? "finding" : "findings"}
        {hasNextPage && " - Scroll to load more"}
      </span>
      {hasNextPage ? (
        <Button
          variant="tertiary"
          size="sm"
          disabled={isFetchingNextPage}
          onClick={onLoadMore}
        >
          {isFetchingNextPage ? "Loading..." : "Load More"}
        </Button>
      ) : null}
    </div>
  );
}
