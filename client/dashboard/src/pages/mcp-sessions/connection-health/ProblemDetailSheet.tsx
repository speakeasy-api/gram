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
import { AskAiButton, type Investigation } from "./AskAiButton";
import { logsSearch } from "./filters";
import {
  appLabel,
  formatClockMinutes,
  formatCount,
  formatDuration,
  formatSince,
  personLabel,
  problemRule,
  serverLabel,
  SOURCE_LABELS,
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
                  description="They name other people, so they need permission to read every log in this project."
                />
              ) : (
                <InlineEmptyState
                  icon="circle-check"
                  heading="This problem is no longer listed."
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
  const apps = new Set(problem.people.flatMap((p) => p.apps.map(appLabel)));
  return apps.size === 1 ? [...apps][0] : undefined;
}

function investigationFor(problem: SuspectedProblemDetail): Investigation {
  const target = problemTarget(problem);
  return {
    question: `Why can't ${
      problem.peopleCount === 1 && problem.people[0]
        ? personLabel(problem.people[0])
        : `these ${problem.peopleCount} people`
    } ${
      problem.part === "speakeasy"
        ? `sign in to MCP${target ? ` on ${target}` : ""}`
        : `connect${target ? ` to ${target}` : ""}`
    }, and is there anything I should change?`,
    context: [
      `Suspected problem · ${problem.headline}`,
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
  const logsHref = `${routes.mcpSessions.provider.connectionLogs.href(providerId)}${logsSearch(problem.filters, now)}`;
  const peopleCount = problem.peopleCount;
  const sinceLabel = formatSince(problem.since, now);
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
                ? ` ${problem.scope.name} has not ${succeeded} since ${formatSince(problem.lastConnectedAt, now)}.`
                : ` None of them has ${succeeded} since ${formatSince(problem.lastConnectedAt, now)}.`
              : ""}
          </p>
          <p className="text-muted-foreground">Rule: {problemRule(problem)}</p>
          {problem.source ? (
            <p className="text-muted-foreground">
              The failures came from{" "}
              {SOURCE_LABELS[problem.source].toLowerCase()}
              {sourceName(problem) ? ` (${sourceName(problem)})` : ""}.
            </p>
          ) : null}
        </div>

        <Block
          title={`${problem.countsWidened ? "Failed attempts with these messages on these servers" : "Failed attempts"} · ${formatCount(failed)}`}
          aside="10-minute intervals, last 24 hours"
        >
          <div className="border p-3">
            <FailureBars buckets={problem.failedPer10Minutes} now={now} />
          </div>
          {problem.countsWidened ? (
            <p className="text-muted-foreground text-sm">
              This problem has too many people or servers to filter by. These
              counts include everyone who failed with these messages on these
              servers.
            </p>
          ) : null}
        </Block>

        <Block title={`People · ${peopleCount}`} aside="Attempts">
          <div className="divide-y border">
            {problem.people.map((p) => (
              <PersonRow
                key={p.person.subjectUrn}
                to={`${routes.mcpSessions.provider.connectionLogs.href(providerId)}${logsSearch(
                  { ...problem.filters, people: [p.person.subjectUrn] },
                  now,
                )}`}
              >
                <div className="flex min-w-0 items-center gap-2">
                  <IdentityAvatar label={personLabel(p)} />
                  <div className="min-w-0">
                    <div className="truncate text-sm">{personLabel(p)}</div>
                    <div className="text-muted-foreground truncate text-xs">
                      {p.apps.map(appLabel).join(", ")}
                      {p.servers.length > 0
                        ? ` · ${p.servers.map(serverLabel).join(", ")}`
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
          {problem.peopleTotal > problem.people.length ? (
            <p className="text-muted-foreground text-sm">
              Showing the first {problem.people.length} of{" "}
              {formatCount(problem.peopleTotal)}. Open Connection Logs to see
              the rest.
            </p>
          ) : null}
        </Block>

        <Block title={`Servers · ${problem.servers.length}`} aside="Attempts">
          <div className="divide-y border">
            {problem.servers.map((s) => (
              <div
                key={`${s.server.id}-${s.member?.id ?? ""}`}
                className="flex items-center justify-between px-4 py-2.5 text-sm"
              >
                <span className="underline underline-offset-2">
                  {serverLabel(s)}
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
          <Link to={logsHref}>
            {problem.countsWidened
              ? "Open matching attempts in Connection Logs"
              : "Open in Connection Logs"}
          </Link>
        </Button>
      </SheetFooter>
    </div>
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
  const chart = useMemo(
    () => ({
      labels: buckets.map((b) =>
        new Date(b.start).getMinutes() === 0 ? formatClockMinutes(b.start) : "",
      ),
      timestamps: buckets.map((b) => Date.parse(b.start)),
      tooltipLabels: buckets.map((b) => formatClockMinutes(b.start)),
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
