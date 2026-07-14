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

export default function RiskOverviewUsersIndex(): JSX.Element {
  return (
    <RequireScope scope="org:admin" level="page">
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <RiskOverviewUsersIndexContent />
        </Page.Body>
      </Page>
    </RequireScope>
  );
}

function RiskOverviewUsersIndexContent() {
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
  const users = useMemo(
    () => overviewQuery.data?.topUsers ?? [],
    [overviewQuery.data],
  );
  const total = users.reduce((acc, u) => acc + Number(u.findings), 0);

  const userDetailRoute = (
    routes.riskOverview as unknown as {
      userDetail?: { href: (...params: string[]) => string };
    }
  ).userDetail;

  const userItems = useMemo<RankedBarItem[]>(
    () =>
      users.map((u) => ({
        label: u.email || u.externalUserId || "Unknown user",
        value: Number(u.findings),
        href:
          u.externalUserId && userDetailRoute
            ? `${userDetailRoute.href(
                encodeURIComponent(u.externalUserId),
              )}${location.search}`
            : undefined,
        sublabel:
          u.externalUserId && u.externalUserId !== u.email
            ? u.externalUserId
            : undefined,
      })),
    [users, userDetailRoute, location.search],
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
            Users
            <ReleaseStageBadge stage="beta" />
          </span>
        }
        subtitle={`All users with finding counts${rangeLabel ? ` across ${rangeLabel}.` : ""}`}
        actions={controls}
      />
      <ListLayout.List>
        {overviewQuery.isLoading ? (
          <div className="text-muted-foreground flex items-center justify-center gap-2 py-12">
            <LoaderCircle className="size-5 animate-spin" />
            <span>Loading users...</span>
          </div>
        ) : users.length === 0 ? (
          <InlineEmptyState
            className="py-12"
            icon={<Inbox />}
            title="No users with findings in this time range"
          />
        ) : (
          <Card>
            <RankedBar items={userItems} formatValue={formatFindingsValue} />
          </Card>
        )}
      </ListLayout.List>
    </ListLayout>
  );
}
