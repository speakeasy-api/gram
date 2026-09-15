import {
  inferenceCapAnchor,
  inferenceCapLabel,
  inferenceCapPausedNote,
  inferenceCapRaiseLabel,
  isInferenceCapReached,
  sortInferenceCaps,
} from "@/components/billing/inference-caps";
import { useInferenceCapsMode } from "@/components/billing/inference-caps-mode";
import { PaygPortalButton } from "@/components/billing/payg-portal-button";
import {
  isStripeBilling,
  isStripeTrialing,
} from "@/components/billing/payg-plan-state";
import { useStripeSubscription } from "@/components/billing/use-stripe-subscription";
import { FullBleedBanner } from "@/components/full-bleed-banner";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useProductTier } from "@/hooks/useProductTier";
import { isNotFoundError } from "@/lib/route-errors";
import { useOrgRoutes } from "@/routes";
import type { InferenceSpendCap } from "@gram/client/models/components/inferencespendcap.js";
import { useGetInferenceSpendCaps } from "@gram/client/react-query/getInferenceSpendCaps.js";
import { ArrowRight, CreditCard, PauseCircle } from "lucide-react";

/**
 * How these banners stay current.
 *
 * Both states are decided by Stripe and by usage this dashboard doesn't drive,
 * so neither one arrives through anything the user did here — a poll is the
 * only way they appear at all. Five minutes is slow enough that an open tab
 * isn't a load generator and fast enough that a paused organization isn't left
 * wondering why its inference stopped.
 *
 * Nothing polls in the background: a tab nobody is looking at can't show a
 * banner, so the request would only be paid for. Coming back to the tab is
 * exactly when the answer matters, which is what the focus refetch covers.
 *
 * The staleness window matches the poll. These banners ride the page header, so
 * they unmount and remount on every navigation, and data React Query considers
 * stale is re-read on mount — which would put an upstream read behind every
 * route change and make the five-minute cadence a floor nobody observes.
 *
 * The shared query client throws everything but a 401/403 to the app error
 * boundary. A banner must never take a page down, so these reads opt out and
 * fail quiet instead.
 */
const BILLING_BANNER_POLL_MS = 5 * 60 * 1000;

const BILLING_BANNER_QUERY = {
  refetchInterval: BILLING_BANNER_POLL_MS,
  refetchIntervalInBackground: false,
  refetchOnWindowFocus: true,
  staleTime: BILLING_BANNER_POLL_MS,
  throwOnError: false,
} as const;

/**
 * Tells an organization its last payment failed, and hands an admin the card
 * form that fixes it.
 *
 * The two gates are the point of the split: the tier and the scope are decided
 * from state already in the session, so an organization with no pay-as-you-go
 * bill — or a viewer who may not read org billing at all — never reaches the
 * subscription query.
 */
export function PaygPaymentFailedBanner(): JSX.Element | null {
  const productTier = useProductTier();

  if (productTier !== "payg") return null;

  return (
    <RequireScope scope="org:read" level="section">
      <PaymentFailedBannerBody />
    </RequireScope>
  );
}

function PaymentFailedBannerBody(): JSX.Element | null {
  const { data, error } = useStripeSubscription(BILLING_BANNER_QUERY);

  // A 404 is an answer, not an outage: the pay-as-you-go tier predates Stripe,
  // so an organization can be on it with no Stripe subscription behind it. That
  // answer outranks whatever is still cached — a subscription that has gone
  // away can't have a payment failing against it.
  if (isNotFoundError(error)) return null;

  // Any other failure leaves the last good answer in the cache, and that answer
  // is the only thing that decides. So a read that breaks while a payment is
  // failing keeps the banner up, and one that breaks before anything ever
  // loaded shows nothing rather than guessing at a state it has never seen.
  if (data?.paymentFailed !== true) return null;

  return (
    <FullBleedBanner
      icon={CreditCard}
      role="alert"
      title="Payment failed"
      description="Your last payment didn't go through. Update your payment method to keep this organization's service running."
      actions={
        <RequireScope
          scope="org:admin"
          level="section"
          fallback={
            <Text small muted>
              Ask an organization admin to update the payment method.
            </Text>
          }
        >
          <PaygPortalButton label="Update payment method" />
        </RequireScope>
      }
    />
  );
}

/**
 * Tells an organization which of its inference has stopped for reaching its own
 * monthly cap, and points at the control that raises that cap.
 *
 * These ride along with the page header rather than living on the billing page:
 * an organization whose assistants have gone quiet has no reason to visit
 * billing, so the notice has to find them wherever they hit the silence.
 *
 * One banner per cap that has been reached. The caps are independent limits on
 * unrelated work — a month can reach both — and a single merged notice would
 * name neither the thing that stopped nor the control that starts it again.
 */
export function PaygCapReachedBanners(): JSX.Element | null {
  const mode = useInferenceCapsMode();

  if (mode === "hidden") return null;

  if (mode === "payg") {
    return (
      <RequireScope scope="org:read" level="section">
        <PaygCapReachedBannersBody />
      </RequireScope>
    );
  }

  return (
    <RequireScope scope="org:read" level="section">
      <CapReachedBannersBody state="trial" />
    </RequireScope>
  );
}

// Checkout converts the product trial in Gram immediately, while Stripe can
// keep the resulting subscription trialing until the paid period begins. The
// session alone therefore cannot decide whether raising a cap is available.
function PaygCapReachedBannersBody(): JSX.Element | null {
  const { data, error } = useStripeSubscription(BILLING_BANNER_QUERY);
  const state: CapChangeState =
    isNotFoundError(error) || data === undefined
      ? "unknown"
      : isStripeTrialing(data)
        ? "trial"
        : isStripeBilling(data)
          ? "editable"
          : "unknown";
  return <CapReachedBannersBody state={state} />;
}

type CapChangeState = "editable" | "trial" | "unknown";

function CapReachedBannersBody({
  state,
}: {
  state: CapChangeState;
}): JSX.Element | null {
  const { data } = useGetInferenceSpendCaps(
    undefined,
    undefined,
    BILLING_BANNER_QUERY,
  );

  // The banners are whatever the list reports as reached. A read that failed
  // with nothing cached says nothing rather than guessing at a state it has
  // never seen — a banner is not the place to report an outage, since it would
  // appear on every page in the app.
  //
  // A disabled key is left out whatever it has spent: its inference is already
  // off for reasons the cap had nothing to do with, and the control the banner
  // links to is read-only for exactly that reason. The notice would name a
  // pause the cap didn't cause and hand an admin an action that can't end it.
  const reached = sortInferenceCaps(data ?? []).filter(
    (cap) => !cap.disabled && isInferenceCapReached(cap),
  );

  if (reached.length === 0) return null;

  return (
    <>
      {reached.map((cap) => (
        <CapReachedBanner key={cap.keyType} cap={cap} state={state} />
      ))}
    </>
  );
}

function CapReachedBanner({
  cap,
  state,
}: {
  cap: InferenceSpendCap;
  state: CapChangeState;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const label = inferenceCapLabel(cap.keyType);

  return (
    <FullBleedBanner
      icon={PauseCircle}
      role="status"
      title={`${label} reached`}
      description={
        state === "trial"
          ? `${label} has reached its trial allowance. Its cap can't be changed until pay as you go begins.`
          : state === "unknown"
            ? `${label} is read-only until this organization's billing status is confirmed.`
            : inferenceCapPausedNote(cap.keyType)
      }
      actions={
        <RequireScope
          scope="org:admin"
          level="section"
          fallback={
            <Text small muted>
              {state === "trial"
                ? "This cap is read-only until pay as you go begins."
                : state === "unknown"
                  ? "This cap can't be changed until billing status is confirmed."
                  : "Ask an organization admin to raise this cap."}
            </Text>
          }
        >
          {/* The link carries the cap's own anchor, so it lands on the one
              control that ends this pause rather than the top of the section
              the other cap also lives in. */}
          <orgRoutes.billing.Link hash={inferenceCapAnchor(cap.keyType)}>
            <Button variant="secondary" size="sm" className="group">
              {state !== "editable"
                ? `View ${label.toLowerCase()}`
                : inferenceCapRaiseLabel(cap.keyType)}
              <ArrowRight className="size-3.5 transition-transform group-hover:translate-x-0.5" />
            </Button>
          </orgRoutes.billing.Link>
        </RequireScope>
      }
    />
  );
}
