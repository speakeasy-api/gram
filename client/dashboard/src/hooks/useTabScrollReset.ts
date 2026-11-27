import { useLayoutEffect, useRef } from "react";
import { useLocation } from "react-router";

/**
 * Detail pages render all their tab routes inside one persistent Page.Body, so
 * switching tabs would otherwise land mid-page at the previous tab's scroll
 * offset. Attach the returned ref to the tab content wrapper; the nearest
 * scroll container (a full-height Page.Body, else the document) resets to the
 * top whenever the tab changes.
 */
export function useTabScrollReset(
  activeTab: string | undefined,
): React.RefObject<HTMLDivElement | null> {
  const ref = useRef<HTMLDivElement>(null);
  const { hash } = useLocation();
  // Layout effect: resetting after paint would flash the old offset for a
  // frame when the incoming tab is tall enough to keep it.
  useLayoutEffect(() => {
    // In-page anchors (e.g. settings section hashes) win over the reset.
    if (hash) return;
    const scroller = ref.current?.closest<HTMLElement>("[data-page-bound]");
    if (!scroller || !ref.current) {
      window.scrollTo({ top: 0 });
      return;
    }
    // A header that scrolls away above the content stays away: the reset
    // goes to the content's top, under any sticky strip, not to the header.
    const padding =
      parseFloat(getComputedStyle(scroller).scrollPaddingTop) || 0;
    const contentTop =
      ref.current.getBoundingClientRect().top -
      scroller.getBoundingClientRect().top +
      scroller.scrollTop -
      padding;
    scroller.scrollTo({
      top: Math.min(scroller.scrollTop, Math.max(0, contentTop)),
    });
  }, [activeTab, hash]);
  return ref;
}
