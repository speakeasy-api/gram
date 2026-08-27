// The operator picks the day a trial ends on; the server takes a count of days
// to add to the day it ends on now. Converting between the two is a subtraction,
// and it is only the right subtraction if both ends are read in one zone.
//
// `fmtDateShort` reads a trial's end in UTC on purpose, so a date the operator
// reads on the record is a UTC day. A calendar hands back a local-midnight Date,
// whose day is in its local fields. Both are reduced to a whole number of days
// from the epoch here, so the subtraction can never land between two days: read
// as instants instead, a fortnight across a daylight-saving boundary is 13.958
// days and floors to 13.
const MS_PER_MINUTE = 60_000;
const MS_PER_HOUR = 60 * MS_PER_MINUTE;
const MS_PER_DAY = 24 * MS_PER_HOUR;

// The UTC day an instant falls on. `undefined` for an absent or unparseable
// field, which is the same answer `fmtDateShort` gives it.
export function trialEndDay(iso?: string): number | undefined {
  if (!iso) return undefined;
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return undefined;
  return (
    Date.UTC(at.getUTCFullYear(), at.getUTCMonth(), at.getUTCDate()) /
    MS_PER_DAY
  );
}

// The day a picked date stands for. A calendar's dates are local midnight, so
// the day is in the local fields and reading the UTC ones would move it.
export function dayOf(picked: Date): number {
  return (
    Date.UTC(picked.getFullYear(), picked.getMonth(), picked.getDate()) /
    MS_PER_DAY
  );
}

// The reverse, for handing a day back to the calendar to select or to bound
// itself by.
export function calendarDate(day: number): Date {
  const at = new Date(day * MS_PER_DAY);
  return new Date(at.getUTCFullYear(), at.getUTCMonth(), at.getUTCDate());
}

// A day as an instant `fmtDateShort` will render as that same day.
export function dayISO(day: number): string {
  return new Date(day * MS_PER_DAY).toISOString();
}

// The UTC day the operator is standing on, so a start that has no existing
// end date to add days to can still offer a calendar whose days agree with
// `fmtDateShort`.
export function utcTodayDay(now: Date = new Date()): number {
  return (
    Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()) /
    MS_PER_DAY
  );
}

function quantity(value: number, unit: "day" | "hour" | "minute"): string {
  return `${value} ${unit}${value === 1 ? "" : "s"}`;
}

// Keep the most useful precision as a trial approaches its end. Rounding up
// prevents a still-running trial from being described as having no time left.
export function formatTrialTimeRemaining(
  endISO: string | undefined,
  now: Date = new Date(),
): string | undefined {
  if (!endISO) return undefined;

  const end = new Date(endISO);
  const remaining = end.getTime() - now.getTime();
  if (
    Number.isNaN(end.getTime()) ||
    Number.isNaN(now.getTime()) ||
    remaining <= 0
  ) {
    return undefined;
  }

  if (remaining > 72 * MS_PER_HOUR) {
    return quantity(Math.ceil(remaining / MS_PER_DAY), "day");
  }
  if (remaining >= 24 * MS_PER_HOUR) {
    return quantity(Math.ceil(remaining / MS_PER_HOUR), "hour");
  }

  const totalMinutes = Math.ceil(remaining / MS_PER_MINUTE);
  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  return [
    hours > 0 ? quantity(hours, "hour") : undefined,
    minutes > 0 ? quantity(minutes, "minute") : undefined,
  ]
    .filter((part): part is string => part !== undefined)
    .join(" ");
}
