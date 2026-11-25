import { ArrowRight, ChevronDown } from "lucide-react";
import { InlineEmptyState } from "@/components/inline-empty-state";
import type { UseQueryResult } from "@tanstack/react-query";
import { useState, type ReactNode } from "react";
import { Link } from "react-router";
import { problemHeadline } from "./format";
import { isUnavailable } from "./api";
import type { ListSuspectedProblemsResponse, SuspectedProblem } from "./types";

const VISIBLE = 3;

const SUSPECTED_PROBLEMS_RULE =
  "Failed the same way and not connected since: 2 or more people on one MCP server, one service or one app version, or one person for 24 hours.";

/** The rule where problems signing in to Speakeasy sit beside problems connecting to a service. */
const MIXED_PROBLEMS_RULE =
  "Failed the same way with no success since: 2 or more people on one MCP server, one service or one app version, or one person for 24 hours.";

/** The rule as the provider Overview states it, which lists only problems signing in to Speakeasy. */
export const SIGN_IN_PROBLEMS_RULE =
  "Failed to sign in the same way on this provider's servers and not signed in since: 2 or more people on one MCP server or one app version, or one person for 24 hours.";

/** Where problems with a service are listed; shown only under a panel that lists problems. */
export function ServiceProblemsNote({
  serversHref,
}: {
  serversHref: string;
}): JSX.Element {
  return (
    <p className="text-muted-foreground text-sm">
      Problems with a service show on the page of each MCP server that uses it,
      under{" "}
      <Link
        to={serversHref}
        className="text-foreground underline-offset-4 hover:underline"
      >
        MCP servers
      </Link>
      .
    </p>
  );
}

/** The one panel that says we suspect something; nothing at all while loading or when there is nothing to list. */
export function SuspectedProblems({
  query,
  now,
  onOpen,
  rule,
  note,
}: {
  query: Pick<
    UseQueryResult<ListSuspectedProblemsResponse>,
    "data" | "isPending" | "isError" | "error"
  >;
  now: Date;
  onOpen: (problem: SuspectedProblem) => void;
  /** The hint under the heading; chosen from the problems listed when absent. */
  rule?: string;
  /** Shown under the panel, only when it lists problems. */
  note?: ReactNode;
}): JSX.Element | null {
  const [expanded, setExpanded] = useState(false);
  // Cached results show at once; with none, nothing is reserved, so an empty result moves nothing.
  if (query.isPending) return null;
  if (query.isError)
    return (
      <InlineEmptyState
        orientation="horizontal"
        className="min-h-28"
        icon="triangle-alert"
        heading="Couldn't load suspected problems"
        description={
          isUnavailable(query.error)
            ? "It took too long to load. Try again in a minute."
            : "Refresh the page to try again."
        }
      />
    );
  const data = query.data;
  // Problems name other people: nothing shows with logging off or to someone who reads only their own logs.
  if (!data?.loggingEnabled || data.viewerScope !== "all") return null;
  const problems = data.problems;
  if (problems.length === 0) return null;
  const hint =
    rule ??
    (problems.some((p) => p.part === "speakeasy")
      ? MIXED_PROBLEMS_RULE
      : SUSPECTED_PROBLEMS_RULE);
  const shown = expanded ? problems : problems.slice(0, VISIBLE);
  const hidden = problems.length - shown.length;
  return (
    <div className="space-y-3">
      <section className="space-y-3" aria-labelledby="suspected-problems">
        <div>
          <h2 id="suspected-problems" className="text-eyebrow">
            Suspected problems · {data.problemsTotal}
          </h2>
          <p className="text-muted-foreground mt-1 text-sm">
            {hint}
            {data.problemsTotal > problems.length
              ? ` Showing the first ${problems.length}.`
              : ""}
          </p>
        </div>
        <ul className="bg-card border-l-destructive divide-y border border-l-2">
          {shown.map((problem) => (
            <li key={problem.id}>
              <button
                type="button"
                onClick={() => onOpen(problem)}
                className="hover:bg-muted/40 flex w-full items-center gap-3 px-5 py-4 text-left"
              >
                <span
                  className="bg-destructive size-2 shrink-0 rounded-full"
                  aria-hidden
                />
                <span className="min-w-0 flex-1">
                  <span className="block text-[15px]">
                    {problemHeadline(problem, now)}
                  </span>
                  <span className="text-muted-foreground block text-xs">
                    {problem.peopleOnServerCount !== undefined &&
                    problem.peopleOnServerCount < problem.peopleCount
                      ? `${problem.peopleOnServerCount} on this server · ${problem.peopleCount} ${problem.scope.kind === "service" ? `on all servers that use ${problem.scope.name}` : "in all"}. `
                      : ""}
                    {problem.fact}
                  </span>
                </span>
                <ArrowRight
                  className="text-muted-foreground size-4 shrink-0"
                  aria-hidden
                />
              </button>
            </li>
          ))}
          {problems.length > VISIBLE ? (
            <li>
              <button
                type="button"
                onClick={() => setExpanded((v) => !v)}
                aria-expanded={expanded}
                className="flex items-center gap-2 px-5 py-3 text-sm"
              >
                <ChevronDown
                  className={`size-4 transition-transform ${expanded ? "rotate-180" : ""}`}
                  aria-hidden
                />
                {expanded ? "Show fewer" : `Show ${hidden} more`}
              </button>
            </li>
          ) : null}
        </ul>
      </section>
      {note}
    </div>
  );
}
