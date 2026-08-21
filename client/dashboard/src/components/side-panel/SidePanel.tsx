import { Text } from "@/components/ui/Text";
import { useIsMobile } from "@/hooks/use-mobile";
import { useLocalStorageState } from "@/hooks/useLocalStorageState";
import { cn } from "@/lib/utils";
import { BookOpen, ExternalLink, GripVertical, X } from "lucide-react";
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
  const isMobile = useIsMobile();
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

  // Nothing opens the panel on mobile, but a window narrowed after it opened
  // would leave the page a sliver: the panel's own minimum is wider than what
  // is left of a phone viewport. The descriptor is kept, so widening the window
  // brings the panel back where it was.
  if (isMobile || !shown) return null;

  const closing = !descriptor;

  const width = clampSidePanelWidth(draggedWidth ?? storedWidth, viewportWidth);

  // Borderless, matching SidebarInset: the gutter and the shadow separate the
  // panel from the page exactly as they separate the page from the sidebar. A
  // border reads as a floating card sitting on top of the app.
  return (
    <aside
      // The header's two lines read as one name: "Google BigQuery MCP setup guide".
      aria-label={[shown.title, shown.subtitle].filter(Boolean).join(" ")}
      inert={closing}
      style={{ width }}
      className={cn(
        "bg-surface-primary relative my-2 mr-2 flex shrink-0 flex-col shadow-sm",
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
      <div className="flex min-h-0 flex-1 overflow-hidden">
        {/* Held at the panel's finished width: only the aside's width animates,
            and contents that resized along with it would re-wrap every frame.
            Instead they slide in from behind the right edge. */}
        <div style={{ width }} className="flex shrink-0 flex-col">
          <div className="flex items-center gap-3 border-b py-3 pr-3 pl-5">
            {/* Whatever the panel is about, wearing its own face. Servers that
                publish no icon fall back to the mark for what the panel holds,
                which is a guide. */}
            <div className="bg-primary/5 flex size-8 shrink-0 items-center justify-center dark:bg-neutral-800">
              {shown.iconUrl ? (
                <img
                  src={shown.iconUrl}
                  alt=""
                  className="size-5 object-contain"
                />
              ) : (
                <BookOpen className="text-muted-foreground size-4" />
              )}
            </div>
            <div className="flex min-w-0 flex-1 flex-col">
              <Text className="truncate font-semibold">{shown.title}</Text>
              {shown.subtitle && (
                <Text variant="small" muted className="truncate">
                  {shown.subtitle}
                </Text>
              )}
            </div>
            <div className="flex shrink-0 items-center gap-1">
              {/* The panel holds a reading copy; this is where the same
                  material lives on the docs site. A new tab, so following it
                  does not abandon a half-finished setup behind it. */}
              {shown.docsUrl && (
                <a
                  href={shown.docsUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-muted-foreground hover:text-foreground bg-muted/40 hover:bg-muted flex items-center gap-1.5 border px-2 py-1 text-xs font-medium transition-colors"
                >
                  Docs
                  <ExternalLink className="size-3" />
                </a>
              )}
              <button
                type="button"
                onClick={closePanel}
                aria-label="Close panel"
                className="text-muted-foreground hover:text-foreground p-1 opacity-70 transition-opacity hover:opacity-100"
              >
                <X className="size-4" />
              </button>
            </div>
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
 * only commit. A preview of `null` discards a drag the browser took away.
 */
function SidePanelResizeHandle({
  width,
  onPreview,
  onCommit,
}: {
  width: number;
  onPreview: (width: number | null) => void;
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
      // A drag the browser takes over (a touch that becomes a scroll, a device
      // that goes away) ends here rather than at pointerup. Left unhandled, the
      // handle keeps its start point, and the next pointer merely passing over
      // the grip would carry on resizing with nothing held down. The in-flight
      // width is dropped rather than committed: a canceled drag is not a width
      // anyone chose.
      onPointerCancel={() => {
        drag.current = null;
        setDragging(false);
        onPreview(null);
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
          "bg-card text-muted-foreground group-hover:text-foreground group-focus-visible:border-primary flex h-8 w-4 items-center justify-center border shadow-sm",
          dragging && "text-foreground",
        )}
      >
        <GripVertical className="size-3.5" />
      </div>
    </div>
  );
}
