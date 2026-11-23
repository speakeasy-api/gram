import { ArrowRight, ChevronDown } from "lucide-react";
import { useState } from "react";
import { problemHeadline } from "./format";
import type { SuspectedProblem } from "./types";

const VISIBLE = 3;

const SUSPECTED_PROBLEMS_RULE =
  "Failed the same way and not connected since: 2 or more people on one MCP server, one service or one app version, or one person for 24 hours.";

/** The rule where problems signing in to Speakeasy sit beside problems connecting to a service. */
const MIXED_PROBLEMS_RULE =
  "Failed the same way with no success since: 2 or more people on one MCP server, one service or one app version, or one person for 24 hours.";

/** The rule as the provider Overview states it, which lists only problems signing in to Speakeasy. */
export const SIGN_IN_PROBLEMS_RULE =
  "Failed to sign in the same way and not signed in since: 2 or more people on one MCP server or one app version, or one person for 24 hours.";

/** The one panel that says we suspect something; `empty`, or nothing, when there is none. */
export function SuspectedProblems({
  problems,
  now,
  onOpen,
  rule,
  empty,
}: {
  problems: SuspectedProblem[];
  now: Date;
  onOpen: (problem: SuspectedProblem) => void;
  /** The hint under the heading; chosen from the problems listed when absent. */
  rule?: string;
  /** Said when there are no problems. */
  empty?: string;
}): JSX.Element | null {
  const [expanded, setExpanded] = useState(false);
  if (problems.length === 0)
    return empty ? (
      <p className="text-muted-foreground text-sm">{empty}</p>
    ) : null;
  const hint =
    rule ??
    (problems.some((p) => p.part === "speakeasy")
      ? MIXED_PROBLEMS_RULE
      : SUSPECTED_PROBLEMS_RULE);
  const shown = expanded ? problems : problems.slice(0, VISIBLE);
  const hidden = problems.length - shown.length;
  return (
    <section className="space-y-3" aria-labelledby="suspected-problems">
      <div>
        <h2 id="suspected-problems" className="text-eyebrow">
          Suspected problems · {problems.length}
        </h2>
        <p className="text-muted-foreground mt-1 text-sm">{hint}</p>
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
  );
}
