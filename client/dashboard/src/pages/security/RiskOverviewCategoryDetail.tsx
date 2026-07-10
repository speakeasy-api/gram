import { MetricCard } from "@/components/chart/MetricCard";
import { RankedBar, type RankedBarItem } from "@/components/chart/RankedBar";
import {
  formatDateRangeLabel,
  useDateRangeFilter,
} from "@/components/observe/useDateRangeFilter";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { useSdkClient } from "@/contexts/Sdk";
import { ChatDetailSheet } from "@/pages/chatLogs/ChatDetailPanel";
import { type DateRangePreset } from "@gram-ai/elements";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { Button } from "@/components/ui/moonshine";
import { Card } from "@/components/ui/card";
import { Heading } from "@/components/ui/heading";
import { InlineEmptyState } from "@/components/ui/inline-empty-state";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "@/components/ui/input-group";
import { Type } from "@/components/ui/type";
import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { useRiskOverview } from "@gram/client/react-query/riskOverview.js";
import { useRiskRuleBreakdown } from "@gram/client/react-query/riskRuleBreakdown.js";
import { ChevronRight, Inbox, LoaderCircle, Search, X } from "lucide-react";
import { useInfiniteQuery } from "@tanstack/react-query";
import {
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
} from "react";
import { useParams, useSearchParams } from "react-router";
import { RULE_CATEGORY_META, type RuleCategory } from "./policy-data";
import { getRuleTitleFallback } from "./risk-utils";
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

export default function RiskOverviewCategoryDetail(): JSX.Element {
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
  const ruleFilter = searchParams.get("rule_id") ?? "";
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
  const setRuleFilter = useCallback(
    (ruleId: string) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (ruleId) {
            next.set("rule_id", ruleId);
          } else {
            next.delete("rule_id");
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

  const ruleBreakdownQuery = useRiskRuleBreakdown({ category, from, to });

  const resultsQuery = useInfiniteQuery({
    queryKey: [
      "risk",
      "results",
      "list",
      "by-category",
      category,
      ruleFilter,
      from.toISOString(),
      to.toISOString(),
    ],
    queryFn: async ({ pageParam }) => {
      return client.risk.results.list({
        cursor: pageParam,
        limit: 50,
        category,
        ruleId: ruleFilter || undefined,
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
        void resultsQuery.fetchNextPage();
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
                format="compact"
                icon="flag"
              />
            </div>
            <RuleBreakdown
              rules={ruleBreakdownQuery.data?.rules ?? []}
              isLoading={ruleBreakdownQuery.isLoading}
              activeRuleId={ruleFilter}
              onSelectRule={setRuleFilter}
            />
            <div className="flex items-center gap-2">
              <RuleIdFilter
                value={ruleFilter}
                onChange={setRuleFilter}
                suggestions={(ruleBreakdownQuery.data?.rules ?? [])
                  .map((r) => r.ruleId)
                  .filter(Boolean)}
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

      <ChatDetailSheet
        chatId={selectedChatId}
        onClose={() => setSelectedChatId(null)}
        onDelete={() => setSelectedChatId(null)}
        riskFocus
      />
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
        <LoaderCircle className="size-5 animate-spin" />
        <span>Loading findings...</span>
      </div>
    );
  }

  if (results.length === 0) {
    return (
      <InlineEmptyState
        className="py-12"
        icon={<Inbox />}
        title="No findings in this category for this time range"
      />
    );
  }

  return (
    <div
      ref={scrollRef}
      onScroll={onScroll}
      className="isolate max-h-[70vh] overflow-y-auto border"
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
        <thead className="bg-muted text-muted-foreground sticky top-0 z-[1] border-b border-border font-mono text-xs tracking-[0.08em] uppercase">
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
                <MaskedMatch
                  resultId={result.id}
                  matchRedacted={result.matchRedacted}
                />
              </td>
              <td className="px-4 py-3 text-right">
                {result.chatId && (
                  <ChevronRight className="text-muted-foreground size-4" />
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function RuleBreakdown({
  rules,
  isLoading,
  activeRuleId,
  onSelectRule,
}: {
  rules: Array<{ ruleId: string; source: string; findings: number }>;
  isLoading: boolean;
  activeRuleId: string;
  onSelectRule: (ruleId: string) => void;
}) {
  if (isLoading && rules.length === 0) {
    return (
      <Card size="sm">
        <Type muted className="text-sm">
          Loading rule breakdown...
        </Type>
      </Card>
    );
  }
  if (rules.length === 0) return null;

  const items: RankedBarItem[] = rules.map((rule) => ({
    label: rule.ruleId ? getRuleTitleFallback(rule.ruleId) : "(no rule_id)",
    value: rule.findings,
    active: activeRuleId === rule.ruleId,
    onSelect: () =>
      onSelectRule(activeRuleId === rule.ruleId ? "" : rule.ruleId),
  }));

  return (
    <Card size="sm" className="gap-3">
      <div className="flex items-center justify-between">
        <Heading variant="h6">Findings by rule</Heading>
        {activeRuleId && (
          <Button variant="tertiary" size="xs" onClick={() => onSelectRule("")}>
            <Button.Text>Clear filter</Button.Text>
          </Button>
        )}
      </div>
      <RankedBar items={items} />
    </Card>
  );
}

function RuleIdFilter({
  value,
  onChange,
  suggestions,
}: {
  value: string;
  onChange: (next: string) => void;
  suggestions?: string[];
}) {
  const [local, setLocal] = useState(value);
  const listId = useId();
  useEffect(() => {
    setLocal(value);
  }, [value]);
  useEffect(() => {
    if (local === value) return;
    const t = setTimeout(() => onChange(local), 350);
    return () => clearTimeout(t);
  }, [local, value, onChange]);

  const options = useMemo(
    () => Array.from(new Set((suggestions ?? []).filter(Boolean))),
    [suggestions],
  );

  return (
    <InputGroup className="w-64">
      <InputGroupAddon>
        <Search className="size-4" />
      </InputGroupAddon>
      <InputGroupInput
        value={local}
        onChange={(e) => setLocal(e.target.value)}
        placeholder="Rule ID contains..."
        aria-label="Filter by rule ID"
        list={options.length > 0 ? listId : undefined}
        autoComplete="off"
      />
      {options.length > 0 && (
        <datalist id={listId}>
          {options.map((opt) => (
            <option key={opt} value={opt} />
          ))}
        </datalist>
      )}
      {local && (
        <InputGroupAddon align="inline-end">
          <InputGroupButton
            size="icon-xs"
            aria-label="Clear rule filter"
            onClick={() => setLocal("")}
          >
            <X className="size-3.5" />
          </InputGroupButton>
        </InputGroupAddon>
      )}
    </InputGroup>
  );
}
