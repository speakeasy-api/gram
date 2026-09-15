import { createContext, useContext, useEffect, useLayoutEffect } from "react";

export interface JourneyStep {
  index: number;
  /**
   * Stable URL name for this step, e.g. "connect-cowork". Deliberately not
   * derived from the index or the title: sections get reordered and retitled,
   * and either would silently break links people had already shared.
   */
  slug: string;
  title: string;
  complete: boolean;
  /** Short marker after the title in the rail, e.g. "Recommended". */
  badge?: string;
  /**
   * A step that is on the card's map but cannot be entered yet, because an
   * earlier step has to land first. It still registers, so the rail counts
   * the whole journey rather than only the part that is reachable today,
   * but nothing navigates to it: not the rail, not the footer, not a link.
   */
  locked?: boolean;
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
  const { index, slug, title, complete, badge, locked } = step;

  // A layout effect so the provider knows every section before the first
  // paint. With a passive effect the initial mount painted once with no steps
  // registered: every section hidden and the footer reading "Mark done".
  useLayoutEffect(() => {
    if (!registry) return;
    registry.register(id, { index, slug, title, complete, badge, locked });
  }, [registry, id, index, slug, title, complete, badge, locked]);

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
