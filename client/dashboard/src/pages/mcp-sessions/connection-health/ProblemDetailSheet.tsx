import { OUTCOME } from "@/components/chart/palette";
import { HealthBarChart } from "./HealthBarChart";
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
import { isUnavailable, useSuspectedProblem } from "./api";
import { AskAiButton } from "./AskAiButton";
import type { Investigation } from "./investigation";
import { logsSearch } from "./filters";
import {
  appLabel,
  axisLabels,
  formatAt,
  formatCount,
  formatDuration,
  formatSince,
  personLabel,
  problemRule,
  serverLabel,
  SOURCE_LABELS,
  sourceSentence,
} from "./format";
import type { SuspectedProblemDetail } from "./types";

/** One suspected problem, opened from the Overview's panel or a server's tab. */
export function ProblemDetailSheet({
  id,
  providerId,
  serverId,
  now,
  onClose,
}: {
  id: string | null;
  providerId: string;
  /** The server tab it was opened from. */
  serverId?: string;
  now: Date;
  onClose: () => void;
}): JSX.Element {
  const query = useSuspectedProblem({ providerId, serverId }, id);
  const problem = query.data?.problem;
  const data = query.data;
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
                  description={
                    isUnavailable(query.error)
                      ? "It took too long to load. Try again in a minute."
                      : "Close it and open it again."
                  }
                />
              ) : query.isPending ? (
                <div className="space-y-3">
                  <Skeleton className="h-6 w-2/3" />
                  <Skeleton className="h-32 w-full" />
                </div>
              ) : data && !data.loggingEnabled ? (
                <InlineEmptyState
                  icon="file-x"
                  heading="Logging is off"
                  description="Connection attempt logs are not kept while logging is off for this organization."
                />
              ) : data?.viewerScope === "own" ? (
                <InlineEmptyState
                  icon="lock"
                  heading="You can't see suspected problems"
                  description="Suspected problems name other people. You need permission to read every log in this project to see them."
                />
              ) : (
                <InlineEmptyState
                  icon="circle-check"
                  heading="This problem is no longer listed"
                />
              )}
            </div>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}

/** What the people could not reach. */
function problemTarget(problem: SuspectedProblemDetail): string | undefined {
  if (problem.scope.kind === "service" || problem.scope.kind === "server")
    return problem.scope.name;
  const server = problem.servers[0];
  return server ? serverLabel(server) : undefined;
}

/** "on mcp.linear.app", "on Internal Wiki"; nothing for one person. */
function problemPlace(problem: SuspectedProblemDetail): string {
  switch (problem.scope.kind) {
    case "service":
    case "server":
    case "app_version":
      return ` on ${problem.scope.name}`;
    case "person":
      return "";
  }
}

/** The service or app the failures came from, when the problem names one. */
function sourceName(problem: SuspectedProblemDetail): string | undefined {
  if (problem.source === "service")
    return problem.scope.kind === "service" ? problem.scope.name : undefined;
  if (problem.source !== "app") return undefined;
  // Name the app only when every person in the problem is in the rows and shows it.
  if (
    problem.peopleTotal > problem.people.length ||
    problem.people.some(
      (p) => p.apps.length === 0 || p.appsTotal > p.apps.length,
    )
  )
    return undefined;
  // Two unlisted apps share a label, so they are told apart by family, version and key.
  const apps = new Map(
    problem.people
      .flatMap((p) => p.apps)
      .map((a) => [
        `${a.family}\u0000${a.version ?? ""}\u0000${a.appKey ?? ""}`,
        a,
      ]),
  );
  const [only] = apps.values();
  return apps.size === 1 && only ? appLabel(only) : undefined;
}

/** A fixed question; every name and label goes in the escaped context, never in the question. */
function investigationFor(problem: SuspectedProblemDetail): Investigation {
  const target = problemTarget(problem);
  return {
    question:
      "Why are the people in this suspected problem failing to connect, and is there anything I should change?",
    context: [
      `Suspected problem · ${problem.headline}`,
      `People: ${problem.people.map((p) => personLabel(p)).join("; ")}`,
      target ? `Where: ${target}` : null,
      `Part: ${problem.part === "speakeasy" ? "signing in to Speakeasy" : "connecting to the service"}`,
      `Since ${problem.since}`,
      problem.fact,
      `Rule: ${problemRule(problem)}`,
      problem.source
        ? `Failures came from: ${SOURCE_LABELS[problem.source]}${sourceName(problem) ? ` (${sourceName(problem)})` : ""}`
        : null,
      `Messages: ${problem.messages.map((m) => `${m.adminMessage ?? m.message} (${m.reason}, ${m.attempts})`).join("; ")}`,
      `Servers: ${problem.servers.map((s) => `${serverLabel(s)} (${s.attempts})`).join("; ")}`,
      problem.lastConnectedAt
        ? `${problem.part === "speakeasy" ? "Last successful sign-in" : "Last successful connection"}: ${problem.lastConnectedAt}`
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
  // One snapshot: the chart, the messages and both links end when the problem was read.
  const asOf = new Date(problem.asOf);
  // Counting starts an hour before the first failure, so attempts already under way are included.
  const countedFrom = formatSince(problem.filters.from, now);
  // The chart shows at most the last 24 hours; the messages and links cover the whole problem.
  const chartIsCut =
    asOf.getTime() - Date.parse(problem.filters.from) > 24 * 60 * 60 * 1000;
  const logsHref = `${routes.mcpSessions.provider.connectionLogs.href(providerId)}${logsSearch(problem.filters, asOf)}`;
  const peopleCount = problem.peopleCount;
  // Dates show against the day the sheet renders, not the day the page opened.
  const today = new Date();
  const sinceLabel = formatSince(problem.since, today);
  const succeeded = problem.part === "speakeasy" ? "signed in" : "connected";
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
          Since {sinceLabel} · latest failure{" "}
          {formatAt(problem.lastFailedAt, today)} ·{" "}
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
              ? `${personLabel(problem.people[0]!)} has been failing`
              : `${peopleCount} people have been failing`}
            {problemPlace(problem)} since {sinceLabel}.
            {problem.lastConnectedAt
              ? problem.scope.kind === "person"
                ? ` ${problem.scope.name} has not ${succeeded} since ${formatSince(problem.lastConnectedAt, now)}.`
                : ` None of them has ${succeeded} since ${formatSince(problem.lastConnectedAt, now)}.`
              : ""}
          </p>
          <p className="text-muted-foreground">Rule: {problemRule(problem)}</p>
          {problem.source ? (
            <p className="text-muted-foreground">
              {sourceSentence(problem.source, sourceName(problem))}
            </p>
          ) : null}
          <p className="text-muted-foreground">
            {chartIsCut
              ? "The messages and Connection Logs links"
              : "The chart, messages and Connection Logs links"}{" "}
            count every failed attempt by{" "}
            {peopleCount === 1 ? "this person" : "these people"} on these
            servers with these messages since {countedFrom}, an hour before the
            first failure, including failures they later recovered from.
            {chartIsCut ? " The chart shows the last 24 hours of them." : ""}
            {problem.countsWidened
              ? " Some filters could not be applied, so unrelated failures may be included."
              : ""}
          </p>
        </div>

        <Block
          title={`Failed attempts ${chartIsCut ? "in the last 24 hours" : `since ${countedFrom}`} · ${formatCount(failed)}`}
          aside="10-minute intervals"
        >
          <div className="border p-3">
            <FailureBars buckets={problem.failedPer10Minutes} now={asOf} />
          </div>
        </Block>

        <Block title={`People · ${peopleCount}`} aside="Attempts">
          <div className="divide-y border">
            {problem.people.map((p) => (
              <PersonRow
                key={p.person.subjectUrn}
                to={`${routes.mcpSessions.provider.connectionLogs.href(providerId)}${logsSearch(
                  { ...problem.filters, people: [p.person.subjectUrn] },
                  asOf,
                )}`}
              >
                <div className="flex min-w-0 items-center gap-2">
                  <IdentityAvatar label={personLabel(p)} />
                  <div className="min-w-0">
                    <div className="truncate text-sm">{personLabel(p)}</div>
                    <div className="text-muted-foreground flex min-w-0 gap-1 text-xs">
                      <Folded
                        labels={p.apps.map(appLabel)}
                        total={p.appsTotal}
                      />
                      {p.servers.length > 0 ? (
                        <>
                          <span aria-hidden>·</span>
                          <Folded
                            labels={p.servers.map(serverLabel)}
                            total={p.serversTotal}
                          />
                        </>
                      ) : null}
                    </div>
                  </div>
                </div>
                <span className="text-destructive text-xs">
                  {p.attempts > 1 ? "started" : "at"}{" "}
                  {formatAt(p.firstFailedAt, today)}
                </span>
                <span className="text-right text-sm">{p.attempts}</span>
              </PersonRow>
            ))}
          </div>
          {problem.peopleTotal > problem.people.length ? (
            <p className="text-muted-foreground text-sm">
              Showing the first {problem.people.length} of{" "}
              {formatCount(problem.peopleTotal)}. Open Connection Logs to see
              the rest.
            </p>
          ) : null}
        </Block>

        <Block title={`Servers · ${problem.serversTotal}`} aside="Attempts">
          <div className="divide-y border">
            {problem.servers.map((s) => (
              <div
                key={`${s.server.id}-${s.member?.id ?? ""}`}
                className="flex items-center justify-between px-4 py-2.5 text-sm"
              >
                <span>{serverLabel(s)}</span>
                <span>{s.attempts}</span>
              </div>
            ))}
          </div>
          {problem.serversTotal > problem.servers.length ? (
            <p className="text-muted-foreground text-sm">
              Showing the {problem.servers.length} with the most attempts.
            </p>
          ) : null}
        </Block>

        <Block
          title={`Messages · ${problem.messages.length}`}
          aside={`Attempts since ${countedFrom}`}
        >
          <div className="divide-y border">
            {problem.messages.map((m) => (
              <div
                key={m.reason}
                className="flex justify-between gap-4 px-4 py-3"
              >
                <div className="min-w-0">
                  <div className="text-sm">{m.adminMessage ?? m.message}</div>
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
              ? "1 other person"
              : `${formatCount(problem.otherFailuresOnService)} other people`}{" "}
            failing on {problem.scope.name}{" "}
            {problem.otherFailuresOnService === 1 ? "is" : "are"} not part of
            this.
          </p>
        ) : null}
      </div>

      <SheetFooter className="bg-card sticky bottom-0 flex-row justify-between border-t px-6 py-4">
        <AskAiButton
          investigation={investigationFor(problem)}
          onLaunched={onClose}
        />
        <Button asChild>
          <Link to={logsHref}>Open in Connection Logs</Link>
        </Button>
      </SheetFooter>
    </div>
  );
}

/** "Linear, GitHub +3": the list truncates, the count never does, and the title has them all. */
function Folded({
  labels,
  total,
}: {
  labels: string[];
  total: number;
}): JSX.Element {
  const more = total - labels.length;
  const list = labels.join(", ");
  return (
    <span
      className="flex min-w-0 gap-1"
      title={more > 0 ? `${list} and ${more} more` : list}
    >
      <span className="min-w-0 truncate">{list}</span>
      {more > 0 ? <span className="shrink-0">+{more}</span> : null}
    </span>
  );
}

const PERSON_ROW =
  "grid grid-cols-[1fr_auto_40px_16px] items-center gap-3 px-4 py-2.5";

/** Links to the person's rows. */
function PersonRow({
  to,
  children,
}: {
  to: string;
  children: ReactNode;
}): JSX.Element {
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

function FailureBars({
  buckets,
  now,
}: {
  buckets: { start: string; attempts: number }[];
  now: Date;
}): JSX.Element {
  const rendered = new Date();
  // Midnight today, so the tooltips re-date after midnight.
  const today = new Date(
    rendered.getFullYear(),
    rendered.getMonth(),
    rendered.getDate(),
  ).getTime();
  const chart = useMemo(
    () => ({
      labels: axisLabels(buckets.map((b) => b.start)),
      timestamps: buckets.map((b) => Date.parse(b.start)),
      tooltipLabels: buckets.map((b) => formatAt(b.start, new Date(today))),
      series: [
        {
          label: "Failed",
          data: buckets.map((b) => b.attempts),
          color: OUTCOME.blocked,
        },
      ],
    }),
    [buckets, today],
  );
  return (
    <HealthBarChart
      label="Failed attempts in 10-minute intervals"
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
