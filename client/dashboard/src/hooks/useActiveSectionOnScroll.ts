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

// Keys that scroll the page. Releasing the pin on any keystroke would drop it
// on unrelated input (search shortcuts, typing in a dialog).
const SCROLL_KEYS = new Set([
  "ArrowUp",
  "ArrowDown",
  "PageUp",
  "PageDown",
  "Home",
  "End",
  " ",
]);

export type ActiveSectionOptions = {
  /**
   * The section the reader explicitly asked for, e.g. the URL hash after
   * clicking a sidebar link. It stays active until the reader scrolls again,
   * so the highlight doesn't spring back when the page can't scroll far enough
   * to bring that section to the activation line (the trailing sections that
   * share the last screenful).
   */
  requestedSectionId?: string | null;
  /**
   * Changes on every navigation, so asking for the section that is already in
   * the hash (clicking the same link twice) pins it again.
   */
  requestKey?: string;
  topOffset?: number;
};

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
  {
    requestedSectionId = null,
    requestKey = "",
    topOffset = 120,
  }: ActiveSectionOptions = {},
): string | null {
  const [activeId, setActiveId] = React.useState<string | null>(null);
  const [pinnedId, setPinnedId] = React.useState<string | null>(null);
  // Collapse the array into a primitive so the effect only re-subscribes when
  // the set of sections actually changes, not on every new array identity.
  const idsKey = sectionIds.join("\n");

  // Keyed on the request alone: re-pinning whenever the section list changes
  // would resurrect a pin the reader already scrolled away from.
  React.useEffect(() => {
    setPinnedId(requestedSectionId === "" ? null : requestedSectionId);
  }, [requestedSectionId, requestKey]);

  // Only real input gestures release the pin. Releasing on `scroll` alone would
  // fire on the smooth scroll the click itself triggers and drop the pin
  // immediately.
  React.useEffect(() => {
    if (pinnedId === null || typeof window === "undefined") return;

    const release = () => setPinnedId(null);
    const releaseOnScrollKey = (event: KeyboardEvent) => {
      if (SCROLL_KEYS.has(event.key)) setPinnedId(null);
    };

    // Dragging the scrollbar emits neither wheel nor touch events, and its
    // gutter can't be located by geometry when the platform draws overlay
    // scrollbars (no width to measure). What distinguishes it is scrolling
    // while the button is held — a plain click scrolls nothing.
    let buttonHeld = false;
    const holdButton = () => {
      buttonHeld = true;
    };
    const releaseButton = () => {
      buttonHeld = false;
    };
    const releaseOnDragScroll = () => {
      if (buttonHeld) setPinnedId(null);
    };

    // Capture, because scroll events from the scrolling container don't bubble.
    const options = { passive: true, capture: true } as const;

    window.addEventListener("wheel", release, options);
    window.addEventListener("touchmove", release, options);
    window.addEventListener("keydown", releaseOnScrollKey, options);
    window.addEventListener("mousedown", holdButton, options);
    window.addEventListener("mouseup", releaseButton, options);
    window.addEventListener("scroll", releaseOnDragScroll, options);
    return () => {
      window.removeEventListener("wheel", release, options);
      window.removeEventListener("touchmove", release, options);
      window.removeEventListener("keydown", releaseOnScrollKey, options);
      window.removeEventListener("mousedown", holdButton, options);
      window.removeEventListener("mouseup", releaseButton, options);
      window.removeEventListener("scroll", releaseOnDragScroll, options);
    };
  }, [pinnedId]);

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

  // Validated during render rather than when pinning, so a section list that
  // grows later (frontmatter, versions) can honor an existing pin without the
  // pin effect having to re-run.
  if (pinnedId !== null && sectionIds.includes(pinnedId)) return pinnedId;
  return activeId;
}
