import { ChartNoData } from "@/components/chart/ChartNoData";
import { chartColors, SHARED_TOOLTIP } from "@/components/chart/chartTheme";
import { OUTCOME, seriesForTheme } from "@/components/chart/palette";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Skeleton } from "@/components/ui/Skeleton";
import { useIsDarkTheme } from "@/lib/theme";
import type { ChartOptions, Plugin } from "chart.js";
import { ArrowRight } from "lucide-react";
import { useMemo, type ReactNode } from "react";
import { Line } from "react-chartjs-2";
import { Link } from "react-router";
import { useSessionActivity, useSignInSummary, type Window } from "./api";
import { formatDayMonth, formatWeekdayDay } from "./format";
import { HealthBarChart, LegendDot } from "./HealthBarChart";
import type { SignInSummaryResponse, SummaryBucket } from "./types";

export type SummaryMetric = "sign-ins" | "sessions";

const HOUR = 3_600_000;

const HOUR_LABEL = new Intl.DateTimeFormat(undefined, {
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
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
  const lineColor = seriesForTheme(useIsDarkTheme())[0] ?? OUTCOME.ok;
  const now = useMemo(() => new Date(window.to), [window.to]);

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
              <ChartMessage>
                Sign-in attempts need logging, which is off for this
                organization.
              </ChartMessage>
            ) : (
              <SignInBars data={signIns.data} now={now} />
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
        <div className="mt-3 flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-4">
            {metric === "sign-ins" ? (
              <>
                <LegendDot color={OUTCOME.ok} label="Signed in" />
                <LegendDot color={OUTCOME.blocked} label="Didn't sign in" />
              </>
            ) : (
              <LegendDot color={lineColor} label="Active sessions" />
            )}
          </div>
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

/** One label per day, under the bucket that holds its noon; hours on a one-day range. */
function barLabels(starts: number[], bucket: SummaryBucket): string[] {
  return starts.map((start) => {
    const date = new Date(start);
    if (bucket === "hour") {
      return date.getHours() % 4 === 0 ? HOUR_LABEL.format(date) : "";
    }
    if (bucket === "day") {
      return date.getDate() % 5 === 0 ? formatDayMonth(date.toISOString()) : "";
    }
    const noon = new Date(date);
    noon.setHours(12, 0, 0, 0);
    const t = noon.getTime();
    return t >= start && t < start + BUCKET_MS.six_hours
      ? formatWeekdayDay(start)
      : "";
  });
}

function SignInBars({
  data,
  now,
}: {
  data: SignInSummaryResponse;
  now: Date;
}): JSX.Element {
  const bars = useMemo(() => signInBars(data), [data]);
  const bucket = data.bucket ?? "hour";
  const chart = useMemo(() => {
    const starts = bars.map((bar) => bar.start);
    return {
      labels: barLabels(starts, bucket),
      timestamps: starts,
      tooltipLabels: starts.map((start) => TOOLTIP_TIME.format(start)),
      series: [
        {
          label: "Signed in",
          data: bars.map((bar) => bar.signedIn),
          color: OUTCOME.ok,
        },
        {
          label: "Didn't sign in",
          data: bars.map((bar) => bar.didNot),
          color: OUTCOME.blocked,
        },
      ],
    };
  }, [bars, bucket]);
  if (bars.every((bar) => bar.signedIn + bar.didNot === 0)) {
    return <ChartNoData />;
  }
  return (
    <HealthBarChart
      labels={chart.labels}
      timestamps={chart.timestamps}
      bucketMs={BUCKET_MS[bucket]}
      tooltipLabels={chart.tooltipLabels}
      series={chart.series}
      now={now}
      height={220}
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
  const font = useMemo(() => {
    const family = getComputedStyle(document.documentElement)
      .getPropertyValue("--font-mono")
      .trim();
    return family || "monospace";
  }, []);
  const data = useMemo(
    () => ({
      labels: points.map((p) =>
        formatDayMonth(new Date(`${p.day}T00:00`).toISOString()),
      ),
      datasets: [
        {
          label: "Active sessions",
          data: points.map((p) => p.activeSessions),
          borderColor: line,
          backgroundColor: line,
          pointRadius: points.map((_, i) => (i === points.length - 1 ? 3 : 0)),
          borderWidth: 1.5,
          tension: 0.2,
        },
      ],
    }),
    [points, line],
  );
  const lastLabel: Plugin<"line"> = {
    id: "activeNowLabel",
    afterDatasetsDraw(chart) {
      const point = chart.getDatasetMeta(0).data.at(-1);
      if (!point) return;
      const ctx = chart.ctx;
      ctx.save();
      ctx.fillStyle = colors.label;
      ctx.font = "12px sans-serif";
      ctx.textAlign = "right";
      ctx.textBaseline = "bottom";
      ctx.fillText(`${activeNow} today`, point.x, point.y - 8);
      ctx.restore();
    },
  };
  const options: ChartOptions<"line"> = {
    responsive: true,
    maintainAspectRatio: false,
    interaction: { mode: "index", intersect: false },
    layout: { padding: { top: 20, right: 8 } },
    plugins: {
      legend: { display: false },
      tooltip: SHARED_TOOLTIP,
    },
    scales: {
      x: {
        grid: { display: false },
        ticks: {
          maxTicksLimit: 5,
          maxRotation: 0,
          color: colors.labelFaded,
          font: { family: font, size: 11 },
        },
      },
      y: {
        beginAtZero: true,
        border: { display: false },
        grid: { color: colors.gridLine },
        ticks: {
          maxTicksLimit: 5,
          precision: 0,
          color: colors.labelFaded,
          font: { family: font, size: 11 },
        },
      },
    },
  };
  return (
    <Line
      key={`${isDark}-${activeNow}`}
      data={data}
      options={options}
      plugins={[lastLabel]}
    />
  );
}
