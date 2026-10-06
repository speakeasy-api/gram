import { ModeSwitchStarfield } from "@/components/mode-switch-starfield";
import { Icon } from "@/components/ui/Icon";
import { cn } from "@/lib/utils";
import { HeadlessContent } from "@/pages/org/HeadlessContent";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { useNavigate } from "react-router";
import {
  CARD_RADIUS_PX,
  EASE_OUT,
  HOLD_MS,
  IDLE,
  MODES,
  ModeSwitchContext,
  SHRINK_MS,
  ZOOM_MS,
  computeGrid,
  parkOnCard,
  slotOf,
  useModeSwitch,
  type Grid,
  type Mode,
  type Parking,
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
      {/* Ink behind the whole chrome: the dark screen the tab cards and the
          starfield sit on. Only while a switch is in flight — the provider is
          above the router, so an always-on backdrop would black out routes
          without an opaque pane of their own, such as login. pointer-events-none
          because it is positioned and would otherwise swallow clicks on the
          panes' non-positioned content. */}
      {state.phase !== "idle" && (
        <div
          aria-hidden="true"
          className="bg-surface-tertiary-fixed-dark pointer-events-none fixed inset-0 z-0"
        />
      )}
      {/* Win95 screensaver behind the grid: only while a switch is in flight,
          raked toward whichever card is being opened. */}
      {state.phase !== "idle" && state.from && state.to && (
        <ModeSwitchStarfield
          direction={slotOf(state.to) > slotOf(state.from) ? 1 : -1}
          fading={state.phase === "zooming"}
        />
      )}
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
        // ring, not border: a pale hairline that separates the card from the
        // ink behind it without the theme border disappearing into it.
        "bg-card fixed z-20 overflow-hidden rounded-[14px] shadow-2xl ring-1 ring-white/25 transition-opacity",
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
  const scale = grid?.scale;
  const isShrinking = phase === "shrinking" && from === mode;
  const isZooming = phase === "zooming" && to === mode;
  const animating = isShrinking || isZooming;
  const surfaceRef = useRef<HTMLDivElement | null>(null);

  // Measured from this pane rather than taken from the grid: the incoming pane
  // is not the one the grid was measured from, and the two can differ in height
  // and scroll position. Layout effect, so the pane is measured untransformed
  // and parked before the browser paints it.
  const [parking, setParking] = useState<Parking | null>(null);
  useLayoutEffect(() => {
    const surface = surfaceRef.current;
    if (!animating || !surface || !card || scale === undefined) {
      setParking(null);
      return;
    }
    setParking(parkOnCard(surface, card, scale));
  }, [animating, card, scale]);

  // The incoming pane mounts already parked on its card, then releases to full
  // size on the next frame.
  const [zoomReleased, setZoomReleased] = useState(false);
  useEffect(() => {
    if (!isZooming) {
      setZoomReleased(false);
      return;
    }
    const frame = window.requestAnimationFrame(() => {
      // Flush the parked style first. Parking lands in the same task as the
      // mount, so without a style read the browser never computes it and the
      // release has no start value to animate from.
      surfaceRef.current?.getBoundingClientRect();
      setZoomReleased(true);
    });
    return () => window.cancelAnimationFrame(frame);
  }, [isZooming]);

  const parked = isShrinking || (isZooming && !zoomReleased);
  // No transition while the incoming pane is parked, so moving onto its card
  // is instant and only the release animates.
  const timing = `${isShrinking ? SHRINK_MS : ZOOM_MS}ms ${EASE_OUT}`;
  const transition =
    isZooming && !zoomReleased
      ? "none"
      : `transform ${timing}, --mode-card-radius ${timing}`;
  // The clip's radius scales with the pane, so the parked radius is scaled up
  // to read as the card's own. It animates with the transform (it is a
  // registered property), keeping the corners steady through the zoom.
  const radius = parked && parking ? parking.parkedRadius : CARD_RADIUS_PX;
  const surfaceStyle = animating
    ? ({
        transform: parked ? parking?.transform : "none",
        clipPath: parking?.clipPath,
        transition,
        "--mode-card-radius": `${radius}px`,
        // A transform makes this element the containing block for the fixed
        // sidebar inside it, so --header-offset (which positions the sidebar
        // below the chrome) is measured from the pane top instead of the
        // viewport. Point it at the top of the on-screen slice: the pane
        // already starts below the chrome, and a scrolled pane's sidebar
        // belongs where the user was looking.
        "--header-offset": `${parking?.clipTop ?? 0}px`,
      } as React.CSSProperties)
    : undefined;

  return (
    <div
      ref={surfaceRef}
      data-mode-surface=""
      style={surfaceStyle}
      className={cn(
        className,
        animating &&
          // bg-background so the backdrop never shows through the pane's own
          // gaps while it is scaled down. Descendant transitions are frozen for
          // the duration: the fixed sidebar re-anchors when the transform is
          // added and removed (a transform makes this element its containing
          // block), and its own left/width transition would animate that
          // re-anchor as a visible slide.
          "bg-background relative z-20 origin-top-left overflow-hidden will-change-transform [&_*]:!transition-none",
      )}
    >
      {children}
      {/* The card's hairline edge. The clip would cut a ring on the pane
          itself (it paints outside the box), so it is drawn inset over the
          on-screen slice instead. */}
      {parking && (
        <div
          aria-hidden="true"
          className="pointer-events-none absolute inset-x-0 z-50 rounded-(--mode-card-radius) ring-1 ring-white/25 ring-inset"
          style={{ top: parking.clipTop, height: parking.sliceHeight }}
        />
      )}
    </div>
  );
}
