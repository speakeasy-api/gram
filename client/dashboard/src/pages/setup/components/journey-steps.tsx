import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

export interface JourneyStep {
  index: number;
  title: string;
  complete: boolean;
}

interface JourneyStepsRegistry {
  register: (id: string, step: JourneyStep) => void;
  unregister: (id: string) => void;
}

const RegistryContext = createContext<JourneyStepsRegistry | null>(null);
const StepsContext = createContext<JourneyStep[]>([]);

// The steps a task page's rail lists come from the sections the task renders:
// each StepSection registers itself here and reports when its outcome lands,
// so the rail is always exactly what is on the page with no second list to
// keep in sync.
export function JourneyStepsProvider({
  children,
}: {
  children: ReactNode;
}): JSX.Element {
  const [steps, setSteps] = useState<Record<string, JourneyStep>>({});

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

  return (
    <RegistryContext.Provider value={registry}>
      <StepsContext.Provider value={ordered}>{children}</StepsContext.Provider>
    </RegistryContext.Provider>
  );
}

/** Called by a section to appear in the rail. A no-op outside a provider. */
export function useRegisterJourneyStep(id: string, step: JourneyStep): void {
  const registry = useContext(RegistryContext);
  const { index, title, complete } = step;

  useEffect(() => {
    if (!registry) return;
    registry.register(id, { index, title, complete });
  }, [registry, id, index, title, complete]);

  useEffect(() => {
    if (!registry) return;
    return () => registry.unregister(id);
  }, [registry, id]);
}

/** The registered steps, ordered by index. */
export function useJourneySteps(): JourneyStep[] {
  return useContext(StepsContext);
}
