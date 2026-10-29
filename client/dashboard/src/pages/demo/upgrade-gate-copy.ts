import {
  getTrialLifecycleFromDates,
  getTrialStatusFromDates,
  isValidDate,
} from "@/lib/trial-status";

const SHARED_BODY =
  "Book 30 minutes with us and we'll find the plan that fits your organization.";

export type GateCopy = {
  /** Color of the status dot. Static — the pulse is for live sessions. */
  dotClassName: string;
  status: string;
  body: string;
  detail: string;
  /** Prefills the booking form's "How'd you hear about us?" question. */
  source: string;
  /** Prefills the booking form's "Additional notes" question. */
  notes: string;
};

function ordinalDay(day: number): string {
  const mod100 = day % 100;
  if (mod100 >= 11 && mod100 <= 13) return `${day}th`;
  switch (day % 10) {
    case 1:
      return `${day}st`;
    case 2:
      return `${day}nd`;
    case 3:
      return `${day}rd`;
    default:
      return `${day}th`;
  }
}

/** "Aug 9th, 2026" for the calendar day of `date` in `timeZone`. */
function formatEndedOn(date: Date, timeZone: string): string {
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone,
    month: "short",
    day: "numeric",
    year: "numeric",
  }).formatToParts(date);
  const value = (type: Intl.DateTimeFormatPartTypes) =>
    parts.find((part) => part.type === type)?.value ?? "";
  return `${value("month")} ${ordinalDay(Number(value("day")))}, ${value("year")}`;
}

// The upgrade page serves two audiences: an org walled after its trial ran out,
// and an org still inside its trial that wants to upgrade early. Only the
// header copy differs; the booking card below is the same either way. A trial
// that has ended is the exception, so the running trial is the default — a
// session with no readable trial gets that wording rather than a third variant.
// `timeZone` is the calendar the ended date is written in. Callers omit it and
// get the viewer's zone.
export function getGateCopy(
  trial: { startedAt: Date; endsAt: Date } | null | undefined,
  now: Date,
  timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone,
): GateCopy {
  const endsAt = trial?.endsAt;

  if (getTrialLifecycleFromDates(trial, now) === "expired") {
    return {
      dotClassName: "bg-(--vermilion)",
      // Belt and braces: "expired" already implies a parseable end date, so the
      // dateless form is unreachable unless that changes.
      status: isValidDate(endsAt)
        ? `Trial ended ${formatEndedOn(endsAt, timeZone)}`
        : "Trial ended",
      body: `Trials run 14 days. ${SHARED_BODY}`,
      detail:
        "Your MCP servers, observability data, and policies are still here when you upgrade.",
      source: "Trial: Expired",
      notes: "Upgrade expired trial",
    };
  }

  const remainingDays = getTrialStatusFromDates(trial, now)?.remainingDays;
  return {
    dotClassName: "bg-(--moss)",
    status:
      remainingDays === undefined
        ? "Trial in progress"
        : `Trial · ${remainingDays} day${remainingDays === 1 ? "" : "s"} left`,
    body: `Upgrade before your trial ends and nothing pauses. ${SHARED_BODY}`,
    detail:
      "Your MCP servers, observability data, and policies carry over unchanged.",
    source: "Trial: Active",
    notes: "Upgrade trial",
  };
}
