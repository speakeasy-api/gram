import { useCallback, useMemo, useState, type ReactNode } from "react";
import {
  RegistryContext,
  ViewContext,
  type JourneyStep,
} from "./journey-steps";

// The steps a task page's rail lists come from the sections the task renders:
// each StepSection registers itself here and reports when its outcome lands,
// so the rail is always exactly what is on the page with no second list to
// keep in sync. The provider also owns which step is on screen; sections
// outside the active one stay mounted (their polling and sheets survive)
// but hidden.
export function JourneyStepsProvider({
  children,
}: {
  children: ReactNode;
}): JSX.Element {
  const [steps, setSteps] = useState<Record<string, JourneyStep>>({});
  const [activeIndex, setActiveIndex] = useState<number | null>(null);

  const register = useCallback((id: string, step: JourneyStep) => {
    setSteps((prev) => {
      const existing = prev[id];
      if (
        existing &&
        existing.index === step.index &&
        existing.title === step.title &&
        existing.complete === step.complete
      ) {
        return prev;
      }
      return { ...prev, [id]: step };
    });
  }, []);

  const unregister = useCallback((id: string) => {
    setSteps((prev) => {
      if (!(id in prev)) return prev;
      const next = { ...prev };
      delete next[id];
      return next;
    });
  }, []);

  const registry = useMemo(
    () => ({ register, unregister }),
    [register, unregister],
  );
  const ordered = useMemo(
    () => Object.values(steps).sort((a, b) => a.index - b.index),
    [steps],
  );
  // Open on the first step that still needs doing; every step done lands on
  // the last so Mark done is in reach.
  const resolvedActive = useMemo(() => {
    if (ordered.length === 0) return null;
    if (activeIndex !== null && ordered.some((s) => s.index === activeIndex)) {
      return activeIndex;
    }
    const firstOpen = ordered.find((s) => !s.complete);
    return (firstOpen ?? ordered[ordered.length - 1]!).index;
  }, [ordered, activeIndex]);

  const view = useMemo(
    () => ({
      onTaskPage: true,
      steps: ordered,
      activeIndex: resolvedActive,
      setActiveIndex,
    }),
    [ordered, resolvedActive],
  );

  return (
    <RegistryContext.Provider value={registry}>
      <ViewContext.Provider value={view}>{children}</ViewContext.Provider>
    </RegistryContext.Provider>
  );
}
