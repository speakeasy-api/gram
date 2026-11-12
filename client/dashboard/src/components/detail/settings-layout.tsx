// oxlint-disable react/only-export-components -- the layout and the hook its sections report through belong together
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { scrollWithinPage } from "@/lib/scroll-within-page";
import { cn } from "@/lib/utils";
import {
  createContext,
  use,
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { UNSAFE_NavigationContext, useLocation } from "react-router";

export type SettingsGroup = {
  /** Anchor id for the group; must not reuse a section's own id. */
  id: string;
  title: string;
  /** The group's settings sections. A group with nothing to show is left out. */
  content: ReactNode;
};

/** How far below the scroll container's top an area lands; matches `scroll-mt-8`. */
const GROUP_SCROLL_MARGIN = 32;

/**
 * A settings tab laid out as areas: a sticky index on the left, and each
 * area's sections under an eyebrow heading on the right. The page frame owns
 * the gutter, so this adds no padding of its own.
 *
 * Pass the group ids to `useScrollToSettingsHash` so the index links, and any
 * deep link to an area, land on it.
 *
 * Sections report unsaved edits with `useReportUnsavedChanges`; leaving the
 * tab while any are dirty asks first.
 */
export function SettingsLayout({
  groups,
}: {
  groups: (SettingsGroup | false | null | undefined)[];
}): JSX.Element {
  const shown = groups.filter((group): group is SettingsGroup => !!group);
  const { activeId, rootRef, select } = useActiveGroup(
    shown.map((group) => group.id),
  );

  return (
    <UnsavedChangesGuard>
      {/* `relative` keeps visually hidden labels (absolutely positioned) inside
          the tab's scroll container. Without it they size the document, and a
          deep link's native hash scroll moves the window under the frame. */}
      <div
        ref={rootRef}
        className="relative grid gap-x-16 gap-y-8 pb-8 lg:grid-cols-[176px_minmax(0,1fr)]"
      >
        <nav
          aria-label="Settings areas"
          // Sticks inside the tab's scroll container, level with its top padding.
          className="sticky top-8 hidden self-start lg:block"
        >
          <ul className="border-border space-y-px border-l">
            {shown.map((group) => {
              const active = group.id === activeId;
              return (
                <li key={group.id}>
                  <a
                    href={`#${group.id}`}
                    aria-current={active ? "location" : undefined}
                    onClick={(event) => {
                      event.preventDefault();
                      select(group.id);
                    }}
                    className={cn(
                      "-ml-px block border-l py-1.5 pl-4 text-sm transition-colors hover:no-underline",
                      active
                        ? "border-foreground text-foreground font-medium"
                        : "text-muted-foreground hover:text-foreground border-transparent",
                    )}
                  >
                    {group.title}
                  </a>
                </li>
              );
            })}
          </ul>
        </nav>

        <div className="min-w-0">
          {shown.map((group) => (
            <section
              key={group.id}
              id={group.id}
              data-settings-group=""
              aria-labelledby={`${group.id}-title`}
              // The last area keeps a viewport of room below it, so the index can
              // bring even a short final area to the top.
              className="border-border scroll-mt-8 border-t pt-8 pb-14 first:border-t-0 first:pt-0 last:min-h-[calc(100vh-24rem)] last:pb-0"
            >
              {/* The sticky index names each area on wide screens, so the
                eyebrow only shows where the index is hidden. */}
              <h2
                id={`${group.id}-title`}
                className="text-eyebrow mb-6 lg:sr-only"
              >
                {group.title}
              </h2>
              <div className="space-y-10">{group.content}</div>
            </section>
          ))}
        </div>
      </div>
    </UnsavedChangesGuard>
  );
}

/**
 * The area the reader is in, for the index highlight: the last area whose top
 * has reached the line areas land on when scrolled to, or the last area once
 * the container is scrolled to the bottom. A clicked area holds the highlight
 * until its scroll settles, so the smooth scroll past earlier areas doesn't
 * flicker through them.
 */
function useActiveGroup(ids: string[]): {
  activeId: string | undefined;
  rootRef: React.RefObject<HTMLDivElement | null>;
  select: (id: string) => void;
} {
  const [activeId, setActiveId] = useState<string | undefined>(ids[0]);
  const rootRef = useRef<HTMLDivElement>(null);
  // The clicked area, held while its scroll runs.
  const heldRef = useRef<string | null>(null);
  const releaseRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const key = ids.join(" ");

  const measure = useCallback(() => {
    const root = rootRef.current;
    const scroller = root?.closest<HTMLElement>("[data-page-bound]");
    if (!root || !scroller) return;
    if (heldRef.current) {
      setActiveId(heldRef.current);
      return;
    }
    const sections = [
      ...root.querySelectorAll<HTMLElement>("[data-settings-group]"),
    ];
    if (sections.length === 0) return;
    const atBottom =
      scroller.scrollTop > 0 &&
      scroller.scrollTop + scroller.clientHeight >= scroller.scrollHeight - 2;
    if (atBottom) {
      setActiveId(sections[sections.length - 1]!.id);
      return;
    }
    // A pixel of slack: an area scrolled to its anchor sits exactly on the
    // line, and subpixel layout can leave it a fraction below.
    const line = scroller.getBoundingClientRect().top + GROUP_SCROLL_MARGIN + 1;
    let current = sections[0]!;
    for (const section of sections) {
      if (section.getBoundingClientRect().top <= line) current = section;
    }
    setActiveId(current.id);
  }, []);

  useEffect(() => {
    const scroller = rootRef.current?.closest<HTMLElement>("[data-page-bound]");
    if (!scroller) return;
    const settle = () => {
      if (releaseRef.current) clearTimeout(releaseRef.current);
      // Released once the scroll goes quiet; `scrollend` alone is missing in
      // some browsers and never fires when there was nothing to scroll.
      releaseRef.current = setTimeout(() => {
        heldRef.current = null;
        measure();
      }, 150);
    };
    const onScroll = () => {
      measure();
      if (heldRef.current) settle();
    };
    measure();
    scroller.addEventListener("scroll", onScroll, { passive: true });
    // A deep link or late-mounting section moves areas without a scroll.
    const resize = new ResizeObserver(() => measure());
    if (rootRef.current) resize.observe(rootRef.current);
    return () => {
      scroller.removeEventListener("scroll", onScroll);
      resize.disconnect();
      if (releaseRef.current) clearTimeout(releaseRef.current);
    };
  }, [key, measure]);

  const select = useCallback((id: string) => {
    const element = document.getElementById(id);
    if (!element) return;
    heldRef.current = id;
    setActiveId(id);
    // Only the page's scroll container moves; scrolling any ancestor would
    // push the pinned header and tabs out of view.
    scrollWithinPage(element, { offset: GROUP_SCROLL_MARGIN });
    window.history.replaceState(window.history.state, "", `#${id}`);
    // Holds even when there was nothing to scroll.
    if (releaseRef.current) clearTimeout(releaseRef.current);
    releaseRef.current = setTimeout(() => {
      heldRef.current = null;
    }, 1000);
  }, []);

  return { activeId, rootRef, select };
}

type ReportDirty = (key: string, dirty: boolean) => void;

const UnsavedChangesContext = createContext<ReportDirty | null>(null);

/**
 * Tells the settings tab this section holds unsaved edits, so leaving asks
 * first. Outside a `SettingsLayout` it does nothing.
 */
export function useReportUnsavedChanges(dirty: boolean): void {
  const report = use(UnsavedChangesContext);
  const key = useId();
  useEffect(() => {
    report?.(key, dirty);
  }, [report, key, dirty]);
  useEffect(() => () => report?.(key, false), [report, key]);
}

type Navigator = React.ContextType<
  typeof UNSAFE_NavigationContext
>["navigator"];

/**
 * Collects the sections' dirty flags and, while any is set, asks before the
 * tab is left: a link to another tab or page, or a reload or close.
 *
 * The app mounts a `BrowserRouter`, which has no `useBlocker`, so in-app
 * navigation is held by wrapping the router's `push`. Programmatic `replace`
 * navigations (following a renamed server, query-string state) and Back pass
 * through; the browser asks before a reload or close.
 */
function UnsavedChangesGuard({ children }: { children: ReactNode }) {
  const [dirtyKeys, setDirtyKeys] = useState<ReadonlySet<string>>(
    () => new Set(),
  );
  const report = useCallback<ReportDirty>((key, dirty) => {
    setDirtyKeys((current) => {
      if (current.has(key) === dirty) return current;
      const next = new Set(current);
      if (dirty) next.add(key);
      else next.delete(key);
      return next;
    });
  }, []);
  const dirty = dirtyKeys.size > 0;
  const { navigator } = use(UNSAFE_NavigationContext);
  const location = useLocation();
  const pathnameRef = useRef(location.pathname);
  pathnameRef.current = location.pathname;
  const [pendingLeave, setPendingLeave] = useState<(() => void) | null>(null);

  useEffect(() => {
    if (!dirty) return;
    const warn = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", warn);
    const restore = holdPushNavigations(
      navigator,
      () => pathnameRef.current,
      (leave) => setPendingLeave(() => leave),
    );
    return () => {
      window.removeEventListener("beforeunload", warn);
      restore();
    };
  }, [dirty, navigator]);

  return (
    <UnsavedChangesContext.Provider value={report}>
      {children}
      <ConfirmDialog
        open={pendingLeave !== null}
        onOpenChange={(open) => {
          if (!open) setPendingLeave(null);
        }}
        title="Discard unsaved changes?"
        description="You have changes on this tab that aren't saved. Leaving now discards them."
        cancelLabel="Keep editing"
        confirmLabel="Discard"
        onConfirm={() => {
          const leave = pendingLeave;
          setPendingLeave(null);
          leave?.();
        }}
      />
    </UnsavedChangesContext.Provider>
  );
}

/**
 * Routes `push` navigations that leave the current path through `onHold`,
 * which gets a callback that completes the navigation. Same-path pushes (a
 * hash link to another section) go straight through. Returns the restore.
 */
function holdPushNavigations(
  navigator: Navigator,
  currentPathname: () => string,
  onHold: (leave: () => void) => void,
): () => void {
  // The own property, so the restore puts back exactly what was there.
  const original = Object.getOwnPropertyDescriptor(navigator, "push");
  const push = navigator.push.bind(navigator);
  navigator.push = (to, state, opts) => {
    const pathname =
      typeof to === "string"
        ? new URL(to, "http://local" + currentPathname()).pathname
        : (to.pathname ?? currentPathname());
    if (pathname === currentPathname()) {
      push(to, state, opts);
      return;
    }
    onHold(() => push(to, state, opts));
  };
  return () => {
    if (original) Object.defineProperty(navigator, "push", original);
    else delete (navigator as Partial<Navigator>).push;
  };
}
