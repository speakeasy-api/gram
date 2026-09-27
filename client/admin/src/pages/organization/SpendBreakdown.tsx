import { useMemo, useState, type JSX } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";
import { RefreshCw, RotateCcw } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { errorMessage } from "@/lib/gramAdminApi";
import { organizationSpendBreakdownQuery } from "@/lib/gramAdminClient";
import { exclusiveEnd, type BillingUsageSearch } from "./billingUsageSearch";
import { MeterUsagePeriod } from "./MeterUsagePeriod";
import { type MeterGranularity } from "./meterUsageUtils";
import { SpendBreakdownChart } from "./SpendBreakdownChart";
import {
  SPEND_PRODUCT_COLOR,
  formatSpendRate,
  formatSpendUsage,
  formatSpendUsd,
  spendCostIsZero,
  sumSpendCosts,
  validateSpendBreakdown,
  type AdminSpendBreakdown,
  type SpendProduct,
  type SpendProductID,
} from "./spendBreakdownUtils";

const route = getRouteApi("/organizations/$idOrSlug/billing");
const ALL_PRODUCT_IDS: SpendProductID[] = [
  "agent_session_storage",
  "risk_content_scans",
  "mcp_egress",
];

export function SpendBreakdown({
  organizationID,
}: {
  organizationID: string;
}): JSX.Element {
  const search = route.useSearch();
  const navigate = route.useNavigate();
  const granularity = search.interval ?? "daily";
  const cumulative = search.cumulative ?? false;
  const [selectedProductIDs, setSelectedProductIDs] =
    useState<SpendProductID[]>(ALL_PRODUCT_IDS);
  const query = useQuery({
    ...organizationSpendBreakdownQuery({
      organizationId: organizationID,
      from: search.from ? new Date(`${search.from}T00:00:00Z`) : undefined,
      to: search.to ? new Date(exclusiveEnd(search.to)) : undefined,
    }),
    placeholderData: keepPreviousData,
    select: validateSpendBreakdown,
  });
  const [lastSuccessfulData, setLastSuccessfulData] =
    useState<AdminSpendBreakdown>();
  if (
    query.data &&
    !query.isPlaceholderData &&
    query.data !== lastSuccessfulData
  ) {
    setLastSuccessfulData(query.data);
  }
  const data = query.data ?? lastSuccessfulData;
  const selectedProducts = useMemo(
    () => new Set(selectedProductIDs),
    [selectedProductIDs],
  );
  const visibleProducts = useMemo(
    () =>
      data?.products.filter((product) => selectedProducts.has(product.id)) ??
      [],
    [data, selectedProducts],
  );
  const selectedTotal = useMemo(() => {
    if (!data || visibleProducts.length === 0) return "0";
    if (visibleProducts.length === data.products.length) {
      return data.totalCostUsd;
    }
    return sumSpendCosts(visibleProducts.map((product) => product.costUsd));
  }, [data, visibleProducts]);
  const update = (patch: BillingUsageSearch): void => {
    void navigate({
      search: (previous) => ({ ...previous, ...patch }),
      resetScroll: false,
    });
  };
  const setProductSelected = (
    productID: SpendProductID,
    selected: boolean,
  ): void => {
    setSelectedProductIDs((current) => {
      if (selected) {
        return current.includes(productID) ? current : [...current, productID];
      }
      return current.filter((id) => id !== productID);
    });
  };

  return (
    <section
      aria-labelledby="spend-breakdown-title"
      className="min-w-0 space-y-4"
    >
      <div>
        <h3 id="spend-breakdown-title" className="text-lg font-semibold">
          Spend by product
        </h3>
        <p className="text-muted-foreground mt-1 text-sm">
          Usage comparison estimated at current PAYG list prices. This is not an
          invoice or the organization’s actual contracted charge. Inference is
          excluded.
        </p>
      </div>

      <div className="bg-card space-y-3 rounded-md border p-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          {data ? (
            <fieldset className="flex flex-wrap items-center gap-4">
              <legend className="sr-only">Products</legend>
              {data.products.map((product) => (
                <label
                  key={product.id}
                  className="flex items-center gap-2 text-sm"
                >
                  <Checkbox
                    checked={selectedProducts.has(product.id)}
                    onCheckedChange={(checked) =>
                      setProductSelected(product.id, checked === true)
                    }
                  />
                  <span
                    aria-hidden="true"
                    className="size-2.5 rounded-full"
                    style={{ backgroundColor: SPEND_PRODUCT_COLOR[product.id] }}
                  />
                  {product.label}
                </label>
              ))}
            </fieldset>
          ) : (
            <span className="text-muted-foreground text-sm">Products</span>
          )}
          <div className="flex flex-wrap items-center gap-2">
            {query.isFetching && data && (
              <Badge variant="outline" role="status">
                <RefreshCw className="animate-spin motion-reduce:animate-none" />
                Updating…
              </Badge>
            )}
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                setSelectedProductIDs(ALL_PRODUCT_IDS);
                void navigate({ search: {}, resetScroll: false });
              }}
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
        </div>
        {data && (
          <MeterUsagePeriod
            window={data.window}
            cycles={data.billingCycles}
            onChange={(from, to) => update({ from, to })}
          />
        )}
      </div>

      {query.isError && (
        <div
          role="alert"
          className="border-destructive text-destructive flex flex-wrap items-center gap-3 rounded-md border p-4 text-sm"
        >
          <span>
            Could not load estimated spend: {errorMessage(query.error)}.
            {data ? " Showing the last successful estimate." : ""}
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

      {!data && query.isPending && (
        <div
          role="status"
          aria-label="Loading estimated spend"
          className="space-y-4"
        >
          <Skeleton className="h-28 w-full" />
          <Skeleton className="h-96 w-full" />
          <Skeleton className="h-40 w-full" />
        </div>
      )}

      {data && (
        <SpendBreakdownContent
          data={data}
          selectedProducts={selectedProducts}
          visibleProducts={visibleProducts}
          selectedTotal={selectedTotal}
          granularity={granularity}
          cumulative={cumulative}
          isFetching={query.isFetching}
          onUpdate={update}
        />
      )}
    </section>
  );
}

function SpendBreakdownContent({
  data,
  selectedProducts,
  visibleProducts,
  selectedTotal,
  granularity,
  cumulative,
  isFetching,
  onUpdate,
}: {
  data: AdminSpendBreakdown;
  selectedProducts: ReadonlySet<SpendProductID>;
  visibleProducts: SpendProduct[];
  selectedTotal: string;
  granularity: MeterGranularity;
  cumulative: boolean;
  isFetching: boolean;
  onUpdate: (patch: BillingUsageSearch) => void;
}): JSX.Element {
  const selectedCount = visibleProducts.length;
  const allSelected =
    selectedCount > 0 && selectedCount === data.products.length;
  const totalDescription = allSelected
    ? "All metered products at current PAYG list prices"
    : `${selectedCount} selected ${selectedCount === 1 ? "product" : "products"} at current PAYG list prices`;

  return (
    <div className="space-y-4" aria-busy={isFetching}>
      <dl className="bg-card grid rounded-md border">
        <div className="min-w-0 p-5">
          <dt className="text-muted-foreground text-xs font-medium">
            Selected estimated spend
          </dt>
          <dd className="mt-2 text-3xl font-semibold tracking-tight tabular-nums">
            {formatSpendUsd(selectedTotal)}
          </dd>
          <dd className="text-muted-foreground mt-2 text-xs">
            {totalDescription}
          </dd>
        </div>
      </dl>

      <div className="bg-card min-w-0 rounded-md border p-4">
        <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
          <div>
            <h4 className="text-sm font-medium">Estimated cost over time</h4>
            <p className="text-muted-foreground mt-1 text-xs">
              Stacked by selected product. An asterisk marks the in-progress
              bucket.
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-4">
            <div
              role="group"
              aria-label="Spend interval"
              className="flex gap-1"
            >
              {(["daily", "weekly", "monthly"] as const).map((interval) => (
                <Button
                  key={interval}
                  variant={granularity === interval ? "secondary" : "ghost"}
                  size="sm"
                  aria-pressed={granularity === interval}
                  onClick={() => onUpdate({ interval })}
                >
                  {interval[0]?.toUpperCase()}
                  {interval.slice(1)}
                </Button>
              ))}
            </div>
            <label className="flex items-center gap-2 text-sm">
              <Switch
                checked={cumulative}
                onCheckedChange={(checked) =>
                  onUpdate({ cumulative: checked || undefined })
                }
              />
              Cumulative
            </label>
          </div>
        </div>

        {selectedCount === 0 ? (
          <p
            role="status"
            className="text-muted-foreground flex h-80 items-center justify-center text-sm"
          >
            Select a product to see its estimated spend.
          </p>
        ) : (
          <>
            {spendCostIsZero(selectedTotal) && (
              <p role="status" className="text-muted-foreground mb-3 text-sm">
                No estimated metered-product spend in this period.
              </p>
            )}
            <SpendBreakdownChart
              data={data}
              selectedProductIDs={selectedProducts}
              granularity={granularity}
              cumulative={cumulative}
            />
          </>
        )}
        <div className="text-muted-foreground mt-3 flex flex-wrap items-center justify-between gap-2 text-xs">
          <span>
            UTC · {granularity === "weekly" ? "Weeks start Monday. " : ""}
            Current PAYG list-price estimates
          </span>
          <span>
            Retrieved{" "}
            {data.queriedAt.toLocaleString("en-US", { timeZone: "UTC" })} UTC
          </span>
        </div>
      </div>

      <SpendProductsTable products={visibleProducts} />

      <details className="text-muted-foreground text-sm">
        <summary className="cursor-pointer">
          How estimates are calculated
        </summary>
        <p className="mt-2">
          Current PAYG list prices apply to every selected date, including
          historical ranges. Risk scanning counts tokens for each scanner
          execution; MCP gateway usage counts egress body bytes only. Credits,
          discounts, taxes, inference, billing adjustments, and contracted
          pricing are excluded. Today’s usage is still in progress.
        </p>
      </details>
    </div>
  );
}

function SpendProductsTable({
  products,
}: {
  products: SpendProduct[];
}): JSX.Element {
  return (
    <div className="bg-card overflow-hidden rounded-md border">
      <Table>
        <TableHeader className="bg-muted">
          <TableRow>
            <TableHead>Product</TableHead>
            <TableHead className="text-right">Usage</TableHead>
            <TableHead className="text-right">Current list rate</TableHead>
            <TableHead className="text-right">Estimated cost</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {products.length === 0 ? (
            <TableRow>
              <TableCell
                colSpan={4}
                className="text-muted-foreground h-20 text-center"
              >
                Select at least one product to see its estimate.
              </TableCell>
            </TableRow>
          ) : (
            products.map((product) => (
              <TableRow key={product.id}>
                <TableCell className="font-medium">{product.label}</TableCell>
                <TableCell
                  className="text-right tabular-nums"
                  title={`${BigInt(product.quantity).toLocaleString("en-US")} ${product.unit === "bytes" ? "bytes" : "tokens"}`}
                >
                  {formatSpendUsage(product)}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {formatSpendRate(product)}
                </TableCell>
                <TableCell className="text-right tabular-nums">
                  {formatSpendUsd(product.costUsd)}
                </TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>
    </div>
  );
}
