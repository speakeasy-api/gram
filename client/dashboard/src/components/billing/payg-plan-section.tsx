import { CancelPaygDialog } from "@/components/billing/cancel-payg-dialog";
import { usePaygCheckoutAccess } from "@/components/billing/payg-checkout-access";
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
import { useStripeSubscription } from "@/components/billing/use-stripe-subscription";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useProductTier } from "@/hooks/useProductTier";
import { isNotFoundError } from "@/lib/route-errors";
import type { ReactNode } from "react";

/**
 * The organization's payment relationship, in either of its two states.
 *
 * With no payment method attached — an active product trial, before checkout
 * has run — the section offers the checkout CTA that attaches one. With a
 * payment method attached, it reports what Stripe is doing right now, the way
 * into the customer portal, and the in-product cancel and resume controls.
 * Which state applies also decides the section's description.
 *
 * The rules live here rather than at the call site so the billing page can
 * place the section without re-deriving which payment state the organization
 * is in.
 *
 * During an active product trial there is no Stripe subscription to report on
 * even when the account type already reads as PAYG: checkout hasn't run, so
 * asking for one would answer 404 — which the attached state would render as
 * "billing isn't managed through Stripe" directly beside the checkout button
 * that is about to set it up. The trial lifecycle comes from the same hook
 * the checkout CTA gates on, so the frame and the CTA inside it read one
 * clock.
 */
export function PaygPlanSection(): JSX.Element | null {
  const productTier = useProductTier();
  const { eligible, trialLifecycle } = usePaygCheckoutAccess("active-trial");

  if (trialLifecycle === "active") {
    // The eligibility gate (flag, admin scope) decides whether there is
    // anything to offer; a section whose only content is an action the viewer
    // cannot take would just be announcing a dead end.
    if (!eligible) return null;
    return (
      <PaymentSection description="Add a payment method to start pay as you go when your trial ends.">
        <StartPaygCheckoutCTA label="Add payment method" />
      </PaymentSection>
    );
  }

  if (productTier !== "payg") return null;

  return (
    <PaymentSection description="Your pay-as-you-go subscription, payment method, and invoices.">
      <PaygPlanBody />
    </PaymentSection>
  );
}

function PaymentSection({
  description,
  children,
}: {
  description: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <Page.Section>
      {/* Secondary section: suppress the area eyebrow. */}
      <Page.Section.Title area="">Payment</Page.Section.Title>
      <Page.Section.Description>{description}</Page.Section.Description>
      <Page.Section.Body>{children}</Page.Section.Body>
    </Page.Section>
  );
}

// The query lives below the tier gate so it never fires for an organization
// that has no self-serve subscription to report on.
function PaygPlanBody(): JSX.Element {
  const { data, error, isError, isFetching, refetch } = useStripeSubscription();

  // A 404 is an answer, not an outage: the pay-as-you-go tier predates Stripe,
  // so an organization can be on it without a Stripe subscription behind it.
  // That answer is stable, so it outranks a cached subscription and gets no
  // retry — there is nothing here that trying again would find.
  if (isNotFoundError(error)) {
    return (
      <Stack gap={1}>
        <Text className="font-medium">No Stripe subscription</Text>
        <Text muted small>
          This organization's billing isn't managed through Stripe, so there's
          no payment method or invoice history to manage here.
        </Text>
      </Stack>
    );
  }

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
  const copy = planCopy(state);

  return (
    <Stack gap={4}>
      <Stack gap={1}>
        <Text className="font-medium">{copy.headline}</Text>
        <Text muted small>
          {copy.detail}
        </Text>
      </Stack>
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

function activeDetail(on: string | null): string {
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
function planCopy(state: PaygPlanState): { headline: string; detail: string } {
  const on = formatBillingDate(state.date);

  switch (state.kind) {
    case "trialing":
      return {
        headline: trialingHeadline(on),
        detail:
          "Your payment method is on file. Billing starts when the trial converts.",
      };
    case "active":
      return { headline: "Pay as you go", detail: activeDetail(on) };
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
