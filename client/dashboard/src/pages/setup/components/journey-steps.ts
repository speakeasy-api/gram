import { createContext, useContext, useEffect } from "react";

export interface JourneyStep {
  index: number;
  title: string;
  complete: boolean;
  /** Short marker after the title in the rail, e.g. "Recommended". */
  badge?: string;
}

export interface JourneyStepsRegistry {
  register: (id: string, step: JourneyStep) => void;
  unregister: (id: string) => void;
}

export interface JourneyView {
  steps: JourneyStep[];
  /** Index (1-based, matching StepSection) of the step being shown. */
  activeIndex: number | null;
  setActiveIndex: (index: number) => void;
}

export const RegistryContext = createContext<JourneyStepsRegistry | null>(null);
export const ViewContext = createContext<JourneyView>({
  steps: [],
  activeIndex: null,
  setActiveIndex: () => {},
});

/** Called by a section to appear in the rail. A no-op outside a provider. */
export function useRegisterJourneyStep(id: string, step: JourneyStep): void {
  const registry = useContext(RegistryContext);
  const { index, title, complete, badge } = step;

  useEffect(() => {
    if (!registry) return;
    registry.register(id, { index, title, complete, badge });
  }, [registry, id, index, title, complete, badge]);

  useEffect(() => {
    if (!registry) return;
    return () => registry.unregister(id);
  }, [registry, id]);
}

/**
 * Whether a section should be on screen. Outside a provider — a section
 * rendered on its own, with no rail to walk it — every section stacks.
 */
export function useIsActiveJourneyStep(index: number): boolean {
  const registry = useContext(RegistryContext);
  const { activeIndex } = useContext(ViewContext);
  if (!registry) return true;
  return activeIndex === index;
}

/** The registered steps in order plus which one is showing. */
export function useJourneyView(): JourneyView {
  return useContext(ViewContext);
}
