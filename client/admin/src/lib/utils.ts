import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}

// Date only, no clock, read in UTC. Every surface that shows a date in a row or
// a panel reads it this way, so the same field cannot come out two ways on two
// pages.
//
// The zone is the whole point. `trial_ends_at` is a real instant, armed as the
// signup moment plus the trial length, and the sweeper that demotes the account
// compares it against server time. Rendered in the reader's own zone that
// instant can name a different day than the one the server acts on: a trial
// ending 2026-05-06T03:00Z reads as 5/5/2026 in California. An operator who
// acts on that day demotes an account that still had a day. Dropping the clock
// means the zone cannot be dropped too.
//
// The other fields formatted here, `created_at` and `disabled_at`, move with
// it. Their rendered day now follows the server rather than the reader's clock,
// and can differ by one near midnight. That is the right frame for an admin
// tool: two operators in two zones read one organization the same way, and the
// day they read is the day the database records.
export function fmtDateShort(iso?: string): string {
  if (!iso) return "-";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "-";
  return d.toLocaleDateString(undefined, { timeZone: "UTC" });
}

// Oldest first, which is the order the record's views draw: the project a
// customer started with and the member who opened the account explain the
// account, so they lead. The id breaks a tie, because two rows created in the
// same second would otherwise swap places between renders.
export function byOldestFirst<T extends { id: string; created_at: string }>(
  a: T,
  b: T,
): number {
  const at = Date.parse(a.created_at);
  const bt = Date.parse(b.created_at);
  if (at !== bt && !Number.isNaN(at) && !Number.isNaN(bt)) return at - bt;
  return a.id.localeCompare(b.id);
}
