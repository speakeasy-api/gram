import { useStripeSubscription } from "./use-stripe-subscription";
import { isNotFoundError } from "@/lib/route-errors";
import { useSession } from "@/contexts/Auth";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useRBAC } from "@/hooks/useRBAC";
import { useTrialNow } from "@/hooks/useTrialNow";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import {
  getTrialLifecycleFromDates,
  type TrialLifecycle,
} from "@/lib/trial-status";

/** Entry-point policy, not proof that checkout is needed.
 * Trial cards retain their trial-only policy; lockout pages retain their gated
 * policy. Billing surfaces resolve all normal self-serve accounts together.
 */
export type PaygCheckoutEligibility =
  | "billing"
  | "setup"
  | "active-trial"
  | "gated";

/**
 * Whether this viewer may start a self-serve checkout, plus the organization
 * the single-flight lock belongs to and the trial lifecycle the answer was
 * derived from — returned so callers that also branch on the trial read the
 * same clock instead of deriving their own.
 *
 * Reads the session from context on every surface. `AuthProvider` returns the
 * lockout pages above `SessionContext.Provider`, so a gated caller supplies the
 * session itself — see `LockoutPaygCheckoutPanel`.
 */
export function usePaygCheckoutAccess(eligibility: PaygCheckoutEligibility): {
  eligible: boolean;
  activeOrganizationId: string;
  trialLifecycle: TrialLifecycle;
} {
  const flag = useFeatureFlag(FEATURE_FLAGS.paygSelfServeBilling);
  const { hasScope } = useRBAC();
  const {
    trial,
    activeOrganizationId,
    whitelisted,
    rawGramAccountType,
    hasActiveSubscription,
  } = useSession();
  // A trial that ends while the page is open has to take the CTA with it, so
  // the lifecycle below reads a clock that re-renders on the trial's own
  // boundaries instead of whenever a parent happens to re-render.
  const now = useTrialNow(trial);
  const trialLifecycle = getTrialLifecycleFromDates(trial, now);

  let eligibleForSurface: boolean;
  const normalEligibility =
    trialLifecycle === "active" ||
    rawGramAccountType === "payg" ||
    ((rawGramAccountType === "free" || rawGramAccountType === "base") &&
      !hasActiveSubscription);
  switch (eligibility) {
    case "billing":
      eligibleForSurface = normalEligibility;
      break;
    case "setup":
      eligibleForSurface = !whitelisted || normalEligibility;
      break;
    case "active-trial":
      eligibleForSurface = trialLifecycle === "active";
      break;
    case "gated":
      eligibleForSurface = !whitelisted;
      break;
  }

  return {
    eligible:
      flag.status === "enabled" && hasScope("org:admin") && eligibleForSurface,
    activeOrganizationId,
    trialLifecycle,
  };
}

export type BillingState = {
  status: "unavailable" | "loading" | "error" | "needs-setup" | "manage";
  trialLifecycle: TrialLifecycle;
  checkoutEligible: boolean;
  checkoutConfirmed: boolean;
  fresh: boolean;
  subscription: ReturnType<typeof useStripeSubscription>;
};

/** Navigation resolves from the authoritative session-scoped SDK query.
 * Organization switches replace the authenticated session and reload the app;
 * keep the existing query identity shared with all subscription consumers.
 * Never starts checkout. Only the guarded setup route may do that.
 * `setup` deliberately retains lockout eligibility; normal billing must not.
 */
export function useBillingState({
  surface = "billing",
  confirming = false,
}: { surface?: "billing" | "setup"; confirming?: boolean } = {}): BillingState {
  const { eligible: checkoutEligible, trialLifecycle } =
    usePaygCheckoutAccess(surface);
  const { rawGramAccountType } = useSession();
  const { hasScope } = useRBAC();
  const canRead = hasScope("org:read") || hasScope("org:admin");
  // Existing PAYG management remains available without the rollout flag, but
  // active trial conversion is still explicitly flag/admin gated.
  const available =
    canRead &&
    (checkoutEligible ||
      (rawGramAccountType === "payg" && trialLifecycle !== "active"));
  const subscription = useStripeSubscription({
    enabled: available,
    ...(surface === "setup"
      ? {
          refetchOnMount: "always" as const,
          retryOnMount: true,
          retry: false,
          refetchInterval: confirming ? 2000 : false,
          refetchOnWindowFocus: false,
          refetchOnReconnect: false,
        }
      : {}),
  });
  const fresh = subscription.isFetchedAfterMount && !subscription.isFetching;
  const absent = isNotFoundError(subscription.error);
  const status: "unavailable" | "loading" | "error" | "needs-setup" | "manage" =
    !available
      ? "unavailable"
      : absent
        ? checkoutEligible
          ? "needs-setup"
          : "unavailable"
        : subscription.data
          ? "manage"
          : subscription.error
            ? "error"
            : "loading";
  return {
    status,
    trialLifecycle,
    checkoutEligible: canRead && checkoutEligible,
    // A management destination is NOT proof of successful checkout. Confirmation
    // requires a fresh, error-free backend read of active/trialing specifically.
    checkoutConfirmed:
      available &&
      fresh &&
      !subscription.error &&
      (subscription.data?.status === "active" ||
        subscription.data?.status === "trialing"),
    fresh,
    subscription,
  };
}
