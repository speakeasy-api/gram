import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { useRBAC } from "@/hooks/useRBAC";
import type { IconName } from "@/components/ui/Icon/names";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { createContext, useContext } from "react";

export type Mode = "canvas" | "headless";

// Tab order in the switcher, which is also left-to-right slot order in the
// grid the panes shrink into.
export const MODES: Array<{ mode: Mode; label: string; icon: IconName }> = [
  { mode: "canvas", label: "Dashboard", icon: "square-mouse-pointer" },
  { mode: "headless", label: "Headless", icon: "terminal" },
];

export const slotOf = (mode: Mode): number =>
  MODES.findIndex((entry) => entry.mode === mode);

// Chrome-for-iOS tab switching, in three beats: the live pane shrinks into its
// card slot, both cards sit side by side for a moment, then the chosen card
// zooms back up to fill the pane.
export const SHRINK_MS = 760;
export const HOLD_MS = 540;
export const ZOOM_MS = 760;
export const EASE_OUT = "cubic-bezier(0.32, 0.72, 0, 1)";

// Height of the mode strip above the panes — the grid is laid out underneath
// it. MODE_SWITCHER_HEIGHT below is the same value in rem, for the layouts.
const STRIP_HEIGHT_PX = 56;
const CARD_GAP_PX = 24;
const GRID_MAX_WIDTH_PX = 1200;
const GRID_INSET_PX = 96;

type CardGeometry = {
  transform: string;
  left: number;
  top: number;
  width: number;
  height: number;
};

export type Grid = {
  cards: [CardGeometry, CardGeometry];
  paneWidth: number;
  paneHeight: number;
  scale: number;
};

/**
 * Where each mode's card sits, and the transform that maps a full-size pane
 * onto it. The pane spans the viewport below the strip, so its rect is derived
 * from the window rather than measured — the panes are mid-animation when this
 * is read, and a measured rect would already include the transform.
 */
export function computeGrid(): Grid {
  // Measured, not assumed: an impersonation banner sits above the strip, so the
  // panes start lower than the strip height alone would suggest.
  const strip = document.querySelector<HTMLElement>("[data-mode-switcher]");
  const chromeHeight = strip
    ? strip.getBoundingClientRect().bottom
    : STRIP_HEIGHT_PX;
  const paneWidth = window.innerWidth;
  const paneHeight = window.innerHeight - chromeHeight;
  const available = Math.min(paneWidth - GRID_INSET_PX, GRID_MAX_WIDTH_PX);
  const cardWidth = (available - CARD_GAP_PX) / 2;
  const scale = cardWidth / paneWidth;
  const cardHeight = paneHeight * scale;
  const originLeft = (paneWidth - (cardWidth * 2 + CARD_GAP_PX)) / 2;
  const top = chromeHeight + (paneHeight - cardHeight) / 2;

  const cards = [0, 1].map((index) => {
    const left = originLeft + index * (cardWidth + CARD_GAP_PX);
    return {
      left,
      top,
      width: cardWidth,
      height: cardHeight,
      // transform-origin is the pane's top-left corner, so the translate is the
      // plain delta between the pane origin and the card origin.
      transform: `translate(${left}px, ${top - chromeHeight}px) scale(${scale})`,
    };
  }) as [CardGeometry, CardGeometry];

  return { cards, paneWidth, paneHeight, scale };
}

type Phase = "idle" | "shrinking" | "zooming";

export type StageState = {
  phase: Phase;
  from: Mode | null;
  to: Mode | null;
  grid: Grid | null;
};

export const IDLE: StageState = {
  phase: "idle",
  from: null,
  to: null,
  grid: null,
};

export type StageValue = StageState & {
  switchTo: (from: Mode, to: Mode, href: string) => void;
};

export const ModeSwitchContext = createContext<StageValue>({
  ...IDLE,
  switchTo: () => undefined,
});

export function useModeSwitch(): StageValue {
  return useContext(ModeSwitchContext);
}

// The sidebar is fixed-positioned from --header-offset, and pages size
// themselves against --banner-offset, so the strip's height has to be a known
// constant both layouts can add into those offsets. Kept in sync with
// STRIP_HEIGHT_PX above.
export const MODE_SWITCHER_HEIGHT = "3.5rem";

/**
 * Whether the chrome shows the mode switcher. The layouts need this too: with
 * the strip hidden they must not reserve its height in the offsets above.
 */
export function useModeSwitcherEnabled(): boolean {
  const rollout = useFeatureFlag(FEATURE_FLAGS.headlessModeSwitcher);
  // Headless mode is an organization-admin surface (its content carries the
  // same gate), so members get neither the entry point nor its reserved height.
  const { hasScope } = useRBAC();
  return rollout.status === "enabled" && hasScope("org:admin");
}
