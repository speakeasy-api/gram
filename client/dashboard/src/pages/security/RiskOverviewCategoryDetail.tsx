import { MetricCard } from "@/components/chart/MetricCard";
import {
  formatDateRangeLabel,
  useDateRangeFilter,
} from "@/components/observe/useDateRangeFilter";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Drawer, DrawerContent } from "@/components/ui/drawer";
import { useSdkClient } from "@/contexts/Sdk";
import { ChatDetailPanel } from "@/pages/chatLogs/ChatDetailPanel";
import { TimeRangePicker, type DateRangePreset } from "@gram-ai/elements";
import type { RiskResult } from "@gram/client/models/components";
import { useRiskOverview } from "@gram/client/react-query/index.js";
import { Icon } from "@speakeasy-api/moonshine";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useCallback, useMemo, useRef } from "react";
import { useParams, useSearchParams } from "react-router";
import { RULE_CATEGORY_META, type RuleCategory } from "./policy-data";
import {
  CategoryLabel,
  MaskedMatch,
  RevealAllProvider,
  RevealAllToggle,
  RuleLabel,
} from "./risk-ui";

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

export default function RiskOverviewCategoryDetail() {
  return (
    <RequireScope scope="org:admin" level="page">
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <RiskOverviewCategoryDetailContent />
        </Page.Body>
      </Page>
    </RequireScope>
  );
}

function RiskOverviewCategoryDetailContent() {
  const { category: encodedCategory = "" } = useParams<{ category: string }>();
  const category = decodeURIComponent(encodedCategory);
  const client = useSdkClient();
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
  const overviewCategory = useMemo(
    () =>
      overviewQuery.data?.topCategories.find((c) => c.category === category),
    [overviewQuery.data?.topCategories, category],
  );

  const resultsQuery = useInfiniteQuery({
    queryKey: [
      "risk",
      "results",
      "list",
      "by-category",
      category,
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: async ({ pageParam }) => {
      return client.risk.results.list({
        cursor: pageParam,
        limit: 50,
        category,
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
  const categoryMeta = RULE_CATEGORY_META[category as RuleCategory];
  const categoryLabel = categoryMeta?.label ?? category;

  const scrollRef = useRef<HTMLDivElement>(null);
  const handleScroll = useCallback(
    (e: React.UIEvent<HTMLDivElement>) => {
      const container = e.currentTarget;
      const distanceFromBottom =
        container.scrollHeight - (container.scrollTop + container.clientHeight);
      if (resultsQuery.isFetchingNextPage || resultsQuery.isFetching) return;
      if (!resultsQuery.hasNextPage) return;
      if (distanceFromBottom < 200) {
        resultsQuery.fetchNextPage();
      }
    },
    [resultsQuery],
  );

  const controls = (
    <div className="flex items-center gap-2">
      <RevealAllToggle />
      <TimeRangePicker
        preset={customRange ? null : dateRange}
        customRange={customRange}
        customRangeLabel={customRangeLabel}
        availablePresets={RISK_OVERVIEW_PRESETS}
        onPresetChange={setDateRangeParam}
        onCustomRangeChange={setCustomRangeParam}
        onClearCustomRange={clearCustomRange}
      />
    </div>
  );

  return (
    <RevealAllProvider>
      <Page.Section>
        <Page.Section.Title stage="beta">{categoryLabel}</Page.Section.Title>
        <Page.Section.Description>
          {categoryMeta?.description ?? "Risk findings in this category"}
          {rangeLabel && ` across ${rangeLabel}.`}
        </Page.Section.Description>
        <Page.Section.CTA>{controls}</Page.Section.CTA>
        <Page.Section.Body>
          <div className="space-y-6">
            <div className="grid grid-cols-2 gap-4 md:grid-cols-3">
              <MetricCard
                title="Findings"
                value={overviewCategory?.findings ?? totalCount}
                format="number"
                icon="flag"
              />
            </div>
            <ResultsTable
              results={results}
              isLoading={resultsQuery.isLoading}
              scrollRef={scrollRef}
              onScroll={handleScroll}
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
    </RevealAllProvider>
  );
}

function ResultsTable({
  results,
  isLoading,
  scrollRef,
  onScroll,
  onSelectChat,
}: {
  results: RiskResult[];
  isLoading: boolean;
  scrollRef: React.RefObject<HTMLDivElement | null>;
  onScroll: (e: React.UIEvent<HTMLDivElement>) => void;
  onSelectChat: (chatId: string) => void;
}) {
  if (isLoading) {
    return (
      <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
        <Icon name="loader-circle" className="size-5 animate-spin" />
        <span>Loading findings...</span>
      </div>
    );
  }

  if (results.length === 0) {
    return (
      <div className="flex flex-col items-center gap-3 py-12 text-center">
        <div className="bg-muted flex size-12 items-center justify-center rounded-full">
          <Icon name="inbox" className="text-muted-foreground size-6" />
        </div>
        <span className="text-foreground font-medium">
          No findings in this category for this time range
        </span>
      </div>
    );
  }

  return (
    <div
      ref={scrollRef}
      onScroll={onScroll}
      className="max-h-[70vh] overflow-y-auto rounded-lg border"
    >
      <table className="w-full table-fixed text-sm">
        <colgroup>
          <col className="w-[180px]" />
          <col className="w-[200px]" />
          <col />
          <col className="w-[160px]" />
          <col className="w-[280px]" />
          <col className="w-[48px]" />
        </colgroup>
        <thead className="bg-muted text-muted-foreground sticky top-0 z-10 text-xs font-medium tracking-wide uppercase shadow-[0_1px_0_0_var(--color-border)]">
          <tr>
            <th className="px-4 py-2 text-left">Time</th>
            <th className="px-4 py-2 text-left">Rule</th>
            <th className="px-4 py-2 text-left">Session</th>
            <th className="px-4 py-2 text-left">User</th>
            <th className="px-4 py-2 text-left">Match</th>
            <th className="px-4 py-2"></th>
          </tr>
        </thead>
        <tbody className="divide-border divide-y">
          {results.map((result) => (
            <tr
              key={result.id}
              role={result.chatId ? "button" : undefined}
              tabIndex={result.chatId ? 0 : undefined}
              onClick={() => {
                if (result.chatId) onSelectChat(result.chatId);
              }}
              onKeyDown={(e) => {
                if (!result.chatId) return;
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  onSelectChat(result.chatId);
                }
              }}
              className={
                result.chatId
                  ? "hover:bg-muted/30 cursor-pointer"
                  : "hover:bg-muted/30"
              }
            >
              <td className="text-muted-foreground truncate px-4 py-3 font-mono text-xs">
                {result.createdAt
                  ? new Date(result.createdAt).toLocaleString()
                  : "-"}
              </td>
              <td className="px-4 py-3">
                <div className="flex min-w-0 flex-col gap-1">
                  <CategoryLabel
                    source={result.source}
                    ruleId={result.ruleId}
                  />
                  <RuleLabel source={result.source} ruleId={result.ruleId} />
                </div>
              </td>
              <td className="truncate px-4 py-3">
                {result.chatTitle || "Untitled"}
              </td>
              <td className="text-muted-foreground truncate px-4 py-3">
                {result.userId ?? "-"}
              </td>
              <td className="px-4 py-3">
                <MaskedMatch value={result.match} />
              </td>
              <td className="px-4 py-3 text-right">
                {result.chatId && (
                  <Icon
                    name="chevron-right"
                    className="text-muted-foreground size-4"
                  />
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
