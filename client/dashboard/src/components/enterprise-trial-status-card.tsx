import { Icon } from "@/components/ui/Icon";
import { Text } from "@/components/ui/Text";
import { useSession } from "@/contexts/Auth";
import {
  getEnterpriseTrialStatus,
  MILLISECONDS_PER_DAY,
} from "@/lib/enterprise-trial";
import { useEffect, useState } from "react";

const SALES_URL = "https://www.speakeasy.com/talk-to-us";

const BLACK_PROGRESS_CLASS = "bg-[var(--color-base-black)]";
const DEEP_GREEN_PROGRESS_CLASS = "bg-[var(--color-brand-c)]";
const ORANGE_PROGRESS_CLASS = "bg-[var(--color-brand-ruby)]";
const RED_PROGRESS_CLASS = "bg-[var(--color-brand-swift)]";

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

export function EnterpriseTrialStatusCard(): React.ReactNode {
  const { enterpriseTrial } = useSession();
  const [now, setNow] = useState(() => new Date());
  const status = getEnterpriseTrialStatus(
    enterpriseTrial && {
      startedAt: enterpriseTrial.startedAt.toISOString(),
      endsAt: enterpriseTrial.endsAt.toISOString(),
    },
    now,
  );

  useEffect(() => {
    setNow(new Date());
  }, [enterpriseTrial?.startedAt, enterpriseTrial?.endsAt]);

  useEffect(() => {
    if (
      enterpriseTrial === null ||
      enterpriseTrial === undefined ||
      status === null
    ) {
      return;
    }

    const currentTime = now.getTime();
    const startsAt = enterpriseTrial.startedAt.getTime();
    const endsAt = enterpriseTrial.endsAt.getTime();
    const nextDayBoundary = startsAt + status.dayNumber * MILLISECONDS_PER_DAY;
    const nextRemainingDaysBoundary =
      endsAt - (status.remainingDays - 1) * MILLISECONDS_PER_DAY;
    const nextUpdateAt = Math.min(
      endsAt,
      nextDayBoundary > currentTime ? nextDayBoundary : Infinity,
      nextRemainingDaysBoundary > currentTime
        ? nextRemainingDaysBoundary
        : Infinity,
    );
    const timer = window.setTimeout(
      () => setNow(new Date()),
      nextUpdateAt - currentTime,
    );

    return () => window.clearTimeout(timer);
  }, [enterpriseTrial, now, status]);

  if (status === null) {
    return null;
  }

  const daysLeft = Math.max(status.totalDays - status.dayNumber, 0);
  const daysLeftLabel = `${daysLeft} day${daysLeft === 1 ? "" : "s"} left`;
  const progressValue = Number(
    Math.min((status.dayNumber / status.totalDays) * 100, 100).toFixed(2),
  );
  const progressLabel = `Day ${status.dayNumber} of ${status.totalDays}`;
  const progressColorClass = getTrialProgressColorClass(
    status.dayNumber,
    status.totalDays,
  );

  return (
    <div className="border-border/60 rounded-lg border bg-card p-3 shadow-sm group-data-[collapsible=icon]:hidden">
      <div className="flex flex-col gap-3">
        <div className="flex items-center justify-between tracking-wide">
          <Text mono small className="uppercase">
            Trial
          </Text>
          <Text mono small className="text-muted">
            Day {status.dayNumber}/{status.totalDays}
          </Text>
        </div>
        <div className="flex flex-col gap-1">
          <Text className="text-base">{daysLeftLabel}</Text>
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
        <a
          href={SALES_URL}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
        >
          Talk to sales
          <Icon name="arrow-right" className="size-3" aria-hidden="true" />
        </a>
      </div>
    </div>
  );
}
