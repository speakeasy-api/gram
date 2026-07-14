import { RankedBar, type RankedBarItem } from "@/components/chart/RankedBar";
import {
  formatDateRangeLabel,
  useDateRangeFilter,
} from "@/components/observe/useDateRangeFilter";
import { Page } from "@/components/page-layout";
import { ListLayout } from "@/components/layouts/list-layout";
import { ReleaseStageBadge } from "@/components/release-stage-badge";
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
  const categories = useMemo(
    () => overviewQuery.data?.topCategories ?? [],
    [overviewQuery.data],
  );
  const total = categories.reduce((acc, c) => acc + Number(c.findings), 0);

  const categoryDetailRoute = (
    routes.riskOverview as unknown as {
      categoryDetail?: { href: (...params: string[]) => string };
    }
  ).categoryDetail;

  const categoryItems = useMemo<RankedBarItem[]>(
    () =>
      categories.map((c) => {
        const meta = RULE_CATEGORY_META[c.category as RuleCategory];
        return {
          label: meta?.label ?? c.category,
          value: Number(c.findings),
          href: categoryDetailRoute
            ? `${categoryDetailRoute.href(
                encodeURIComponent(c.category),
              )}${location.search}`
            : undefined,
          sublabel: meta?.description,
        };
      }),
    [categories, categoryDetailRoute, location.search],
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
    <ListLayout>
      <ListLayout.Header
        title={
          <span className="inline-flex items-center gap-2">
            Risk categories
            <ReleaseStageBadge stage="beta" />
          </span>
        }
        subtitle={`All categories with finding counts${rangeLabel ? ` across ${rangeLabel}.` : ""}`}
        actions={controls}
      />
      <ListLayout.List>
        {overviewQuery.isLoading ? (
          <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
            <LoaderCircle className="size-5 animate-spin" />
            <span>Loading categories...</span>
          </div>
        ) : categories.length === 0 ? (
          <InlineEmptyState
            className="py-12"
            icon={<Inbox />}
            title="No categories with findings in this time range"
          />
        ) : (
          <Card>
            <RankedBar
              items={categoryItems}
              formatValue={formatFindingsValue}
            />
          </Card>
        )}
      </ListLayout.List>
    </ListLayout>
  );
}
