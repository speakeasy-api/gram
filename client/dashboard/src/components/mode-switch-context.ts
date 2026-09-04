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

/** Where each mode's card sits, and the transform from the live pane. */
export function computeGrid(): Grid {
  const surface = document.querySelector<HTMLElement>("[data-mode-surface]");
  const surfaceRect = surface?.getBoundingClientRect();
  const paneTop = surfaceRect?.top ?? 0;
  const paneWidth = surfaceRect?.width ?? window.innerWidth;
  const paneHeight = surfaceRect?.height ?? window.innerHeight;
  const available = Math.min(paneWidth - GRID_INSET_PX, GRID_MAX_WIDTH_PX);
  const cardWidth = (available - CARD_GAP_PX) / 2;
  const scale = cardWidth / paneWidth;
  const cardHeight = paneHeight * scale;
  const originLeft = (paneWidth - (cardWidth * 2 + CARD_GAP_PX)) / 2;
  const top = paneTop + (paneHeight - cardHeight) / 2;

  const cards = [0, 1].map((index) => {
    const left = originLeft + index * (cardWidth + CARD_GAP_PX);
    return {
      left,
      top,
      width: cardWidth,
      height: cardHeight,
      // transform-origin is the pane's top-left corner, so the translate is the
      // plain delta between the pane origin and the card origin.
      transform: `translate(${left}px, ${top - paneTop}px) scale(${scale})`,
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

/** Whether the current user should see the mode switcher. */
export function useModeSwitcherEnabled(): boolean {
  const rollout = useFeatureFlag(FEATURE_FLAGS.headlessModeSwitcher);
  // Headless mode is an organization-admin surface (its content carries the
  // same gate), so members get neither the entry point nor its reserved height.
  const { hasScope } = useRBAC();
  return rollout.status === "enabled" && hasScope("org:admin");
}
