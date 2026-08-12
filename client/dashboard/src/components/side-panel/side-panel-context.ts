import { createContext, useContext } from "react";

/**
 * What is open in the side panel.
 *
 * Serializable by design: the panel is mounted above the router outlet and
 * outlives the page that opened it, so a descriptor cannot hold a live object
 * or close over page state. Each kind refetches what it needs from these keys,
 * which is free in practice because the opener has already warmed the query
 * cache.
 */
export type SidePanelDescriptor = {
  kind: "setup-guide";
  title: string;
  /** Second header line, naming what the panel holds when the title names its subject. */
  subtitle?: string;
  /** The subject's own icon. Falls back to the panel kind's generic mark. */
  iconUrl?: string;
  /** Where the same material lives on the docs site, when it has one page. */
  docsUrl?: string;
  props: { registrySpecifier?: string; serverUrl?: string };
};

export const SIDE_PANEL_WIDTH_KEY = "gram.side-panel.width";

// The panel opens at its widest and is only ever dragged narrower, so the
// worst case a page has to reflow into is fixed at SIDE_PANEL_MAX_WIDTH.
export const SIDE_PANEL_MAX_WIDTH = 560;
export const SIDE_PANEL_MIN_WIDTH = 360;

// What the page keeps for itself, and the sidebar at its expanded width. A
// collapsed sidebar only ever leaves more room than this reserves.
const PAGE_MIN_WIDTH = 480;
const SIDEBAR_WIDTH = 256;

/**
 * Resolves the panel's width for a viewport.
 *
 * The panel yields first: it gives up space until it reaches its own minimum,
 * and only below that does the page drop under its floor. Nothing is clamped
 * at all until the viewport falls under ~1300px.
 */
export function clampSidePanelWidth(
  width: number,
  viewportWidth: number,
): number {
  const roomToGrow = viewportWidth - SIDEBAR_WIDTH - PAGE_MIN_WIDTH;
  return Math.max(
    SIDE_PANEL_MIN_WIDTH,
    Math.min(width, SIDE_PANEL_MAX_WIDTH, roomToGrow),
  );
}

type SidePanelContextValue = {
  descriptor: SidePanelDescriptor | null;
  openPanel: (descriptor: SidePanelDescriptor) => void;
  closePanel: () => void;
};

export const SidePanelContext = createContext<SidePanelContextValue | null>(
  null,
);

export function useSidePanel(): SidePanelContextValue {
  const context = useContext(SidePanelContext);
  if (!context) {
    throw new Error("useSidePanel must be used inside a SidePanelProvider");
  }
  return context;
}
