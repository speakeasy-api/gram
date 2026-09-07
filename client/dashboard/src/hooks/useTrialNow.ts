import {
  getTrialStatusFromDates,
  isValidDate,
  MILLISECONDS_PER_DAY,
} from "@/lib/trial-status";
import { useEffect, useState } from "react";

/**
 * Reference time for trial-derived UI, re-rendered at the moments a trial
 * derivation can actually change: the next day boundary, the next
 * remaining-days boundary, and the end of the trial itself. Nothing is
 * scheduled in between and nothing is scheduled once the trial has ended, so a
 * surface that reads a trial stays correct without polling and without waiting
 * on a parent to re-render it.
 */
export function useTrialNow(
  trial: { startedAt: Date; endsAt: Date } | null | undefined,
): Date {
  const [now, setNow] = useState(() => new Date());
  const status = getTrialStatusFromDates(trial, now);
  const startedAt = isValidDate(trial?.startedAt)
    ? trial.startedAt.getTime()
    : undefined;
  const endsAt = isValidDate(trial?.endsAt)
    ? trial.endsAt.getTime()
    : undefined;
  const nowTime = now.getTime();
  const dayNumber = status?.dayNumber;
  const remainingDays = status?.remainingDays;

  useEffect(() => {
    setNow(new Date());
  }, [trial?.startedAt, trial?.endsAt]);

  useEffect(() => {
    if (
      startedAt === undefined ||
      endsAt === undefined ||
      dayNumber === undefined ||
      remainingDays === undefined
    ) {
      return;
    }

    const nextDayBoundary = startedAt + dayNumber * MILLISECONDS_PER_DAY;
    const nextRemainingDaysBoundary =
      endsAt - (remainingDays - 1) * MILLISECONDS_PER_DAY;
    const nextUpdateAt = Math.min(
      endsAt,
      nextDayBoundary > nowTime ? nextDayBoundary : Infinity,
      nextRemainingDaysBoundary > nowTime
        ? nextRemainingDaysBoundary
        : Infinity,
    );
    const timer = window.setTimeout(
      () => setNow(new Date()),
      nextUpdateAt - nowTime,
    );

    return () => window.clearTimeout(timer);
  }, [dayNumber, endsAt, nowTime, remainingDays, startedAt]);

  return now;
}
