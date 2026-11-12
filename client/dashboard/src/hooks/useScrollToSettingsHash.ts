import { useEffect } from "react";
import { useLocation } from "react-router";
import { scrollWithinPage } from "@/lib/scroll-within-page";

/** How long a deep link keeps its target in place while sections load. */
const SETTLE_MS = 2000;

/**
 * Scrolls a settings page to the section its URL hash names, when the hash is
 * one of `ids`. Pass a module-level array: a new one each render would
 * re-run the scroll. `ready` holds the scroll until late-mounting sections
 * are on the page, so a cold-load deep link finds its target.
 *
 * Sections above the target can still grow as their own data loads, so for a
 * moment after the scroll the target is held at the top, until the reader
 * scrolls themselves.
 */
export function useScrollToSettingsHash(
  ids: readonly string[],
  ready = true,
): void {
  const location = useLocation();

  useEffect(() => {
    const targetId = location.hash.replace("#", "");
    if (!ready || !ids.includes(targetId)) {
      return;
    }

    let release = () => {};
    const animationFrame = window.requestAnimationFrame(() => {
      const target = document.getElementById(targetId);
      if (!target) return;
      // Only the page's scroll container moves, so a pinned header stays put.
      scrollWithinPage(target);
      release = holdInPlace(target);
    });

    return () => {
      window.cancelAnimationFrame(animationFrame);
      release();
    };
  }, [ids, location.hash, ready]);
}

/** Re-scrolls to `target` when content resizes, until the reader takes over. */
function holdInPlace(target: HTMLElement): () => void {
  const scroller =
    target.parentElement?.closest<HTMLElement>("[data-page-bound]");
  const content = scroller?.firstElementChild;
  if (!scroller || !content || typeof ResizeObserver === "undefined") {
    return () => {};
  }
  const resize = new ResizeObserver(() =>
    scrollWithinPage(target, { behavior: "auto" }),
  );
  resize.observe(content);
  const stop = () => {
    resize.disconnect();
    clearTimeout(timer);
    for (const event of ["wheel", "touchstart", "keydown", "pointerdown"]) {
      scroller.removeEventListener(event, stop);
    }
  };
  const timer = setTimeout(stop, SETTLE_MS);
  for (const event of ["wheel", "touchstart", "keydown", "pointerdown"]) {
    scroller.addEventListener(event, stop, { passive: true });
  }
  return stop;
}
