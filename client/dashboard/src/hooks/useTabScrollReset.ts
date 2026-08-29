import { useEffect, useRef } from "react";
import { useLocation } from "react-router";

/**
 * Detail pages render all their tab routes inside one persistent Page.Body, so
 * switching tabs would otherwise land mid-page at the previous tab's scroll
 * offset. Attach the returned ref to the tab content wrapper; the nearest
 * page scroll container resets to the top whenever the tab changes.
 */
export function useTabScrollReset(
  activeTab: string | undefined,
): React.RefObject<HTMLDivElement | null> {
  const ref = useRef<HTMLDivElement>(null);
  const { hash } = useLocation();
  useEffect(() => {
    // In-page anchors (e.g. settings section hashes) win over the reset.
    if (hash) return;
    ref.current?.closest("[data-page-scroll]")?.scrollTo({ top: 0 });
  }, [activeTab, hash]);
  return ref;
}
