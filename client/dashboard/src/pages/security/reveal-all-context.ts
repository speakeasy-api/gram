import { createContext, useContext } from "react";

// Page-level "reveal all / hide all" state for MaskedMatch grids. Pages mount
// <RevealAllProvider /> (in risk-ui.tsx) and call <RevealAllToggle />; each
// MaskedMatch (and ChatDetailPanel's MaskedMatchInline) subscribes via
// useRevealAll() and re-syncs whenever generation bumps.
export type RevealAllContextValue = {
  revealAll: boolean;
  setRevealAll: (next: boolean) => void;
  // Bumps whenever revealAll is toggled. MaskedMatch listens to this so a
  // global toggle resets any per-row state, even when the new value matches
  // the row's current local state.
  generation: number;
};

export const RevealAllContext = createContext<RevealAllContextValue | null>(
  null,
);

// Returns null when no <RevealAllProvider /> is in the tree so consumers can
// fall back to local-only state. The returned object is the stable memoized
// value from the Provider, so it's safe to destructure individual primitives
// (revealAll, generation, setRevealAll) into useEffect deps.
export function useRevealAll(): RevealAllContextValue | null {
  return useContext(RevealAllContext);
}
