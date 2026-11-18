import { ArrowRight, ChevronDown } from "lucide-react";
import { useState } from "react";
import { problemHeadline } from "./format";
import type { SuspectedProblem } from "./types";

const VISIBLE = 3;

const SUSPECTED_PROBLEMS_RULE =
  "Failed the same way and not connected since: 3 or more people on one server, service or app version, or one person for 24 hours.";

/** The one panel that says we suspect something; nothing when there is none. */
export function SuspectedProblems({
  problems,
  now,
  onOpen,
}: {
  problems: SuspectedProblem[];
  now: Date;
  onOpen: (problem: SuspectedProblem) => void;
}): JSX.Element | null {
  const [expanded, setExpanded] = useState(false);
  if (problems.length === 0) return null;
  const shown = expanded ? problems : problems.slice(0, VISIBLE);
  const hidden = problems.length - shown.length;
  return (
    <section className="space-y-3" aria-labelledby="suspected-problems">
      <div>
        <h2 id="suspected-problems" className="text-eyebrow">
          Suspected problems · {problems.length}
        </h2>
        <p className="text-muted-foreground mt-1 text-sm">
          {SUSPECTED_PROBLEMS_RULE}
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
                  {problem.peopleOnServer !== undefined &&
                  problem.peopleOnServer < problem.people
                    ? `${problem.peopleOnServer} on this server · ${problem.people} in all. `
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
