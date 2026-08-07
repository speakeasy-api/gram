import * as React from "react";

// The app shell scrolls an inner container rather than the window, so the
// observer must resolve the real scroll ancestor of the sections to place its
// activation line correctly. Falls back to the viewport when nothing scrolls.
function findVerticalScrollParent(el: HTMLElement | null): HTMLElement | null {
  let node: HTMLElement | null = el?.parentElement ?? null;
  while (
    node &&
    !/auto|scroll|overlay/.test(getComputedStyle(node).overflowY)
  ) {
    node = node.parentElement;
  }
  return node;
}

/**
 * Tracks which of the given page sections the reader is currently looking at
 * and returns its id, so a sidebar can keep its active item in sync as the
 * user scrolls. `sectionIds` must be listed in document order; the topmost
 * section whose start has scrolled past the activation line wins, and the
 * first section stays active until the reader scrolls past it.
 *
 * Returns `null` until the observed sections have been measured (e.g. before
 * the page content mounts or in environments without IntersectionObserver),
 * letting callers fall back to their existing active-state logic.
 */
export function useActiveSectionOnScroll(
  sectionIds: readonly string[],
  topOffset = 120,
): string | null {
  const [activeId, setActiveId] = React.useState<string | null>(null);
  // Collapse the array into a primitive so the effect only re-subscribes when
  // the set of sections actually changes, not on every new array identity.
  const idsKey = sectionIds.join("\n");

  React.useEffect(() => {
    if (
      typeof window === "undefined" ||
      typeof IntersectionObserver === "undefined"
    ) {
      return;
    }

    const ids = idsKey.length > 0 ? idsKey.split("\n") : [];
    if (ids.length === 0) {
      setActiveId(null);
      return;
    }

    let scrollRoot: HTMLElement | null = null;
    let intersectionObserver: IntersectionObserver | null = null;
    let observedCount = 0;
    let frame = 0;

    const recompute = () => {
      const lineTop =
        (scrollRoot ? scrollRoot.getBoundingClientRect().top : 0) + topOffset;
      let current: string | null = null;
      for (const id of ids) {
        const el = document.getElementById(id);
        if (!el) continue;
        // Once a section starts below the activation line, every later section
        // is below it too, so the last one above the line is the active one.
        if (el.getBoundingClientRect().top <= lineTop + 1) {
          current = id;
        } else {
          break;
        }
      }
      // Before the first section reaches the line, keep the first one active.
      setActiveId(current ?? ids[0] ?? null);
    };

    const scheduleRecompute = () => {
      if (frame !== 0) return;
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        recompute();
      });
    };

    // Returns true once every section is being observed.
    const attach = (): boolean => {
      const elements: HTMLElement[] = [];
      for (const id of ids) {
        const el = document.getElementById(id);
        if (el) elements.push(el);
      }
      // Nothing new to observe; skip rebuilding the observer.
      if (elements.length === observedCount)
        return elements.length === ids.length;

      observedCount = elements.length;
      scrollRoot = findVerticalScrollParent(elements[0]!);
      intersectionObserver?.disconnect();
      // Shifting the root's top edge down to the activation line makes the
      // observer fire exactly when a section boundary crosses it, which is the
      // only moment the active section can change.
      intersectionObserver = new IntersectionObserver(scheduleRecompute, {
        root: scrollRoot ?? null,
        rootMargin: `-${topOffset}px 0px 0px 0px`,
        threshold: 0,
      });
      for (const el of elements) intersectionObserver.observe(el);
      scheduleRecompute();
      return elements.length === ids.length;
    };

    // This hook runs from the sidebar, which mounts before the page content
    // that owns the sections — observing nothing here would freeze the
    // highlight on the first section. Watch for them until all are observed.
    let mutationObserver: MutationObserver | null = null;
    if (!attach() && typeof MutationObserver !== "undefined") {
      mutationObserver = new MutationObserver(() => {
        if (attach()) {
          mutationObserver?.disconnect();
          mutationObserver = null;
        }
      });
      mutationObserver.observe(document.body, {
        childList: true,
        subtree: true,
      });
    }

    return () => {
      if (frame !== 0) window.cancelAnimationFrame(frame);
      intersectionObserver?.disconnect();
      mutationObserver?.disconnect();
    };
  }, [idsKey, topOffset]);

  return activeId;
}
