import {
  CHAT_SPEND_CAP_ANCHOR,
  isChatSpendCapReached,
} from "@/components/billing/chat-spend-cap";
import { PaygPortalButton } from "@/components/billing/payg-portal-button";
import { useStripeSubscription } from "@/components/billing/use-stripe-subscription";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useProductTier } from "@/hooks/useProductTier";
import { isNotFoundError } from "@/lib/route-errors";
import { useOrgRoutes } from "@/routes";
import { useGetCreditUsage } from "@gram/client/react-query/getCreditUsage.js";
import { ArrowRight, CreditCard, PauseCircle } from "lucide-react";

/**
 * How these banners stay current.
 *
 * Both states are decided by Stripe and by usage this dashboard doesn't drive,
 * so neither one arrives through anything the user did here — a poll is the
 * only way they appear at all. Five minutes is slow enough that an open tab
 * isn't a load generator and fast enough that a paused organization isn't left
 * wondering why chat stopped.
 *
 * Nothing polls in the background: a tab nobody is looking at can't show a
 * banner, so the request would only be paid for. Coming back to the tab is
 * exactly when the answer matters, which is what the focus refetch covers.
 *
 * The shared query client throws everything but a 401/403 to the app error
 * boundary. A banner must never take a page down, so these reads opt out and
 * fail quiet instead.
 */
const BILLING_BANNER_QUERY = {
  refetchInterval: 5 * 60 * 1000,
  refetchIntervalInBackground: false,
  refetchOnWindowFocus: true,
  throwOnError: false,
} as const;

/**
 * Full-bleed strip above the page content, carrying one billing state and the
 * one thing to do about it.
 *
 * Sized and spaced to match `OnboardingBanner`, which occupies the same slot —
 * an organization can be shown both, and two strips of different heights read
 * as one broken layout.
 */
function BillingBanner({
  icon: BannerIcon,
  role,
  title,
  description,
  action,
}: {
  icon: typeof CreditCard;
  role: "alert" | "status";
  title: string;
  description: string;
  action: React.ReactNode;
}): JSX.Element {
  return (
    <div
      role={role}
      className="border-border/60 bg-muted/20 dark:bg-white/[0.03] w-full border-b"
    >
      <div className="mx-auto flex max-w-7xl items-center gap-4 px-8 py-5">
        <div className="bg-background border-border/60 flex size-10 shrink-0 items-center justify-center border">
          <BannerIcon className="size-5" strokeWidth={1.75} />
        </div>

        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <Text
            variant="subheading"
            as="span"
            className="text-foreground text-sm leading-tight font-semibold"
          >
            {title}
          </Text>
          <Text small muted className="max-w-10/12 text-sm">
            {description}
          </Text>
        </div>

        <div className="flex shrink-0 items-center gap-1">{action}</div>
      </div>
    </div>
  );
}

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
    <BillingBanner
      icon={CreditCard}
      role="alert"
      title="Payment failed"
      description="Your last payment didn't go through. Update your payment method to keep this organization's service running."
      action={
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
 * Tells an organization that chat has stopped because it reached its own
 * monthly cap, and points at the field that raises it.
 *
 * This one rides along with the page header rather than living on the billing
 * page: an organization whose chat has gone quiet has no reason to visit
 * billing, so the banner has to find them wherever they hit the silence.
 */
export function PaygCapPausedBanner(): JSX.Element | null {
  const productTier = useProductTier();

  if (productTier !== "payg") return null;

  return (
    <RequireScope scope="org:read" level="section">
      <CapPausedBannerBody />
    </RequireScope>
  );
}

function CapPausedBannerBody(): JSX.Element | null {
  const orgRoutes = useOrgRoutes();
  const { data } = useGetCreditUsage(
    undefined,
    undefined,
    BILLING_BANNER_QUERY,
  );

  if (!isChatSpendCapReached(data)) return null;

  return (
    <BillingBanner
      icon={PauseCircle}
      role="status"
      title="Chat spend cap reached"
      description="Chat and the other AI-powered dashboard experiences are paused for the rest of the month. Raise the cap to start them again."
      action={
        <orgRoutes.billing.Link hash={CHAT_SPEND_CAP_ANCHOR}>
          <Button variant="secondary" size="sm" className="group">
            Review spend cap
            <ArrowRight className="size-3.5 transition-transform group-hover:translate-x-0.5" />
          </Button>
        </orgRoutes.billing.Link>
      }
    />
  );
}
