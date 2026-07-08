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
import { Icon } from "@speakeasy-api/moonshine";
import { useMemo } from "react";
import { Link, useLocation } from "react-router";
import { RULE_CATEGORY_META, type RuleCategory } from "./policy-data";

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

export default function RiskOverviewCategoriesIndex(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <RiskOverviewCategoriesIndexContent />
        </Page.Body>
      </Page>
    </RequireScope>
  );
}

function RiskOverviewCategoriesIndexContent() {
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
  const categories = overviewQuery.data?.topCategories ?? [];
  const total = categories.reduce((acc, c) => acc + Number(c.findings), 0);
  const max = categories[0]?.findings ?? 0;

  const categoryDetailRoute = (
    routes.riskOverview as unknown as {
      categoryDetail?: { href: (...params: string[]) => string };
    }
  ).categoryDetail;

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
      <Page.Section.Title stage="beta">Risk categories</Page.Section.Title>
      <Page.Section.Description>
        All categories with finding counts
        {rangeLabel && ` across ${rangeLabel}.`}
      </Page.Section.Description>
      <Page.Section.CTA>{controls}</Page.Section.CTA>
      <Page.Section.Body>
        {overviewQuery.isLoading ? (
          <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
            <Icon name="loader-circle" className="size-5 animate-spin" />
            <span>Loading categories...</span>
          </div>
        ) : categories.length === 0 ? (
          <div className="flex flex-col items-center gap-3 py-12 text-center">
            <div className="bg-muted flex size-12 items-center justify-center rounded-full">
              <Icon name="inbox" className="text-muted-foreground size-6" />
            </div>
            <span className="text-foreground font-medium">
              No categories with findings in this time range
            </span>
          </div>
        ) : (
          <ul className="divide-border divide-y rounded-lg border">
            {categories.map((c, i) => {
              const meta = RULE_CATEGORY_META[c.category as RuleCategory];
              const label = meta?.label ?? c.category;
              const href = categoryDetailRoute
                ? `${categoryDetailRoute.href(
                    encodeURIComponent(c.category),
                  )}${location.search}`
                : null;
              const pct =
                max > 0 ? (Number(c.findings) / Number(max)) * 100 : 0;
              const totalPct =
                total > 0 ? (Number(c.findings) / total) * 100 : 0;
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
                        {Number(c.findings).toLocaleString()}
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
                    {meta?.description && (
                      <div className="text-muted-foreground mt-1 text-xs">
                        {meta.description}
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
                <li key={c.category}>
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
