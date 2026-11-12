/** Session length drafts: a number and a unit, saved as hours. */
export type DurationUnit = "hour" | "day" | "week";

export type SessionDurationDraft = { number: number; unit: DurationUnit };

export const DURATION_UNIT_HOURS: Record<DurationUnit, number> = {
  hour: 1,
  day: 24,
  week: 24 * 7,
};

export function splitIntoUnit(hours: number): SessionDurationDraft {
  if (hours > 0 && hours % DURATION_UNIT_HOURS.week === 0) {
    return { number: hours / DURATION_UNIT_HOURS.week, unit: "week" };
  }
  if (hours > 0 && hours % DURATION_UNIT_HOURS.day === 0) {
    return { number: hours / DURATION_UNIT_HOURS.day, unit: "day" };
  }
  return { number: Math.max(0, hours), unit: "hour" };
}
