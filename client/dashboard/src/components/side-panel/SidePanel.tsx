import { Text } from "@/components/ui/Text";
import { useIsMobile } from "@/hooks/use-mobile";
import { cn } from "@/lib/utils";
import { BookOpen, ExternalLink, X } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useLocation } from "react-router";
import { SidePanelKind, SidePanelKindHeaderAction } from "./panel-kinds";
import {
  clampSidePanelWidth,
  SIDE_PANEL_MAX_WIDTH,
  SidePanelContext,
  useSidePanel,
  type SidePanelDescriptor,
} from "./side-panel-context";

// Paired with `.side-panel-exit` in App.css: the panel has to stay mounted for
// as long as its collapse takes, or the page would snap back to full width
// while the panel is still on screen.
const EXIT_MS = 200;

/**
 * Holds the open panel for the whole project app.
 *
 * The panel belongs to the page that opened it, so navigating closes it: a
 * sheet describing something on the page behind it is stale the moment that
 * page goes away. `children` is the same element across state changes, so
 * opening and closing re-renders only the components that read this context,
 * not the page.
 */
export function SidePanelProvider({
  children,
}: {
  children: React.ReactNode;
}): React.JSX.Element {
  const [descriptor, setDescriptor] = useState<SidePanelDescriptor | null>(
    null,
  );
  const { pathname } = useLocation();

  useEffect(() => {
    setDescriptor(null);
  }, [pathname]);

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
 * The panel itself, floating over the page content rather than displacing it.
 *
 * Dismisses like the app's other sheets — a click outside closes it — but it
 * is still not a dialog: no overlay and no focus trap, so the page behind it
 * stays legible and usable right up until the click that closes it. Escape
 * closes it only when focus is already inside, leaving Escape free for the
 * dialogs, popovers and command palette on the left.
 */
export function SidePanelSurface(): React.JSX.Element | null {
  const { descriptor, closePanel } = useSidePanel();
  const isMobile = useIsMobile();
  // Trails the descriptor by one exit animation, so a closed panel keeps
  // rendering its last contents while it collapses.
  const [shown, setShown] = useState(descriptor);
  const [viewportWidth, setViewportWidth] = useState(() => window.innerWidth);
  const panelRef = useRef<HTMLElement | null>(null);

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

  // Dismisses like every other sheet in the app: a click on the page behind
  // it closes it. On pointerdown rather than click, so a press that starts
  // outside and drags across the panel still counts as outside. Skipped while
  // nothing is open so the app carries no listener it does not need.
  useEffect(() => {
    if (!descriptor) return;
    const onPointerDown = (event: PointerEvent) => {
      const panel = panelRef.current;
      if (panel && !panel.contains(event.target as Node)) closePanel();
    };
    document.addEventListener("pointerdown", onPointerDown);
    return () => document.removeEventListener("pointerdown", onPointerDown);
  }, [descriptor, closePanel]);

  // Nothing opens the panel on mobile, but a window narrowed after it opened
  // would leave the page a sliver: the panel's own minimum is wider than what
  // is left of a phone viewport. The descriptor is kept, so widening the window
  // brings the panel back where it was.
  if (isMobile || !shown) return null;

  const closing = !descriptor;

  // One width, yielding only to a viewport too narrow to hold it.
  const width = clampSidePanelWidth(SIDE_PANEL_MAX_WIDTH, viewportWidth);

  // Borderless, matching SidebarInset: the gutter and the shadow separate the
  // panel from the page exactly as they separate the page from the sidebar. A
  // border reads as a floating card sitting on top of the app.
  return (
    <aside
      ref={panelRef}
      // The header's two lines read as one name: "Google BigQuery MCP setup guide".
      aria-label={[shown.title, shown.subtitle].filter(Boolean).join(" ")}
      inert={closing}
      style={{ width }}
      className={cn(
        // Floats over the page rather than displacing it: reflowing the whole
        // page to open a detail sheet moved the thing you had just clicked.
        // Anchored below the fixed chrome, with a left border and a shadow
        // doing the separating.
        "bg-background border-border fixed top-(--header-offset) right-0 bottom-0 z-30 flex flex-col border-l shadow-xl",
        closing ? "side-panel-exit" : "side-panel-enter",
      )}
      onKeyDown={(event) => {
        if (event.key === "Escape") closePanel();
      }}
    >
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
              <SidePanelKindHeaderAction descriptor={shown} />
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
