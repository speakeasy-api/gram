import { useMemo, type JSX } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";
import { ChevronDown, RefreshCw } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { errorMessage } from "@/lib/gramAdminApi";
import { customerUsageQuery } from "@/lib/gramAdminClient";
import {
  SPEND_PRODUCT_COLOR,
  type SpendProductID,
} from "@/pages/organization/spendBreakdownUtils";
import {
  CustomerUsageCard,
  CustomerUsageCardSkeleton,
} from "./CustomerUsageCard";
import {
  ALL_PRODUCT_IDS,
  CUSTOMER_PLANS,
  customerUsageControls,
  plansSearchValue,
  productsSearchValue,
  type CustomerPlan,
  type CustomerUsageSearch,
  type CustomerUsageSort,
} from "./customerUsageSearch";
import {
  PLAN_LABELS,
  customerUsageView,
  planFilterLabel,
  type CustomerUsageSummary,
} from "./customerUsageUtils";

const route = getRouteApi("/customer-usage");

const PRODUCT_LABELS: Record<SpendProductID, string> = {
  agent_session_storage: "Agent session storage",
  risk_content_scans: "Risk scanning",
  mcp_egress: "MCP gateway",
};

const SORT_LABELS: Record<CustomerUsageSort, string> = {
  spend: "Spend (high to low)",
  change: "Biggest change",
  name: "Customer name (A to Z)",
  plan: "Plan type",
};

const SKELETON_CARDS = 6;

// 3 columns from 1440px of content width, 2 from 1024px, 1 below. Measured on
// the page's own width rather than the viewport, so the sidebar does not count.
const GRID_CLASSES =
  "grid grid-cols-1 items-stretch gap-4 @min-[1024px]:grid-cols-2 @min-[1440px]:grid-cols-3";

export function CustomerUsage(): JSX.Element {
  const search = route.useSearch();
  const navigate = route.useNavigate();
  const controls = useMemo(() => customerUsageControls(search), [search]);
  const query = useQuery({
    ...customerUsageQuery({ interval: controls.interval }),
    placeholderData: keepPreviousData,
  });
  const view = useMemo(
    () =>
      query.data
        ? customerUsageView(query.data.customers, controls)
        : undefined,
    [query.data, controls],
  );

  const update = (patch: CustomerUsageSearch): void => {
    void navigate({
      search: (previous) => ({ ...previous, ...patch }),
      replace: true,
      resetScroll: false,
    });
  };
  const reset = (): void => {
    void navigate({ search: {}, replace: true, resetScroll: false });
  };
  const setPlanSelected = (plan: CustomerPlan, selected: boolean): void => {
    const next = new Set(controls.plans);
    if (selected) next.add(plan);
    else next.delete(plan);
    update({ plans: plansSearchValue(next) });
  };
  const setProductSelected = (
    productID: SpendProductID,
    selected: boolean,
  ): void => {
    const next = new Set(controls.products);
    if (selected) next.add(productID);
    else next.delete(productID);
    update({ products: productsSearchValue(next) });
  };

  const renderCard = (
    summary: CustomerUsageSummary,
    collapsible: boolean,
  ): JSX.Element | null =>
    query.data ? (
      <CustomerUsageCard
        key={summary.customer.organizationId}
        summary={summary}
        queriedAt={query.data.queriedAt}
        selectedProducts={controls.products}
        interval={controls.interval}
        cumulative={controls.cumulative}
        collapsible={collapsible}
        onRetry={() => void query.refetch()}
        isRetrying={query.isFetching}
      />
    ) : null;

  const noMatches =
    view !== undefined && view.active.length === 0 && view.idle.length === 0;

  return (
    <div className="@container space-y-6 p-6">
      <div className="space-y-2">
        <h1 className="text-2xl font-semibold">Customer usage</h1>
        <p className="text-muted-foreground text-sm">
          Usage estimated at current PAYG list prices. This is not an invoice or
          the organization’s actual contracted charge. Inference is excluded.
        </p>
      </div>

      <div className="bg-card space-y-3 rounded-md border p-3">
        <div className="flex flex-wrap items-center gap-2">
          <Input
            aria-label="Search customers"
            placeholder="Search customers"
            className="w-full sm:w-64"
            value={controls.q}
            onChange={(event) => update({ q: event.target.value || undefined })}
          />
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="outline"
                aria-label={`Plans: ${planFilterLabel(controls.plans)}`}
                className="font-normal"
              >
                {planFilterLabel(controls.plans)}
                <ChevronDown className="text-muted-foreground" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start">
              {CUSTOMER_PLANS.map((plan) => (
                <DropdownMenuCheckboxItem
                  key={plan}
                  checked={controls.plans.has(plan)}
                  // Keep the menu open so several plans can be picked in a row.
                  onSelect={(event) => event.preventDefault()}
                  onCheckedChange={(checked) => setPlanSelected(plan, checked)}
                >
                  {PLAN_LABELS[plan]}
                </DropdownMenuCheckboxItem>
              ))}
              {controls.plans.size > 0 && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuItem
                    onSelect={() => update({ plans: undefined })}
                  >
                    Show all plans
                  </DropdownMenuItem>
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
          <Select
            value={controls.sort}
            onValueChange={(value) =>
              update({
                sort:
                  value === "spend" ? undefined : (value as CustomerUsageSort),
              })
            }
          >
            <SelectTrigger aria-label="Sort">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {(Object.keys(SORT_LABELS) as CustomerUsageSort[]).map((sort) => (
                <SelectItem key={sort} value={sort}>
                  {SORT_LABELS[sort]}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="flex flex-wrap items-center justify-between gap-3">
          <fieldset className="flex flex-wrap items-center gap-4">
            <legend className="sr-only">Products</legend>
            {ALL_PRODUCT_IDS.map((productID) => (
              <label
                key={productID}
                className="flex items-center gap-2 text-sm"
              >
                <Checkbox
                  checked={controls.products.has(productID)}
                  onCheckedChange={(checked) =>
                    setProductSelected(productID, checked === true)
                  }
                />
                <span
                  aria-hidden="true"
                  className="size-2.5 rounded-full"
                  style={{ backgroundColor: SPEND_PRODUCT_COLOR[productID] }}
                />
                {PRODUCT_LABELS[productID]}
              </label>
            ))}
          </fieldset>

          <div className="flex flex-wrap items-center gap-4">
            <div
              role="group"
              aria-label="Spend interval"
              className="flex gap-1"
            >
              {(["daily", "weekly", "monthly"] as const).map((interval) => (
                <Button
                  key={interval}
                  variant={
                    controls.interval === interval ? "secondary" : "ghost"
                  }
                  size="sm"
                  aria-pressed={controls.interval === interval}
                  onClick={() =>
                    update({
                      interval: interval === "monthly" ? undefined : interval,
                    })
                  }
                >
                  {interval[0]?.toUpperCase()}
                  {interval.slice(1)}
                </Button>
              ))}
            </div>
            <label className="flex items-center gap-2 text-sm">
              <Switch
                checked={controls.cumulative}
                onCheckedChange={(checked) =>
                  update({ cumulative: checked ? undefined : false })
                }
              />
              Cumulative
            </label>
            <div className="flex items-center gap-2">
              {query.isFetching && query.data && (
                <Badge variant="outline" role="status">
                  <RefreshCw className="animate-spin motion-reduce:animate-none" />
                  Updating…
                </Badge>
              )}
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
              {query.data && (
                <span className="text-muted-foreground text-xs">
                  Retrieved{" "}
                  {query.data.queriedAt.toLocaleString("en-US", {
                    timeZone: "UTC",
                  })}{" "}
                  UTC
                </span>
              )}
            </div>
          </div>
        </div>
      </div>

      {query.isError && (
        <div
          role="alert"
          className="border-destructive text-destructive flex flex-wrap items-center gap-3 rounded-md border p-4 text-sm"
        >
          <span>
            Could not load customer usage: {errorMessage(query.error)}.
            {query.data ? " Showing the last successful estimate." : ""}
          </span>
          <Button
            variant="outline"
            size="sm"
            disabled={query.isFetching}
            onClick={() => void query.refetch()}
          >
            {query.isFetching ? "Retrying…" : "Retry"}
          </Button>
        </div>
      )}

      {!query.data && query.isPending && (
        <div
          role="status"
          aria-label="Loading customer usage"
          className={GRID_CLASSES}
        >
          {Array.from({ length: SKELETON_CARDS }, (_, index) => (
            <CustomerUsageCardSkeleton key={index} />
          ))}
        </div>
      )}

      {noMatches && (
        <p className="text-muted-foreground rounded-md border border-dashed p-8 text-center text-sm">
          No customers match these filters.{" "}
          <Button variant="link" className="h-auto p-0" onClick={reset}>
            Reset filters
          </Button>
        </p>
      )}

      {view && view.active.length > 0 && (
        <section
          aria-label="Customers with usage this cycle"
          className={GRID_CLASSES}
        >
          {view.active.map((summary) => renderCard(summary, false))}
        </section>
      )}

      {view && view.idle.length > 0 && (
        <section aria-labelledby="no-usage-title" className="space-y-3">
          <h2 id="no-usage-title" className="text-lg font-semibold">
            No usage this cycle{" "}
            <span className="text-muted-foreground text-sm font-normal">
              ({view.idle.length})
            </span>
          </h2>
          <div className={GRID_CLASSES}>
            {view.idle.map((summary) => renderCard(summary, true))}
          </div>
        </section>
      )}
    </div>
  );
}
