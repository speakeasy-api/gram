import { useBillingState } from "./use-billing-state";
import { CancelPaygDialog } from "@/components/billing/cancel-payg-dialog";
import {
  canCancelPaygPlan,
  canResumePaygPlan,
  formatBillingDate,
  paygPlanState,
  type PaygPlanState,
  type StripeSubscriptionLike,
} from "@/components/billing/payg-plan-state";
import { PaygPortalButton } from "@/components/billing/payg-portal-button";
import { ResumePaygButton } from "@/components/billing/resume-payg-button";
import { StartPaygCheckoutCTA } from "@/components/billing/start-payg-checkout-cta";
import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useSession } from "@/contexts/Auth";
import type { ReactNode } from "react";
import { Heading } from "../ui/Heading";

/**
 * Offers checkout during an active trial or when Free/Base or PAYG has no subscription.
 * Starting checkout leaves the trial unchanged until Stripe confirms completion.
 * Unsubscribed PAYG organizations retain a way to finish billing setup.
 * Attached subscriptions expose their live state and management controls.
 */
export function PaygPlanSection(): JSX.Element | null {
  const billing = useBillingState();
  const { rawGramAccountType } = useSession();
  if (billing.status === "unavailable") return null;
  if (billing.status === "needs-setup") {
    if (rawGramAccountType === "payg" && billing.trialLifecycle !== "active") {
      return (
        <PaymentSection>
          <Stack gap={3}>
            <Text className="font-medium">Finish setting up billing</Text>
            <Text muted small>
              Complete checkout to activate your pay-as-you-go subscription.
            </Text>
            <StartPaygCheckoutCTA
              label="Add payment method"
              eligibility="billing"
            />
          </Stack>
        </PaymentSection>
      );
    }
    return (
      <AddPaymentMethodSection
        activeTrial={billing.trialLifecycle === "active"}
      />
    );
  }
  return (
    <PaymentSection>
      <PaygPlanBody billing={billing} />
    </PaymentSection>
  );
}

// Copy variants are code-owned; an offer only selects presentation, never access.
function AddPaymentMethodSection({
  activeTrial = false,
}: {
  activeTrial?: boolean;
}): JSX.Element {
  const { acceptedSignupOfferFlow } = useSession();
  const generic = activeTrial
    ? "Add a payment method to start pay as you go when your trial ends."
    : "Add a payment method to start pay as you go.";
  return (
    <PaymentSection description={generic}>
      <Stack gap={4}>
        {acceptedSignupOfferFlow === "tailscale_partner_gateway" && (
          <Alert variant="info">
            Your accepted Tailscale partner promotion will be applied at
            checkout.
          </Alert>
        )}
        <StartPaygCheckoutCTA
          label="Add payment method"
          eligibility="billing"
        />
      </Stack>
    </PaymentSection>
  );
}

function PaymentSection({
  description,
  children,
}: {
  description?: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <section className="space-y-6">
      <div className="border-b border-border pb-2">
        <Heading variant="h3">Payment</Heading>
      </div>
      {description && <Text muted>{description}</Text>}
      {children}
    </section>
  );
}

// Keep the existing PAYG subscription management and checkout recovery path.
function PaygPlanBody({
  billing,
}: {
  billing: ReturnType<typeof useBillingState>;
}): JSX.Element {
  const { data, isError, isFetching, refetch } = billing.subscription;

  // A refetch that fails leaves the last successful subscription in the cache,
  // so the query reports data and an error together. The state and its actions
  // stay — an admin mid-cancellation shouldn't lose the dialog to a background
  // failure — and the staleness is reported beside them.
  if (data) {
    return (
      <Stack gap={4}>
        <PaygPlanDetails subscription={data} />
        {isError && (
          <Text muted small role="alert">
            Couldn't refresh your subscription, so what's shown may be out of
            date.
          </Text>
        )}
      </Stack>
    );
  }

  // Nothing was ever cached, so there is no state to show. What is left here is
  // transient, and it never reaches an error boundary — a Stripe outage must
  // not take the billing page down — so recovery belongs here: a retry of this
  // one query.
  if (isError) {
    return (
      <Stack direction="horizontal" align="center" gap={3}>
        <Text muted small role="alert">
          Couldn't load your subscription.
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
      <Skeleton className="h-5 w-2/3" />
      <Skeleton className="h-4 w-full" />
      <Skeleton className="h-9 w-64" />
    </div>
  );
}

function PaygPlanDetails({
  subscription,
}: {
  subscription: StripeSubscriptionLike;
}): JSX.Element {
  const state = paygPlanState(subscription);
  const copy = planCopy(state, subscription);

  return (
    <Stack gap={6}>
      <Stack gap={2}>
        <Text className="font-medium">{copy.headline}</Text>
        <Text muted small>
          {copy.detail}
        </Text>
      </Stack>
      <AppliedDiscount subscription={subscription} />
      {/* A failed payment is reported by the banner heading this page, where it
          can't be scrolled past — not by a line of body text down here. */}
      {/* A member reads the state above but gets no controls: every action
          here is admin-only at the API, so a visible one would only invite a
          request that is going to be refused. */}
      <RequireScope
        scope="org:admin"
        level="section"
        fallback={
          <Text muted small>
            Only organization admins can manage billing.
          </Text>
        }
      >
        <Stack direction="horizontal" gap={3} align="start" wrap="wrap">
          <PaygPortalButton />
          {canResumePaygPlan(state) && <ResumePaygButton />}
          {canCancelPaygPlan(state) && (
            <CancelPaygDialog endsOn={state.date} trialing={state.trialing} />
          )}
        </Stack>
      </RequireScope>
    </Stack>
  );
}

function trialingHeadline(on: string | null): string {
  if (on === null) return "Trial — converts to pay as you go when it ends";
  return `Trial — converts to pay as you go on ${on}`;
}

// Checkout anchors the first paid month to the next UTC midnight, so the
// period right after activation is only that short interval. Two days is
// longer than that interval and shorter than any real month, so the end date
// stays once the subscription is on a monthly period.
const INITIAL_PAYG_PERIOD_MAX_MS = 2 * 24 * 60 * 60 * 1000;

function isInitialPaygBillingInterval(
  start: Date | null | undefined,
  end: Date | null | undefined,
): boolean {
  if (!(start instanceof Date) || !(end instanceof Date)) return false;
  const length = end.getTime() - start.getTime();
  return (
    Number.isFinite(length) && length > 0 && length < INITIAL_PAYG_PERIOD_MAX_MS
  );
}

function activeDetail(
  start: Date | null | undefined,
  end: Date | null | undefined,
): string {
  const on = formatBillingDate(end);
  if (on !== null && isInitialPaygBillingInterval(start, end)) {
    return "You're billed monthly for what you use.";
  }
  const base = "You're billed for the usage in each billing period.";
  if (on === null) return base;
  return `${base} The current period ends on ${on}.`;
}

function endingHeadline(on: string | null, trialing: boolean): string {
  const subject = trialing ? "Trial" : "Pay as you go";
  if (on === null) {
    return `${subject} — ends at the end of the current billing period`;
  }
  return `${subject} — ends on ${on}`;
}

// A trial that is set to cancel never converts, so nothing is ever billed for
// it. Promising the final invoice the paid path gets would be telling a
// customer to expect a charge that isn't coming.
function endingDetail(trialing: boolean): string {
  if (trialing) {
    return "Your trial runs until then and pay as you go never starts, so there is nothing to invoice. Resume to convert to pay as you go when the trial ends.";
  }
  return "Your service continues until then and a final invoice follows. Resume to keep pay as you go running.";
}

/** The headline and supporting line for each plan state. */
function planCopy(
  state: PaygPlanState,
  subscription: StripeSubscriptionLike,
): { headline: string; detail: string } {
  const on = formatBillingDate(state.date);

  switch (state.kind) {
    case "trialing":
      return {
        headline: trialingHeadline(on),
        detail:
          "Your payment method is on file. Billing starts when the trial converts.",
      };
    case "active":
      return {
        headline: "Pay as you go",
        detail: activeDetail(
          subscription.currentPeriodStart,
          subscription.currentPeriodEnd,
        ),
      };
    case "ending":
      return {
        headline: endingHeadline(on, state.trialing),
        detail: endingDetail(state.trialing),
      };
    case "ended":
      return {
        headline: "Pay as you go — ended",
        detail:
          "This organization has no active pay-as-you-go subscription. Past invoices are still in the billing portal.",
      };
    case "inactive":
      return {
        headline: "Pay as you go — not active",
        detail:
          "Stripe hasn't started billing this subscription. Open the billing portal to finish setting up your payment method.",
      };
  }
}

// Attribution is not proof of a discount: only the live subscription supplies
// these verified terms. Recheck expiration to avoid showing an expired cache entry.
function AppliedDiscount({
  subscription,
}: {
  subscription: StripeSubscriptionLike;
}) {
  const { discountDescription, discountEndsAt, gatewayPromotionApplied } =
    subscription;
  if (
    !discountDescription ||
    !["active", "past_due"].includes(subscription.status) ||
    (discountEndsAt &&
      (!Number.isFinite(discountEndsAt.getTime()) ||
        discountEndsAt.getTime() <= Date.now()))
  )
    return null;
  const end = formatBillingDate(discountEndsAt);
  return (
    <Alert variant="info">
      <Stack>
        <Text variant="subheading">
          {gatewayPromotionApplied
            ? "Partner promotion applied"
            : "Discount applied"}
        </Text>
        <Text variant="small">
          {discountDescription}
          {end ? ` Discount ends ${end}.` : ""}
        </Text>
      </Stack>
    </Alert>
  );
}
