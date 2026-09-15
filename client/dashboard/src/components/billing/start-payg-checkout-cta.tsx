import {
  type PaygCheckoutEligibility,
  usePaygCheckoutAccess,
} from "@/components/billing/payg-checkout-access";
import {
  PAYG_CHECKOUT_ERROR_MESSAGE,
  useStartPaygCheckout,
} from "@/components/billing/use-start-payg-checkout";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { ButtonSize } from "@/components/ui/lib/types";
import { cn } from "@/lib/utils";

const CTA_LABEL = "Start pay as you go";

/**
 * Puts an organization onto pay as you go by handing the admin a Stripe
 * checkout session for a card.
 *
 * Shared by the billing page, the sidebar trial card, and the dashboard
 * lockout gate so the gating, the single-flight mutation, and the navigation
 * rules can only be changed in one place. Several surfaces can be mounted at
 * once, so the single-flight lock is organization-scoped module state rather
 * than per-instance — see `payg-checkout-lock`. Every gate is opt-in: a flag
 * that is loading, disabled, unregistered, or errored renders nothing, as does
 * a viewer without `org:admin` or an organization that is not eligible for the
 * asking surface. Callers keep their existing talk-to-sales fallback — this
 * CTA is additive.
 */
export function StartPaygCheckoutCTA({
  size = "md",
  className,
  label = CTA_LABEL,
  eligibility = "active-trial",
}: {
  size?: ButtonSize;
  className?: string;
  /** Customer-facing action for the surface that owns this shared behavior. */
  label?: string;
  eligibility?: PaygCheckoutEligibility;
}): JSX.Element | null {
  const { eligible, activeOrganizationId } = usePaygCheckoutAccess(eligibility);
  const { startCheckout, isPending, error } =
    useStartPaygCheckout(activeOrganizationId);

  if (!eligible) return null;

  return (
    <div className={cn("flex flex-col items-start gap-2", className)}>
      <Button
        variant="primary"
        size={size}
        onClick={startCheckout}
        disabled={isPending}
      >
        {label}
      </Button>
      {error ? (
        <Text small destructive role="alert">
          {PAYG_CHECKOUT_ERROR_MESSAGE}
        </Text>
      ) : null}
    </div>
  );
}
