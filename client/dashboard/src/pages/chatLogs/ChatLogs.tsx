import { CopilotSidebar, useCopilotState } from "@/components/copilot-sidebar";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { cn } from "@/lib/utils";
import type { ChatOverviewWithResolutions } from "@gram/client/models/components";
import { useListChatsWithResolutions } from "@gram/client/react-query";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { useState, useMemo, useCallback } from "react";
import { useSearchParams } from "react-router";
import { ChatDetailPanel } from "./ChatDetailPanel";
import { ChatLogsFilters } from "./ChatLogsFilters";
import { ChatLogsTable } from "./ChatLogsTable";
import {
  DateRangeSelect,
  DateRangePreset,
  getDateRange,
} from "../observability/date-range-select";
import { Drawer, DrawerContent } from "@/components/ui/drawer";

// Score legend component
function ScoreLegend() {
  return (
    <div className="flex items-center px-5 py-3 bg-background border-b text-xs text-muted-foreground whitespace-nowrap overflow-x-auto">
      {/* Spacer to align icon with score rings - matches the score ring column width */}
      <div className="w-[44px] shrink-0 flex items-center justify-center">
        <Icon name="gauge" className="size-5" />
      </div>
      <div className="flex items-center gap-4 flex-1">
        <div className="flex items-center gap-2 shrink-0">
          <span className="font-medium">Resolution Score</span>
          <span className="text-muted-foreground/70">
            — How well the assistant resolved user goals
          </span>
        </div>
        <div className="flex items-center gap-4 ml-auto shrink-0">
          <div className="flex items-center gap-1.5">
            <span className="size-2 rounded-full bg-emerald-500" />
            <span>80-100 Good</span>
          </div>
          <div className="flex items-center gap-1.5">
            <span className="size-2 rounded-full bg-amber-500" />
            <span>50-79 Fair</span>
          </div>
          <div className="flex items-center gap-1.5">
            <span className="size-2 rounded-full bg-rose-500" />
            <span>0-49 Poor</span>
          </div>
        </div>
      </div>
    </div>
  );
}

// Valid date range presets
const validPresets: DateRangePreset[] = ["24h", "7d", "30d", "90d"];

function isValidPreset(value: string | null): value is DateRangePreset {
  return value !== null && validPresets.includes(value as DateRangePreset);
}

export default function ChatLogs() {
  const [searchParams, setSearchParams] = useSearchParams();
  const [selectedChat, setSelectedChat] =
    useState<ChatOverviewWithResolutions | null>(null);

  const [offset, setOffset] = useState(0);
  const limit = 50;

  // Copilot config - includes both chat and logs tools for comprehensive analysis
  const observabilityToolFilter = useCallback(
    ({ toolName }: { toolName: string }) => {
      const name = toolName.toLowerCase();
      return name.includes("chat") || name.includes("logs");
    },
    [],
  );
  const mcpConfig = useObservabilityMcpConfig({
    toolsToInclude: observabilityToolFilter,
  });

  // Parse URL params
  const urlRange = searchParams.get("range");
  const urlFrom = searchParams.get("from");
  const urlTo = searchParams.get("to");
  const urlSearch = searchParams.get("search");
  const urlStatus = searchParams.get("status");

  // Derive state from URL
  const dateRange: DateRangePreset = isValidPreset(urlRange) ? urlRange : "30d";

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
    return getDateRange(dateRange);
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

  const { data, isLoading, error } = useListChatsWithResolutions({
    search: searchQuery || undefined,
    resolutionStatus: resolutionStatus || undefined,
    from: timeRange.from,
    to: timeRange.to,
    limit,
    offset,
  });

  const chats = data?.chats ?? [];
  const hasMore = chats.length === limit;

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
    <CopilotSidebar
      mcpConfig={mcpConfig}
      title="How can I help you debug?"
      subtitle="Search chat sessions, analyze failures, or explore logs"
      contextInfo={dateRangeContext}
      suggestions={[
        {
          title: "Failed Chats",
          label: "Analyze failed chats",
          prompt:
            "Show me recent chat sessions that failed. What patterns do you see in the failures?",
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
            "Help me debug a chat session. Search both the chat data and raw logs to understand what happened.",
        },
      ]}
    >
      <ChatLogsContent
        dateRange={dateRange}
        setDateRangeParam={setDateRangeParam}
        customRange={customRange}
        clearCustomRange={clearCustomRange}
        searchQuery={searchQuery}
        setSearchQuery={setSearchQuery}
        resolutionStatus={resolutionStatus}
        setResolutionStatus={setResolutionStatus}
        chats={chats}
        selectedChat={selectedChat}
        setSelectedChat={setSelectedChat}
        isLoading={isLoading}
        error={error}
        hasMore={hasMore}
        offset={offset}
        setOffset={setOffset}
        limit={limit}
      />
    </CopilotSidebar>
  );
}

// Separate component to use useCopilotState inside CopilotSidebar context
function ChatLogsContent({
  dateRange,
  setDateRangeParam,
  customRange,
  clearCustomRange,
  searchQuery,
  setSearchQuery,
  resolutionStatus,
  setResolutionStatus,
  chats,
  selectedChat,
  setSelectedChat,
  isLoading,
  error,
  hasMore,
  offset,
  setOffset,
  limit,
}: {
  dateRange: DateRangePreset;
  setDateRangeParam: (preset: DateRangePreset) => void;
  customRange: { from: Date; to: Date } | null;
  clearCustomRange: () => void;
  searchQuery: string;
  setSearchQuery: (value: string) => void;
  resolutionStatus: string;
  setResolutionStatus: (value: string) => void;
  chats: ChatOverviewWithResolutions[];
  selectedChat: ChatOverviewWithResolutions | null;
  setSelectedChat: (chat: ChatOverviewWithResolutions | null) => void;
  isLoading: boolean;
  error: Error | null;
  hasMore: boolean;
  offset: number;
  setOffset: (offset: number) => void;
  limit: number;
}) {
  const { isExpanded: isCopilotOpen } = useCopilotState();

  return (
    <div className="flex flex-col h-full w-full overflow-hidden">
      {/* Header section */}
      <div className="p-6 border-b shrink-0">
        <div
          className={cn(
            "flex gap-4 mb-4 transition-all duration-300",
            isCopilotOpen
              ? "flex-col items-stretch"
              : "flex-row items-center justify-between",
          )}
        >
          <div className="min-w-0">
            <h1 className="text-2xl font-semibold mb-1">Logs</h1>
            <p className="text-sm text-muted-foreground">
              View and debug individual chat conversations
            </p>
          </div>
          <div className="flex-shrink-0">
            <DateRangeSelect
              value={dateRange}
              onValueChange={setDateRangeParam}
              customRange={customRange}
              onClearCustomRange={clearCustomRange}
            />
          </div>
        </div>
        <ChatLogsFilters
          searchQuery={searchQuery}
          onSearchQueryChange={setSearchQuery}
          resolutionStatus={resolutionStatus}
          onResolutionStatusChange={setResolutionStatus}
        />
      </div>

      {/* Content section */}
      <div className="flex-1 overflow-y-auto relative min-h-0">
        <div className="sticky top-0 z-10">
          <ScoreLegend />
        </div>
        <ChatLogsTable
          chats={chats}
          selectedChatId={selectedChat?.id}
          onSelectChat={setSelectedChat}
          isLoading={isLoading}
          error={error}
        />

        {(hasMore || offset > 0) && (
          <div className="p-4 flex justify-center gap-2">
            <Button
              onClick={() => setOffset(Math.max(0, offset - limit))}
              disabled={offset === 0}
            >
              Previous
            </Button>
            <Button
              onClick={() => setOffset(offset + limit)}
              disabled={!hasMore}
            >
              Next
            </Button>
          </div>
        )}
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
            />
          )}
        </DrawerContent>
      </Drawer>
    </div>
  );
}
