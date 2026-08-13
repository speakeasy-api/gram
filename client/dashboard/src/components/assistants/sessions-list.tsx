import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { StatTile, StatTileGroup } from "@/components/chart/stat-tile";
import {
  formatDateRangeLabel,
  useDateRangeFilter,
} from "@/components/observe/useDateRangeFilter";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { formatPlatform } from "@/lib/formatPlatform";
import { cn } from "@/lib/utils";
import { formatUsageCost } from "@/pages/chatLogs/claudeUsage";
import { useChatDetailSheet } from "@/pages/chatLogs/useChatDetailSheet";
import { HookSourceIcon } from "@/pages/hooks/HookSourceIcon";
import { useRoutes } from "@/routes";
import { ChatOverview } from "@gram/client/models/components/chatoverview.js";
import { useListChats } from "@gram/client/react-query/listChats.js";
import { format } from "date-fns";
import { Loader2 } from "lucide-react";
import { useMemo } from "react";

const PREVIEW_LIMIT = 8;

// One fetch feeds both the stats banner and the row list. Metrics are summed
// over the fetched page, so the banner is exact until an assistant exceeds
// this many sessions in the selected range — then the tiles disclose the cap.
const STATS_FETCH_LIMIT = 100;

/**
 * The assistant detail panel's Sessions tab: an aggregate stats banner over a
 * selectable time range (default last 30 days), plus a compact session list in
 * the Agent Sessions row shape. Selecting a session opens the same
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

  const { data, isLoading, error } = useListChats(
    {
      assistantId,
      // Onboarding threads are plumbing, not assistant traffic.
      excludeSourceKind: "setup",
      from,
      to,
      sortBy: "last_message_timestamp",
      sortOrder: "desc",
      limit: STATS_FETCH_LIMIT,
    },
    undefined,
    { retry: false, throwOnError: false },
  );

  const chats = data?.chats ?? [];
  const total = data?.total ?? chats.length;
  const rangeLabel = formatDateRangeLabel(dateRange, customRangeLabel);

  if (error) {
    return (
      <Text small muted>
        Couldn't load sessions. {error.message}
      </Text>
    );
  }

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
            onPresetChange={setDateRangeParam}
            onCustomRangeChange={setCustomRangeParam}
            onClearCustomRange={clearCustomRange}
          />
        </div>

        {isLoading ? (
          <Stack align="center" justify="center" className="py-12">
            <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
          </Stack>
        ) : (
          <SessionsContent
            assistantId={assistantId}
            chats={chats}
            total={total}
            rangeLabel={rangeLabel}
            selectedChatId={selectedChatId}
            onOpenChat={openChat}
          />
        )}
      </Stack>

      {sheet}
    </>
  );
}

function SessionsContent({
  assistantId,
  chats,
  total,
  rangeLabel,
  selectedChatId,
  onOpenChat,
}: {
  assistantId: string;
  chats: ChatOverview[];
  total: number;
  rangeLabel: string;
  selectedChatId: string | null;
  onOpenChat: (chatId: string) => void;
}): JSX.Element {
  const routes = useRoutes();

  const totals = useMemo(() => {
    let cost = 0;
    let tokens = 0;
    let messages = 0;
    for (const chat of chats) {
      cost += chat.totalCost ?? 0;
      tokens += chat.totalTokens ?? 0;
      messages += chat.numMessages;
    }
    return { cost, tokens, messages };
  }, [chats]);

  // The Sessions tile uses the exact server total; the summed tiles are capped
  // at the fetched page and disclose it.
  const metricsSubtext =
    total > chats.length ? `latest ${chats.length} sessions` : undefined;

  return (
    <>
      <Stack gap={0}>
        <StatTileGroup>
          <StatTile
            title="Sessions"
            value={total}
            format="number"
            tone="information"
          />
          <StatTile
            title="Messages"
            value={totals.messages}
            format="compact"
            tone="information"
            subtext={metricsSubtext}
          />
        </StatTileGroup>
        <StatTileGroup className="-mt-px">
          <StatTile
            title="Cost"
            value={totals.cost}
            format="currency"
            tone="neutral"
            subtext={metricsSubtext}
          />
          <StatTile
            title="Tokens"
            value={totals.tokens}
            format="compact"
            tone="neutral"
            subtext={metricsSubtext}
          />
        </StatTileGroup>
      </Stack>

      {chats.length === 0 ? (
        <Text small muted>
          No sessions in the {rangeLabel}. Conversations with this assistant
          will appear here.
        </Text>
      ) : (
        <div className="divide-border/60 overflow-hidden border divide-y">
          {chats.slice(0, PREVIEW_LIMIT).map((chat) => (
            <SessionRow
              key={chat.id}
              chat={chat}
              isSelected={selectedChatId === chat.id}
              onSelect={() => onOpenChat(chat.id)}
            />
          ))}
        </div>
      )}

      {total > Math.min(chats.length, PREVIEW_LIMIT) && (
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
