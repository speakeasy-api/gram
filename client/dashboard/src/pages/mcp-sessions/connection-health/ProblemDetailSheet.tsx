import { OUTCOME } from "@/components/chart/palette";
import { HealthBarChart } from "./HealthBarChart";
import { IdentityAvatar } from "@/components/identity-avatar";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { Badge } from "@/components/ui/Badge";
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
import { SUSPECTED_PROBLEMS_ARE_SAMPLE, useSuspectedProblem } from "./api";
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
          <ProblemBody
            problem={problem}
            providerId={providerId}
            now={now}
            onClose={onClose}
          />
        ) : (
          <>
            <SheetHeader className="border-b px-6 py-5">
              <SheetTitle className="sr-only">Suspected problem</SheetTitle>
              <SheetDescription className="sr-only">
                One suspected problem
              </SheetDescription>
            </SheetHeader>
            <div className="p-6">
              {query.isError ? (
                <InlineEmptyState
                  icon="triangle-alert"
                  heading="Couldn't load this problem"
                  description="Close it and open it again."
                />
              ) : query.isPending ? (
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

/** What the people could not reach: "Linear" for the Linear service. */
function problemTarget(problem: SuspectedProblemDetail): string | undefined {
  if (problem.scope.kind === "service")
    return problem.scope.name.replace(/ service$/, "");
  if (problem.scope.kind === "server") return problem.scope.name;
  const server = problem.servers[0];
  return server
    ? serverLabel({ server: server.server, member: server.member })
    : undefined;
}

/** "on the Linear service", "on Internal Wiki"; nothing for one person. */
function problemPlace(problem: SuspectedProblemDetail): string {
  switch (problem.scope.kind) {
    case "service":
      return ` on the ${problem.scope.name}`;
    case "server":
    case "app_version":
      return ` on ${problem.scope.name}`;
    case "person":
      return "";
  }
}

function investigationFor(problem: SuspectedProblemDetail): Investigation {
  const target = problemTarget(problem);
  return {
    question: `Why can't ${
      problem.peopleCount === 1 && problem.people[0]
        ? personLabel(problem.people[0])
        : `these ${problem.peopleCount} people`
    } connect${target ? ` to ${target}` : ""}, and is there anything I should change?`,
    context: [
      `Suspected problem · ${problem.headline}`,
      SUSPECTED_PROBLEMS_ARE_SAMPLE
        ? "This suspected problem is sample data: its people, servers and times are made up."
        : null,
      `Since ${problem.since}`,
      problem.fact,
      `Rule: ${PROBLEM_RULES[problem.rule]}`,
      problem.source ? `Source: ${SOURCE_LABELS[problem.source]}` : null,
      `Messages: ${problem.messages.map((m) => `${m.message} (${m.reason}, ${m.attempts})`).join("; ")}`,
      `Servers: ${problem.servers.map((s) => `${serverLabel({ server: s.server, member: s.member })} (${s.attempts})`).join("; ")}`,
      problem.lastConnectedAt
        ? `Last successful connection: ${problem.lastConnectedAt}`
        : null,
    ]
      .filter(Boolean)
      .join("\n"),
  };
}

function ProblemBody({
  problem,
  providerId,
  now,
  onClose,
}: {
  problem: SuspectedProblemDetail;
  providerId: string;
  now: Date;
  onClose: () => void;
}): JSX.Element {
  const routes = useRoutes();
  const failed = problem.failedPer10Minutes.reduce(
    (sum, b) => sum + b.attempts,
    0,
  );
  const logsHref = `${routes.mcpSessions.provider.connectionLogs.href(providerId)}${logsSearch(problem.filters, now)}`;
  const peopleCount = problem.peopleCount;
  const sinceLabel = formatSince(problem.since, now);
  return (
    <div className="flex min-h-full flex-col">
      <SheetHeader className="gap-1.5 border-b px-6 py-5 pr-12">
        <div className="text-eyebrow flex items-center gap-2">
          <span className="bg-destructive size-2 rounded-full" aria-hidden />
          Suspected problem
          {SUSPECTED_PROBLEMS_ARE_SAMPLE ? (
            <Badge variant="warning">Sample data</Badge>
          ) : null}
        </div>
        <SheetTitle className="font-mono text-lg font-normal">
          {problem.headline}
        </SheetTitle>
        <SheetDescription>
          Since {sinceLabel} · latest failure{" "}
          {formatClockMinutes(problem.lastFailedAt)} ·{" "}
          {formatDuration(
            Date.parse(problem.lastFailedAt) - Date.parse(problem.since),
          )}
        </SheetDescription>
      </SheetHeader>

      <div className="flex-1 space-y-6 px-6 py-5">
        <div className="border-foreground space-y-2 border-l-2 pl-3 text-sm">
          <div className="text-eyebrow">Why this is listed</div>
          <p>
            {peopleCount === 1
              ? `${personLabel(problem.people[0]!)} failed`
              : `${peopleCount} people failed`}
            {problemPlace(problem)} since {sinceLabel}.
            {problem.lastConnectedAt
              ? problem.scope.kind === "person"
                ? ` ${problem.scope.name} has not connected since ${formatSince(problem.lastConnectedAt, now)}.`
                : ` No one has connected to it since ${formatSince(problem.lastConnectedAt, now)}.`
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
            <FailureBars buckets={problem.failedPer10Minutes} now={now} />
          </div>
        </Block>

        <Block title={`People · ${peopleCount}`} aside="Attempts">
          <div className="divide-y border">
            {problem.people.map((p) => (
              <PersonRow
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
              >
                <div className="flex min-w-0 items-center gap-2">
                  <IdentityAvatar label={personLabel(p)} />
                  <div className="min-w-0">
                    <div className="truncate text-sm">{personLabel(p)}</div>
                    <div className="text-muted-foreground truncate text-xs">
                      {appLabel(p.app)}
                      {p.server
                        ? ` · ${serverLabel({ server: p.server, member: p.member })}`
                        : ""}
                    </div>
                  </div>
                </div>
                <span className="text-destructive text-xs">
                  {p.attempts > 1 ? "started" : "at"}{" "}
                  {formatClockMinutes(p.firstFailedAt)}
                </span>
                <span className="text-right text-sm">{p.attempts}</span>
              </PersonRow>
            ))}
          </div>
        </Block>

        <Block title={`Servers · ${problem.servers.length}`} aside="Attempts">
          <div className="divide-y border">
            {problem.servers.map((s) => (
              <div
                key={`${s.server.id}-${s.member?.id ?? ""}`}
                className="flex items-center justify-between px-4 py-2.5 text-sm"
              >
                <span className="underline underline-offset-2">
                  {serverLabel({ server: s.server, member: s.member })}
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
                  {m.technicalMessage ? (
                    <div className="text-muted-foreground text-sm">
                      {m.technicalMessage}
                    </div>
                  ) : null}
                  <div className="text-muted-foreground font-mono text-xs">
                    {m.reason}
                  </div>
                </div>
                <span className="text-sm">{m.attempts}</span>
              </div>
            ))}
          </div>
        </Block>
        {problem.otherFailuresOnService > 0 ? (
          <p className="text-muted-foreground text-sm">
            {problem.otherFailuresOnService === 1
              ? "1 other failure"
              : `${formatCount(problem.otherFailuresOnService)} other failures`}{" "}
            on {problem.filters.services?.[0] ?? "the same service"} in this
            time range {problem.otherFailuresOnService === 1 ? "is" : "are"} not
            part of this.
          </p>
        ) : null}
      </div>

      <SheetFooter className="bg-card sticky bottom-0 flex-row justify-between border-t px-6 py-4">
        <AskAiButton
          investigation={investigationFor(problem)}
          onLaunched={onClose}
        />
        {SUSPECTED_PROBLEMS_ARE_SAMPLE ? (
          <span className="text-muted-foreground self-center text-sm">
            Sample data has no rows in Connection Logs.
          </span>
        ) : (
          <Button asChild>
            <Link to={logsHref}>Open in Connection Logs</Link>
          </Button>
        )}
      </SheetFooter>
    </div>
  );
}

const PERSON_ROW =
  "grid grid-cols-[1fr_auto_40px_16px] items-center gap-3 px-4 py-2.5";

/** Links to the person's rows; sample people have none, so they do not link. */
function PersonRow({
  to,
  children,
}: {
  to: string;
  children: ReactNode;
}): JSX.Element {
  if (SUSPECTED_PROBLEMS_ARE_SAMPLE) {
    return (
      <div className={PERSON_ROW}>
        {children}
        <span />
      </div>
    );
  }
  return (
    <Link to={to} className={`hover:bg-muted/40 ${PERSON_ROW}`}>
      {children}
      <ArrowRight className="text-muted-foreground size-4" aria-hidden />
    </Link>
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
  now,
}: {
  buckets: { start: string; attempts: number }[];
  now: Date;
}): JSX.Element {
  const chart = useMemo(
    () => ({
      labels: buckets.map((b) => {
        const d = new Date(b.start);
        return d.getMinutes() === 0 ? BUCKET_LABEL.format(d) : "";
      }),
      timestamps: buckets.map((b) => Date.parse(b.start)),
      tooltipLabels: buckets.map((b) => BUCKET_LABEL.format(new Date(b.start))),
      series: [
        {
          label: "Failed",
          data: buckets.map((b) => b.attempts),
          color: OUTCOME.blocked,
        },
      ],
    }),
    [buckets],
  );
  return (
    <HealthBarChart
      labels={chart.labels}
      timestamps={chart.timestamps}
      bucketMs={10 * 60_000}
      tooltipLabels={chart.tooltipLabels}
      series={chart.series}
      now={now}
      height={120}
    />
  );
}
