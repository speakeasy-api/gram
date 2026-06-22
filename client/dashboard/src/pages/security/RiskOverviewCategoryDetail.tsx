import { MetricCard } from "@/components/chart/MetricCard";
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
import type { RiskResult } from "@gram/client/models/components";
import {
  useRiskOverview,
  useRiskRuleBreakdown,
} from "@gram/client/react-query/index.js";
import { Icon } from "@speakeasy-api/moonshine";
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
      className="isolate max-h-[70vh] overflow-y-auto rounded-lg border"
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
        <thead className="bg-muted text-muted-foreground sticky top-0 z-[1] text-xs font-medium tracking-wide uppercase shadow-[0_1px_0_0_var(--color-border)]">
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
      <div className="text-muted-foreground rounded-lg border p-4 text-sm">
        Loading rule breakdown...
      </div>
    );
  }
  if (rules.length === 0) return null;
  const max = rules[0]?.findings || 1;

  return (
    <div className="space-y-3 rounded-lg border p-4">
      <div className="flex items-center justify-between">
        <h4 className="text-sm font-medium">Findings by rule</h4>
        {activeRuleId && (
          <button
            type="button"
            onClick={() => onSelectRule("")}
            className="text-muted-foreground hover:text-foreground text-xs"
          >
            Clear filter
          </button>
        )}
      </div>
      <ul className="space-y-2">
        {rules.map((rule, i) => {
          const isActive = activeRuleId === rule.ruleId;
          const label = rule.ruleId
            ? getRuleTitleFallback(rule.ruleId)
            : "(no rule_id)";
          return (
            <li key={rule.ruleId || `__none_${i}`}>
              <button
                type="button"
                onClick={() => onSelectRule(isActive ? "" : rule.ruleId)}
                aria-pressed={isActive}
                className={`hover:bg-muted/40 -mx-2 flex w-full items-center gap-3 rounded px-2 py-1.5 transition-colors ${
                  isActive ? "bg-muted" : ""
                }`}
              >
                <span className="text-muted-foreground w-4 shrink-0 text-right text-xs">
                  {i + 1}
                </span>
                <div className="min-w-0 flex-1">
                  <div className="mb-1 flex items-center justify-between gap-2">
                    <span
                      className="truncate text-left text-sm"
                      title={rule.ruleId}
                    >
                      {label}
                    </span>
                    <span className="text-muted-foreground shrink-0 text-xs">
                      {rule.findings.toLocaleString()}
                    </span>
                  </div>
                  <div className="bg-muted h-1 w-full rounded-full">
                    <div
                      className="h-1 rounded-full bg-blue-700 dark:bg-blue-500"
                      style={{ width: `${(rule.findings / max) * 100}%` }}
                    />
                  </div>
                </div>
              </button>
            </li>
          );
        })}
      </ul>
    </div>
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
    <div className="border-border focus-within:border-ring inline-flex h-9 items-center gap-2 rounded-md border px-2">
      <Icon name="search" className="text-muted-foreground size-4 shrink-0" />
      <input
        type="text"
        value={local}
        onChange={(e) => setLocal(e.target.value)}
        placeholder="Rule ID contains..."
        className="placeholder:text-muted-foreground w-[240px] bg-transparent text-sm outline-none"
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
        <button
          type="button"
          onClick={() => setLocal("")}
          className="text-muted-foreground hover:text-foreground"
          aria-label="Clear rule filter"
        >
          <Icon name="x" className="size-3.5" />
        </button>
      )}
    </div>
  );
}
