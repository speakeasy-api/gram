import type { AccessRequest } from "@gram/client/models/components/accessrequest.js";
import type { Duration } from "@gram/client/models/components/requestaccessform.js";
import { format, isSameDay } from "date-fns";
import {
  approvalChoices,
  MAX_GRANT_MS,
  MIN_END_OF_DAY_LEAD_MS,
} from "./approvalChoices";

/** A requester's answer to "How long do you need it?". */
export type RequestedDurationKey = "none" | "1h" | "2h" | "4h" | "eod";

const HOUR_SECONDS = 60 * 60;

/** The fixed windows, by length, as the request and the approval name them. */
const FIXED_WINDOWS: { key: Duration; seconds: number; label: string }[] = [
  { key: "1h", seconds: HOUR_SECONDS, label: "1 hour" },
  { key: "2h", seconds: 2 * HOUR_SECONDS, label: "2 hours" },
  { key: "4h", seconds: 4 * HOUR_SECONDS, label: "4 hours" },
];

/**
 * The answers a requester can give: no preference, the windows an approver
 * can pick, and End of day while the requester's day has room for it. End of
 * day follows the requester's own clock.
 */
export function requestDurationChoices(
  now: Date,
): { key: RequestedDurationKey; label: string }[] {
  return [
    { key: "none", label: "No preference" },
    ...approvalChoices(now).map((choice) => ({
      key: choice.key as RequestedDurationKey,
      label: choice.label,
    })),
  ];
}

/**
 * The request form fields for an answer, worked out at the moment the
 * request is sent: a fixed window is sent as a duration, End of day as the
 * requester's local 23:59, which the server stores in UTC.
 */
export function requestedDurationFields(
  key: RequestedDurationKey,
  now: Date,
): { duration?: Duration; requestedUntil?: Date } {
  if (key === "none") return {};
  if (key === "eod") {
    const endOfDay = approvalChoices(now).find(
      (choice) => choice.key === "eod",
    );
    return endOfDay ? { requestedUntil: endOfDay.expiresAt } : {};
  }
  return { duration: key };
}

/**
 * What the requester asked for, as a phrase that follows "access": "for 2
 * hours" or "until 18:00", or undefined when they did not say.
 */
export function requestedDurationLabel(
  request: Pick<AccessRequest, "requestedDurationSeconds" | "requestedUntil">,
  now: Date,
): string | undefined {
  if (request.requestedUntil) {
    const until = request.requestedUntil;
    return `until ${deadlineLabel(until, now)}`;
  }
  const seconds = request.requestedDurationSeconds;
  if (!seconds) return undefined;
  const fixed = FIXED_WINDOWS.find((window) => window.seconds === seconds);
  if (fixed) return `for ${fixed.label}`;
  const hours = Math.floor(seconds / HOUR_SECONDS);
  const minutes = Math.floor((seconds % HOUR_SECONDS) / 60);
  const plural = (count: number, unit: string) =>
    `${count} ${unit}${count === 1 ? "" : "s"}`;
  return `for ${[
    hours ? plural(hours, "hour") : "",
    minutes ? plural(minutes, "minute") : "",
  ]
    .filter(Boolean)
    .join(" ")}`;
}

/** One approval window an administrator can pick for a request. */
export type ApprovalWindow = {
  key: string;
  label: string;
  expiresAt: Date;
};

/**
 * The windows an administrator can approve a request for, and the one that
 * matches what the requester asked for. A fixed duration picks its window;
 * a deadline picks End of day when it is the administrator's own end of day,
 * and is otherwise offered as a window of its own, so approving it gives
 * exactly what was asked. Without an ask, or with a deadline too close or too
 * far to grant, the first window is suggested.
 */
export function approvalWindowsFor(
  request: Pick<AccessRequest, "requestedDurationSeconds" | "requestedUntil">,
  now: Date,
): { windows: ApprovalWindow[]; suggested: string } {
  const windows: ApprovalWindow[] = approvalChoices(now);
  const first = windows[0]!.key;

  const until = request.requestedUntil;
  if (until) {
    const endOfDay = windows.find((window) => window.key === "eod");
    if (endOfDay && endOfDay.expiresAt.getTime() === until.getTime()) {
      return { windows, suggested: "eod" };
    }
    // The same bounds as End of day: enough left to decide, within a day.
    const lead = until.getTime() - now.getTime();
    if (lead >= MIN_END_OF_DAY_LEAD_MS && lead <= MAX_GRANT_MS) {
      return {
        windows: [
          ...windows,
          {
            key: "asked",
            label: `Until ${deadlineLabel(until, now)}`,
            expiresAt: until,
          },
        ],
        suggested: "asked",
      };
    }
    return { windows, suggested: first };
  }

  const seconds = request.requestedDurationSeconds;
  const fixed = FIXED_WINDOWS.find((window) => window.seconds === seconds);
  return { windows, suggested: fixed ? fixed.key : first };
}

/** A deadline as the reader's clock shows it: the time today, else the date. */
function deadlineLabel(until: Date, now: Date): string {
  return isSameDay(until, now)
    ? format(until, "HH:mm")
    : format(until, "d MMM, HH:mm");
}
