// oxlint-disable react/only-export-components -- provider + surface + hook ship together
import {
  BRAND_MESH_SURFACE_CLASS,
  BrandMeshLayers,
} from "@/components/brand-mesh";
import { Icon } from "@/components/ui/Icon";
import type { IconName } from "@/components/ui/Icon/names";
import { cn } from "@/lib/utils";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";
import { HeadlessContent } from "@/pages/org/HeadlessContent";
import { useNavigate } from "react-router";

export type Mode = "canvas" | "headless";

// Tab order in the switcher, which is also left-to-right slot order in the
// grid the panes shrink into.
export const MODES: Array<{ mode: Mode; label: string; icon: IconName }> = [
  { mode: "canvas", label: "Dashboard", icon: "square-mouse-pointer" },
  { mode: "headless", label: "Headless", icon: "terminal" },
];

const slotOf = (mode: Mode) => MODES.findIndex((entry) => entry.mode === mode);

// Chrome-for-iOS tab switching, in three beats: the live pane shrinks into its
// card slot, both cards sit side by side for a moment, then the chosen card
// zooms back up to fill the pane.
const SHRINK_MS = 520;
const HOLD_MS = 380;
const ZOOM_MS = 560;
const EASE_OUT = "cubic-bezier(0.32, 0.72, 0, 1)";

// Height of the mode strip above the panes — the grid is laid out underneath
// it. Kept in sync with MODE_SWITCHER_HEIGHT in mode-switcher.tsx.
const STRIP_HEIGHT_PX = 48;
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

type Grid = {
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
function computeGrid(): Grid {
  const paneWidth = window.innerWidth;
  const paneHeight = window.innerHeight - STRIP_HEIGHT_PX;
  const available = Math.min(paneWidth - GRID_INSET_PX, GRID_MAX_WIDTH_PX);
  const cardWidth = (available - CARD_GAP_PX) / 2;
  const scale = cardWidth / paneWidth;
  const cardHeight = paneHeight * scale;
  const originLeft = (paneWidth - (cardWidth * 2 + CARD_GAP_PX)) / 2;
  const top = STRIP_HEIGHT_PX + (paneHeight - cardHeight) / 2;

  const cards = [0, 1].map((index) => {
    const left = originLeft + index * (cardWidth + CARD_GAP_PX);
    return {
      left,
      top,
      width: cardWidth,
      height: cardHeight,
      // transform-origin is the pane's top-left corner, so the translate is the
      // plain delta between the pane origin and the card origin.
      transform: `translate(${left}px, ${top - STRIP_HEIGHT_PX}px) scale(${scale})`,
    };
  }) as [CardGeometry, CardGeometry];

  return { cards, paneWidth, paneHeight, scale };
}

type Phase = "idle" | "shrinking" | "zooming";

type StageState = {
  phase: Phase;
  from: Mode | null;
  to: Mode | null;
  grid: Grid | null;
};

const IDLE: StageState = { phase: "idle", from: null, to: null, grid: null };

type StageValue = StageState & {
  switchTo: (from: Mode, to: Mode, href: string) => void;
};

const ModeSwitchContext = createContext<StageValue>({
  ...IDLE,
  switchTo: () => undefined,
});

export function useModeSwitch(): StageValue {
  return useContext(ModeSwitchContext);
}

/**
 * Drives the mode-switch animation. Mounted above the router so it outlives the
 * route swap that happens between the shrink and the zoom.
 */
export function ModeSwitchProvider({
  children,
}: {
  children: React.ReactNode;
}): JSX.Element {
  const [state, setState] = useState<StageState>(IDLE);
  const navigate = useNavigate();
  const timers = useRef<number[]>([]);

  useEffect(
    () => () => timers.current.forEach((id) => window.clearTimeout(id)),
    [],
  );

  const switchTo = useCallback(
    (from: Mode, to: Mode, href: string) => {
      if (state.phase !== "idle" || from === to) return;

      const grid = computeGrid();
      setState({ phase: "shrinking", from, to, grid });

      timers.current.push(
        window.setTimeout(() => {
          // The route swaps while both cards are parked in the grid, so the
          // pane being replaced is already a thumbnail when it disappears.
          void navigate(href);
          setState({ phase: "zooming", from, to, grid });
        }, SHRINK_MS + HOLD_MS),
        window.setTimeout(() => setState(IDLE), SHRINK_MS + HOLD_MS + ZOOM_MS),
      );
    },
    [navigate, state.phase],
  );

  // The mode whose card is a static placeholder for this beat: the destination
  // while the current pane shrinks, then the mode just left while the new one
  // zooms in.
  const ghostMode =
    state.phase === "shrinking"
      ? state.to
      : state.phase === "zooming"
        ? state.from
        : null;

  return (
    <ModeSwitchContext.Provider value={{ ...state, switchTo }}>
      {/* One mesh for the whole chrome, always mounted and always full-bleed:
          the strip is transparent and the panes are opaque, so the mesh reads
          as a single continuous surface behind both instead of restarting
          below the strip. */}
      <div
        aria-hidden="true"
        className={cn(BRAND_MESH_SURFACE_CLASS, "fixed inset-0 z-0")}
      >
        <BrandMeshLayers />
      </div>
      {ghostMode && state.grid && (
        <ModeGhostCard
          mode={ghostMode}
          grid={state.grid}
          fading={state.phase === "zooming"}
        />
      )}
      {children}
    </ModeSwitchContext.Provider>
  );
}

/**
 * The other mode's card while the live pane is a thumbnail beside it. Headless
 * mounts its real pane, scaled down, so both cards read as tab previews; the
 * dashboard card stays a label, because re-rendering the whole app tree for a
 * 1.4s animation is not worth the cost.
 */
function ModeGhostCard({
  mode,
  grid,
  fading,
}: {
  mode: Mode;
  grid: Grid;
  fading: boolean;
}) {
  const entry = MODES[slotOf(mode)]!;
  const card = grid.cards[slotOf(mode)]!;

  return (
    <div
      aria-hidden="true"
      className={cn(
        "bg-card border-border fixed z-20 overflow-hidden rounded-[14px] border shadow-2xl transition-opacity",
        fading ? "opacity-0" : "opacity-100",
      )}
      style={{
        left: card.left,
        top: card.top,
        width: card.width,
        height: card.height,
        transitionDuration: `${fading ? ZOOM_MS : SHRINK_MS}ms`,
      }}
    >
      {mode === "headless" ? (
        <div
          className="pointer-events-none origin-top-left"
          style={{
            width: grid.paneWidth,
            height: grid.paneHeight,
            transform: `scale(${grid.scale})`,
          }}
        >
          <HeadlessContent />
        </div>
      ) : (
        <div className="flex h-full flex-col items-center justify-center gap-3">
          <Icon name={entry.icon} className="text-muted-foreground h-6 w-6" />
          <span className="text-eyebrow">{entry.label}</span>
        </div>
      )}
    </div>
  );
}

/**
 * Wraps a mode's pane so it can shrink into (and zoom out of) its card slot.
 * Both modes render one — the class list is the pane's own layout.
 */
export function ModeSurface({
  mode,
  className,
  children,
}: {
  mode: Mode;
  className?: string;
  children: React.ReactNode;
}): JSX.Element {
  const { phase, from, to, grid } = useModeSwitch();
  const card = grid?.cards[slotOf(mode)];
  const isShrinking = phase === "shrinking" && from === mode;
  const isZooming = phase === "zooming" && to === mode;

  // The incoming pane mounts already parked on its card, then releases to full
  // size on the next frame so the browser has a start value to animate from.
  const [zoomReleased, setZoomReleased] = useState(false);
  useEffect(() => {
    if (!isZooming) {
      setZoomReleased(false);
      return;
    }
    const frame = window.requestAnimationFrame(() => setZoomReleased(true));
    return () => window.cancelAnimationFrame(frame);
  }, [isZooming]);

  const animating = isShrinking || isZooming;
  const parked = isShrinking || (isZooming && !zoomReleased);

  return (
    <div
      className={cn(
        className,
        animating &&
          // bg-background so the mesh never shows through the pane's own gaps
          // while it is scaled down.
          "bg-background relative z-20 origin-top-left overflow-hidden rounded-[14px] shadow-2xl will-change-transform",
      )}
      style={
        animating
          ? {
              transform: parked ? card?.transform : "none",
              transition: `transform ${isShrinking ? SHRINK_MS : ZOOM_MS}ms ${EASE_OUT}`,
            }
          : undefined
      }
    >
      {children}
    </div>
  );
}
