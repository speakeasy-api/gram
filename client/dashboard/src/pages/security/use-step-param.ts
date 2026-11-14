import { useQueryState } from "nuqs";
import { useState } from "react";

export interface PolicyStep {
  id: string;
  title: string;
  description: string;
  badge?: string;
}

// Back the active step with a `?step=<id>` URL param so browser back/forward
// (and refresh, and shareable links) traverse the steps. history: "push" makes
// each step change its own history entry.
export function useStepParam(
  steps: PolicyStep[],
): [number, (index: number) => void] {
  const [raw, setRaw] = useQueryState("step", { history: "push" });
  const found = steps.findIndex((s) => s.id === raw);
  const index = found >= 0 ? found : 0;
  const setIndex = (i: number) => {
    void setRaw(steps[i]?.id ?? null);
  };
  return [index, setIndex];
}

/** Indexes of the steps the author has opened so far, starting with the one
 *  the editor mounted on. A new policy is created only once every step has
 *  been seen, so the defaults on later steps (action, audience) never apply
 *  without the author looking at them. */
export function useVisitedSteps(current: number): ReadonlySet<number> {
  const [visited, setVisited] = useState<ReadonlySet<number>>(
    () => new Set([current]),
  );
  if (visited.has(current)) return visited;

  const next = new Set(visited);
  next.add(current);
  setVisited(next);
  return next;
}

/** Why Create is not available yet, or undefined once every step has been
 *  opened. */
export function unvisitedStepsReason(
  visited: ReadonlySet<number>,
  steps: readonly PolicyStep[],
): string | undefined {
  const missing = steps
    .filter((_, i) => !visited.has(i))
    .map((step) => step.title);
  if (missing.length === 0) return undefined;
  if (missing.length === 1) return `Check ${missing[0]} to create`;
  const last = missing[missing.length - 1];
  return `Check ${missing.slice(0, -1).join(", ")} and ${last} to create`;
}
