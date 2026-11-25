import { useQueryState } from "nuqs";

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
