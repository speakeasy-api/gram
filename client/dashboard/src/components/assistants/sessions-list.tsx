import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
import {
  formatDateRangeLabel,
  useDateRangeFilter,
} from "@/components/observe/useDateRangeFilter";
import { Icon } from "@/components/ui/Icon";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { formatPlatform } from "@/lib/formatPlatform";
import { cn } from "@/lib/utils";
import { formatUsageCost } from "@/pages/chatLogs/claudeUsage";
import { useChatDetailSheet } from "@/pages/chatLogs/useChatDetailSheet";
import { HookSourceIcon } from "@/pages/hooks/HookSourceIcon";
import { useRoutes } from "@/routes";
import { ChatOverview } from "@gram/client/models/components/chatoverview.js";
import { useAssistantSessionSummary } from "@gram/client/react-query/assistantSessionSummary.js";
import { useListChats } from "@gram/client/react-query/listChats.js";
import { format } from "date-fns";
import { Loader2 } from "lucide-react";
import { useEffect, useState } from "react";

const SESSION_PAGE_LIMIT = 8;

function emptySessionsMessage(offset: number): string {
  if (offset > 0) return "This page has no sessions. Go to the previous page.";
  return "No sessions yet. Conversations with this assistant will appear here.";
}

/**
 * The assistant detail panel's Sessions tab: an aggregate stats banner over a
 * selectable time range (default last 30 days), plus an independently
 * paginated session list in the Agent Sessions row shape. Selecting a session opens the same
 * ChatDetailSheet the Agent Sessions page uses — an overlay, not a navigation —
 * so the detail view is identical. The footer links to the full, filterable
 * page.
 */
export function AssistantSessionsList({
  assistantId,
}: {
  assistantId: string;
}): JSX.Element {
  const { selectedChatId, openChat, sheet } = useChatDetailSheet();
  const [offset, setOffset] = useState(0);

  const {
    dateRange,
    customRange,
    customRangeLabel,
    from,
    to,
    setDateRangeParam,
    setCustomRangeParam,
    clearCustomRange,
  } = useDateRangeFilter("30d");

  const summary = useAssistantSessionSummary(
    { assistantId, from, to },
    undefined,
    { retry: false, throwOnError: false },
  );

  const sessions = useListChats(
    {
      assistantId,
      // Onboarding threads are plumbing, not assistant traffic.
      excludeSourceKind: "setup",
      sortBy: "last_message_timestamp",
      sortOrder: "desc",
      limit: SESSION_PAGE_LIMIT,
      offset,
    },
    undefined,
    { retry: false, throwOnError: false },
  );

  useEffect(() => setOffset(0), [assistantId]);

  const chats = sessions.data?.chats ?? [];
  const total = sessions.data?.total ?? 0;
  const rangeLabel = formatDateRangeLabel(dateRange, customRangeLabel);
  const hasPrevious = offset > 0;
  const hasNext = offset + chats.length < total;

  return (
    <>
      <Stack gap={3}>
        <div className="flex items-center justify-between gap-2">
          <Text small muted>
            Activity over the {rangeLabel}.
          </Text>
          <TimeRangePicker
            preset={customRange ? null : dateRange}
            customRange={customRange}
            customRangeLabel={customRangeLabel}
            onPresetChange={(preset) =>
              setDateRangeParam(preset, { tab: "sessions" })
            }
            onCustomRangeChange={(rangeFrom, rangeTo, label) =>
              setCustomRangeParam(rangeFrom, rangeTo, label, {
                tab: "sessions",
              })
            }
            onClearCustomRange={() => clearCustomRange({ tab: "sessions" })}
          />
        </div>

        <SessionSummaryTiles
          data={summary.data}
          isLoading={summary.isLoading}
          error={summary.error}
        />

        <SessionListResult
          assistantId={assistantId}
          chats={chats}
          total={total}
          offset={offset}
          selectedChatId={selectedChatId}
          isLoading={sessions.isLoading}
          error={sessions.error}
          hasPrevious={hasPrevious}
          hasNext={hasNext}
          onOpenChat={openChat}
          onOffsetChange={setOffset}
        />
      </Stack>

      {sheet}
    </>
  );
}

function SessionListResult({
  assistantId,
  chats,
  total,
  offset,
  selectedChatId,
  isLoading,
  error,
  hasPrevious,
  hasNext,
  onOpenChat,
  onOffsetChange,
}: {
  assistantId: string;
  chats: ChatOverview[];
  total: number;
  offset: number;
  selectedChatId: string | null;
  isLoading: boolean;
  error: Error | null;
  hasPrevious: boolean;
  hasNext: boolean;
  onOpenChat: (chatId: string) => void;
  onOffsetChange: (offset: number) => void;
}): JSX.Element {
  if (isLoading) {
    return (
      <Stack align="center" justify="center" className="py-12">
        <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
      </Stack>
    );
  }
  if (error) {
    return (
      <Text small muted>
        Couldn't load sessions. {error.message}
      </Text>
    );
  }

  return (
    <>
      <SessionsPage
        assistantId={assistantId}
        chats={chats}
        total={total}
        offset={offset}
        selectedChatId={selectedChatId}
        onOpenChat={onOpenChat}
      />
      {(hasPrevious || hasNext) && (
        <div className="flex items-center justify-center gap-3 border-t pt-3">
          <Button
            variant="tertiary"
            size="sm"
            disabled={!hasPrevious}
            onClick={() =>
              onOffsetChange(Math.max(0, offset - SESSION_PAGE_LIMIT))
            }
          >
            Previous
          </Button>
          <Text small muted className="font-mono tabular-nums">
            Page {Math.floor(offset / SESSION_PAGE_LIMIT) + 1} of{" "}
            {Math.ceil(total / SESSION_PAGE_LIMIT)}
          </Text>
          <Button
            variant="tertiary"
            size="sm"
            disabled={!hasNext}
            onClick={() => onOffsetChange(offset + SESSION_PAGE_LIMIT)}
          >
            Next
          </Button>
        </div>
      )}
    </>
  );
}

function SessionSummaryTiles({
  data,
  isLoading,
  error,
}: {
  data:
    | {
        sessions: number;
        messages: number;
        totalCost: number;
        totalTokens: number;
      }
    | undefined;
  isLoading: boolean;
  error: Error | null;
}): JSX.Element {
  const displayValue = isLoading ? "—" : undefined;
  return (
    <>
      <Stack gap={0}>
        <StatTileGroup>
          <StatTile
            title="Sessions"
            value={data?.sessions ?? 0}
            displayValue={displayValue}
            format="number"
            tone="information"
          />
          <StatTile
            title="Messages"
            value={data?.messages ?? 0}
            displayValue={displayValue}
            format="compact"
            tone="information"
          />
        </StatTileGroup>
        <StatTileGroup className="-mt-px">
          <StatTile
            title="Cost"
            value={data?.totalCost ?? 0}
            displayValue={displayValue}
            format="currency"
            tone="neutral"
          />
          <StatTile
            title="Tokens"
            value={data?.totalTokens ?? 0}
            displayValue={displayValue}
            format="compact"
            tone="neutral"
          />
        </StatTileGroup>
      </Stack>
      {error && (
        <Text small muted>
          Couldn't load activity totals. {error.message}
        </Text>
      )}
    </>
  );
}

function SessionsPage({
  assistantId,
  chats,
  total,
  offset,
  selectedChatId,
  onOpenChat,
}: {
  assistantId: string;
  chats: ChatOverview[];
  total: number;
  offset: number;
  selectedChatId: string | null;
  onOpenChat: (chatId: string) => void;
}): JSX.Element {
  const routes = useRoutes();

  return (
    <>
      {chats.length === 0 ? (
        <Text small muted>
          {emptySessionsMessage(offset)}
        </Text>
      ) : (
        <div className="divide-border/60 overflow-hidden border divide-y">
          {chats.map((chat) => (
            <SessionRow
              key={chat.id}
              chat={chat}
              isSelected={selectedChatId === chat.id}
              onSelect={() => onOpenChat(chat.id)}
            />
          ))}
        </div>
      )}

      {total > 0 && (
        <routes.agentSessions.Link
          queryParams={{ assistantId }}
          className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 self-start px-1 py-1 text-xs no-underline transition-colors hover:no-underline"
        >
          View all sessions
          <Icon name="chevron-right" className="h-3 w-3" />
        </routes.agentSessions.Link>
      )}
    </>
  );
}

function SessionRow({
  chat,
  isSelected,
  onSelect,
}: {
  chat: ChatOverview;
  isSelected: boolean;
  onSelect: () => void;
}): JSX.Element {
  const lastActivity = chat.lastMessageTimestamp ?? chat.createdAt;
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "hover:bg-muted/50 block w-full px-3 py-2.5 text-left transition-colors",
        isSelected && "bg-primary/5",
      )}
    >
      <Text small className="line-clamp-2 font-medium">
        {chat.title || "Untitled session"}
      </Text>
      <div className="text-muted-foreground mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px]">
        <span>
          {chat.numMessages} {chat.numMessages === 1 ? "message" : "messages"}
        </span>
        {chat.totalCost != null && chat.totalCost > 0 && (
          <>
            <span className="text-muted-foreground/40">·</span>
            <span>{formatUsageCost(chat.totalCost)}</span>
          </>
        )}
        <span className="text-muted-foreground/40">·</span>
        <span>{format(lastActivity, "MMM d, HH:mm")}</span>
        {chat.source && (
          <>
            <span className="text-muted-foreground/40">·</span>
            <span className="inline-flex items-center gap-1">
              <HookSourceIcon source={chat.source} className="size-3" />
              {formatPlatform(chat.source)}
            </span>
          </>
        )}
      </div>
    </button>
  );
}
