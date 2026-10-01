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

// Matches the cards' rounded-[14px], so a clipped pane keeps the card's corners.
const CARD_RADIUS_PX = 14;

/** A card slot, in viewport coordinates. */
export type CardGeometry = {
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
 * The on-screen part of a pane. The dashboard scrolls the document, so its pane
 * is usually taller than the viewport and may be scrolled; only the part the
 * user is looking at becomes the card.
 */
function visibleSlice(rect: DOMRect): { top: number; height: number } {
  const top = Math.max(rect.top, 0);
  const bottom = Math.min(rect.bottom, window.innerHeight);
  return { top, height: Math.max(bottom - top, 0) };
}

/** Where each mode's card sits, sized from the live pane's on-screen slice. */
export function computeGrid(): Grid {
  const surface = document.querySelector<HTMLElement>("[data-mode-surface]");
  const surfaceRect = surface?.getBoundingClientRect();
  const slice = surfaceRect
    ? visibleSlice(surfaceRect)
    : { top: 0, height: window.innerHeight };
  const paneLeft = surfaceRect?.left ?? 0;
  const paneWidth = surfaceRect?.width ?? window.innerWidth;
  const paneHeight = slice.height;
  const available = Math.min(paneWidth - GRID_INSET_PX, GRID_MAX_WIDTH_PX);
  const cardWidth = (available - CARD_GAP_PX) / 2;
  const scale = cardWidth / paneWidth;
  const cardHeight = paneHeight * scale;
  const originLeft = paneLeft + (paneWidth - (cardWidth * 2 + CARD_GAP_PX)) / 2;
  const top = slice.top + (paneHeight - cardHeight) / 2;

  const cards = [0, 1].map((index) => ({
    left: originLeft + index * (cardWidth + CARD_GAP_PX),
    top,
    width: cardWidth,
    height: cardHeight,
  })) as [CardGeometry, CardGeometry];

  return { cards, paneWidth, paneHeight, scale };
}

export type Parking = {
  transform: string;
  clipPath: string;
  /** Distance from the pane's top edge to the top of its on-screen slice. */
  clipTop: number;
  sliceHeight: number;
};

/**
 * The transform that lands a pane's on-screen slice on its card, and the clip
 * that hides the rest of the pane. Measured from the pane itself, so it must
 * run while the pane is untransformed.
 */
export function parkOnCard(
  surface: HTMLElement,
  card: CardGeometry,
  scale: number,
): Parking {
  const rect = surface.getBoundingClientRect();
  const slice = visibleSlice(rect);
  const clipTop = slice.top - rect.top;
  const clipBottom = Math.max(rect.bottom - (slice.top + slice.height), 0);
  // transform-origin is the pane's top-left corner, so a point `y` below the
  // pane top lands at rect.top + translateY + y * scale.
  const translateX = card.left - rect.left;
  const translateY = card.top - rect.top - clipTop * scale;
  return {
    transform: `translate(${translateX}px, ${translateY}px) scale(${scale})`,
    clipPath: `inset(${clipTop}px 0 ${clipBottom}px 0 round ${CARD_RADIUS_PX}px)`,
    clipTop,
    sliceHeight: slice.height,
  };
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
