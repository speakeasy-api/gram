import { MetricCard } from "@/components/chart/MetricCard";
import {
  formatDateRangeLabel,
  useDateRangeFilter,
} from "@/components/observe/useDateRangeFilter";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { ChatDetailSheet } from "@/pages/chatLogs/ChatDetailPanel";
import { type DateRangePreset } from "@gram-ai/elements";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { useListChats } from "@gram/client/react-query/listChats.js";
import { useRiskOverview } from "@gram/client/react-query/riskOverview.js";
import { useRiskUserBreakdown } from "@gram/client/react-query/riskUserBreakdown.js";
import { RULE_CATEGORY_META, type RuleCategory } from "./policy-data";
import { getRuleTitleFallback } from "./risk-utils";
import { Icon } from "@/components/ui/moonshine";
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

export default function RiskOverviewUserDetail(): JSX.Element {
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

  const chatsQuery = useListChats(
    {
      externalUserId,
      from,
      to,
      limit: 100,
    },
    undefined,
    { throwOnError: false },
  );

  const breakdownQuery = useRiskUserBreakdown(
    { externalUserId, from, to },
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
        <Page.Section.Title stage="beta" className="normal-case">
          {userLabel}
        </Page.Section.Title>
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
                format="compact"
                icon="flag"
              />
              <MetricCard
                title="Chat Sessions"
                value={totalChats}
                format="compact"
                icon="message-square"
              />
            </div>
            <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
              <CategoryBreakdown
                categories={breakdownQuery.data?.categories ?? []}
                isLoading={breakdownQuery.isLoading}
              />
              <RuleBreakdown
                rules={breakdownQuery.data?.rules ?? []}
                isLoading={breakdownQuery.isLoading}
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

      <ChatDetailSheet
        chatId={selectedChatId}
        onClose={() => setSelectedChatId(null)}
        onDelete={() => setSelectedChatId(null)}
        riskFocus
      />
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

function CategoryBreakdown({
  categories,
  isLoading,
}: {
  categories: Array<{ category: string; findings: number }>;
  isLoading: boolean;
}) {
  if (isLoading && categories.length === 0) {
    return (
      <div className="text-muted-foreground rounded-lg border p-4 text-sm">
        Loading category breakdown...
      </div>
    );
  }
  if (categories.length === 0) return null;
  const max = categories[0]?.findings || 1;

  return (
    <div className="space-y-3 rounded-lg border p-4">
      <h4 className="text-sm font-medium">Findings by category</h4>
      <ul className="space-y-2">
        {categories.map((c, i) => {
          const meta = RULE_CATEGORY_META[c.category as RuleCategory];
          const label = meta?.label ?? c.category;
          return (
            <li key={c.category} className="flex items-center gap-3">
              <span className="text-muted-foreground w-4 shrink-0 text-right text-xs">
                {i + 1}
              </span>
              <div className="min-w-0 flex-1">
                <div className="mb-1 flex items-center justify-between gap-2">
                  <span className="truncate text-sm">{label}</span>
                  <span className="text-muted-foreground shrink-0 text-xs">
                    {Number(c.findings).toLocaleString()}
                  </span>
                </div>
                <div className="bg-muted h-1 w-full rounded-full">
                  <div
                    className="h-1 rounded-full bg-blue-700 dark:bg-blue-500"
                    style={{
                      width: `${(Number(c.findings) / Number(max)) * 100}%`,
                    }}
                  />
                </div>
              </div>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

function RuleBreakdown({
  rules,
  isLoading,
}: {
  rules: Array<{ ruleId: string; source: string; findings: number }>;
  isLoading: boolean;
}) {
  if (isLoading && rules.length === 0) {
    return (
      <div className="text-muted-foreground rounded-lg border p-4 text-sm">
        Loading rule breakdown...
      </div>
    );
  }
  if (rules.length === 0) return null;
  const max = rules[0]?.findings || 1;

  return (
    <div className="space-y-3 rounded-lg border p-4">
      <h4 className="text-sm font-medium">Findings by rule</h4>
      <ul className="space-y-2">
        {rules.map((r, i) => {
          const label = r.ruleId
            ? getRuleTitleFallback(r.ruleId)
            : "(no rule_id)";
          return (
            <li
              key={r.ruleId || `__none_${i}`}
              className="flex items-center gap-3"
            >
              <span className="text-muted-foreground w-4 shrink-0 text-right text-xs">
                {i + 1}
              </span>
              <div className="min-w-0 flex-1">
                <div className="mb-1 flex items-center justify-between gap-2">
                  <span
                    className="truncate text-sm"
                    title={r.ruleId || undefined}
                  >
                    {label}
                  </span>
                  <span className="text-muted-foreground shrink-0 text-xs">
                    {Number(r.findings).toLocaleString()}
                  </span>
                </div>
                <div className="bg-muted h-1 w-full rounded-full">
                  <div
                    className="h-1 rounded-full bg-blue-700 dark:bg-blue-500"
                    style={{
                      width: `${(Number(r.findings) / Number(max)) * 100}%`,
                    }}
                  />
                </div>
              </div>
            </li>
          );
        })}
      </ul>
    </div>
  );
}
