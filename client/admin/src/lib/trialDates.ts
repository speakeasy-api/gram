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
const MS_PER_DAY = 86_400_000;

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
