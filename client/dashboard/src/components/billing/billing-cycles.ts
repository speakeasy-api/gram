import { type TokensUnderManagement } from "@gram/client/models/components/tokensundermanagement.js";

// Billing-cycle helpers shared by the TUM billing section and the
// BillingCyclePicker view. Kept in a non-component module so the view file can
// satisfy the react-refresh "only export components" rule.

// A billing cycle option, sourced from usage.getTokensUnderManagement (cycles
// are anchored to the org's contracted anchor day, not calendar months).
export type BillingCycle = {
  start: Date;
  end: Date;
  // Billed TUM tokens for the cycle.
  tokens: number;
  // Whether this is the active cycle.
  current: boolean;
};

// The selectable cycles from a TUM response, most recent first. The active
// cycle comes from the top-level fields when history omits it, and its token
// count always prefers the live top-level number. Which cycle is active is
// the server's call (tum.periodStart) — the browser clock can sit outside
// the server-reported window and must not demote the live cycle.
export function cyclesFromTum(tum: TokensUnderManagement): BillingCycle[] {
  const activeStart = tum.periodStart.getTime();
  const byStart = new Map<number, BillingCycle>();
  for (const p of tum.history) {
    const current = p.periodStart.getTime() === activeStart;
    byStart.set(p.periodStart.getTime(), {
      start: p.periodStart,
      end: p.periodEnd,
      tokens: current ? tum.tokens : p.tokens,
      current,
    });
  }
  if (!byStart.has(tum.periodStart.getTime())) {
    byStart.set(tum.periodStart.getTime(), {
      start: tum.periodStart,
      end: tum.periodEnd,
      tokens: tum.tokens,
      current: true,
    });
  }
  return [...byStart.values()].sort(
    (a, b) => b.start.getTime() - a.start.getTime(),
  );
}

// The year keeps cycles distinguishable across the 12-cycle history window
// (the same anchored range recurs every 12 months).
const cycleDateFormat = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
  year: "numeric",
  timeZone: "UTC",
});

export function formatCycleRange(cycle: BillingCycle): string {
  return `${cycleDateFormat.format(cycle.start)} – ${cycleDateFormat.format(cycle.end)}`;
}

export function cycleKey(cycle: BillingCycle): string {
  return cycle.start.toISOString();
}

// React Query staleTime for data scoped to a cycle: closed cycles are
// immutable (telemetry for a past window never changes), so their queries
// never refetch; the active cycle stays reasonably fresh. Keyed on the
// server-derived current flag, not the browser clock.
export function cycleStaleTime(cycle: BillingCycle): number {
  return cycle.current ? 60_000 : Infinity;
}
