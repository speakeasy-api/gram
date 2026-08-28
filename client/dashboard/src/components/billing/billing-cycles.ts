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

// Whether any cycle in the server's history window recorded billed usage.
// Only the billed totals count: a cycle whose days recompute nonzero against
// a zero billed total scales those days by zero everywhere they render, so
// admitting it would put the zeroed explorer back where the empty state
// belongs.
export function cyclesHaveUsage(cycles: BillingCycle[]): boolean {
  return cycles.some((c) => c.tokens > 0);
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

// The billed per-day token series, normalized so each cycle's days sum to
// its billed total, plus the cycle windows (ms ranges) the data fully
// describes — days outside every covered window have no billed answer.
export type BilledDays = {
  byDate: Map<string, number>;
  covered: { start: number; end: number }[];
};

// Billed tokens per UTC day across every known cycle. The daily series is
// advisory — a finalized cycle serves its sealed snapshot total while the
// days recompute live and can drift (late telemetry) or expire (aggregate
// TTL) — so each cycle's days are scaled to sum to its billed total, the
// number on the usage card; cumulative rounding keeps the series integral
// without losing the exact sum.
//
// `covered` records the cycle windows the billed data fully describes —
// including zero-token cycles, where every day is a known zero (a sealed
// zero total beats whatever late telemetry recomputed). A cycle with a
// nonzero total but no daily shape (the synthesized active-cycle fallback)
// stays uncovered, and consumers fall back to analytics totals there.
export function billedDaysFromCycles(cycles: BillingCycle[]): BilledDays {
  const byDate = new Map<string, number>();
  const covered: { start: number; end: number }[] = [];
  for (const c of cycles) {
    const daysSum = c.days.reduce((sum, d) => sum + d.tokens, 0);
    if (daysSum === 0) {
      // A zero-token cycle is fully known ONLY once sealed: the active
      // cycle reads zero before its first snapshot lands, and marking it
      // covered would report billed zeros over real live traffic.
      if (c.tokens === 0 && !c.current) {
        covered.push({ start: c.start.getTime(), end: c.end.getTime() });
      }
      continue;
    }
    covered.push({ start: c.start.getTime(), end: c.end.getTime() });
    const scale = c.tokens / daysSum;
    let acc = 0;
    let prevRounded = 0;
    for (const d of c.days) {
      acc += d.tokens * scale;
      const rounded = Math.round(acc);
      byDate.set(d.date, rounded - prevRounded);
      prevRounded = rounded;
    }
  }
  return { byDate, covered };
}

// The UTC calendar day of a daily bucket, as "YYYY-MM-DD" — the key the
// billed per-day series (BillingCycle.days) aligns on. Bucket timestamps are
// unix-nano strings that exceed Number precision; divide as BigInt first.
export function bucketDateKey(nano: string): string {
  try {
    return new Date(Number(BigInt(nano) / 1_000_000n))
      .toISOString()
      .slice(0, 10);
  } catch {
    return "";
  }
}

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

// Title for a custom-range usage card: the picker's parse label when the
// range came from one, else the local display dates ("Jul 5 – Jul 18"),
// matching the range picker's own format.
const rangeDayFormat = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
});

export function formatPeriodLabel(period: BillingPeriod): string {
  if (period.label) return period.label;
  const { from, to } = periodDisplayRange(period);
  const fromLabel = rangeDayFormat.format(from);
  const toLabel = rangeDayFormat.format(to);
  return fromLabel === toLabel ? fromLabel : `${fromLabel} – ${toLabel}`;
}

// Whether the billed daily series fully answers a period: its bounds sit on
// UTC day boundaries and every day falls inside a covered cycle window.
// Cycles are contiguous, so a sorted sweep over the windows suffices.
function periodCoveredByBilled(
  period: { start: Date; end: Date },
  covered: { start: number; end: number }[],
): boolean {
  if (!isUTCMidnight(period.start) || !isUTCMidnight(period.end)) return false;
  const sorted = [...covered].sort((a, b) => a.start - b.start);
  const end = period.end.getTime();
  let cursor = period.start.getTime();
  for (const w of sorted) {
    if (cursor >= end) break;
    if (w.end <= cursor) continue;
    if (w.start > cursor) return false;
    cursor = w.end;
  }
  return cursor >= end;
}

// Sum of a per-UTC-day series over a period. Keys are "YYYY-MM-DD", which
// parse back to the day's start instant.
function sumDaysInPeriod(
  byDate: Map<string, number>,
  period: { start: Date; end: Date },
): number {
  const start = period.start.getTime();
  const end = period.end.getTime();
  let sum = 0;
  for (const [date, tokens] of byDate) {
    const ms = Date.parse(date);
    if (ms >= start && ms < end) sum += tokens;
  }
  return sum;
}

// Per-day billed overage tokens under a contracted allowance: within each
// cycle carrying a daily series, a crossing-point walk over the normalized
// billed days (BilledDays.byDate), recorded as the day-over-day increase of
// the cumulative excess — which prorates the crossing day and telescopes to
// exactly the cycle's billed overage. Cycles without a daily series
// contribute nothing; their windows are only covered when sealed at zero,
// where overage is zero anyway.
export function overageDaysFromBilled(
  cycles: BillingCycle[],
  billed: BilledDays,
  limit: number,
): Map<string, number> {
  const overage = new Map<string, number>();
  for (const cycle of cycles) {
    const dates = cycle.days.map((d) => d.date).sort();
    let cumulative = 0;
    let prevExcess = 0;
    for (const date of dates) {
      cumulative += billed.byDate.get(date) ?? 0;
      const excess = Math.max(0, cumulative - limit);
      overage.set(date, excess - prevExcess);
      prevExcess = excess;
    }
  }
  return overage;
}

// The billed answers for one period, resolved once so the usage card, the
// chart headline, and the details table all read the same numbers — their
// agreement is structural, not three recomputations that must be kept in
// lockstep.
export type PeriodFigures = {
  // Whether the billed daily series fully answers the period's window.
  covered: boolean;
  // Billed tokens in the period; null when the billed data can't answer it
  // (consumers fall back to the analytics totals).
  tokens: number | null;
  // Billed overage tokens in the period; null when not attributable (no
  // contracted allowance, or an uncovered window).
  overage: number | null;
};

export function resolvePeriodFigures(
  period: BillingPeriod,
  billedDays: BilledDays,
  overageDays: Map<string, number> | null,
  limit: number | null,
): PeriodFigures {
  const cycle = period.cycle;
  const covered = periodCoveredByBilled(period, billedDays.covered);
  if (cycle) {
    return {
      covered,
      tokens: cycle.tokens,
      overage: limit != null ? Math.max(0, cycle.tokens - limit) : null,
    };
  }
  if (!covered) {
    return { covered, tokens: null, overage: null };
  }
  return {
    covered,
    tokens: sumDaysInPeriod(billedDays.byDate, period),
    overage:
      overageDays != null
        ? Math.round(sumDaysInPeriod(overageDays, period))
        : null,
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
