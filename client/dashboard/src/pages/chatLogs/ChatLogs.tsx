import { InsightsConfig } from "@/components/insights-sidebar";
import { EnableLoggingOverlay } from "@/components/EnableLoggingOverlay";
import { ObservabilitySkeleton } from "@/components/ObservabilitySkeleton";
import { Page } from "@/components/page-layout";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { useLogsEnabledErrorCheck } from "@/hooks/useLogsEnabled";
import { cn } from "@/lib/utils";
import { resolutionBgColors } from "@/lib/resolution-colors";
import type { ChatOverviewWithResolutions } from "@gram/client/models/components";
import {
  SortBy,
  SortOrder as ApiSortOrder,
} from "@gram/client/models/operations/listchatswithresolutions";
import {
  useListChatsWithResolutions,
  useChatDeleteMutation,
  invalidateAllListChatsWithResolutions,
} from "@gram/client/react-query";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useState, useMemo, useCallback, useRef, useEffect } from "react";
import { useSearchParams } from "react-router";
import { ChatDetailPanel } from "./ChatDetailPanel";
import { ChatLogsFilters } from "./ChatLogsFilters";
import { ChatLogsTable } from "./ChatLogsTable";
import {
  TimeRangePicker,
  type DateRangePreset,
  getPresetRange,
} from "@gram-ai/elements";
import { Drawer, DrawerContent } from "@/components/ui/drawer";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { ArrowUpIcon, ArrowDownIcon } from "lucide-react";

type SortField = "chronological" | "messageCount" | "score";
type SortOrder = "asc" | "desc";

// Map frontend sort field to API sort field
function toApiSortBy(field: SortField): SortBy {
  switch (field) {
    case "chronological":
      return SortBy.CreatedAt;
    case "messageCount":
      return SortBy.NumMessages;
    case "score":
      return SortBy.Score;
  }
}

function toApiSortOrder(order: SortOrder): ApiSortOrder {
  return order === "asc" ? ApiSortOrder.Asc : ApiSortOrder.Desc;
}

// Reusable score indicator with colored dot
function ScoreIndicator({
  colorClass,
  children,
}: {
  colorClass: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-center gap-1.5">
      <span className={cn("size-2 rounded-full", colorClass)} />
      <span>{children}</span>
    </div>
  );
}

// Score legend component
function ScoreLegend() {
  return (
    <div className="bg-background text-muted-foreground flex items-center overflow-x-auto border-b px-5 py-3 text-xs whitespace-nowrap">
      {/* Spacer to align icon with score rings - matches the score ring column width */}
      <div className="flex w-[44px] shrink-0 items-center justify-center">
        <Icon name="gauge" className="size-5" />
      </div>
      <div className="flex flex-1 items-center gap-4">
        <div className="flex shrink-0 items-center gap-2">
          <span className="font-medium">Resolution Score</span>
          <span className="text-muted-foreground/70">
            — How well the assistant resolved user goals
          </span>
        </div>
        <div className="ml-auto flex shrink-0 items-center gap-4">
          <ScoreIndicator colorClass={resolutionBgColors.success}>
            80-100 Good
          </ScoreIndicator>
          <ScoreIndicator colorClass={resolutionBgColors.partial}>
            50-79 Fair
          </ScoreIndicator>
          <ScoreIndicator colorClass={resolutionBgColors.failure}>
            0-49 Poor
          </ScoreIndicator>
        </div>
      </div>
    </div>
  );
}

// Valid date range presets
const validPresets: DateRangePreset[] = [
  "15m",
  "1h",
  "4h",
  "1d",
  "2d",
  "3d",
  "7d",
  "15d",
  "30d",
  "90d",
];

function isValidPreset(value: string | null): value is DateRangePreset {
  return value !== null && validPresets.includes(value as DateRangePreset);
}

export default function ChatLogs() {
  const [searchParams, setSearchParams] = useSearchParams();
  const [selectedChat, setSelectedChat] =
    useState<ChatOverviewWithResolutions | null>(null);

  const [offset, setOffset] = useState(0);
  const limit = 50;

  // Copilot config - whitelist of tools for chat session analysis
  const mcpConfig = useObservabilityMcpConfig({
    toolsToInclude: [
      "gram_search_logs",
      "gram_search_chats",
      "gram_get_deployment_logs",
      "gram_load_chat",
      "gram_list_chats_with_resolutions",
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
            setSelectedChat((current) =>
              current?.id === chatId ? null : current,
            );
            invalidateAllListChatsWithResolutions(queryClient);
          },
        },
      );
    },
    [deleteChatMutation, queryClient],
  );

  // Parse URL params
  const urlRange = searchParams.get("range");
  const urlFrom = searchParams.get("from");
  const urlTo = searchParams.get("to");
  const urlSearch = searchParams.get("search");
  const urlChatId = searchParams.get("chatId");
  const urlStatus = searchParams.get("status");
  const urlSort = searchParams.get("sort") as SortField | null;
  const urlOrder = searchParams.get("order") as SortOrder | null;

  // Derive state from URL
  const dateRange: DateRangePreset = isValidPreset(urlRange) ? urlRange : "30d";
  const sortField: SortField =
    urlSort === "messageCount" || urlSort === "score"
      ? urlSort
      : "chronological";
  const sortOrder: SortOrder = urlOrder === "asc" ? "asc" : "desc";

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
  const resolutionStatus = urlStatus ?? "";

  // Calculate the time range for the query
  const timeRange = useMemo(() => {
    if (customRange) {
      return { from: customRange.from, to: customRange.to };
    }
    return getPresetRange(dateRange);
  }, [customRange, dateRange]);

  // Update URL helpers
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
      setOffset(0); // Reset pagination when filters change
    },
    [setSearchParams],
  );

  const setDateRangeParam = useCallback(
    (preset: DateRangePreset) => {
      updateSearchParams({
        range: preset,
        from: null,
        to: null,
      });
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
    updateSearchParams({
      from: null,
      to: null,
    });
  }, [updateSearchParams]);

  const setSearchQuery = useCallback(
    (value: string) => {
      updateSearchParams({ search: value || null });
    },
    [updateSearchParams],
  );

  const setResolutionStatus = useCallback(
    (value: string) => {
      updateSearchParams({ status: value || null });
    },
    [updateSearchParams],
  );

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

  const toggleSortOrder = useCallback(() => {
    setSortOrder(sortOrder === "desc" ? "asc" : "desc");
  }, [sortOrder, setSortOrder]);

  const { data, isLoading, error, refetch, isLogsDisabled } =
    useLogsEnabledErrorCheck(
      useListChatsWithResolutions(
        {
          search: searchQuery || undefined,
          resolutionStatus: resolutionStatus || undefined,
          from: timeRange.from,
          to: timeRange.to,
          sortBy: toApiSortBy(sortField),
          sortOrder: toApiSortOrder(sortOrder),
          limit,
          offset,
        },
        undefined, // security
        {
          throwOnError: false,
        },
      ),
    );

  // Chats are sorted server-side via sortBy/sortOrder params
  const chats = data?.chats ?? [];
  // Keep total stable across page changes to avoid flickering
  const lastTotalRef = useRef(0);
  if (data?.total !== undefined && data.total > 0) {
    lastTotalRef.current = data.total;
  }
  const total = lastTotalRef.current;
  const hasMore =
    total > 0 ? offset + chats.length < total : chats.length === limit;

  // Auto-select a chat if chatId is in the URL (e.g. from risk findings deep-link)
  const autoSelectedRef = useRef(false);
  useEffect(() => {
    if (!urlChatId || autoSelectedRef.current || chats.length === 0) return;
    const match = chats.find((c) => c.id === urlChatId);
    if (match) {
      setSelectedChat(match);
      autoSelectedRef.current = true;
    }
  }, [urlChatId, chats, setSelectedChat]);

  // Format date range for copilot context
  const dateRangeContext = useMemo(() => {
    const formatDate = (d: Date) =>
      d.toLocaleDateString("en-US", {
        month: "short",
        day: "numeric",
        year: "numeric",
      });
    return `Viewing logs from ${formatDate(timeRange.from)} to ${formatDate(timeRange.to)}${
      resolutionStatus ? `. Filtered to ${resolutionStatus} status.` : ""
    }${searchQuery ? ` Search query: "${searchQuery}"` : ""}`;
  }, [timeRange.from, timeRange.to, resolutionStatus, searchQuery]);

  return (
    <>
      <InsightsConfig
        mcpConfig={mcpConfig}
        title="How can I help you debug?"
        subtitle="Search agent sessions, analyze failures, or explore logs"
        contextInfo={dateRangeContext}
        hideTrigger={isLogsDisabled}
        suggestions={[
          {
            title: "Failed Chats",
            label: "Analyze failed chats",
            prompt:
              "Show me recent agent sessions that failed. What patterns do you see in the failures?",
          },
          {
            title: "Search Logs",
            label: "Search raw logs",
            prompt:
              "Search the raw telemetry logs for errors or warnings in the current period",
          },
          {
            title: "Debug Session",
            label: "Debug a specific chat",
            prompt:
              "Help me debug an agent session. Search both the chat data and raw logs to understand what happened.",
          },
        ]}
      />
      <ChatLogsContent
        dateRange={dateRange}
        setDateRangeParam={setDateRangeParam}
        setCustomRangeParam={setCustomRangeParam}
        customRange={customRange}
        clearCustomRange={clearCustomRange}
        searchQuery={searchQuery}
        setSearchQuery={setSearchQuery}
        resolutionStatus={resolutionStatus}
        setResolutionStatus={setResolutionStatus}
        sortField={sortField}
        setSortField={setSortField}
        sortOrder={sortOrder}
        toggleSortOrder={toggleSortOrder}
        chats={chats}
        selectedChat={selectedChat}
        setSelectedChat={setSelectedChat}
        isLoading={isLoading}
        error={error}
        isLogsDisabled={isLogsDisabled}
        onLogsEnabled={refetch}
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

// Separate component to render inside InsightsSidebar context
function ChatLogsContent({
  dateRange,
  setDateRangeParam,
  setCustomRangeParam,
  customRange,
  clearCustomRange,
  searchQuery,
  setSearchQuery,
  resolutionStatus,
  setResolutionStatus,
  sortField,
  setSortField,
  sortOrder,
  toggleSortOrder,
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
  resolutionStatus: string;
  setResolutionStatus: (value: string) => void;
  sortField: SortField;
  setSortField: (value: SortField) => void;
  sortOrder: SortOrder;
  toggleSortOrder: () => void;
  chats: ChatOverviewWithResolutions[];
  selectedChat: ChatOverviewWithResolutions | null;
  setSelectedChat: (chat: ChatOverviewWithResolutions | null) => void;
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
      <div className="flex h-full flex-col overflow-hidden">
        <Page>
          <Page.Header>
            <Page.Header.Breadcrumbs fullWidth />
          </Page.Header>
          <Page.Body fullWidth className="space-y-6">
            <div className="flex min-w-0 flex-col gap-1">
              <h1 className="text-xl font-semibold">Agent Sessions</h1>
              <p className="text-muted-foreground text-sm">
                View and debug individual agent sessions
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
          </Page.Body>
        </Page>
      </div>
    );
  }

  return (
    <>
      <div className="flex h-full flex-col overflow-hidden">
        <Page>
          <Page.Header>
            <Page.Header.Breadcrumbs fullWidth />
          </Page.Header>
          <Page.Body fullWidth noPadding overflowHidden>
            <div className="flex min-h-0 w-full flex-1 flex-col">
              {/* Header section */}
              <div className="shrink-0 space-y-4 px-8 py-4">
                <div className="flex min-w-0 flex-col gap-1">
                  <h1 className="text-xl font-semibold">Agent Sessions</h1>
                  <p className="text-muted-foreground text-sm">
                    View and debug individual agent sessions
                  </p>
                </div>
                {/* Filter row - all controls on one line */}
                <div className="flex items-center gap-3">
                  <ChatLogsFilters
                    searchQuery={searchQuery}
                    onSearchQueryChange={setSearchQuery}
                    resolutionStatus={resolutionStatus}
                    onResolutionStatusChange={setResolutionStatus}
                  />
                  <div className="ml-auto flex shrink-0 items-center gap-3">
                    {/* Sort segmented control: label + dropdown + direction button */}
                    <div className="border-border flex h-10 items-center rounded-md border">
                      <span className="text-muted-foreground px-3 text-sm font-medium">
                        Sort
                      </span>
                      <div className="bg-border h-5 w-px" />
                      <Select
                        value={sortField}
                        onValueChange={(v) => setSortField(v as SortField)}
                      >
                        <SelectTrigger className="h-full min-w-[100px] rounded-none border-0 shadow-none">
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent className="w-[280px]">
                          <SelectItem
                            value="chronological"
                            description="Sort by when the chat was created"
                          >
                            Date
                          </SelectItem>
                          <SelectItem
                            value="messageCount"
                            description="Sort by number of messages in the chat"
                          >
                            Messages
                          </SelectItem>
                          <SelectItem
                            value="score"
                            description="Sort by resolution score"
                          >
                            Score
                          </SelectItem>
                        </SelectContent>
                      </Select>
                      <div className="bg-border h-5 w-px" />
                      <div className="flex items-center px-1.5">
                        <SimpleTooltip tooltip="Sort direction">
                          <button
                            type="button"
                            onClick={toggleSortOrder}
                            className="text-muted-foreground hover:text-foreground hover:bg-accent flex size-7 items-center justify-center rounded transition-colors"
                          >
                            {sortOrder === "desc" ? (
                              <ArrowDownIcon className="size-4" />
                            ) : (
                              <ArrowUpIcon className="size-4" />
                            )}
                          </button>
                        </SimpleTooltip>
                      </div>
                    </div>
                    <TimeRangePicker
                      preset={customRange ? null : dateRange}
                      customRange={customRange}
                      onPresetChange={setDateRangeParam}
                      onCustomRangeChange={setCustomRangeParam}
                      onClearCustomRange={clearCustomRange}
                    />
                  </div>
                </div>
              </div>

              {/* Content section - full width */}
              <div className="min-h-0 flex-1 overflow-hidden border-t">
                <div className="bg-background flex h-full flex-col overflow-hidden">
                  {/* Score legend header */}
                  <div className="shrink-0">
                    <ScoreLegend />
                  </div>

                  {/* Scrollable chat list */}
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

                  {/* Sticky pagination at bottom */}
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
          </Page.Body>
        </Page>
      </div>

      {/* Right side: Slide-out drawer */}
      <Drawer
        open={!!selectedChat}
        onOpenChange={(open) => !open && setSelectedChat(null)}
        direction="right"
      >
        <DrawerContent className="!w-[720px] sm:!max-w-[720px]">
          {selectedChat && (
            <ChatDetailPanel
              chatId={selectedChat.id}
              resolutions={selectedChat.resolutions}
              onClose={() => setSelectedChat(null)}
              onDelete={onDeleteChat}
            />
          )}
        </DrawerContent>
      </Drawer>
    </>
  );
}
