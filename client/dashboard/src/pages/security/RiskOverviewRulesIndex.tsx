import {
  formatDateRangeLabel,
  useDateRangeFilter,
} from "@/components/observe/useDateRangeFilter";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { useRoutes } from "@/routes";
import { type DateRangePreset } from "@gram-ai/elements";
import { TimeRangePicker } from "@/components/DashboardTimeRangePicker";
import { useRiskOverview } from "@gram/client/react-query/riskOverview.js";
import { Icon } from "@/components/ui/moonshine";
import { useMemo } from "react";
import { Link, useLocation } from "react-router";
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
  const rules = overviewQuery.data?.topRules ?? [];
  const total = rules.reduce((acc, r) => acc + Number(r.findings), 0);
  const max = rules[0]?.findings ?? 0;

  const riskEventsHref = routes.riskEvents.href();

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
            <Icon name="loader-circle" className="size-5 animate-spin" />
            <span>Loading rules...</span>
          </div>
        ) : rules.length === 0 ? (
          <div className="flex flex-col items-center gap-3 py-12 text-center">
            <div className="bg-muted flex size-12 items-center justify-center rounded-full">
              <Icon name="inbox" className="text-muted-foreground size-6" />
            </div>
            <span className="text-foreground font-medium">
              No rules with findings in this time range
            </span>
          </div>
        ) : (
          <ul className="divide-border divide-y rounded-lg border">
            {rules.map((r, i) => {
              const label = r.ruleId
                ? getRuleTitleFallback(r.ruleId)
                : "(no rule_id)";
              const params = new URLSearchParams(location.search);
              if (r.ruleId) params.set("rule_id", r.ruleId);
              const href = r.ruleId
                ? `${riskEventsHref}?${params.toString()}`
                : null;
              const pct =
                max > 0 ? (Number(r.findings) / Number(max)) * 100 : 0;
              const totalPct =
                total > 0 ? (Number(r.findings) / total) * 100 : 0;
              const body = (
                <div className="flex items-center gap-4 px-4 py-3">
                  <span className="text-muted-foreground w-6 shrink-0 text-right text-xs">
                    {i + 1}
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="mb-1 flex items-center justify-between gap-2">
                      <span className="truncate text-sm font-medium">
                        {label}
                      </span>
                      <span className="text-muted-foreground shrink-0 text-xs tabular-nums">
                        {Number(r.findings).toLocaleString()}
                        {total > 0 && (
                          <span className="ml-2">({totalPct.toFixed(1)}%)</span>
                        )}
                      </span>
                    </div>
                    <div className="bg-muted h-1 w-full rounded-full">
                      <div
                        className="h-1 rounded-full bg-blue-700 dark:bg-blue-500"
                        style={{ width: `${pct}%` }}
                      />
                    </div>
                    {r.ruleId && (
                      <div className="text-muted-foreground mt-1 truncate font-mono text-xs">
                        {r.ruleId}
                      </div>
                    )}
                  </div>
                  {href && (
                    <Icon
                      name="chevron-right"
                      className="text-muted-foreground size-4 shrink-0"
                    />
                  )}
                </div>
              );
              return (
                <li key={r.ruleId || `__none_${i}`}>
                  {href ? (
                    <Link to={href} className="hover:bg-muted/40 block">
                      {body}
                    </Link>
                  ) : (
                    body
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </Page.Section.Body>
    </Page.Section>
  );
}
