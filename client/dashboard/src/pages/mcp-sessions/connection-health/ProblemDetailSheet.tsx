import { OUTCOME } from "@/components/chart/palette";
import { StackedTimeBarChart } from "@/components/chart/StackedTimeBarChart";
import { IdentityAvatar } from "@/components/identity-avatar";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Skeleton } from "@/components/ui/Skeleton";
import { useRoutes } from "@/routes";
import { ArrowRight } from "lucide-react";
import { useMemo, type ReactNode } from "react";
import { Link } from "react-router";
import { useSuspectedProblem } from "./api";
import { AskAiButton, type Investigation } from "./AskAiButton";
import { logsSearch } from "./filters";
import {
  appLabel,
  formatClockMinutes,
  formatCount,
  formatDuration,
  formatSince,
  personLabel,
  PROBLEM_RULES,
  serverLabel,
  SOURCE_LABELS,
} from "./format";
import type { SuspectedProblemDetail } from "./types";

/** One suspected problem, opened from the Overview's panel. */
export function ProblemDetailSheet({
  id,
  providerId,
  now,
  onClose,
}: {
  id: string | null;
  providerId: string;
  now: Date;
  onClose: () => void;
}): JSX.Element {
  const query = useSuspectedProblem(id);
  const problem = query.data?.problem;
  return (
    <Sheet
      open={!!id}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <SheetContent className="bg-card w-full gap-0 overflow-y-auto p-0 sm:max-w-[600px]">
        {problem ? (
          <ProblemBody problem={problem} providerId={providerId} now={now} />
        ) : (
          <>
            <SheetHeader className="border-b px-6 py-5">
              <SheetTitle className="sr-only">Suspected problem</SheetTitle>
              <SheetDescription className="sr-only">
                One suspected problem
              </SheetDescription>
            </SheetHeader>
            <div className="p-6">
              {query.isPending ? (
                <div className="space-y-3">
                  <Skeleton className="h-6 w-2/3" />
                  <Skeleton className="h-32 w-full" />
                </div>
              ) : (
                <InlineEmptyState
                  icon="circle-check"
                  heading="No longer suspected"
                  description="People affected by it have connected since, or it was never listed for this project."
                />
              )}
            </div>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}

function investigationFor(problem: SuspectedProblemDetail): Investigation {
  return {
    question: `Why can't ${
      problem.people.length === 1
        ? personLabel(problem.people[0]!)
        : `these ${problem.people.length} people`
    } connect, and is there anything I should change?`,
    context: [
      `Suspected problem · ${problem.headline}`,
      `Since ${problem.since}`,
      problem.fact,
      `Rule: ${PROBLEM_RULES[problem.rule]}`,
      problem.source ? `Source: ${SOURCE_LABELS[problem.source]}` : null,
      `Messages: ${problem.messages.map((m) => `${m.message} (${m.reason}, ${m.attempts})`).join("; ")}`,
      `Servers: ${problem.servers.map((s) => `${serverLabel({ server: s.server })} (${s.attempts})`).join("; ")}`,
      problem.lastConnectedAt
        ? `Last successful connection: ${problem.lastConnectedAt}`
        : null,
    ]
      .filter(Boolean)
      .join(". "),
  };
}

function ProblemBody({
  problem,
  providerId,
  now,
}: {
  problem: SuspectedProblemDetail;
  providerId: string;
  now: Date;
}): JSX.Element {
  const routes = useRoutes();
  const failed = problem.failedPer10Minutes.reduce(
    (sum, b) => sum + b.attempts,
    0,
  );
  const logsHref = `${routes.mcpSessions.provider.connectionLogs.href(providerId)}${logsSearch(problem.filters, now)}`;
  const peopleCount = problem.people.length;
  const sinceLabel = formatSince(problem.since, now);
  return (
    <div className="flex min-h-full flex-col">
      <SheetHeader className="gap-1.5 border-b px-6 py-5 pr-12">
        <div className="text-eyebrow flex items-center gap-2">
          <span className="bg-destructive size-2 rounded-full" aria-hidden />
          Suspected problem
        </div>
        <SheetTitle className="font-mono text-lg font-normal">
          {problem.headline}
        </SheetTitle>
        <SheetDescription>
          Since {sinceLabel} ·{" "}
          {formatDuration(now.getTime() - Date.parse(problem.since))}
        </SheetDescription>
      </SheetHeader>

      <div className="flex-1 space-y-6 px-6 py-5">
        <div className="border-foreground space-y-2 border-l-2 pl-3 text-sm">
          <div className="text-eyebrow">Why this is listed</div>
          <p>
            {peopleCount === 1
              ? `${personLabel(problem.people[0]!)} failed`
              : `${peopleCount} people failed`}{" "}
            since {sinceLabel}.
            {problem.lastConnectedAt
              ? ` No one has connected since ${formatSince(problem.lastConnectedAt, now)}.`
              : ""}
          </p>
          <p className="text-muted-foreground">
            Rule: {PROBLEM_RULES[problem.rule]}
          </p>
          {problem.source ? (
            <p className="text-muted-foreground">
              Source: {SOURCE_LABELS[problem.source].toLowerCase()}
              {problem.source === "service" && problem.filters.services?.[0]
                ? ` (${problem.filters.services[0]})`
                : ""}
              .
            </p>
          ) : null}
        </div>

        <Block
          title={`Failed attempts · ${formatCount(failed)}`}
          aside="10-minute intervals"
        >
          <div className="border p-3">
            <FailureBars buckets={problem.failedPer10Minutes} />
          </div>
        </Block>

        <Block title={`People · ${peopleCount}`} aside="Attempts">
          <div className="divide-y border">
            {problem.people.map((p) => (
              <Link
                key={`${p.person?.subjectUrn ?? p.network?.key}-${p.server?.id}`}
                to={`${routes.mcpSessions.provider.connectionLogs.href(providerId)}${logsSearch(
                  {
                    ...problem.filters,
                    people: p.person ? [p.person.subjectUrn] : undefined,
                    networks:
                      !p.person && p.network ? [p.network.key] : undefined,
                  },
                  now,
                )}`}
                className="hover:bg-muted/40 grid grid-cols-[1fr_auto_40px_16px] items-center gap-3 px-4 py-2.5"
              >
                <div className="flex min-w-0 items-center gap-2">
                  <IdentityAvatar label={personLabel(p)} />
                  <div className="min-w-0">
                    <div className="truncate text-sm">{personLabel(p)}</div>
                    <div className="text-muted-foreground truncate text-xs">
                      {appLabel(p.app)}
                      {p.server
                        ? ` · ${serverLabel({ server: p.server })}`
                        : ""}
                    </div>
                  </div>
                </div>
                <span className="text-destructive text-xs">
                  {p.attempts > 1 ? "started" : "at"}{" "}
                  {formatClockMinutes(p.firstFailedAt)}
                </span>
                <span className="text-right text-sm">{p.attempts}</span>
                <ArrowRight
                  className="text-muted-foreground size-4"
                  aria-hidden
                />
              </Link>
            ))}
          </div>
        </Block>

        <Block title={`Servers · ${problem.servers.length}`} aside="Attempts">
          <div className="divide-y border">
            {problem.servers.map((s) => (
              <div
                key={s.server.id}
                className="flex items-center justify-between px-4 py-2.5 text-sm"
              >
                <span className="underline underline-offset-2">
                  {serverLabel({ server: s.server })}
                </span>
                <span>{s.attempts}</span>
              </div>
            ))}
          </div>
        </Block>

        <Block title={`Messages · ${problem.messages.length}`} aside="Attempts">
          <div className="divide-y border">
            {problem.messages.map((m) => (
              <div
                key={m.reason}
                className="flex justify-between gap-4 px-4 py-3"
              >
                <div className="min-w-0">
                  <div className="text-sm">{m.message}</div>
                  <div className="text-muted-foreground font-mono text-xs">
                    {m.reason}
                  </div>
                </div>
                <span className="text-sm">{m.attempts}</span>
              </div>
            ))}
          </div>
        </Block>
      </div>

      <SheetFooter className="bg-card sticky bottom-0 flex-row justify-between border-t px-6 py-4">
        <AskAiButton investigation={investigationFor(problem)} />
        <Button asChild>
          <Link to={logsHref}>Open in Connection Logs</Link>
        </Button>
      </SheetFooter>
    </div>
  );
}

function Block({
  title,
  aside,
  children,
}: {
  title: string;
  aside?: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <section className="space-y-2">
      <div className="text-eyebrow flex justify-between">
        <h3>{title}</h3>
        {aside ? <span>{aside}</span> : null}
      </div>
      {children}
    </section>
  );
}

const BUCKET_LABEL = new Intl.DateTimeFormat(undefined, {
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
});

function FailureBars({
  buckets,
}: {
  buckets: { start: string; attempts: number }[];
}): JSX.Element {
  const chart = useMemo(
    () => ({
      labels: buckets.map((b) => {
        const d = new Date(b.start);
        return d.getMinutes() === 0 ? BUCKET_LABEL.format(d) : "";
      }),
      timestamps: buckets.map((b) => Date.parse(b.start)),
      tooltipLabels: buckets.map((b) => BUCKET_LABEL.format(new Date(b.start))),
      datasets: [
        {
          label: "Failed",
          data: buckets.map((b) => b.attempts),
          backgroundColor: OUTCOME.blocked,
        },
      ],
    }),
    [buckets],
  );
  return (
    <StackedTimeBarChart
      labels={chart.labels}
      timestamps={chart.timestamps}
      bucketMs={10 * 60_000}
      tooltipLabels={chart.tooltipLabels}
      datasets={chart.datasets}
      height={120}
    />
  );
}
