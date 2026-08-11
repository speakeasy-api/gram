import { Icon } from "@/components/ui/Icon";
import { Text } from "@/components/ui/Text";
import { useSession } from "@/contexts/Auth";
import {
  getTrialLifecycleFromDates,
  getTrialStatusFromDates,
  isValidDate,
  MILLISECONDS_PER_DAY,
} from "@/lib/trial-status";
import { useEffect, useState } from "react";
import { Link } from "react-router";

// The in-app upgrade gate, which prefills the booking form from the session.
// The marketing site's /talk-to-us cannot.
const SALES_PATH = "/talk-to-us";

const BLACK_PROGRESS_CLASS = "bg-(--color-base-black)";
const DEEP_GREEN_PROGRESS_CLASS = "bg-(--color-brand-c)";
const ORANGE_PROGRESS_CLASS = "bg-(--color-brand-ruby)";
const RED_PROGRESS_CLASS = "bg-(--color-brand-swift)";

function getTrialProgressColorClass(
  dayNumber: number,
  totalDays: number,
): string {
  const progress = dayNumber / totalDays;

  if (progress <= 2 / 7) {
    return BLACK_PROGRESS_CLASS;
  }

  if (progress <= 4 / 7) {
    return DEEP_GREEN_PROGRESS_CLASS;
  }

  if (progress <= 6 / 7) {
    return ORANGE_PROGRESS_CLASS;
  }

  return RED_PROGRESS_CLASS;
}

export function TrialStatusCard(): React.ReactNode {
  const { trial } = useSession();
  const [now, setNow] = useState(() => new Date());
  const status = getTrialStatusFromDates(trial, now);
  const lifecycle = getTrialLifecycleFromDates(trial, now);
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

  if (status === null && lifecycle !== "expired") {
    return null;
  }

  const daysLeft = status?.remainingDays;
  const daysLeftLabel =
    daysLeft === undefined
      ? undefined
      : `${daysLeft} day${daysLeft === 1 ? "" : "s"} left`;
  let progressValue = 100;
  let progressLabel = "Trial ended";
  let progressColorClass = RED_PROGRESS_CLASS;

  if (status !== null) {
    progressValue = Number((status.progress * 100).toFixed(2));
    progressLabel = `Day ${status.dayNumber} of ${status.totalDays}`;
    progressColorClass = getTrialProgressColorClass(
      status.dayNumber,
      status.totalDays,
    );
  }

  let salesLinkLabel = "Talk to sales";
  if (status === null || status.remainingDays <= 1) {
    salesLinkLabel = "Talk to sales about upgrading";
  }

  return (
    <div className="border-border border bg-card p-3 group-data-[collapsible=icon]:hidden">
      <div className="flex flex-col gap-3">
        <div className="flex items-center justify-between tracking-wide">
          <Text mono small className="uppercase">
            Trial
          </Text>
          {status !== null && (
            <Text mono small className="text-muted">
              Day {status.dayNumber}/{status.totalDays}
            </Text>
          )}
        </div>
        <div className="flex flex-col gap-1">
          <Text className="text-base">
            {status === null ? "Your trial has ended" : daysLeftLabel}
          </Text>
          <div
            role="progressbar"
            aria-label={progressLabel}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={progressValue}
            className="bg-muted h-1 w-full overflow-hidden rounded-full"
          >
            <div
              aria-hidden="true"
              className={`h-full rounded-full ${progressColorClass}`}
              style={{ width: `${progressValue}%` }}
            />
          </div>
        </div>
        <Link
          to={SALES_PATH}
          className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
        >
          {salesLinkLabel}
          <Icon name="arrow-right" className="size-3" aria-hidden="true" />
        </Link>
      </div>
    </div>
  );
}
