import { MetricCard } from "@/components/chart/MetricCard";
import {
  formatDateRangeLabel,
  useDateRangeFilter,
} from "@/components/observe/useDateRangeFilter";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Drawer, DrawerContent } from "@/components/ui/drawer";
import { ChatDetailPanel } from "@/pages/chatLogs/ChatDetailPanel";
import { TimeRangePicker, type DateRangePreset } from "@gram-ai/elements";
import {
  useListChatsWithResolutions,
  useRiskOverview,
} from "@gram/client/react-query/index.js";
import { Icon } from "@speakeasy-api/moonshine";
import { useCallback, useMemo } from "react";
import { useParams, useSearchParams } from "react-router";

const RISK_OVERVIEW_PRESETS: DateRangePreset[] = [
  "15m",
  "1h",
  "4h",
  "1d",
  "2d",
  "3d",
  "7d",
  "15d",
  "30d",
];

export default function RiskOverviewUserDetail() {
  return (
    <RequireScope scope="org:admin" level="page">
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <RiskOverviewUserDetailContent />
        </Page.Body>
      </Page>
    </RequireScope>
  );
}

function RiskOverviewUserDetailContent() {
  const { externalUserId: encodedExternalUserId = "" } = useParams<{
    externalUserId: string;
  }>();
  const externalUserId = decodeURIComponent(encodedExternalUserId);
  const [searchParams, setSearchParams] = useSearchParams();
  const selectedChatId = searchParams.get("chat_id");
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

  const {
    dateRange,
    customRange,
    customRangeLabel,
    from,
    to,
    setDateRangeParam,
    setCustomRangeParam,
    clearCustomRange,
  } = useDateRangeFilter();
  const rangeLabel = useMemo(
    () => formatDateRangeLabel(dateRange, customRangeLabel),
    [dateRange, customRangeLabel],
  );

  const overviewQuery = useRiskOverview({ from, to });
  const userEntry = useMemo(
    () =>
      overviewQuery.data?.topUsers.find(
        (u) => u.externalUserId === externalUserId,
      ),
    [overviewQuery.data?.topUsers, externalUserId],
  );

  const chatsQuery = useListChatsWithResolutions(
    {
      externalUserId,
      from,
      to,
      limit: 100,
    },
    undefined,
    { throwOnError: false },
  );

  const chats = chatsQuery.data?.chats ?? [];
  const totalChats = chatsQuery.data?.total ?? chats.length;
  const userLabel = userEntry?.email || externalUserId || "Unknown user";

  const controls = (
    <TimeRangePicker
      preset={customRange ? null : dateRange}
      customRange={customRange}
      customRangeLabel={customRangeLabel}
      availablePresets={RISK_OVERVIEW_PRESETS}
      onPresetChange={setDateRangeParam}
      onCustomRangeChange={setCustomRangeParam}
      onClearCustomRange={clearCustomRange}
    />
  );

  return (
    <>
      <Page.Section>
        <Page.Section.Title stage="beta">{userLabel}</Page.Section.Title>
        <Page.Section.Description>
          Risk findings and chat sessions for this user
          {rangeLabel && ` across ${rangeLabel}.`}
        </Page.Section.Description>
        <Page.Section.CTA>{controls}</Page.Section.CTA>
        <Page.Section.Body>
          <div className="space-y-6">
            <div className="grid grid-cols-2 gap-4 md:grid-cols-3">
              <MetricCard
                title="Findings"
                value={userEntry?.findings ?? 0}
                format="number"
                icon="flag"
              />
              <MetricCard
                title="Chat Sessions"
                value={totalChats}
                format="number"
                icon="message-square"
              />
            </div>
            <ChatList
              chats={chats}
              isLoading={chatsQuery.isLoading}
              onSelectChat={setSelectedChatId}
            />
          </div>
        </Page.Section.Body>
      </Page.Section>

      <Drawer
        open={!!selectedChatId}
        onOpenChange={(open) => !open && setSelectedChatId(null)}
        direction="right"
      >
        <DrawerContent className="data-[vaul-drawer-direction=right]:w-[720px] data-[vaul-drawer-direction=right]:sm:max-w-[720px]">
          {selectedChatId && (
            <ChatDetailPanel
              chatId={selectedChatId}
              resolutions={[]}
              onClose={() => setSelectedChatId(null)}
              onDelete={() => setSelectedChatId(null)}
              collapseNonRisk
            />
          )}
        </DrawerContent>
      </Drawer>
    </>
  );
}

type Chat = {
  id: string;
  title?: string | undefined;
  externalUserId?: string | undefined;
  numMessages?: number | undefined;
  lastMessageTimestamp?: Date | undefined;
};

function ChatList({
  chats,
  isLoading,
  onSelectChat,
}: {
  chats: Chat[];
  isLoading: boolean;
  onSelectChat: (chatId: string) => void;
}) {
  if (isLoading) {
    return (
      <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
        <Icon name="loader-circle" className="size-5 animate-spin" />
        <span>Loading chats...</span>
      </div>
    );
  }

  if (chats.length === 0) {
    return (
      <div className="flex flex-col items-center gap-3 py-12 text-center">
        <div className="bg-muted flex size-12 items-center justify-center rounded-full">
          <Icon name="inbox" className="text-muted-foreground size-6" />
        </div>
        <span className="text-foreground font-medium">
          No chats in this time range
        </span>
      </div>
    );
  }

  return (
    <ul className="divide-border divide-y rounded-lg border">
      {chats.map((chat) => (
        <li key={chat.id}>
          <button
            type="button"
            onClick={() => onSelectChat(chat.id)}
            className="hover:bg-muted/40 flex w-full items-center gap-3 px-4 py-3 text-left transition-colors"
          >
            <Icon
              name="message-square"
              className="text-muted-foreground size-4 shrink-0"
            />
            <div className="min-w-0 flex-1">
              <div className="truncate text-sm font-medium">
                {chat.title || "Untitled chat"}
              </div>
              <div className="text-muted-foreground truncate text-xs">
                {chat.numMessages ?? 0} message
                {chat.numMessages === 1 ? "" : "s"}
                {chat.lastMessageTimestamp
                  ? ` · last ${new Date(
                      chat.lastMessageTimestamp,
                    ).toLocaleString()}`
                  : ""}
              </div>
            </div>
            <Icon
              name="chevron-right"
              className="text-muted-foreground size-4 shrink-0"
            />
          </button>
        </li>
      ))}
    </ul>
  );
}
