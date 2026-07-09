import { LogWorkbench } from "@/components/log-workbench";
import {
  defineFilters,
  useFilterState,
  type FilterValue,
} from "@/components/filters";
import { Page } from "@/components/page-layout";
import { useSdkClient } from "@/contexts/Sdk";
import { cn } from "@/lib/utils";
import { ChatDetailSheet } from "@/pages/chatLogs/ChatDetailPanel";
import { getPresetRange } from "@gram-ai/elements";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useRiskOverview } from "@gram/client/react-query/riskOverview.js";
import { Button } from "@/components/ui/moonshine";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useVirtualizer } from "@tanstack/react-virtual";
import {
  CircleAlert,
  History,
  Inbox,
  LoaderCircle,
  Share2,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, type RefObject } from "react";
import { useSearchParams } from "react-router";
import { toast } from "sonner";
import {
  CategoryLabel,
  MaskedMatch,
  RevealAllProvider,
  RevealAllToggle,
  RuleLabel,
} from "./risk-ui";

const RISK_EVENTS_GRID =
  "grid grid-cols-[172px_minmax(0,0.9fr)_minmax(0,1fr)_minmax(0,1.15fr)_minmax(0,1fr)_minmax(0,1.25fr)_minmax(0,1.1fr)_110px] gap-3";

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
  { id: "unique", label: "Unique matches only", kind: "boolean" },
]);

export default function RiskEvents(): JSX.Element {
  const client = useSdkClient();
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedChatId = searchParams.get("chat_id");
  const containerRef = useRef<HTMLDivElement>(null);

  const { values, setValue, clearValue, clearAll } =
    useFilterState(RISK_FILTERS);
  const policyFilter = values.policy_id ?? "";
  const ruleFilter = values.rule_id;
  const userFilter = values.user_id;
  const uniqueOnly = values.unique;

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
    }),
    [policies, ruleSuggestions],
  );

  const fromIso = from?.toISOString();
  const toIso = to?.toISOString();

  // Reset the virtualized list to the top whenever a filter changes, so users
  // don't stay at a stale offset and miss the newly filtered results.
  useEffect(() => {
    containerRef.current?.scrollTo({ top: 0 });
  }, [policyFilter, ruleFilter, userFilter, uniqueOnly, fromIso, toIso]);

  const resultsQuery = useInfiniteQuery({
    queryKey: [
      "risk",
      "results",
      "list",
      policyFilter,
      ruleFilter,
      userFilter,
      uniqueOnly,
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
        title="Risk Events"
        stage="beta"
        description="Review policy findings across recent analyzed chats."
        actions={<RevealAllToggle />}
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
            {resultsQuery.isFetching && results.length > 0 ? (
              <div className="bg-primary/20 h-1 shrink-0">
                <div className="bg-primary h-full animate-pulse" />
              </div>
            ) : null}
          </>
        }
        header={
          <div className="min-w-[1120px]">
            <RiskEventsHeader />
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
        contentClassName="min-w-[1120px]"
      >
        <RiskEventsRows
          error={resultsQuery.error}
          isLoading={isInitialLoading}
          results={results}
          policyNameById={policyNameById}
          scrollRef={containerRef}
          onSelectChat={setSelectedChatId}
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
    <div className="flex shrink-0 items-center gap-2 border-b border-amber-500/30 bg-amber-500/10 px-5 py-2 text-sm text-amber-700 dark:text-amber-400">
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

function RiskEventsHeader() {
  return (
    <div
      className={cn(
        RISK_EVENTS_GRID,
        "bg-muted/30 text-muted-foreground shrink-0 items-center border-b px-5 py-2.5 text-xs font-medium tracking-wide uppercase",
      )}
    >
      <div className="min-w-0">Timestamp</div>
      <div className="min-w-0">Category</div>
      <div className="min-w-0">Rule</div>
      <div className="min-w-0">Session Name</div>
      <div className="min-w-0">User</div>
      <div className="min-w-0">Match</div>
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
  scrollRef,
  onSelectChat,
}: {
  error: Error | null;
  isLoading: boolean;
  results: RiskResult[];
  policyNameById: Map<string, string>;
  scrollRef: RefObject<HTMLDivElement | null>;
  onSelectChat: (chatId: string | null) => void;
}) {
  const rowVirtualizer = useVirtualizer({
    count: results.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 52,
    overscan: 12,
  });

  if (error) {
    return (
      <div className="flex flex-col items-center gap-3 py-12">
        <div className="bg-destructive/10 flex size-12 items-center justify-center rounded-full">
          <CircleAlert className="text-destructive size-6" />
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
        <LoaderCircle className="size-5 animate-spin" />
        <span>Loading risk events...</span>
      </div>
    );
  }

  if (results.length === 0) {
    return (
      <div className="flex flex-col items-center gap-3 py-12 text-center">
        <div className="bg-muted flex size-12 items-center justify-center rounded-full">
          <Inbox className="text-muted-foreground size-6" />
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
              onSelectChat={onSelectChat}
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
  onSelectChat,
}: {
  result: RiskResult;
  policyName: string | undefined;
  onSelectChat: (chatId: string | null) => void;
}) {
  const isShadowMCP = result.source === "shadow_mcp";

  const handleShare = useCallback(
    async (e: React.MouseEvent) => {
      e.stopPropagation();
      if (!result.chatId) return;
      const url = new URL(window.location.href);
      url.searchParams.set("chat_id", result.chatId);
      try {
        await navigator.clipboard.writeText(url.toString());
        toast.success("Link copied to clipboard");
      } catch {
        toast.error("Failed to copy link");
      }
    },
    [result.chatId],
  );

  return (
    <div
      role={result.chatId ? "button" : undefined}
      tabIndex={result.chatId ? 0 : undefined}
      className={cn(
        RISK_EVENTS_GRID,
        "hover:bg-muted/30 w-full items-center border-b px-5 py-3 text-left text-sm transition-colors",
        !result.chatId && "cursor-default",
      )}
      onClick={() => {
        if (result.chatId) {
          onSelectChat(result.chatId);
        }
      }}
      onKeyDown={(e) => {
        if (!result.chatId) return;
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onSelectChat(result.chatId);
        }
      }}
    >
      <div className="text-muted-foreground min-w-0 font-mono text-xs">
        {result.createdAt ? new Date(result.createdAt).toLocaleString() : "-"}
      </div>
      <div className="min-w-0 truncate">
        <CategoryLabel source={result.source} ruleId={result.ruleId} />
      </div>
      <div className="min-w-0 truncate">
        <RuleLabel source={result.source} ruleId={result.ruleId} />
      </div>
      <div className="min-w-0 truncate">{result.chatTitle ?? "Untitled"}</div>
      <div className="min-w-0 truncate">{result.userId ?? "-"}</div>
      <div className="min-w-0 truncate">
        {isShadowMCP && result.matchRedacted ? (
          <span className="font-mono text-xs" title={result.matchRedacted}>
            {result.matchRedacted}
          </span>
        ) : (
          <MaskedMatch
            resultId={result.id}
            matchRedacted={result.matchRedacted}
          />
        )}
      </div>
      <div className="min-w-0 truncate" title={policyName}>
        {policyName ?? "-"}
      </div>
      <div className="flex min-w-0 justify-center">
        {result.chatId ? (
          <button
            type="button"
            onClick={(e) => {
              void handleShare(e);
            }}
            onKeyDown={(e) => e.stopPropagation()}
            className="text-muted-foreground hover:text-foreground inline-flex items-center transition-colors"
            aria-label="Copy link to this event"
            title="Copy link to this event"
          >
            <Share2 className="h-3 w-3" />
          </button>
        ) : null}
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
