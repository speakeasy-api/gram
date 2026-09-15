import { Badge } from "@/components/ui/Badge";
import { Skeleton } from "@/components/ui/Skeleton";
import { dateTimeFormatters, formatRelativeTime } from "@/lib/dates";
import type { RiskAnalysisStatusResult } from "@gram/client/models/components/riskanalysisstatusresult.js";
import { useRiskAnalysisStatus } from "@gram/client/react-query/riskAnalysisStatus.js";

/**
 * Re-poll cadence. The coordinator wakes within about 30s of new chat traffic
 * and never on a timer, so polling any faster cannot surface a newer state.
 */
const REFETCH_INTERVAL_MS = 30_000;

/**
 * Close statuses that mean the last run ended normally. continued_as_new is a
 * long-lived coordinator rolling its history over, not a failure.
 */
const NORMAL_OUTCOMES: ReadonlySet<string> = new Set([
  "completed",
  "continued_as_new",
]);

/**
 * Quiet "last analyzed" indicator for the Watchdog controls. Distinguishes an
 * empty page that means "no findings" from one that means "nothing has been
 * analyzed yet". Renders nothing on error: the badge is an aside, and the
 * signals query already owns the page's error surface.
 */
export function AnalysisStatusBadge(): JSX.Element | null {
  const statusQuery = useRiskAnalysisStatus(undefined, undefined, {
    refetchInterval: REFETCH_INTERVAL_MS,
    throwOnError: false,
  });

  if (statusQuery.isPending) {
    return (
      <Skeleton>
        <span className="h-5 w-28" />
      </Skeleton>
    );
  }
  if (!statusQuery.isSuccess) return null;

  return <StatusBadge status={statusQuery.data} />;
}

function StatusBadge({
  status,
}: {
  status: RiskAnalysisStatusResult;
}): JSX.Element {
  switch (status.state) {
    case "running":
      return (
        <Badge
          variant="neutral"
          background={false}
          tooltip={absoluteLabel("Running since", status.runningSince)}
        >
          <Badge.LeftIcon>
            <span
              aria-hidden
              className="bg-success-default size-1.5 animate-pulse rounded-full"
            />
          </Badge.LeftIcon>
          <Badge.Text>Analyzing now</Badge.Text>
        </Badge>
      );
    case "idle":
      return <IdleBadge status={status} />;
    case "never":
      return (
        <Badge variant="neutral" background={false}>
          No analysis yet
        </Badge>
      );
  }
}

function IdleBadge({
  status,
}: {
  status: RiskAnalysisStatusResult;
}): JSX.Element {
  const relative = formatRelativeTime(status.lastRunAt ?? null);
  const label = relative ? `Last analyzed ${relative}` : "Last analyzed";
  const outcome = status.lastRunOutcome;
  const abnormal = outcome !== undefined && !NORMAL_OUTCOMES.has(outcome);

  return (
    <Badge
      variant={abnormal ? "warning" : "neutral"}
      background={false}
      tooltip={absoluteLabel("Last run finished", status.lastRunAt)}
    >
      {abnormal ? `${label} · ${outcome.replaceAll("_", " ")}` : label}
    </Badge>
  );
}

/** Tooltip copy with the full local timestamp, or nothing when unknown. */
function absoluteLabel(prefix: string, at: Date | undefined): string | null {
  if (!at) return null;
  return `${prefix} ${dateTimeFormatters.full.format(at)}`;
}
