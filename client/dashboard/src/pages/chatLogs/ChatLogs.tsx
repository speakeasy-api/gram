import type { ChatOverviewWithResolutions } from "@gram/client/models/components";
import { useListChatsWithResolutions } from "@gram/client/react-query";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
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

  // Parse URL params
  const urlRange = searchParams.get("range");
  const urlFrom = searchParams.get("from");
  const urlTo = searchParams.get("to");
  const urlUser = searchParams.get("user");
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

  const externalCustomerId = urlUser ?? "";
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

  const setExternalCustomerId = useCallback(
    (value: string) => {
      updateSearchParams({ user: value || null });
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
    externalUserId: externalCustomerId || undefined,
    resolutionStatus: resolutionStatus || undefined,
    from: timeRange.from,
    to: timeRange.to,
    limit,
    offset,
  });

  const chats = data?.chats ?? [];
  const hasMore = chats.length === limit;

  return (
    <div className="flex h-full">
      {/* Left side: List view */}
      <div className="flex-1 flex flex-col border-r">
        <div className="p-6 border-b">
          <div className="flex items-start justify-between mb-4">
            <div>
              <h1 className="text-2xl font-semibold mb-1">Logs</h1>
              <p className="text-sm text-muted-foreground">
                View and debug individual chat conversations
              </p>
            </div>
            <DateRangeSelect
              value={dateRange}
              onValueChange={setDateRangeParam}
              customRange={customRange}
              onClearCustomRange={clearCustomRange}
            />
          </div>
          <ChatLogsFilters
            externalCustomerId={externalCustomerId}
            onExternalCustomerIdChange={setExternalCustomerId}
            resolutionStatus={resolutionStatus}
            onResolutionStatusChange={setResolutionStatus}
          />
        </div>

        <div className="flex-1 overflow-y-auto">
          <ChatLogsTable
            chats={chats}
            selectedChatId={selectedChat?.id}
            onSelectChat={setSelectedChat}
            isLoading={isLoading}
            error={error}
          />

          {hasMore && (
            <div className="p-4 flex justify-center gap-2">
              <Button
                onClick={() => setOffset(Math.max(0, offset - limit))}
                disabled={offset === 0}
              >
                Previous
              </Button>
              <Button onClick={() => setOffset(offset + limit)}>Next</Button>
            </div>
          )}
        </div>
      </div>

      {/* Right side: Detail panel */}
      <div className="w-1/2">
        {selectedChat ? (
          <ChatDetailPanel
            chatId={selectedChat.id}
            resolutions={selectedChat.resolutions}
            onClose={() => setSelectedChat(null)}
          />
        ) : (
          <div className="h-full flex items-center justify-center text-muted-foreground">
            <Stack direction="vertical" gap={2} align="center">
              <Icon name="messages-square" className="size-12 opacity-50" />
              <p>Select a chat to view details</p>
            </Stack>
          </div>
        )}
      </div>
    </div>
  );
}
