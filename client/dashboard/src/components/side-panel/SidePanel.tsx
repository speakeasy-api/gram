import { Type } from "@/components/ui/type";
import { useLocalStorageState } from "@/hooks/useLocalStorageState";
import { cn } from "@/lib/utils";
import { GripVertical, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { SidePanelKind } from "./panel-kinds";
import {
  clampSidePanelWidth,
  SIDE_PANEL_MAX_WIDTH,
  SIDE_PANEL_MIN_WIDTH,
  SIDE_PANEL_WIDTH_KEY,
  SidePanelContext,
  useSidePanel,
  type SidePanelDescriptor,
} from "./side-panel-context";

const KEYBOARD_STEP = 16;

// Paired with `.side-panel-exit` in App.css: the panel has to stay mounted for
// as long as its collapse takes, or the page would snap back to full width
// while the panel is still on screen.
const EXIT_MS = 200;

/**
 * Holds the open panel for the whole project app.
 *
 * Mounted above the router outlet so a panel survives navigation: you can open
 * a setup guide on one page and keep reading it while you follow its steps on
 * another. `children` is the same element across state changes, so opening and
 * closing re-renders only the components that read this context, not the page.
 */
export function SidePanelProvider({
  children,
}: {
  children: React.ReactNode;
}): React.JSX.Element {
  const [descriptor, setDescriptor] = useState<SidePanelDescriptor | null>(
    null,
  );

  const value = useMemo(
    () => ({
      descriptor,
      openPanel: setDescriptor,
      closePanel: () => setDescriptor(null),
    }),
    [descriptor],
  );

  return (
    <SidePanelContext.Provider value={value}>
      {children}
    </SidePanelContext.Provider>
  );
}

/**
 * The panel itself, rendered as a sibling of the page content so the page
 * reflows around it rather than being covered by it.
 *
 * Deliberately not a dialog: no overlay, no focus trap, no click-outside. The
 * whole point is that the app stays usable while this is open, so Escape
 * closes it only when focus is already inside, leaving Escape free for the
 * dialogs, popovers and command palette on the left.
 */
export function SidePanelSurface(): React.JSX.Element | null {
  const { descriptor, closePanel } = useSidePanel();
  // Trails the descriptor by one exit animation, so a closed panel keeps
  // rendering its last contents while it collapses.
  const [shown, setShown] = useState(descriptor);
  const [storedWidth, setStoredWidth] = useLocalStorageState(
    SIDE_PANEL_WIDTH_KEY,
    SIDE_PANEL_MAX_WIDTH,
  );
  // Held separately from the stored width so a drag repaints every frame but
  // only touches localStorage once, on release.
  const [draggedWidth, setDraggedWidth] = useState<number | null>(null);
  const [viewportWidth, setViewportWidth] = useState(() => window.innerWidth);

  useEffect(() => {
    const onResize = () => setViewportWidth(window.innerWidth);
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  useEffect(() => {
    if (descriptor) {
      setShown(descriptor);
      return;
    }
    const timer = setTimeout(() => setShown(null), EXIT_MS);
    // Reopening mid-collapse cancels the unmount, so the panel reopens with
    // the contents and scroll position it already had.
    return () => clearTimeout(timer);
  }, [descriptor]);

  if (!shown) return null;

  const closing = !descriptor;

  const width = clampSidePanelWidth(draggedWidth ?? storedWidth, viewportWidth);

  // Borderless, matching SidebarInset: the gutter and the shadow separate the
  // panel from the page exactly as they separate the page from the sidebar. A
  // border reads as a floating card sitting on top of the app.
  return (
    <aside
      aria-label={shown.title}
      inert={closing}
      style={{ width }}
      className={cn(
        "bg-surface-primary relative my-2 mr-2 flex shrink-0 flex-col rounded-xl shadow-sm",
        closing ? "side-panel-exit" : "side-panel-enter",
      )}
      onKeyDown={(event) => {
        if (event.key === "Escape") closePanel();
      }}
    >
      <SidePanelResizeHandle
        width={width}
        onPreview={setDraggedWidth}
        onCommit={(next) => {
          setStoredWidth(next);
          setDraggedWidth(null);
        }}
      />
      {/* Clips inside the handle so the grip can sit out in the gutter. */}
      <div className="flex min-h-0 flex-1 overflow-hidden rounded-xl">
        {/* Held at the panel's finished width: only the aside's width animates,
            and contents that resized along with it would re-wrap every frame.
            Instead they slide in from behind the right edge. */}
        <div style={{ width }} className="flex shrink-0 flex-col">
          <div className="flex items-center justify-between gap-2 border-b py-3 pr-3 pl-5">
            <Type className="truncate font-semibold">{shown.title}</Type>
            <button
              type="button"
              onClick={closePanel}
              aria-label="Close panel"
              className="text-muted-foreground hover:text-foreground shrink-0 rounded-sm opacity-70 transition-opacity hover:opacity-100"
            >
              <X className="size-4" />
            </button>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto">
            <SidePanelKind descriptor={shown} />
          </div>
        </div>
      </div>
    </aside>
  );
}

/**
 * The grip on the panel's leading edge, by pointer or by arrow key.
 *
 * Reports a width twice over a drag: `onPreview` for every frame in flight and
 * `onCommit` once on release, so the panel repaints continuously while only
 * the settled width is worth persisting. Keyboard steps land settled, so they
 * only commit.
 */
function SidePanelResizeHandle({
  width,
  onPreview,
  onCommit,
}: {
  width: number;
  onPreview: (width: number) => void;
  onCommit: (width: number) => void;
}): React.JSX.Element {
  const [dragging, setDragging] = useState(false);
  // A ref, not state: pointerdown and pointermove can land in the same task,
  // and a guard reading state would still see the pre-drag value.
  const drag = useRef<{ x: number; width: number } | null>(null);

  // The panel grows leftwards, into the page, so travel is measured backwards
  // and ArrowLeft is the one that widens.
  const resizeTo = (next: number) =>
    clampSidePanelWidth(next, window.innerWidth);
  const draggedTo = (start: { x: number; width: number }, clientX: number) =>
    resizeTo(start.width + (start.x - clientX));

  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize panel"
      aria-valuenow={width}
      aria-valuemin={SIDE_PANEL_MIN_WIDTH}
      aria-valuemax={SIDE_PANEL_MAX_WIDTH}
      tabIndex={0}
      onPointerDown={(event) => {
        event.currentTarget.setPointerCapture(event.pointerId);
        drag.current = { x: event.clientX, width };
        setDragging(true);
      }}
      onPointerMove={(event) => {
        const start = drag.current;
        if (!start) return;
        onPreview(draggedTo(start, event.clientX));
      }}
      onPointerUp={(event) => {
        const start = drag.current;
        if (!start) return;
        event.currentTarget.releasePointerCapture(event.pointerId);
        onCommit(draggedTo(start, event.clientX));
        drag.current = null;
        setDragging(false);
      }}
      onKeyDown={(event) => {
        if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
        event.preventDefault();
        const step = event.key === "ArrowLeft" ? KEYBOARD_STEP : -KEYBOARD_STEP;
        onCommit(resizeTo(width + step));
      }}
      className="group absolute inset-y-0 -left-2 z-10 flex w-2 cursor-col-resize items-center justify-center outline-none"
    >
      <div
        className={cn(
          "bg-card text-muted-foreground group-hover:text-foreground group-focus-visible:border-primary flex h-8 w-4 items-center justify-center rounded-md border shadow-sm",
          dragging && "text-foreground",
        )}
      >
        <GripVertical className="size-3.5" />
      </div>
    </div>
  );
}
