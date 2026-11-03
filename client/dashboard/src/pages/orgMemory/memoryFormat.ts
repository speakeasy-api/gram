import { pluralize } from "@/lib/format";
import { format } from "date-fns";

export type DigestLevel = "day" | "week" | "month";

/** A period boundary as the API sends it: an RFC date or YYYY-MM-DD text. */
type PeriodDate = { toString(): string } | string;

/**
 * Parses a YYYY-MM-DD period boundary as a local calendar date. Periods are
 * UTC days, so parsing them as instants would shift every label one day back
 * for viewers west of UTC.
 */
export function parsePeriodDate(value: PeriodDate): Date {
  const [y, m, d] = String(value).slice(0, 10).split("-").map(Number);
  return new Date(y ?? 1970, (m ?? 1) - 1, d ?? 1);
}

/** The label a period goes by: "Tue, Oct 27", "Oct 26 – Nov 1", "October 2026". */
export function formatPeriod(
  level: string,
  start: PeriodDate,
  end: PeriodDate,
): string {
  const from = parsePeriodDate(start);
  switch (level) {
    case "day":
      return format(from, "EEE, MMM d");
    case "month":
      return format(from, "MMMM yyyy");
    default: {
      // period_end is exclusive: the last day shown is the one before it.
      const last = parsePeriodDate(end);
      last.setDate(last.getDate() - 1);
      return from.getMonth() === last.getMonth()
        ? `${format(from, "MMM d")} – ${format(last, "d")}`
        : `${format(from, "MMM d")} – ${format(last, "MMM d")}`;
    }
  }
}

/** The level one step down: a month folds weeks, a week folds days. */
export function childLevel(level: DigestLevel): DigestLevel | null {
  if (level === "month") return "week";
  if (level === "week") return "day";
  return null;
}

/** What a digest folded, in the words the timeline uses. */
export function describeDigestSources(digest: {
  level: string;
  sessionCount: number;
  memoCount: number;
  childCount: number;
}): string {
  if (digest.level === "day") {
    return `${pluralize(digest.sessionCount, "session")} · ${pluralize(digest.memoCount, "memo")}`;
  }
  return pluralize(
    digest.childCount,
    digest.level === "month" ? "week" : "day",
  );
}

/** The first line of a digest, for the timeline's one-line preview. */
export function firstLine(text: string): string {
  return (
    text
      .split(/\n+/)
      .find((line) => line.trim() !== "")
      ?.trim() ?? ""
  );
}
