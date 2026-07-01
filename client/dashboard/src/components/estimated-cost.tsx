import { Info } from "lucide-react";

import { cn } from "@/lib/utils";

import {
  costMeasureLabel,
  ESTIMATED_COST_TOOLTIP,
  isMeteredBilling,
} from "./estimated-cost-utils";
import { SimpleTooltip } from "./ui/tooltip";

/**
 * A small info affordance disclosing that a cost value is an API-rate estimate.
 * Renders nothing for a confidently metered scope (real cost needs no caveat);
 * otherwise a hover explains the estimate. Reuse this everywhere the cost
 * measure is surfaced so the caveat is disclosed uniformly.
 */
export function EstimatedCostIndicator({
  billingMode,
  className,
}: {
  billingMode?: string | null;
  className?: string;
}): JSX.Element | null {
  if (isMeteredBilling(billingMode)) return null;
  return (
    <SimpleTooltip tooltip={ESTIMATED_COST_TOOLTIP}>
      <Info
        aria-label="How estimated cost is calculated"
        className={cn(
          "text-muted-foreground inline-block size-3 shrink-0 cursor-help align-text-top",
          className,
        )}
      />
    </SimpleTooltip>
  );
}

/**
 * The cost-measure label paired with its estimate indicator, for cost column
 * headers and stat labels. Shows "Cost" with no indicator for a metered scope,
 * and "Est. cost" + indicator otherwise.
 */
export function CostMeasureLabel({
  billingMode,
  className,
}: {
  billingMode?: string | null;
  className?: string;
}): JSX.Element {
  return (
    <span className={cn("inline-flex items-center gap-1", className)}>
      {costMeasureLabel(billingMode)}
      <EstimatedCostIndicator billingMode={billingMode} />
    </span>
  );
}
