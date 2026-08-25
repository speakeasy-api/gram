import {
  BRAND_MESH_SURFACE_CLASS,
  BrandMeshLayers,
} from "@/components/brand-mesh";
import { Icon } from "@/components/ui/Icon";
import { cn } from "@/lib/utils";
import { HeadlessContent } from "@/pages/org/HeadlessContent";
import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";
import {
  EASE_OUT,
  HOLD_MS,
  IDLE,
  MODES,
  ModeSwitchContext,
  SHRINK_MS,
  ZOOM_MS,
  computeGrid,
  slotOf,
  useModeSwitch,
  type Grid,
  type Mode,
  type StageState,
} from "./mode-switch-context";

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
        className={cn(
          BRAND_MESH_SURFACE_CLASS,
          // Decoration only: positioned, so without this it would sit over the
          // panes' non-positioned content and swallow every click.
          "pointer-events-none fixed inset-0 z-0",
        )}
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
  const surfaceStyle: React.CSSProperties | undefined = animating
    ? {
        transform: parked ? card?.transform : "none",
        transition: `transform ${isShrinking ? SHRINK_MS : ZOOM_MS}ms ${EASE_OUT}`,
      }
    : undefined;

  return (
    <div
      // A transform makes this element the containing block for the fixed
      // sidebar inside it, so --header-offset (which positions the sidebar
      // below the chrome) would be measured from the pane top and push it down
      // a second time. The pane already starts below the chrome, so zero it
      // for the duration of the animation.
      style={
        animating
          ? ({
              ...surfaceStyle,
              "--header-offset": "0px",
            } as React.CSSProperties)
          : surfaceStyle
      }
      className={cn(
        className,
        animating &&
          // bg-background so the mesh never shows through the pane's own gaps
          // while it is scaled down.
          "bg-background relative z-20 origin-top-left overflow-hidden rounded-[14px] shadow-2xl will-change-transform",
      )}
    >
      {children}
    </div>
  );
}
