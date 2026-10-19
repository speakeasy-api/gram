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

function trialDaysRemaining(checkout: StripeCheckoutSession): number {
  const trial = checkout.recurring?.trial;
  return trial
    ? Math.max(0, Math.ceil((trial.trialEnd * 1000 - Date.now()) / 86_400_000))
    : 0;
}

export function PaygCheckoutHeader({
  checkout,
}: {
  checkout: StripeCheckoutSession;
}): JSX.Element {
  const days = trialDaysRemaining(checkout);
  return (
    <div className="space-y-2">
      <Heading variant="h2" className="text-display-sm font-thin">
        Pay as you go pricing
      </Heading>
      <p className="text-sm text-default">
        {days > 0
          ? `${days} days during a trial, billing starts when the trial converts.`
          : `Billed based on usage${
              checkout.recurring?.interval === "month" &&
              checkout.recurring.intervalCount === 1
                ? " each month"
                : ""
            }.`}
      </p>
    </div>
  );
}

/** PROTOTYPE: order summary sourced from the active Stripe session. */
export function PaygCheckoutSummary({
  checkout,
}: {
  checkout: StripeCheckoutSession;
}): JSX.Element {
  const days = trialDaysRemaining(checkout);
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
    <section aria-label="Order summary" className="min-w-0 space-y-8">
      <ul className="space-y-6">
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
          // Byte rates have limited Stripe precision: show a currency-sized GiB
          // rate, not conversion noise. Keep fractional cents for token rates.
          const rate = perByte
            ? new Intl.NumberFormat(undefined, {
                style: "currency",
                currency: checkout.currency,
              }).format(amount * 2 ** 30)
            : currency.format(amount);
          return (
            <li key={item.id} className="space-y-2">
              <div className="flex items-start justify-between gap-4">
                <h3 className="font-medium text-foreground">{item.name}</h3>
                {days > 0 && (
                  <span className="shrink-0 text-sm text-default">
                    {days} days free
                  </span>
                )}
              </div>
              {item.description && (
                <p className="text-sm text-muted">{item.description}</p>
              )}
              <p className="text-sm text-default">
                {days > 0 ? "Then " : ""}
                {rate}
                {perToken
                  ? ` per M ${unitLabel === "tokens scanned" ? "tokens scanned" : "tokens"}`
                  : perByte
                    ? " per GiB"
                    : unitLabel
                      ? ` per ${unitLabel}`
                      : " per unit"}
                {item.recurring?.usageType === "metered"
                  ? ", billed based on usage"
                  : ""}
              </p>
            </li>
          );
        })}
      </ul>
      <dl className="space-y-3 border-t border-neutral-default pt-5 text-sm">
        <div className="flex justify-between gap-4 text-default">
          <dt>{days > 0 ? "Total after trial" : "Total"}</dt>
          <dd>
            {metered
              ? "Price varies with usage"
              : days > 0
                ? (checkout.recurring?.dueNext.total.amount ??
                  "Calculated after trial")
                : checkout.total.total.amount}
          </dd>
        </div>
        <div className="flex justify-between gap-4 text-lg font-medium text-foreground">
          <dt>Due today</dt>
          <dd>{checkout.total.total.amount}</dd>
        </div>
      </dl>
    </section>
  );
}
