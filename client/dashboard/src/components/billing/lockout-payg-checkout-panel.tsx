import { usePaygCheckoutAccess } from "@/components/billing/payg-checkout-access";
import { StartPaygCheckoutCTA } from "@/components/billing/start-payg-checkout-cta";
import { SessionContext, useSessionData } from "@/contexts/Auth";

/**
 * Self-serve way out of a dashboard lockout, shown above the booking calendar
 * on both gates: the organization that never trialed and the one whose
 * enterprise trial expired.
 *
 * `AuthProvider` returns those gates above `SessionContext.Provider`, so the
 * panel puts the session the shared CTA reads back in front of it rather than
 * teaching the CTA a second way to find one. Until the session resolves there
 * is nothing to be eligible for, so nothing renders.
 */
export function LockoutPaygCheckoutPanel(): JSX.Element | null {
  const { session } = useSessionData();

  if (!session) return null;

  return (
    <SessionContext.Provider value={session}>
      <GatedCheckoutPanel />
    </SessionContext.Provider>
  );
}

/**
 * Renders nothing unless the viewer can actually check out, so the gate keeps
 * its booking-only shape for members, for organizations that are not walled
 * off, and for every unresolved state of the rollout flag.
 */
function GatedCheckoutPanel(): JSX.Element | null {
  const { eligible } = usePaygCheckoutAccess("gated");

  if (!eligible) return null;

  return (
    <div className="flex w-full flex-col gap-4 border border-(--edge) bg-(--card) p-5">
      <div className="flex flex-col gap-1.5">
        <span className="auth-mono text-xs text-(--muted)">Self-serve</span>
        <p className="text-[16px] tracking-[0.0025em]">
          Start on pay as you go instead.
        </p>
        <p className="text-sm text-(--muted-strong)">
          Add a card to unlock your workspace now and pay for what you use.
          Booking a call stays open either way.
        </p>
      </div>
      <StartPaygCheckoutCTA eligibility="gated" />
    </div>
  );
}
