import { useSession } from "@/contexts/Auth";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useRBAC } from "@/hooks/useRBAC";
import { useTrialNow } from "@/hooks/useTrialNow";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { getTrialLifecycleFromDates } from "@/lib/trial-status";

/**
 * Which surface is asking, which decides what makes an organization eligible.
 *
 * - `"active-trial"`: the in-app surfaces (billing page, sidebar trial card),
 *   where checkout converts a running trial.
 * - `"gated"`: the dashboard lockout pages, where checkout is the way out of
 *   the gate for an organization that is walled off entirely.
 */
export type PaygCheckoutEligibility = "active-trial" | "gated";

/**
 * Whether this viewer may start a self-serve checkout, plus the organization
 * the single-flight lock belongs to.
 *
 * Reads the session from context on every surface. `AuthProvider` returns the
 * lockout pages above `SessionContext.Provider`, so a gated caller supplies the
 * session itself — see `LockoutPaygCheckoutPanel`.
 */
export function usePaygCheckoutAccess(eligibility: PaygCheckoutEligibility): {
  eligible: boolean;
  activeOrganizationId: string;
} {
  const flag = useFeatureFlag(FEATURE_FLAGS.paygSelfServeBilling);
  const { hasScope } = useRBAC();
  const { trial, activeOrganizationId, whitelisted } = useSession();
  // A trial that ends while the page is open has to take the CTA with it, so
  // the lifecycle below reads a clock that re-renders on the trial's own
  // boundaries instead of whenever a parent happens to re-render.
  const now = useTrialNow(trial);

  // A gated organization is one the dashboard has walled off, so the trial is
  // beside the point: never trialed and trial expired both belong here. An
  // organization that is not walled off keeps the booking-only gate it had.
  const eligibleForSurface =
    eligibility === "gated"
      ? !whitelisted
      : getTrialLifecycleFromDates(trial, now) === "active";

  return {
    eligible:
      flag.status === "enabled" && hasScope("org:admin") && eligibleForSurface,
    activeOrganizationId,
  };
}
