import { ChartNoData } from "@/components/chart/ChartNoData";
import {
  chartColors,
  EXPANDED_LEGEND,
  SHARED_TOOLTIP,
} from "@/components/chart/chartTheme";
import { OUTCOME, seriesForTheme } from "@/components/chart/palette";
import { StackedTimeBarChart } from "@/components/chart/StackedTimeBarChart";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Skeleton } from "@/components/ui/Skeleton";
import { useIsDarkTheme } from "@/lib/theme";
import type { ChartOptions } from "chart.js";
import { ArrowRight } from "lucide-react";
import { useMemo, type ReactNode } from "react";
import { Line } from "react-chartjs-2";
import { Link } from "react-router";
import { useSessionActivity, useSignInSummary, type Window } from "./api";
import type { SignInSummaryResponse, SummaryBucket } from "./types";

export type SummaryMetric = "sign-ins" | "sessions";

const HOUR = 3_600_000;

const SHORT_DAY = new Intl.DateTimeFormat(undefined, {
  weekday: "short",
  day: "numeric",
});
const HOUR_LABEL = new Intl.DateTimeFormat(undefined, {
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
});
const DAY_MONTH = new Intl.DateTimeFormat(undefined, {
  day: "numeric",
  month: "short",
});
const TOOLTIP_TIME = new Intl.DateTimeFormat(undefined, {
  weekday: "short",
  day: "numeric",
  month: "short",
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
});

const BUCKET_MS: Record<SummaryBucket, number> = {
  hour: HOUR,
  six_hours: 6 * HOUR,
  day: 24 * HOUR,
};

function signInBars(data: SignInSummaryResponse) {
  return (data.buckets ?? []).map((bucket) => ({
    start: Date.parse(bucket.start),
    signedIn: bucket.signIns.succeeded + bucket.renewals.succeeded,
    didNot: bucket.signIns.didNot + bucket.renewals.didNot,
  }));
}

/** The Overview's summary: sign-in attempts as bars, or active sessions. */
export function SummaryPanel({
  providerId,
  window,
  days,
  metric,
  onMetricChange,
  rangeControl,
  logsHref,
  sessionsHref,
}: {
  providerId: string;
  window: Window;
  days: number;
  metric: SummaryMetric;
  onMetricChange: (metric: SummaryMetric) => void;
  rangeControl: ReactNode;
  logsHref: string;
  sessionsHref: string;
}): JSX.Element {
  const scope = useMemo(() => ({ providerId }), [providerId]);
  const bucket: SummaryBucket =
    days <= 1 ? "hour" : days <= 7 ? "six_hours" : "day";
  const signIns = useSignInSummary(scope, window, bucket);
  const sessions = useSessionActivity(providerId, window, {
    enabled: metric === "sessions",
  });

  return (
    <section className="space-y-3" aria-labelledby="summary-heading">
      <div className="flex items-end justify-between gap-4">
        <div>
          <h2 id="summary-heading" className="text-eyebrow">
            Summary
          </h2>
          <p className="text-muted-foreground mt-1 text-sm">
            {metric === "sign-ins"
              ? "Sign-ins and renewals through this provider."
              : "Sessions through this provider: signed in, not expired or revoked."}
          </p>
        </div>
        {rangeControl}
      </div>
      <div className="bg-card border p-5">
        <div className="mb-4 flex justify-end">
          <SegmentedControl
            ariaLabel="Summary chart"
            value={metric}
            onChange={onMetricChange}
            options={[
              { value: "sign-ins", label: "Sign-in attempts" },
              { value: "sessions", label: "Active sessions" },
            ]}
          />
        </div>
        <div className="h-[220px]">
          {metric === "sign-ins" ? (
            signIns.isPending ? (
              <Skeleton className="h-full w-full" />
            ) : signIns.isError || !signIns.data ? (
              <ChartMessage>Couldn&apos;t load sign-in attempts.</ChartMessage>
            ) : !signIns.data.loggingEnabled ? (
              <ChartMessage>Sign-in attempts need logging.</ChartMessage>
            ) : (
              <SignInBars data={signIns.data} dayLabels={days > 1} />
            )
          ) : sessions.isPending ? (
            <Skeleton className="h-full w-full" />
          ) : sessions.data ? (
            <ActiveSessionsLine
              points={sessions.data.days}
              activeNow={sessions.data.activeNow}
            />
          ) : (
            <ChartMessage>Couldn&apos;t load active sessions.</ChartMessage>
          )}
        </div>
        <div className="mt-3 flex justify-end">
          <Link
            to={metric === "sign-ins" ? logsHref : sessionsHref}
            className="inline-flex items-center gap-1 text-sm hover:underline"
          >
            {metric === "sign-ins"
              ? "Open in Connection Logs"
              : "Open in Active Sessions"}
            <ArrowRight className="size-3.5" aria-hidden />
          </Link>
        </div>
      </div>
    </section>
  );
}

function ChartMessage({ children }: { children: ReactNode }): JSX.Element {
  return (
    <div className="text-muted-foreground flex h-full items-center justify-center text-sm">
      {children}
    </div>
  );
}

function SignInBars({
  data,
  dayLabels,
}: {
  data: SignInSummaryResponse;
  dayLabels: boolean;
}): JSX.Element {
  const bars = useMemo(() => signInBars(data), [data]);
  const chart = useMemo(() => {
    const labels = bars.map((bar) =>
      dayLabels ? SHORT_DAY.format(bar.start) : HOUR_LABEL.format(bar.start),
    );
    return {
      labels,
      timestamps: bars.map((bar) => bar.start),
      tooltipLabels: bars.map((bar) => TOOLTIP_TIME.format(bar.start)),
      datasets: [
        {
          label: "Signed in",
          data: bars.map((bar) => bar.signedIn),
          backgroundColor: OUTCOME.ok,
          stack: "attempts",
        },
        {
          label: "Didn't sign in",
          data: bars.map((bar) => bar.didNot),
          backgroundColor: OUTCOME.blocked,
          stack: "attempts",
        },
      ],
    };
  }, [bars, dayLabels]);
  if (bars.every((bar) => bar.signedIn + bar.didNot === 0)) {
    return <ChartNoData />;
  }
  return (
    <StackedTimeBarChart
      labels={chart.labels}
      timestamps={chart.timestamps}
      bucketMs={BUCKET_MS[data.bucket ?? "hour"]}
      tooltipLabels={chart.tooltipLabels}
      datasets={chart.datasets}
      height={220}
      expanded
    />
  );
}

function ActiveSessionsLine({
  points,
  activeNow,
}: {
  points: { day: string; activeSessions: number }[];
  activeNow: number;
}): JSX.Element {
  const isDark = useIsDarkTheme();
  const colors = chartColors(isDark);
  const line = seriesForTheme(isDark)[0] ?? OUTCOME.ok;
  const data = useMemo(
    () => ({
      labels: points.map((p) => DAY_MONTH.format(new Date(`${p.day}T00:00`))),
      datasets: [
        {
          label: `Active sessions · ${activeNow} now`,
          data: points.map((p) => p.activeSessions),
          borderColor: line,
          backgroundColor: line,
          pointRadius: points.map((_, i) => (i === points.length - 1 ? 3 : 0)),
          borderWidth: 1.5,
          tension: 0.2,
        },
      ],
    }),
    [points, activeNow, line],
  );
  const options: ChartOptions<"line"> = {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: "index", intersect: false },
    plugins: {
      legend: EXPANDED_LEGEND as unknown as NonNullable<
        ChartOptions<"line">["plugins"]
      >["legend"],
      tooltip: SHARED_TOOLTIP,
    },
    scales: {
      x: {
        grid: { display: false },
        ticks: { maxTicksLimit: 6, color: colors.labelFaded },
      },
      y: {
        beginAtZero: true,
        grid: { color: colors.gridLine },
        ticks: { maxTicksLimit: 5, color: colors.labelFaded },
      },
    },
  };
  return <Line data={data} options={options} />;
}
