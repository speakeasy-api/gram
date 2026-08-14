import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}

// Date only, no clock, read in UTC. Every surface that shows a date in a row or
// a panel reads it this way, so the same field cannot come out two ways on two
// pages.
//
// The zone is the whole point. A trial ends at a UTC midnight, and rendering
// that instant in the reader's own zone names the day before it everywhere west
// of UTC: a trial ending 2026-05-06 reads as 5/5/2026 in California. An
// operator who acts a day early demotes an account that still had a day.
// Dropping the clock means the zone cannot be dropped too.
//
// This also moves the other fields formatted here, `created_at` and
// `disabled_at`, which are real moments rather than UTC midnights. Their
// rendered day now follows the server rather than the reader's clock, and can
// differ by one near midnight. That is the right frame for an admin tool: two
// operators in two zones read one organization the same way, and the day they
// read is the day the database records.
export function fmtDateShort(iso?: string): string {
  if (!iso) return "-";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "-";
  return d.toLocaleDateString(undefined, { timeZone: "UTC" });
}
