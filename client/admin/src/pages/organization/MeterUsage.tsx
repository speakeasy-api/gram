import { type JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";
import { RefreshCw, RotateCcw } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { errorMessage } from "@/lib/gramAdminApi";
import { organizationMeterUsageQuery } from "@/lib/gramAdminClient";
import {
  formatDailyMeterRate,
  formatMeterQuantity,
  type MeterFamily,
} from "./meterUsage";
import { exclusiveEnd, type BillingUsageSearch } from "./billingUsageSearch";
import { MeterUsageChart } from "./MeterUsageChart";
import { MeterUsagePeriod } from "./MeterUsagePeriod";

const route = getRouteApi("/organizations/$idOrSlug/billing");
const products: Record<MeterFamily, { label: string; description: string }> = {
  agent_session_storage: {
    label: "Storage",
    description: "Tokens under management",
  },
  mcp_bandwidth: {
    label: "MCP bandwidth",
    description: "MCP data transferred",
  },
  risk_content_scans: {
    label: "Risk scans",
    description: "Tokens scanned for risk",
  },
};

export function MeterUsage({
  organizationID,
}: {
  organizationID: string;
}): JSX.Element {
  const search = route.useSearch();
  const navigate = route.useNavigate();
  const family = search.product ?? "agent_session_storage";
  const granularity = search.interval ?? "daily";
  const cumulative = search.cumulative ?? false;
  const query = useQuery(
    organizationMeterUsageQuery({
      organizationId: organizationID,
      family,
      from: search.from ? new Date(`${search.from}T00:00:00Z`) : undefined,
      to: search.to ? new Date(exclusiveEnd(search.to)) : undefined,
    }),
  );
  const data = query.data;
  const update = (patch: BillingUsageSearch): void => {
    void navigate({
      search: (previous) => ({ ...previous, ...patch }),
      resetScroll: false,
    });
  };

  return (
    <section aria-labelledby="meter-usage-title" className="min-w-0 space-y-4">
      <div>
        <h3 id="meter-usage-title" className="text-lg font-semibold">
          Meter usage
        </h3>
        <p className="text-muted-foreground mt-1 text-sm">
          Storage, MCP bandwidth, and risk-scanning totals by UTC day. Today’s
          usage updates as readings arrive. These are not invoice estimates.
        </p>
      </div>
      <Tabs
        value={family}
        onValueChange={(product) => update({ product: product as MeterFamily })}
      >
        <div className="bg-card flex flex-wrap items-center justify-between gap-3 rounded-md border p-3">
          <TabsList aria-label="Usage product">
            {Object.entries(products).map(([value, product]) => (
              <TabsTrigger key={value} value={value}>
                {product.label}
              </TabsTrigger>
            ))}
          </TabsList>
          <div className="flex flex-wrap items-center gap-2">
            <Button
              variant="ghost"
              size="sm"
              onClick={() => void navigate({ search: {}, resetScroll: false })}
            >
              <RotateCcw />
              Reset
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={query.isFetching}
              onClick={() => void query.refetch()}
            >
              <RefreshCw
                className={
                  query.isFetching
                    ? "animate-spin motion-reduce:animate-none"
                    : undefined
                }
              />
              Refresh
            </Button>
          </div>
          {data && (
            <div className="w-full">
              <MeterUsagePeriod
                window={data.window}
                cycles={data.billingCycles}
                onChange={(from, to) => update({ from, to })}
              />
            </div>
          )}
        </div>
        <TabsContent
          value={family}
          className="space-y-4 pt-2"
          aria-busy={query.isFetching}
        >
          {query.isError && (
            <p
              role="alert"
              className="text-destructive rounded-md border p-4 text-sm"
            >
              Could not load meter usage: {errorMessage(query.error)}.{" "}
              {data ? "Showing the last successful reading. " : ""}Use Refresh
              to retry.
            </p>
          )}
          {!data && query.isPending && (
            <div
              role="status"
              aria-label="Loading meter usage"
              className="space-y-4"
            >
              <Skeleton className="h-28 w-full" />
              <Skeleton className="h-96 w-full" />
            </div>
          )}
          {data && (
            <>
              <dl className="bg-card grid divide-y rounded-md border sm:grid-cols-2 sm:divide-x sm:divide-y-0">
                <UsageStat
                  label="Total usage"
                  value={formatMeterQuantity(data.total, data.unit)}
                  description={products[family].description}
                  exact={`${BigInt(data.total).toLocaleString("en-US")} ${data.unit === "bytes" ? "bytes" : "tokens"}`}
                />
                <UsageStat
                  label="Average daily usage"
                  value={formatDailyMeterRate(data)}
                  description="Elapsed selected duration, capped at retrieval time"
                />
              </dl>
              <div className="bg-card min-w-0 rounded-md border p-4">
                <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
                  <h4 className="text-sm font-medium">
                    {products[family].description} over time
                  </h4>
                  <div className="flex flex-wrap items-center gap-4">
                    <div
                      role="group"
                      aria-label="Usage interval"
                      className="flex gap-1"
                    >
                      {(["daily", "weekly", "monthly"] as const).map(
                        (interval) => (
                          <Button
                            key={interval}
                            variant={
                              granularity === interval ? "secondary" : "ghost"
                            }
                            size="sm"
                            aria-pressed={granularity === interval}
                            onClick={() => update({ interval })}
                          >
                            {interval[0]?.toUpperCase()}
                            {interval.slice(1)}
                          </Button>
                        ),
                      )}
                    </div>
                    <label className="flex items-center gap-2 text-sm">
                      <Switch
                        checked={cumulative}
                        onCheckedChange={(checked) =>
                          update({ cumulative: checked || undefined })
                        }
                      />
                      Cumulative
                    </label>
                  </div>
                </div>
                {BigInt(data.total) === 0n && (
                  <p
                    role="status"
                    className="text-muted-foreground mb-3 text-sm"
                  >
                    No usage recorded for this product in the selected period.
                  </p>
                )}
                <MeterUsageChart
                  data={data}
                  granularity={granularity}
                  cumulative={cumulative}
                />
                <div className="text-muted-foreground mt-3 flex flex-wrap items-center justify-between gap-2 text-xs">
                  <span>
                    UTC ·{" "}
                    {granularity === "weekly" ? "Weeks start Monday. " : ""}
                    Partial periods include only the selected dates.
                  </span>
                  <span>
                    Retrieved{" "}
                    {data.queriedAt.toLocaleString("en-US", {
                      timeZone: "UTC",
                    })}{" "}
                    UTC
                  </span>
                </div>
              </div>
            </>
          )}
        </TabsContent>
      </Tabs>
    </section>
  );
}

function UsageStat({
  label,
  value,
  description,
  exact,
}: {
  label: string;
  value: string;
  description: string;
  exact?: string;
}): JSX.Element {
  return (
    <div className="min-w-0 p-5">
      <dt className="text-muted-foreground text-xs font-medium">{label}</dt>
      <dd
        className="mt-2 text-3xl font-semibold tracking-tight tabular-nums"
        title={exact}
      >
        {value}
      </dd>
      <dd className="text-muted-foreground mt-2 text-xs">{description}</dd>
    </div>
  );
}
