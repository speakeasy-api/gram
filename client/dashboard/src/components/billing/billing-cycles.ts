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
  // Billed tokens per UTC day (days without usage omitted). This is the
  // org-wide series overage attribution derives its crossing point from —
  // it must not follow the page's project filter.
  days: { date: string; tokens: number }[];
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
      // RFCDate serializes to the "YYYY-MM-DD" the buckets align on.
      days: p.days.map((d) => ({ date: d.date.toString(), tokens: d.tokens })),
    });
  }
  if (!byStart.has(tum.periodStart.getTime())) {
    byStart.set(tum.periodStart.getTime(), {
      start: tum.periodStart,
      end: tum.periodEnd,
      tokens: tum.tokens,
      current: true,
      days: [],
    });
  }
  return [...byStart.values()].sort(
    (a, b) => b.start.getTime() - a.start.getTime(),
  );
}

const cycleMonthFormat = new Intl.DateTimeFormat("en-US", {
  month: "long",
  timeZone: "UTC",
});

// Cycles are named by their start month ("June Billing Cycle") — the
// 12-cycle window never repeats a month, and the range picker beside the
// dropdown shows the precise dates.
export function formatCycleName(cycle: BillingCycle): string {
  return `${cycleMonthFormat.format(cycle.start)} Billing Cycle`;
}

export function cycleKey(cycle: BillingCycle): string {
  return cycle.start.toISOString();
}

// The time window the TUM chart and details table scope to: a full billing
// cycle, or a custom range (typed into the range picker or drilled into by
// clicking a chart bar).
export type BillingPeriod = {
  start: Date;
  // Exclusive upper bound for cycles; range-picker instants sit at the last
  // covered moment — either way the queries treat it as the window's edge.
  end: Date;
  // The exactly-matching billing cycle when the period is one, else null.
  // Billed normalization and overage only apply to full org cycles.
  cycle: BillingCycle | null;
  // Display label for custom ranges (e.g. the range picker's parse label).
  label?: string;
};

export function periodFromCycle(cycle: BillingCycle): BillingPeriod {
  return { start: cycle.start, end: cycle.end, cycle, label: undefined };
}

const MS_PER_DAY = 24 * 60 * 60 * 1000;

// The Unix epoch sits at a UTC midnight, so this holds exactly for UTC
// day boundaries.
function isUTCMidnight(d: Date): boolean {
  return d.getTime() % MS_PER_DAY === 0;
}

// What the time-range picker should display for a period. The picker renders
// instants in LOCAL time, but day-aligned periods (billing cycles, calendar
// picks, bar-click drill-downs) are UTC-day windows with an exclusive end —
// displayed raw, a June cycle would read "May 31 – Jul 1" anywhere west of
// UTC. Map those to local midnights of their UTC calendar days, last day
// inclusive. Ranges with real times (natural-language parses) pass through.
export function periodDisplayRange(period: BillingPeriod): {
  from: Date;
  to: Date;
} {
  const last = new Date(period.end.getTime() - 1);
  if (!isUTCMidnight(period.start) || !isUTCMidnight(period.end)) {
    // The display is date-granular, so the -1ms only shows for ends sitting
    // exactly on a midnight — where it is what makes a range like
    // "yesterday" (ending at today 00:00 local) read as yesterday's date
    // instead of today's. Display-only: nothing feeds this range back.
    return { from: period.start, to: last };
  }
  return {
    from: new Date(
      period.start.getUTCFullYear(),
      period.start.getUTCMonth(),
      period.start.getUTCDate(),
    ),
    to: new Date(last.getUTCFullYear(), last.getUTCMonth(), last.getUTCDate()),
  };
}

// React Query staleTime for data scoped to a period: closed windows are
// immutable (telemetry for a past window never changes), so their queries
// never refetch. Cycles key on the server-derived current flag, not the
// browser clock; custom ranges fall back to the clock with a one-hour guard
// that absorbs skew and late-arriving telemetry.
export function periodStaleTime(period: BillingPeriod): number {
  if (period.cycle) {
    return period.cycle.current ? 60_000 : Infinity;
  }
  return period.end.getTime() <= Date.now() - 60 * 60 * 1000
    ? Infinity
    : 60_000;
}
