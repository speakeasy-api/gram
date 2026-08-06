import { Card } from "@/components/ui/Card";
import { useSession } from "@/contexts/Auth";
import { getEnterpriseTrialStatus } from "@/lib/enterprise-trial";
import { Timer } from "lucide-react";
import { useEffect, useState } from "react";

const SALES_URL = "https://www.speakeasy.com/talk-to-us";
const MILLISECONDS_PER_DAY = 24 * 60 * 60 * 1000;

export function EnterpriseTrialStatusCard(): JSX.Element | null {
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

  const daysLeftLabel = `${status.remainingDays} day${status.remainingDays === 1 ? "" : "s"} left`;
  const progressValue = status.progress * 100;
  const progressLabel = `Day ${status.dayNumber} of ${status.totalDays}`;
  const trialStatusLabel = `Enterprise trial: ${progressLabel}, ${daysLeftLabel}`;

  return (
    <Card className="shadow-none group-data-[collapsible=icon]:border-0 group-data-[collapsible=icon]:bg-transparent group-data-[collapsible=icon]:shadow-none group-data-[collapsible=icon]:[&>div]:p-2">
      <div
        role="group"
        aria-label={trialStatusLabel}
        title={`Trial: ${daysLeftLabel}`}
        className="flex items-center gap-2 group-data-[collapsible=icon]:justify-center"
      >
        <Timer className="size-4 shrink-0 text-muted-foreground" aria-hidden />
        <div className="min-w-0 flex-1 group-data-[collapsible=icon]:hidden">
          <div className="flex items-center justify-between gap-2">
            <Card.Title className="text-xs">TRIAL</Card.Title>
            <span className="text-muted-foreground text-xs">
              Day {status.dayNumber}/{status.totalDays}
            </span>
          </div>
          <div className="mt-2 flex items-center justify-between gap-2">
            <span className="text-sm font-medium">{daysLeftLabel}</span>
            <a
              href={SALES_URL}
              target="_blank"
              rel="noopener noreferrer"
              className="text-primary text-sm hover:underline"
            >
              Talk to Sales
            </a>
          </div>
          <progress
            className="mt-2 h-1 w-full"
            value={progressValue}
            max={100}
            aria-label={progressLabel}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={progressValue}
          />
        </div>
      </div>
    </Card>
  );
}
