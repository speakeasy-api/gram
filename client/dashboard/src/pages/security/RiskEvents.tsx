import {
  defineFilters,
  useFilterState,
  type FilterValue,
} from "@/components/filters";
import { ListLayout } from "@/components/layouts/list-layout";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { useSdkClient } from "@/contexts/Sdk";
import { ChatDetailSheet } from "@/pages/chatLogs/ChatDetailPanel";
import { getPresetRange } from "@gram-ai/elements";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { useRiskListPolicies } from "@gram/client/react-query/riskListPolicies.js";
import { useRiskOverview } from "@gram/client/react-query/riskOverview.js";
import { InlineEmptyState } from "@/components/ui/inline-empty-state";
import { LoadMoreFooter } from "@/components/ui/load-more-footer";
import { Table, type Column } from "@/components/ui/table";
import { useInfiniteQuery } from "@tanstack/react-query";
import {
  CircleAlert,
  History,
  Inbox,
  LoaderCircle,
  Share2,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef } from "react";
import { useSearchParams } from "react-router";
import { toast } from "sonner";
import {
  CategoryLabel,
  MaskedMatch,
  RevealAllProvider,
  RevealAllToggle,
  RuleLabel,
} from "./risk-ui";

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

  const columns = useMemo<Column<RiskResult>[]>(
    () => [
      {
        key: "createdAt",
        header: "Timestamp",
        width: "172px",
        render: (r) => (
          <span className="text-muted-foreground min-w-0 truncate font-mono text-xs">
            {r.createdAt ? new Date(r.createdAt).toLocaleString() : "-"}
          </span>
        ),
      },
      {
        key: "category",
        header: "Category",
        width: "0.9fr",
        render: (r) => <CategoryLabel source={r.source} ruleId={r.ruleId} />,
      },
      {
        key: "ruleId",
        header: "Rule",
        width: "1fr",
        render: (r) => <RuleLabel source={r.source} ruleId={r.ruleId} />,
      },
      {
        key: "chatTitle",
        header: "Session Name",
        width: "1.15fr",
        render: (r) => (
          <span className="min-w-0 truncate">{r.chatTitle ?? "Untitled"}</span>
        ),
      },
      {
        key: "userId",
        header: "User",
        width: "1fr",
        render: (r) => (
          <span className="min-w-0 truncate">{r.userId ?? "-"}</span>
        ),
      },
      {
        key: "match",
        header: "Match",
        width: "1.25fr",
        render: (r) =>
          r.source === "shadow_mcp" && r.matchRedacted ? (
            <span
              className="min-w-0 truncate font-mono text-xs"
              title={r.matchRedacted}
            >
              {r.matchRedacted}
            </span>
          ) : (
            <MaskedMatch resultId={r.id} matchRedacted={r.matchRedacted} />
          ),
      },
      {
        key: "policy",
        header: "Policy",
        width: "1.1fr",
        render: (r) => (
          <span
            className="min-w-0 truncate"
            title={policyNameById.get(r.policyId)}
          >
            {policyNameById.get(r.policyId) ?? "-"}
          </span>
        ),
      },
      {
        key: "actions",
        header: "",
        width: "110px",
        render: (r) => <ShareCell chatId={r.chatId} />,
      },
    ],
    [policyNameById],
  );

  return (
    <RevealAllProvider>
      <ListLayout className="h-full min-h-0 flex-1 px-8 py-8">
        <ListLayout.Header
          title={
            <span className="inline-flex items-center gap-2">
              Risk Events
              <ReleaseStageBadge stage="beta" />
            </span>
          }
          subtitle="Review policy findings across recent analyzed chats."
          actions={<RevealAllToggle />}
        />
        <ListLayout.Toolbar>
          <ListLayout.Toolbar.Filters
            schema={RISK_FILTERS}
            values={values}
            optionsById={filterOptions}
            onChange={setValue as (id: string, value: FilterValue) => void}
            onClear={clearValue as (id: string) => void}
            onClearAll={clearAll}
          />
          <ListLayout.Toolbar.Refresh
            onRefresh={() => void resultsQuery.refetch()}
            isRefreshing={resultsQuery.isFetching}
          />
        </ListLayout.Toolbar>
        <ListLayout.List className="min-h-0 flex-1 overflow-hidden">
          <div className="bg-card flex h-full min-h-0 flex-1 flex-col overflow-hidden border">
            {viewingInactivePolicy ? (
              <InactivePolicyNotice policyName={selectedPolicy?.name} />
            ) : null}
            {resultsQuery.isFetching && results.length > 0 ? (
              <div className="bg-primary/20 h-1 shrink-0">
                <div className="bg-primary h-full animate-pulse" />
              </div>
            ) : null}
            <div
              ref={containerRef}
              className="min-h-0 flex-1 overflow-auto"
              onScroll={handleScroll}
            >
              {resultsQuery.error ? (
                <InlineEmptyState
                  className="py-12"
                  icon={<CircleAlert className="text-destructive" />}
                  title="Error loading risk events"
                  description={resultsQuery.error.message}
                />
              ) : isInitialLoading ? (
                <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
                  <LoaderCircle className="size-5 animate-spin" />
                  <span>Loading risk events...</span>
                </div>
              ) : (
                <Table
                  columns={columns}
                  data={results}
                  rowKey={(r) => r.id}
                  onRowClick={(r) => {
                    if (r.chatId) setSelectedChatId(r.chatId);
                  }}
                  className="min-w-[1120px] rounded-none border-none"
                  noResultsMessage={
                    <InlineEmptyState
                      className="py-12"
                      icon={<Inbox />}
                      title="No risk events found"
                      description="Findings will appear here as messages are analyzed."
                    />
                  }
                />
              )}
            </div>
          </div>
        </ListLayout.List>
        {results.length > 0 && (
          <ListLayout.Footer>
            <LoadMoreFooter
              shown={results.length}
              total={totalCount}
              noun="findings"
              hasMore={resultsQuery.hasNextPage}
              isLoading={resultsQuery.isFetchingNextPage}
              onLoadMore={() => {
                void resultsQuery.fetchNextPage();
              }}
            />
          </ListLayout.Footer>
        )}
      </ListLayout>

      <ChatDetailSheet
        chatId={selectedChatId}
        onClose={() => setSelectedChatId(null)}
        onDelete={() => setSelectedChatId(null)}
        riskFocus
      />
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
    <div className="border-warning/30 bg-warning/10 text-warning flex shrink-0 items-center gap-2 border-b px-5 py-2 text-sm">
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

// The per-row "copy link" affordance. Stops propagation so clicking it doesn't
// also open the chat detail sheet via the row's onRowClick.
function ShareCell({ chatId }: { chatId: string | null | undefined }) {
  const handleShare = useCallback(
    async (e: React.MouseEvent) => {
      e.stopPropagation();
      if (!chatId) return;
      const url = new URL(window.location.href);
      url.searchParams.set("chat_id", chatId);
      try {
        await navigator.clipboard.writeText(url.toString());
        toast.success("Link copied to clipboard");
      } catch {
        toast.error("Failed to copy link");
      }
    },
    [chatId],
  );

  if (!chatId) return null;

  return (
    <div className="flex w-full justify-center">
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
    </div>
  );
}
