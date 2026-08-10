import {
  getTrialLifecycleFromDates,
  getTrialStatusFromDates,
  isValidDate,
} from "@/lib/trial-status";
import { format } from "date-fns";

const SHARED_BODY =
  "Book 30 minutes with us and we'll find the plan that fits your organization.";

export type GateCopy = {
  /** Color of the status dot. Static — the pulse is for live sessions. */
  dotClassName: string;
  status: string;
  body: string;
  detail: string;
};

// The upgrade page serves two audiences: an org walled after its trial ran out,
// and an org still inside its trial that wants to upgrade early. Only the
// header copy differs; the booking card below is the same either way. A trial
// that has ended is the exception, so the running trial is the default — a
// session with no readable trial gets that wording rather than a third variant.
export function getGateCopy(
  trial: { startedAt: Date; endsAt: Date } | null | undefined,
  now: Date,
): GateCopy {
  const endsAt = trial?.endsAt;

  if (getTrialLifecycleFromDates(trial, now) === "expired") {
    return {
      dotClassName: "bg-[var(--vermilion)]",
      // Belt and braces: "expired" already implies a parseable end date, so the
      // dateless form is unreachable unless that changes.
      status: isValidDate(endsAt)
        ? `Trial ended ${format(endsAt, "MMM do, yyyy")}`
        : "Trial ended",
      body: `Trials run 14 days. ${SHARED_BODY}`,
      detail:
        "Your MCP servers, observability data, and policies are still here when you upgrade.",
    };
  }

  const remainingDays = getTrialStatusFromDates(trial, now)?.remainingDays;
  return {
    dotClassName: "bg-[var(--moss)]",
    status:
      remainingDays === undefined
        ? "Trial in progress"
        : `Trial · ${remainingDays} day${remainingDays === 1 ? "" : "s"} left`,
    body: `Upgrade before your trial ends and nothing pauses. ${SHARED_BODY}`,
    detail:
      "Your MCP servers, observability data, and policies carry over unchanged.",
  };
}
