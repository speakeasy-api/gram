import {
  formatBillingDate,
  isStripeBilling,
  isStripeTrialing,
} from "@/components/billing/payg-plan-state";
import { useStripeSubscription } from "@/components/billing/use-stripe-subscription";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useSession } from "@/contexts/Auth";
import { type ProductTier, useProductTier } from "@/hooks/useProductTier";
import { useTrialNow } from "@/hooks/useTrialNow";
import { isNotFoundError } from "@/lib/route-errors";
import {
  getTrialLifecycleFromDates,
  type TrialLifecycle,
} from "@/lib/trial-status";
import {
  invalidateAllGetCreditUsage,
  useGetCreditUsage,
} from "@gram/client/react-query/getCreditUsage.js";
import { useSetSpendCapMutation } from "@gram/client/react-query/setSpendCap.js";
import { useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { Link } from "react-router";

/** The bounds the API accepts for a monthly chat spend cap, in whole USD. */
const MIN_SPEND_CAP_USD = 1;
const MAX_SPEND_CAP_USD = 10_000;

// The in-app booking gate, which prefills the form from the session — the same
// path the trial card sends people to. Not the marketing site's /talk-to-us.
const SALES_PATH = "/talk-to-us";

function formatUsd(amount: number): string {
  return `$${amount.toLocaleString("en-US")}`;
}

const FIELD_LABEL = "Monthly chat spend cap (USD)";
const MIN_LABEL = formatUsd(MIN_SPEND_CAP_USD);
const MAX_LABEL = formatUsd(MAX_SPEND_CAP_USD);
const RANGE_MESSAGE = `Enter a whole dollar amount between ${MIN_LABEL} and ${MAX_LABEL}.`;

/**
 * What the spend cap section does for this organization.
 *
 * The cap is a pay-as-you-go control, so it only exists once checkout has put
 * the organization onto PAYG. Tiers with no PAYG bill for a cap to apply to
 * see nothing at all.
 *
 * An active product trial takes precedence over the tier. Trials run on the
 * enterprise tier, but the account type can already read as PAYG while the
 * trial is still running, and in both cases there is no pay-as-you-go bill yet
 * — so an active product trial gets the cap shown as locked rather than
 * editable, and the section touches neither query nor mutation.
 *
 * Once checkout converts the product trial, the session stops carrying it
 * while Stripe can still be trialing the subscription for days. So a PAYG
 * organization with no product trial left is not necessarily being billed: the
 * live Stripe status decides, which is `"payg"` below.
 */
type SpendCapMode = "payg" | "product-trial" | "hidden";

function spendCapMode(
  productTier: ProductTier,
  trialLifecycle: TrialLifecycle,
): SpendCapMode {
  if (productTier !== "payg" && productTier !== "enterprise") return "hidden";
  if (trialLifecycle === "active") return "product-trial";
  // Enterprise off a trial is on a contract, which bills through its own terms.
  return productTier === "payg" ? "payg" : "hidden";
}

const PRODUCT_TRIAL_NOTE =
  "Your trial has no spend cap. The monthly chat spend cap starts when pay as you go begins, and you can set it here once checkout is complete.";

const NO_SUBSCRIPTION_NOTE =
  "The monthly chat spend cap applies to pay-as-you-go billing through Stripe. This organization has no Stripe subscription, so there is no cap to set here.";

const NOT_BILLING_NOTE =
  "The monthly chat spend cap applies to pay-as-you-go billing through Stripe. This organization's subscription isn't billing, so there is no cap to set here.";

/** The same note for a Stripe trial, which knows when billing takes over. */
function stripeTrialNote(convertsOn: Date | null | undefined): string {
  const on = formatBillingDate(convertsOn);
  if (on === null) {
    return "Your trial has no spend cap. The monthly chat spend cap starts when pay as you go begins, and you can set it here once your trial converts.";
  }
  return `Your trial has no spend cap. The monthly chat spend cap starts when pay as you go begins on ${on}, and you can set it here then.`;
}

/**
 * The monthly ceiling on what an organization can spend on chat and the other
 * AI-powered dashboard experiences.
 *
 * The tier rule lives here rather than at the call site so the billing page can
 * place the section in both of its branches without either one re-deriving when
 * a cap applies.
 */
export function ChatSpendCapSection(): JSX.Element | null {
  const productTier = useProductTier();
  const { trial } = useSession();
  // A trial that ends while the page is open has to take the locked cap with
  // it, so this reads a clock that re-renders on the trial's own boundaries.
  const now = useTrialNow(trial);
  const mode = spendCapMode(
    productTier,
    getTrialLifecycleFromDates(trial, now),
  );

  if (mode === "hidden") return null;

  return (
    <Page.Section>
      {/* Secondary section below Usage: suppress the area eyebrow. */}
      <Page.Section.Title area="">Chat spend cap</Page.Section.Title>
      <Page.Section.Description>
        Limit what this organization can spend each month on chat and the other
        AI-powered dashboard experiences.
      </Page.Section.Description>
      <Page.Section.Body>
        {mode === "product-trial" ? (
          <LockedSpendCap note={PRODUCT_TRIAL_NOTE} />
        ) : (
          <PaygSpendCapGate />
        )}
      </Page.Section.Body>
    </Page.Section>
  );
}

/**
 * The live Stripe subscription decides whether a PAYG organization is being
 * billed yet, and it fails closed: only a read that succeeded just now can
 * open the editable cap.
 *
 * A failing read keeps the form away even when the cache still holds a
 * subscription. The cached copy is exactly what goes stale across the moment
 * that matters — a trial converting, or a subscription ending — so treating it
 * as good enough would invite an admin to set a cap against a bill whose state
 * this dashboard can no longer confirm.
 */
function PaygSpendCapGate(): JSX.Element {
  const { data, error, isError, isFetching, refetch } = useStripeSubscription();

  // A 404 is an answer, not an outage: the pay-as-you-go tier predates Stripe,
  // so an organization can be on it without a Stripe subscription behind it.
  // There is no cap to set and nothing a retry would find, so the field stays
  // locked and the recheck goes away.
  if (isNotFoundError(error)) {
    return <LockedSpendCap note={NO_SUBSCRIPTION_NOTE} placeholder="" />;
  }

  if (isError) {
    return (
      <Stack direction="horizontal" align="center" gap={3}>
        <Text muted small role="alert">
          Couldn't check your subscription, so the chat spend cap can't be
          edited right now.
        </Text>
        <Button
          variant="secondary"
          size="sm"
          disabled={isFetching}
          onClick={() => void refetch()}
        >
          {isFetching ? "RECHECKING..." : "RECHECK"}
        </Button>
      </Stack>
    );
  }

  if (data === undefined) {
    return (
      <div className="max-w-md space-y-4">
        <Skeleton className="h-9 w-full" />
        <Skeleton className="h-9 w-32" />
      </div>
    );
  }

  if (isStripeTrialing(data)) {
    return <LockedSpendCap note={stripeTrialNote(data.trialEnd)} />;
  }

  // Only a subscription Stripe is actually billing has a bill for a cap to
  // apply to. Everything else — canceled, unpaid, paused, never completed —
  // is locked rather than editable, so the section reaches neither the cap
  // query nor its endpoint.
  if (!isStripeBilling(data)) {
    return <LockedSpendCap note={NOT_BILLING_NOTE} placeholder="" />;
  }

  return <PaygSpendCap />;
}

// Nothing here is writable, so the section never mounts the mutation for an
// organization with no pay-as-you-go bill for a cap to apply to — whether that
// is a trial that hasn't converted or no Stripe subscription at all.
function LockedSpendCap({
  note,
  placeholder = "Set once pay as you go starts",
}: {
  note: string;
  placeholder?: string;
}): JSX.Element {
  return (
    <Stack gap={2} className="max-w-md">
      <Label htmlFor="chat-spend-cap-locked">{FIELD_LABEL}</Label>
      <Input
        id="chat-spend-cap-locked"
        type="number"
        value=""
        disabled
        placeholder={placeholder}
      />
      <Text muted small>
        {note}
      </Text>
    </Stack>
  );
}

// `getCreditUsage` is what the usage meters read the cap from, so it is the one
// value the section shows — a second source could disagree with the meter
// sitting directly above it.
function PaygSpendCap(): JSX.Element {
  // The shared query client throws everything but a 401/403 to the app error
  // boundary, which would take the whole billing page down over one section
  // and skip the branches below. This section handles its own failures, so it
  // opts out and keeps them inline.
  const { data, isError, refetch, isFetching } = useGetCreditUsage(
    undefined,
    undefined,
    { throwOnError: false },
  );

  // A refetch that fails leaves the last successful value in the cache, so the
  // query reports data and an error together. The form stays mounted in the
  // same child position — swapping it for the error message would throw away
  // whatever the admin had typed — and the failure is reported beside it.
  if (data) {
    return (
      <Stack gap={4}>
        <RequireScope
          scope="org:admin"
          level="section"
          fallback={<SpendCapReadOnly cap={data.monthlyCredits} />}
        >
          <SpendCapForm initial={data.monthlyCredits} />
        </RequireScope>
        {isError && (
          <Text muted small role="alert">
            Couldn't refresh the chat spend cap, so the amount shown may be out
            of date. Saving still works.
          </Text>
        )}
      </Stack>
    );
  }

  // Nothing was ever cached, so there is no cap to show and no form to keep.
  // The failure never reaches an error boundary, so recovery belongs here: a
  // retry of this one query rather than a reload of the whole billing page.
  if (isError) {
    return (
      <Stack direction="horizontal" align="center" gap={3}>
        <Text muted small role="alert">
          Couldn't load the chat spend cap.
        </Text>
        <Button
          variant="secondary"
          size="sm"
          disabled={isFetching}
          onClick={() => void refetch()}
        >
          {isFetching ? "RETRYING..." : "RETRY"}
        </Button>
      </Stack>
    );
  }

  return (
    <div className="max-w-md space-y-4">
      <Skeleton className="h-9 w-full" />
      <Skeleton className="h-9 w-32" />
    </div>
  );
}

// A member sees the cap they are spending under but gets no control — the
// endpoint is admin-only, so a disabled field would only invite a request that
// is going to be refused.
function SpendCapReadOnly({ cap }: { cap: number }): JSX.Element {
  return (
    <Stack gap={1} className="max-w-md">
      <Text className="text-eyebrow">{FIELD_LABEL}</Text>
      <Text>{formatUsd(cap)}</Text>
      <Text muted small>
        Only organization admins can change the chat spend cap.
      </Text>
    </Stack>
  );
}

/** Whether `value` is a cap the API will accept, and why it isn't. */
function spendCapError(value: string): string | null {
  const trimmed = value.trim();
  const amount = Number(trimmed);
  const valid =
    trimmed !== "" &&
    Number.isInteger(amount) &&
    amount >= MIN_SPEND_CAP_USD &&
    amount <= MAX_SPEND_CAP_USD;

  return valid ? null : RANGE_MESSAGE;
}

// The field seeds from the loaded value and re-seeds only while it is pristine:
// a cap changed elsewhere (another admin, the save's own invalidation) has to
// reach an untouched field, but a background refetch landing mid-edit must not
// overwrite what this admin typed.
function SpendCapForm({ initial }: { initial: number }): JSX.Element {
  const queryClient = useQueryClient();
  const [cap, setCap] = useState(() => String(initial));
  // The value the field was last seeded from. Comparing against it — rather
  // than against a dirty flag — means an admin who edits back to the seeded
  // amount is pristine again, and it survives a save because the invalidated
  // query comes back with the amount now in the field.
  const [seeded, setSeeded] = useState(initial);

  // Adjusting state during render rather than in an effect: React re-runs this
  // component before committing, so the field never paints the stale cap.
  if (seeded !== initial) {
    setSeeded(initial);
    if (cap === String(seeded)) setCap(String(initial));
  }

  const mutation = useSetSpendCapMutation({
    onSuccess: () => {
      // The usage meters and this field both read the cap back from the credit
      // usage query, so the whole key has to be refreshed — not just the exact
      // one this section subscribes to.
      void invalidateAllGetCreditUsage(queryClient);
    },
  });

  const validationError = spendCapError(cap);

  const handleChange = (value: string) => {
    setCap(value);
    // "Saved."/failure text left beside a field that has since been edited
    // reads as feedback about the value now in the field.
    if (mutation.isSuccess || mutation.isError) mutation.reset();
  };

  // An out-of-range cap is rejected here, where the amount can be corrected,
  // instead of coming back as a transient-looking API failure the admin is
  // invited to retry.
  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (validationError !== null) return;

    mutation.mutate({
      request: { spendCap: { monthlyCredits: Number(cap.trim()) } },
    });
  };

  return (
    <form onSubmit={handleSubmit}>
      <Stack gap={4} className="max-w-md">
        <Stack gap={2}>
          <Label htmlFor="chat-spend-cap">{FIELD_LABEL}</Label>
          <Input
            id="chat-spend-cap"
            type="number"
            inputMode="numeric"
            min={MIN_SPEND_CAP_USD}
            max={MAX_SPEND_CAP_USD}
            step={1}
            value={cap}
            onChange={handleChange}
            error={validationError !== null}
          />
          {validationError === null ? (
            <Text muted small>
              Chat stops once this month's spend reaches the cap. Raise or lower
              it at any time.
            </Text>
          ) : (
            <Text small destructive role="alert">
              {validationError}
            </Text>
          )}
          {/* The maximum is the ceiling the endpoint enforces, so anything
              above it is a conversation rather than a form. This sits outside
              the validation branch on purpose: it is the way forward for the
              admin who just had a larger amount rejected. */}
          <Text muted small>
            Need a cap above {MAX_LABEL}?{" "}
            <Link to={SALES_PATH} className="underline underline-offset-2">
              Talk to us
            </Link>
            .
          </Text>
        </Stack>
        <Stack direction="horizontal" align="center" gap={3}>
          <Button type="submit" disabled={mutation.isPending}>
            {mutation.isPending ? "SAVING..." : "SAVE SPEND CAP"}
          </Button>
          {mutation.isSuccess && (
            <Text muted small role="status">
              Saved.
            </Text>
          )}
          {mutation.isError && (
            <Text small destructive role="alert">
              Couldn't save the chat spend cap. Try again.
            </Text>
          )}
        </Stack>
      </Stack>
    </form>
  );
}
