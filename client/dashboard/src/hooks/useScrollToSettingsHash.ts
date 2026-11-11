import { useEffect } from "react";
import { useLocation } from "react-router";
import { scrollWithinPage } from "@/lib/scroll-within-page";

/**
 * Scrolls a settings page to the section its URL hash names, when the hash is
 * one of `ids`. Pass a module-level array: a new one each render would
 * re-run the scroll. `ready` holds the scroll until late-mounting sections
 * are on the page, so a cold-load deep link finds its target.
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

    const animationFrame = window.requestAnimationFrame(() => {
      const target = document.getElementById(targetId);
      // Only the page's scroll container moves, so a pinned header stays put.
      if (target) scrollWithinPage(target);
    });

    return () => window.cancelAnimationFrame(animationFrame);
  }, [ids, location.hash, ready]);
}
