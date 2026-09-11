import { useCallback, useMemo, useState, type ReactNode } from "react";
import { useSearchParams } from "react-router";
import {
  RegistryContext,
  ViewContext,
  type JourneyStep,
} from "./journey-steps";

/** Query parameter naming the sub-step on screen, e.g. ?step=connect-cowork. */
const STEP_PARAM = "step";

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
  const [pickedIndex, setPickedIndex] = useState<number | null>(null);
  const [searchParams, setSearchParams] = useSearchParams();
  const linkedSlug = searchParams.get(STEP_PARAM);

  const register = useCallback((id: string, step: JourneyStep) => {
    setSteps((prev) => {
      const existing = prev[id];
      if (
        existing &&
        existing.index === step.index &&
        existing.slug === step.slug &&
        existing.title === step.title &&
        existing.complete === step.complete &&
        existing.badge === step.badge
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
  // A ?step= that names one of this card's steps wins: it is how someone
  // arrived. Otherwise open on the first step that still needs doing; every
  // step done lands on the last so Mark done is in reach. A slug the card
  // does not have (a stale link, or one for a different card) falls through
  // to that same default rather than showing nothing.
  const resolvedActive = useMemo(() => {
    if (ordered.length === 0) return null;
    const linked = linkedSlug
      ? ordered.find((step) => step.slug === linkedSlug)
      : undefined;
    if (linked) return linked.index;
    if (pickedIndex !== null && ordered.some((s) => s.index === pickedIndex)) {
      return pickedIndex;
    }
    const firstOpen = ordered.find((s) => !s.complete);
    return (firstOpen ?? ordered[ordered.length - 1]!).index;
  }, [ordered, pickedIndex, linkedSlug]);

  // Walking the rail rewrites ?step= so the address bar is always a link to
  // what is on screen. It replaces rather than pushes: Back belongs to the
  // board the reader came from, not to each step they passed through.
  const setActiveIndex = useCallback(
    (index: number) => {
      setPickedIndex(index);
      const step = ordered.find((s) => s.index === index);
      if (!step) return;
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.set(STEP_PARAM, step.slug);
          return next;
        },
        { replace: true },
      );
    },
    [ordered, setSearchParams],
  );

  const view = useMemo(
    () => ({
      steps: ordered,
      activeIndex: resolvedActive,
      setActiveIndex,
    }),
    [ordered, resolvedActive, setActiveIndex],
  );

  return (
    <RegistryContext.Provider value={registry}>
      <ViewContext.Provider value={view}>{children}</ViewContext.Provider>
    </RegistryContext.Provider>
  );
}
