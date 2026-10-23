import { Badge } from "@/components/ui/Badge";
import { Heading } from "@/components/ui/Heading";
import type { StripeCheckoutSession } from "@stripe/stripe-js";

// Exact catalog names from .mise-tasks/stripe/setup.mts, plus the existing TUM
// display name. Checkout exposes priceId, but not stable lookup keys or product
// metadata; environment-specific price IDs cannot identify these units here.
const catalogUnits: Readonly<Record<string, string>> = {
  "AI Control Plane PAYG": "tokens",
  "Tokens under management": "tokens",
  "Sandbox MCP egress": "bytes",
  "Sandbox risk scans": "tokens scanned",
};

export function PaygCheckoutHeader(): JSX.Element {
  return (
    <div className="flex items-center justify-between gap-4">
      <Heading variant="h4" className="text-xl font-semibold">
        Pricing
      </Heading>
      <Badge variant="neutral" className="shrink-0">
        Pay as you go
      </Badge>
    </div>
  );
}

/** PROTOTYPE: order summary sourced from the active Stripe session. */
export function PaygCheckoutSummary({
  checkout,
}: {
  checkout: StripeCheckoutSession;
}): JSX.Element {
  const trial = checkout.recurring?.trial;
  const activeTrial = trial != null && trial.trialEnd * 1000 > Date.now();
  const trialEnd = activeTrial
    ? new Intl.DateTimeFormat(undefined, { dateStyle: "long" }).format(
        new Date(trial.trialEnd * 1000),
      )
    : null;
  const metered = checkout.lineItems.some(
    (item) => item.recurring?.usageType === "metered",
  );
  const currency = new Intl.NumberFormat(undefined, {
    style: "currency",
    currency: checkout.currency,
    minimumFractionDigits: 2,
    maximumFractionDigits: 20,
  });

  return (
    <section aria-label="Order summary" className="min-w-0 space-y-6">
      <ul className="divide-y divide-border/50 border-b border-border pb-2">
        {checkout.lineItems.map((item) => {
          const unit = item.unitAmountDecimal ?? item.unitAmount;
          // Explicit Stripe units always win; unknown unlabeled items stay per unit.
          const unitLabel =
            item.unitLabel ??
            (Object.hasOwn(catalogUnits, item.name)
              ? catalogUnits[item.name]
              : undefined);
          const perToken =
            unitLabel === "token" ||
            unitLabel === "tokens" ||
            unitLabel === "tokens scanned";
          const perByte = unitLabel === "byte" || unitLabel === "bytes";
          const [coefficient, exponent = "0"] = String(
            unit.minorUnitsAmount,
          ).split("e");
          // Shift decimal places rather than multiplying tiny floating-point rates.
          const amount = Number(
            `${coefficient}e${Number(exponent) + (perToken ? 6 : 0) - Math.log10(checkout.minorUnitsAmountDivisor)}`,
          );
          const perGiBAmount = amount * 2 ** 30;
          // Keep the existing fractional precision for positive subcent GiB rates.
          // Ordinary GiB rates use currency precision to hide conversion noise.
          const rate = perByte
            ? new Intl.NumberFormat(undefined, {
                style: "currency",
                currency: checkout.currency,
                maximumFractionDigits:
                  perGiBAmount > 0 && perGiBAmount < 0.01 ? 20 : undefined,
              }).format(perGiBAmount)
            : currency.format(amount);
          const rateUnit = perToken
            ? `million ${unitLabel === "tokens scanned" ? "tokens scanned" : "tokens"}`
            : perByte
              ? "GiB of egress"
              : (unitLabel ?? "unit");
          return (
            <li
              key={item.id}
              className="flex items-start justify-between py-3 first:pt-0"
            >
              <div className="min-w-0">
                <h3 className="text-lg font-medium text-foreground">
                  {item.name}
                </h3>
              </div>
              <p className="shrink-0 text-right">
                <span className="block font-mono text-xl font-medium text-foreground tabular-nums">
                  {rate}
                </span>{" "}
                <span className="block font-mono text-xs leading-5 text-muted">
                  per {rateUnit}
                </span>
              </p>
            </li>
          );
        })}
      </ul>
      <dl>
        <div className="relative space-y-2 pb-6 pl-6">
          <dt className="font-mono text-xs uppercase tracking-wide text-muted">
            <span
              aria-hidden="true"
              className="absolute left-0 top-1 size-2 bg-foreground"
            />
            <span
              aria-hidden="true"
              className="absolute bottom-0 left-1 top-5 border-l border-neutral-default"
            />
            Today
          </dt>
          <dd className="flex items-baseline justify-between gap-4 text-base text-foreground">
            <span>{activeTrial ? "Trial active" : "Payment due today"}</span>
            <span className="shrink-0 text-right font-mono tabular-nums text-muted">
              {checkout.total.total.amount}
            </span>
          </dd>
        </div>
        <div className="relative space-y-2 pl-6">
          <dt className="font-mono text-xs uppercase tracking-wide text-muted">
            <span
              aria-hidden="true"
              className="absolute left-0 top-1 size-2 border border-foreground"
            />
            {activeTrial
              ? `Trial ends ${trialEnd}`
              : metered
                ? "Billing"
                : "Total"}
          </dt>
          <dd className="flex items-baseline justify-between gap-4 text-base text-foreground">
            <span>
              {metered
                ? checkout.recurring?.interval === "month" &&
                  checkout.recurring.intervalCount === 1
                  ? "Billed monthly based on usage"
                  : "Billed based on usage"
                : checkout.recurring?.interval === "month" &&
                    checkout.recurring.intervalCount === 1
                  ? "Billed monthly"
                  : "Payment due"}
            </span>
            <span className="shrink-0 text-right font-mono tabular-nums text-muted">
              {metered
                ? "Usage-based"
                : (checkout.recurring?.dueNext.total.amount ??
                  checkout.total.total.amount)}
            </span>
          </dd>
        </div>
      </dl>
      <dl className="border-t border-border pt-4">
        <div className="flex items-baseline justify-between gap-4">
          <dt className="text-lg font-medium text-foreground">Due today</dt>
          <dd className="font-mono text-xl font-medium text-foreground tabular-nums">
            {checkout.total.total.amount}
          </dd>
        </div>
      </dl>
    </section>
  );
}
