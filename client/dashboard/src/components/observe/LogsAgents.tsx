import { InsightsConfig } from "@/components/insights-dock";
import { INSIGHTS_SUGGESTIONS } from "@/lib/insights-suggestions";
import { EnableLoggingOverlay } from "@/components/EnableLoggingOverlay";
import { ObservabilitySkeleton } from "@/components/ObservabilitySkeleton";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import type { ChatOverview } from "@gram/client/models/components";
import {
  HasRisk,
  SortBy,
  SortOrder as ApiSortOrder,
} from "@gram/client/models/operations/listchats";
import {
  useChatDeleteMutation,
  invalidateAllListChats,
  useAssistantsGet,
  useListChats,
} from "@gram/client/react-query";
import { Badge } from "@/components/ui/badge";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useState, useMemo, useCallback, useRef, useEffect } from "react";
import { useSearchParams } from "react-router";
import { ChatDetailSheet } from "@/pages/chatLogs/ChatDetailPanel";
import { ChatLogsTable } from "@/pages/chatLogs/ChatLogsTable";
import {
  defineFilters,
  type FilterValue,
  type OptionsById,
} from "@/components/filters";
import { Page } from "@/components/page-layout";
import { type DateRangePreset, getPresetRange } from "@gram-ai/elements";
import { isValidPreset } from "@/components/observe/observeFilterUtils";

type SortField = "chronological" | "messageCount";
type SortOrder = "asc" | "desc";

function toApiSortBy(field: SortField): SortBy {
  switch (field) {
    case "chronological":
      return SortBy.LastMessageTimestamp;
    case "messageCount":
      return SortBy.NumMessages;
  }
}

function toApiHasRisk(value: string): HasRisk | undefined {
  if (value === "true") return HasRisk.True;
  if (value === "false") return HasRisk.False;
  return undefined;
}

// Read the min-risk-score URL param. Empty, non-integer, or < 1 is treated as
// "no threshold" — a minimum of 0 means "≥ 0", i.e. everything, so it's
// indistinguishable from no filter (and the API rejects it).
function parseMinRiskScore(value: string | null): number | undefined {
  const trimmed = (value ?? "").trim();
  if (trimmed === "") return undefined;
  const n = Number(trimmed);
  if (!Number.isInteger(n) || n < 1) return undefined;
  return n;
}

function toApiSortOrder(order: SortOrder): ApiSortOrder {
  return order === "asc" ? ApiSortOrder.Asc : ApiSortOrder.Desc;
}

const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function isUuid(value: string | null): value is string {
  return !!value && UUID_RE.test(value);
}

const SESSION_FILTERS = defineFilters([
  {
    id: "date",
    label: "Date range",
    kind: "daterange",
    pinned: true,
    defaultPreset: "30d",
  },
  { id: "has_risk", label: "Risk", kind: "select", allLabel: "All" },
  {
    id: "min_risk_score",
    label: "Min risk score",
    kind: "number",
    min: 1,
    placeholder: "e.g. 3 (≥ 3 findings)",
  },
]);

const HAS_RISK_OPTIONS: OptionsById = {
  has_risk: [
    { value: "true", label: "With Risk" },
    { value: "false", label: "No Risk" },
  ],
};

export function LogsAgentsContent(): JSX.Element {
  const [searchParams, setSearchParams] = useSearchParams();

  const [offset, setOffset] = useState(0);
  const limit = 50;

  const [cachedChat, setCachedChat] = useState<ChatOverview | null>(null);

  const mcpConfig = useObservabilityMcpConfig({
    toolsToInclude: [
      "gram_search_logs",
      "gram_search_chats",
      "gram_get_deployment_logs",
      "gram_load_chat",
      "gram_list_chats",
    ],
  });

  const queryClient = useQueryClient();
  const deleteChatMutation = useChatDeleteMutation();

  const handleDeleteChat = useCallback(
    (chatId: string) => {
      deleteChatMutation.mutate(
        { request: { id: chatId } },
        {
          onSuccess: () => {
            setSearchParams((prev) => {
              if (prev.get("chatId") !== chatId) return prev;
              const next = new URLSearchParams(prev);
              next.delete("chatId");
              return next;
            });
            setCachedChat((current) =>
              current?.id === chatId ? null : current,
            );
            void invalidateAllListChats(queryClient);
          },
        },
      );
    },
    [deleteChatMutation, queryClient, setSearchParams],
  );

  const urlRange = searchParams.get("range");
  const urlFrom = searchParams.get("from");
  const urlTo = searchParams.get("to");
  const urlSearch = searchParams.get("search");
  const urlChatId = searchParams.get("chatId");
  const urlHasRisk = searchParams.get("has_risk");
  const urlMinRiskScore = searchParams.get("min_risk_score");
  const urlAssistantId = searchParams.get("assistantId");
  const urlSort = searchParams.get("sort") as SortField | null;
  const urlOrder = searchParams.get("order") as SortOrder | null;

  const dateRange: DateRangePreset = isValidPreset(urlRange) ? urlRange : "30d";
  const sortField: SortField =
    urlSort === "messageCount" ? urlSort : "chronological";
  const sortOrder: SortOrder = urlOrder === "asc" ? "asc" : "desc";
  const hasRisk: string =
    urlHasRisk === "true" || urlHasRisk === "false" ? urlHasRisk : "";
  const minRiskScore = useMemo(
    () => parseMinRiskScore(urlMinRiskScore),
    [urlMinRiskScore],
  );

  const customRange = useMemo(() => {
    if (urlFrom && urlTo) {
      const from = new Date(urlFrom);
      const to = new Date(urlTo);
      if (!isNaN(from.getTime()) && !isNaN(to.getTime())) {
        return { from, to };
      }
    }
    return null;
  }, [urlFrom, urlTo]);

  const searchQuery = urlSearch ?? "";
  const assistantId = isUuid(urlAssistantId) ? urlAssistantId : "";

  const { data: filteredAssistant } = useAssistantsGet(
    { id: assistantId },
    undefined,
    {
      enabled: !!assistantId,
      retry: false,
      throwOnError: false,
      refetchOnWindowFocus: false,
    },
  );

  const timeRange = useMemo(() => {
    if (customRange) {
      return { from: customRange.from, to: customRange.to };
    }
    return getPresetRange(dateRange);
  }, [customRange, dateRange]);

  const updateSearchParams = useCallback(
    (updates: Record<string, string | null>) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev);
        for (const [key, value] of Object.entries(updates)) {
          if (value === null) {
            next.delete(key);
          } else {
            next.set(key, value);
          }
        }
        return next;
      });
      setOffset(0);
    },
    [setSearchParams],
  );

  const setDateRangeParam = useCallback(
    (preset: DateRangePreset) => {
      updateSearchParams({ range: preset, from: null, to: null });
    },
    [updateSearchParams],
  );

  const setCustomRangeParam = useCallback(
    (from: Date, to: Date) => {
      updateSearchParams({
        range: null,
        from: from.toISOString(),
        to: to.toISOString(),
      });
    },
    [updateSearchParams],
  );

  const clearCustomRange = useCallback(() => {
    updateSearchParams({ from: null, to: null });
  }, [updateSearchParams]);

  const setSearchQuery = useCallback(
    (value: string) => {
      updateSearchParams({ search: value || null });
    },
    [updateSearchParams],
  );

  const setHasRisk = useCallback(
    (value: string) => {
      // "No Risk" means zero findings, which contradicts any positive
      // threshold — clear the score so the two controls never disagree.
      updateSearchParams(
        value === "false"
          ? { has_risk: value, min_risk_score: null }
          : { has_risk: value || null },
      );
    },
    [updateSearchParams],
  );

  const setMinRiskScore = useCallback(
    (value: number | null) => {
      // A threshold below 1 ("≥ 0" = everything) is meaningless, so treat it as
      // clearing the filter rather than sending a 0 the API rejects.
      const threshold = value !== null && value >= 1 ? value : null;
      // Entering a real threshold contradicts the "No Risk" presence option,
      // so drop that selection when a score is set.
      const clearNoRisk = threshold !== null && hasRisk === "false";
      updateSearchParams({
        min_risk_score: threshold === null ? null : String(threshold),
        ...(clearNoRisk ? { has_risk: null } : {}),
      });
    },
    [updateSearchParams, hasRisk],
  );

  // Single setSearchParams so the synchronous clears don't clobber each other
  // (react-router's setSearchParams reads a memoized snapshot).
  const clearAllFilters = useCallback(() => {
    updateSearchParams({
      range: null,
      from: null,
      to: null,
      has_risk: null,
      min_risk_score: null,
    });
  }, [updateSearchParams]);

  const clearAssistantFilter = useCallback(() => {
    updateSearchParams({ assistantId: null });
  }, [updateSearchParams]);

  const setSortField = useCallback(
    (value: SortField) => {
      updateSearchParams({ sort: value === "chronological" ? null : value });
    },
    [updateSearchParams],
  );

  const setSortOrder = useCallback(
    (value: SortOrder) => {
      updateSearchParams({ order: value === "desc" ? null : value });
    },
    [updateSearchParams],
  );

  const { data, isLoading, error, refetch, isLogsDisabled } =
    useLogsEnabledErrorCheck(
      useListChats(
        {
          search: searchQuery || undefined,
          // A custom threshold supersedes the binary presence filter: "count >
          // n" (n >= 0) already implies risk is present, so we don't also send
          // has_risk and risk a contradictory pair.
          hasRisk:
            minRiskScore !== undefined ? undefined : toApiHasRisk(hasRisk),
          minRiskScore,
          assistantId: assistantId || undefined,
          from: timeRange.from,
          to: timeRange.to,
          sortBy: toApiSortBy(sortField),
          sortOrder: toApiSortOrder(sortOrder),
          limit,
          offset,
        },
        undefined,
        { throwOnError: false },
      ),
    );

  const chats = useMemo(() => data?.chats ?? [], [data?.chats]);
  const lastTotalRef = useRef(0);
  if (data?.total !== undefined && data.total > 0) {
    lastTotalRef.current = data.total;
  }
  const total = lastTotalRef.current;
  const hasMore =
    total > 0 ? offset + chats.length < total : chats.length === limit;

  const selectedChat = useMemo<ChatOverview | null>(() => {
    if (!urlChatId) return null;
    const fromList = chats.find((c) => c.id === urlChatId);
    if (fromList) return fromList;
    if (cachedChat?.id === urlChatId) return cachedChat;
    return null;
  }, [urlChatId, chats, cachedChat]);

  useEffect(() => {
    if (!urlChatId) {
      if (cachedChat) setCachedChat(null);
      return;
    }
    const fromList = chats.find((c) => c.id === urlChatId);
    if (fromList && fromList !== cachedChat) {
      setCachedChat(fromList);
    }
  }, [urlChatId, chats, cachedChat]);

  const setSelectedChat = useCallback(
    (chat: ChatOverview | null) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev);
        if (chat) {
          next.set("chatId", chat.id);
        } else {
          next.delete("chatId");
        }
        return next;
      });
      if (chat) setCachedChat(chat);
    },
    [setSearchParams],
  );

  const dateRangeContext = useMemo(() => {
    const formatDate = (d: Date) =>
      d.toLocaleDateString("en-US", {
        month: "short",
        day: "numeric",
        year: "numeric",
      });
    return `Viewing logs from ${formatDate(timeRange.from)} to ${formatDate(timeRange.to)}${
      searchQuery ? ` Search query: "${searchQuery}"` : ""
    }${
      filteredAssistant
        ? `. Scoped to assistant "${filteredAssistant.name}".`
        : ""
    }`;
  }, [timeRange.from, timeRange.to, searchQuery, filteredAssistant]);

  return (
    <>
      <InsightsConfig
        mcpConfig={mcpConfig}
        title="How can I help you debug?"
        subtitle="Search agent sessions, analyze failures, or explore logs"
        contextInfo={dateRangeContext}
        hideTrigger={isLogsDisabled}
        suggestions={INSIGHTS_SUGGESTIONS["agent-sessions"]}
      />
      <AgentSessionsPageContent
        dateRange={dateRange}
        setDateRangeParam={setDateRangeParam}
        setCustomRangeParam={setCustomRangeParam}
        customRange={customRange}
        clearCustomRange={clearCustomRange}
        searchQuery={searchQuery}
        setSearchQuery={setSearchQuery}
        hasRisk={hasRisk}
        setHasRisk={setHasRisk}
        minRiskScore={minRiskScore}
        setMinRiskScore={setMinRiskScore}
        clearAllFilters={clearAllFilters}
        assistantName={filteredAssistant?.name ?? null}
        hasAssistantFilter={!!assistantId}
        clearAssistantFilter={clearAssistantFilter}
        sortField={sortField}
        setSortField={setSortField}
        sortOrder={sortOrder}
        setSortOrder={setSortOrder}
        chats={chats}
        selectedChat={selectedChat}
        setSelectedChat={setSelectedChat}
        isLoading={isLoading}
        error={error}
        isLogsDisabled={isLogsDisabled}
        onLogsEnabled={() => void refetch()}
        hasMore={hasMore}
        offset={offset}
        setOffset={setOffset}
        limit={limit}
        total={total}
        onDeleteChat={handleDeleteChat}
      />
    </>
  );
}

function AgentSessionsPageContent({
  dateRange,
  setDateRangeParam,
  setCustomRangeParam,
  customRange,
  clearCustomRange,
  searchQuery,
  setSearchQuery,
  hasRisk,
  setHasRisk,
  minRiskScore,
  setMinRiskScore,
  clearAllFilters,
  assistantName,
  hasAssistantFilter,
  clearAssistantFilter,
  sortField,
  setSortField,
  sortOrder,
  setSortOrder,
  chats,
  selectedChat,
  setSelectedChat,
  isLoading,
  error,
  isLogsDisabled,
  onLogsEnabled,
  hasMore,
  offset,
  setOffset,
  limit,
  total,
  onDeleteChat,
}: {
  dateRange: DateRangePreset;
  setDateRangeParam: (preset: DateRangePreset) => void;
  setCustomRangeParam: (from: Date, to: Date) => void;
  customRange: { from: Date; to: Date } | null;
  clearCustomRange: () => void;
  searchQuery: string;
  setSearchQuery: (value: string) => void;
  hasRisk: string;
  setHasRisk: (value: string) => void;
  minRiskScore: number | undefined;
  setMinRiskScore: (value: number | null) => void;
  clearAllFilters: () => void;
  assistantName: string | null;
  hasAssistantFilter: boolean;
  clearAssistantFilter: () => void;
  sortField: SortField;
  setSortField: (value: SortField) => void;
  sortOrder: SortOrder;
  setSortOrder: (value: SortOrder) => void;
  chats: ChatOverview[];
  selectedChat: ChatOverview | null;
  setSelectedChat: (chat: ChatOverview | null) => void;
  isLoading: boolean;
  error: Error | null;
  isLogsDisabled: boolean;
  onLogsEnabled: () => void;
  hasMore: boolean;
  offset: number;
  setOffset: (offset: number) => void;
  limit: number;
  total: number;
  onDeleteChat: (chatId: string) => void;
}) {
  if (isLogsDisabled) {
    return (
      <div className="min-h-0 w-full flex-1 space-y-6 overflow-y-auto p-8 pb-24">
        <div className="flex min-w-0 flex-col gap-1">
          <h1 className="text-xl font-semibold">Agent Sessions</h1>
          <p className="text-muted-foreground text-sm">
            View and debug individual agent sessions captured for organization
            members in this project
          </p>
        </div>
        <div className="relative flex-1">
          <div
            className="pointer-events-none h-full select-none"
            aria-hidden="true"
          >
            <ObservabilitySkeleton />
          </div>
          <EnableLoggingOverlay onEnabled={onLogsEnabled} />
        </div>
      </div>
    );
  }

  return (
    <>
      <div className="flex min-h-0 w-full flex-1 flex-col">
        <div className="shrink-0 space-y-4 px-8 py-4">
          <div className="flex min-w-0 flex-col gap-1">
            <h1 className="text-xl font-semibold">Agent Sessions</h1>
            <p className="text-muted-foreground text-sm">
              View and debug individual agent sessions captured for organization
              members in this project
            </p>
          </div>
          {hasAssistantFilter && (
            <Badge
              variant="secondary"
              className="w-fit gap-1.5 px-2.5 py-1 text-xs"
            >
              <Icon name="bot" className="size-3" />
              <span>
                Assistant:{" "}
                <span className="font-medium">
                  {assistantName ?? "Loading…"}
                </span>
              </span>
              <button
                type="button"
                onClick={clearAssistantFilter}
                aria-label="Clear assistant filter"
                className="hover:bg-muted-foreground/20 -mr-1 ml-0.5 flex size-4 items-center justify-center rounded"
              >
                <Icon name="x" className="size-3" />
              </button>
            </Badge>
          )}
          <Page.Toolbar>
            <Page.Toolbar.Search
              value={searchQuery}
              onChange={setSearchQuery}
              placeholder="Search by chat ID, user ID, or title..."
              debounceMs={500}
            />
            <Page.Toolbar.Filters
              schema={SESSION_FILTERS}
              values={{
                date: {
                  preset: customRange ? null : dateRange,
                  customRange,
                  customLabel: null,
                },
                has_risk: hasRisk || null,
                min_risk_score: minRiskScore ?? null,
              }}
              optionsById={HAS_RISK_OPTIONS}
              onChange={(id: string, value: FilterValue) => {
                if (id === "date") {
                  const dateValue = value as {
                    preset: DateRangePreset | null;
                    customRange: { from: Date; to: Date } | null;
                  };
                  if (dateValue.customRange) {
                    setCustomRangeParam(
                      dateValue.customRange.from,
                      dateValue.customRange.to,
                    );
                  } else if (dateValue.preset) {
                    setDateRangeParam(dateValue.preset);
                  } else {
                    clearCustomRange();
                  }
                } else if (id === "has_risk") {
                  setHasRisk((value as string | null) ?? "");
                } else if (id === "min_risk_score") {
                  setMinRiskScore(value as number | null);
                }
              }}
              onClear={(id: string) => {
                if (id === "date") {
                  setDateRangeParam("30d");
                } else if (id === "has_risk") {
                  setHasRisk("");
                } else if (id === "min_risk_score") {
                  setMinRiskScore(null);
                }
              }}
              onClearAll={clearAllFilters}
            />
            <Page.Toolbar.SortBy
              value={sortField}
              onChange={(v) => setSortField(v as SortField)}
              options={[
                { value: "chronological", label: "Date" },
                { value: "messageCount", label: "Message Count" },
              ]}
              direction={sortOrder}
              onDirectionChange={setSortOrder}
            />
          </Page.Toolbar>
        </div>

        <div className="min-h-0 flex-1 overflow-hidden border-t">
          <div className="bg-background flex h-full flex-col overflow-hidden">
            <div className="flex-1 overflow-y-auto">
              <ChatLogsTable
                chats={chats}
                selectedChatId={selectedChat?.id}
                onSelectChat={setSelectedChat}
                onDeleteChat={onDeleteChat}
                isLoading={isLoading}
                error={error}
              />
            </div>
            {(hasMore || offset > 0) && (
              <div className="bg-background flex shrink-0 items-center justify-center gap-4 border-t p-4">
                <Button
                  onClick={() => setOffset(Math.max(0, offset - limit))}
                  disabled={offset === 0}
                >
                  Previous
                </Button>
                <span className="text-muted-foreground text-sm tabular-nums">
                  Page {Math.floor(offset / limit) + 1}
                  {total > 0 && ` of ${Math.ceil(total / limit)}`}
                </span>
                <Button
                  onClick={() => setOffset(offset + limit)}
                  disabled={!hasMore}
                >
                  Next
                </Button>
              </div>
            )}
          </div>
        </div>
      </div>

      <ChatDetailSheet
        chatId={selectedChat?.id ?? null}
        onClose={() => setSelectedChat(null)}
        onDelete={onDeleteChat}
        dimNonRisk={hasRisk === "true" || minRiskScore !== undefined}
      />
    </>
  );
}
