import { RankedBar, type RankedBarItem } from "@/components/chart/RankedBar";
import {
  formatDateRangeLabel,
  useDateRangeFilter,
} from "@/components/observe/useDateRangeFilter";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { useRoutes } from "@/routes";
import { type DateRangePreset } from "@gram-ai/elements";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { Card } from "@/components/ui/card";
import { InlineEmptyState } from "@/components/ui/inline-empty-state";
import { useRiskOverview } from "@gram/client/react-query/riskOverview.js";
import { Inbox, LoaderCircle } from "lucide-react";
import { useCallback, useMemo } from "react";
import { useLocation } from "react-router";
import { getRuleTitleFallback } from "./risk-utils";

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

export default function RiskOverviewRulesIndex(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <RiskOverviewRulesIndexContent />
        </Page.Body>
      </Page>
    </RequireScope>
  );
}

function RiskOverviewRulesIndexContent() {
  const routes = useRoutes();
  const location = useLocation();
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
  const rules = useMemo(
    () => overviewQuery.data?.topRules ?? [],
    [overviewQuery.data],
  );
  const total = rules.reduce((acc, r) => acc + Number(r.findings), 0);

  const riskEventsHref = routes.riskEvents.href();

  const ruleItems = useMemo<RankedBarItem[]>(
    () =>
      rules.map((r) => {
        const params = new URLSearchParams(location.search);
        if (r.ruleId) params.set("rule_id", r.ruleId);
        return {
          label: r.ruleId ? getRuleTitleFallback(r.ruleId) : "(no rule_id)",
          value: Number(r.findings),
          href: r.ruleId ? `${riskEventsHref}?${params.toString()}` : undefined,
          sublabel: r.ruleId || undefined,
        };
      }),
    [rules, riskEventsHref, location.search],
  );

  const formatFindingsValue = useCallback(
    (value: number) =>
      total > 0
        ? `${value.toLocaleString()} (${((value / total) * 100).toFixed(1)}%)`
        : value.toLocaleString(),
    [total],
  );

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
    <Page.Section>
      <Page.Section.Title stage="beta">Rules</Page.Section.Title>
      <Page.Section.Description>
        All rules with finding counts
        {rangeLabel && ` across ${rangeLabel}.`}
      </Page.Section.Description>
      <Page.Section.CTA>{controls}</Page.Section.CTA>
      <Page.Section.Body>
        {overviewQuery.isLoading ? (
          <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
            <LoaderCircle className="size-5 animate-spin" />
            <span>Loading rules...</span>
          </div>
        ) : rules.length === 0 ? (
          <InlineEmptyState
            className="py-12"
            icon={<Inbox />}
            title="No rules with findings in this time range"
          />
        ) : (
          <Card>
            <RankedBar items={ruleItems} formatValue={formatFindingsValue} />
          </Card>
        )}
      </Page.Section.Body>
    </Page.Section>
  );
}
