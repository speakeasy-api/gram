import { useMemo, useState, type JSX } from "react";
import { Link } from "@tanstack/react-router";
import {
  ArrowDownRight,
  ArrowUpRight,
  ChevronDown,
  Pin,
  PinOff,
} from "lucide-react";

import { TrialStateBadge } from "@/components/Trial";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { badgeTone } from "@/lib/badgeTone";
import { cn } from "@/lib/utils";
import { inclusiveEnd } from "@/pages/organization/billingUsageSearch";
import { SpendBreakdownChart } from "@/pages/organization/SpendBreakdownChart";
import { SpendProductsTable } from "@/pages/organization/SpendProductsTable";
import {
  formatSpendUsd,
  type SpendProductID,
} from "@/pages/organization/spendBreakdownUtils";
import type { CustomerPlan } from "./customerUsageSearch";
import {
  PLAN_LABELS,
  type CustomerChange,
  type CustomerUsageSummary,
} from "./customerUsageUtils";

const CHART_HEIGHT = 200;

const cycleDate = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
  timeZone: "UTC",
});

function formatCycle(from: Date, to: Date): string {
  const end = new Date(`${inclusiveEnd(to)}T00:00:00Z`);
  return `${cycleDate.format(from)} – ${cycleDate.format(end)} UTC`;
}

function formatPercent(percent: number): string {
  const rounded = Math.round(percent);
  return `${rounded > 0 ? "+" : ""}${rounded}%`;
}

function signedUsd(value: string): string {
  const formatted = formatSpendUsd(value.replace(/^-/, ""));
  if (value.startsWith("-")) return `−${formatted}`;
  return Number(value) === 0 ? formatted : `+${formatted}`;
}

function ChangeIndicator({ change }: { change: CustomerChange }): JSX.Element {
  if (change.kind === "none") {
    return (
      <span className="text-muted-foreground text-sm">
        <span aria-hidden="true">—</span>
        <span className="sr-only">No previous cycle to compare</span>
      </span>
    );
  }
  const amount = Number(change.changeUsd);
  const up = amount > 0;
  const down = amount < 0;
  const Icon = down ? ArrowDownRight : ArrowUpRight;
  return (
    <span
      className={cn(
        "flex items-center gap-1 text-sm font-medium tabular-nums",
        up && "text-emerald-700 dark:text-emerald-400",
        down && "text-red-700 dark:text-red-400",
        !up && !down && "text-muted-foreground",
      )}
    >
      {(up || down) && <Icon aria-hidden="true" className="size-4" />}
      {signedUsd(change.changeUsd)}
      <span className="font-normal">
        {change.kind === "new" ? "New" : formatPercent(change.percent)}
      </span>
    </span>
  );
}

export function CustomerUsageCard({
  summary,
  queriedAt,
  selectedProducts,
  interval,
  cumulative,
  collapsible = false,
  pinned,
  onTogglePinned,
  onRetry,
  isRetrying,
}: {
  summary: CustomerUsageSummary;
  queriedAt: Date;
  selectedProducts: ReadonlySet<SpendProductID>;
  interval: "daily" | "weekly" | "monthly";
  cumulative: boolean;
  // Cards in "No usage this cycle" start as their header and can be expanded.
  collapsible?: boolean;
  pinned: boolean;
  onTogglePinned: () => void;
  onRetry: () => void;
  isRetrying: boolean;
}): JSX.Element {
  const { customer, cycleCostUsd, change } = summary;
  const [expanded, setExpanded] = useState(!collapsible);
  const chartSource = useMemo(
    () => ({
      products: customer.products,
      window: customer.window,
      queriedAt,
    }),
    [customer.products, customer.window, queriedAt],
  );
  const visibleProducts = customer.products.filter((product) =>
    selectedProducts.has(product.id),
  );
  const headingID = `customer-usage-${customer.organizationId}`;

  return (
    <article
      aria-labelledby={headingID}
      className="bg-card flex h-full min-w-0 flex-col gap-4 rounded-md border p-4"
    >
      <header className="space-y-3">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0 space-y-1.5">
            <h3 id={headingID} className="truncate font-semibold">
              {customer.name}
            </h3>
            <div className="flex flex-wrap items-center gap-1.5">
              <Badge variant="outline" className={badgeTone.neutral}>
                {PLAN_LABELS[customer.accountType as CustomerPlan] ??
                  customer.accountType}
              </Badge>
              <TrialStateBadge state={customer.trialState} />
            </div>
            <p className="text-muted-foreground text-xs">
              {formatCycle(
                customer.currentCycle.from,
                customer.currentCycle.to,
              )}
            </p>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <Button variant="link" size="sm" className="h-auto px-0" asChild>
              <Link
                to="/organizations/$idOrSlug/billing"
                params={{ idOrSlug: customer.slug }}
              >
                View billing
              </Link>
            </Button>
            <Button
              variant={pinned ? "secondary" : "ghost"}
              size="icon-sm"
              aria-pressed={pinned}
              aria-label={
                pinned
                  ? `Unpin ${customer.name}`
                  : `Pin ${customer.name} to top`
              }
              title={pinned ? "Unpin" : "Pin to top"}
              onClick={onTogglePinned}
            >
              {pinned ? <PinOff /> : <Pin />}
            </Button>
          </div>
        </div>
        {customer.error === undefined && (
          <div className="flex flex-wrap items-end justify-between gap-2">
            <div>
              <p className="text-muted-foreground text-xs font-medium">
                This cycle
              </p>
              <p className="text-2xl font-semibold tracking-tight tabular-nums">
                {formatSpendUsd(cycleCostUsd)}
              </p>
            </div>
            <div className="text-right">
              <p className="text-muted-foreground text-xs font-medium">
                vs same point last cycle
              </p>
              <ChangeIndicator change={change} />
            </div>
          </div>
        )}
      </header>

      {customer.error !== undefined ? (
        <div
          role="alert"
          className="border-destructive text-destructive flex flex-1 flex-col items-start justify-center gap-3 rounded-md border p-4 text-sm"
        >
          <span>{customer.error}</span>
          <Button
            variant="outline"
            size="sm"
            disabled={isRetrying}
            onClick={onRetry}
          >
            {isRetrying ? "Retrying…" : "Retry"}
          </Button>
        </div>
      ) : collapsible && !expanded ? (
        <Button
          variant="ghost"
          size="sm"
          className="self-start"
          aria-expanded={false}
          onClick={() => setExpanded(true)}
        >
          <ChevronDown />
          Show chart and products
        </Button>
      ) : visibleProducts.length === 0 ? (
        <p className="text-muted-foreground flex flex-1 items-center justify-center text-sm">
          Select a product to see its estimated spend.
        </p>
      ) : (
        <>
          <div className="min-w-0">
            <SpendBreakdownChart
              data={chartSource}
              selectedProductIDs={selectedProducts}
              granularity="bucket"
              cumulative={cumulative}
              height={CHART_HEIGHT}
              compact
            />
            <p className="text-muted-foreground mt-1 text-xs">
              {interval === "monthly"
                ? "One bar per billing cycle."
                : interval === "weekly"
                  ? "Current cycle by week. Weeks start Monday."
                  : "Current cycle by day."}{" "}
              * marks the in-progress bar.
            </p>
          </div>
          <div className="mt-auto">
            <SpendProductsTable products={visibleProducts} compact />
          </div>
        </>
      )}
    </article>
  );
}

export function CustomerUsageCardSkeleton(): JSX.Element {
  return (
    <div className="bg-card space-y-4 rounded-md border p-4">
      <Skeleton className="h-5 w-1/2" />
      <Skeleton className="h-8 w-1/3" />
      <Skeleton className="h-48 w-full" />
      <Skeleton className="h-28 w-full" />
    </div>
  );
}
