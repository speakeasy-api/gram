// oxlint-disable react/only-export-components -- the layout and the hook its sections report through belong together
import { ReadinessIssueNotice } from "./readiness-issues";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { scrollWithinPage } from "@/lib/scroll-within-page";
import { cn } from "@/lib/utils";
import {
  createContext,
  use,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
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

/** How far below the scroll container's top a section lands; matches `scroll-mt-8`. */
const SECTION_SCROLL_MARGIN = 32;

type SectionHeading = {
  key: string;
  groupId: string;
  title: string;
  element: HTMLElement;
};

type ReportHeading = (key: string, heading: SectionHeading | null) => void;

const SettingsGroupContext = createContext<{
  groupId: string;
  report: ReportHeading;
} | null>(null);

/**
 * Lists a section in the settings tab's sticky index under `title`, which
 * must be the heading the section shows. `element` is the section's top,
 * where its index link lands. Outside a `SettingsLayout` it does nothing.
 */
export function useSettingsSectionHeading(
  title: string,
  element: HTMLElement | null,
): void {
  const group = use(SettingsGroupContext);
  const key = useId();
  useLayoutEffect(() => {
    if (!group || !element || !title) return;
    group.report(key, { key, groupId: group.groupId, title, element });
    return () => group.report(key, null);
  }, [group, key, title, element]);
}

type IndexLink = {
  key: string;
  title: string;
  href: string;
  target: () => HTMLElement | null;
};

type IndexArea = {
  group: SettingsGroup;
  /** How many section headings the area shows. */
  headingCount: number;
  links: IndexLink[];
};

/**
 * A settings tab laid out as areas: a sticky index on the left, and each
 * area's sections on the right. The index is built from the section headings
 * that render, so every entry reads exactly as a heading on the page. An
 * area with several sections shows its name as an eyebrow, on the page and
 * in the index; an area with one section lets that section's heading speak
 * for it. The page frame owns the gutter, so this adds no padding of its own.
 *
 * Sections join the index with `useSettingsSectionHeading` (`SettingsSection`
 * titles do this already). Pass the group ids to `useScrollToSettingsHash` so
 * deep links to an area land on it.
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
  const [headings, setHeadings] = useState<readonly SectionHeading[]>([]);
  const report = useCallback<ReportHeading>((key, heading) => {
    setHeadings((current) => {
      const previous = current.find((entry) => entry.key === key);
      if (!heading) {
        return previous
          ? current.filter((entry) => entry.key !== key)
          : current;
      }
      if (
        previous?.title === heading.title &&
        previous.element === heading.element
      ) {
        return current;
      }
      // Page order, whatever order the sections mounted in.
      return [...current.filter((entry) => entry.key !== key), heading].sort(
        (a, b) =>
          a.element.compareDocumentPosition(b.element) &
          Node.DOCUMENT_POSITION_FOLLOWING
            ? -1
            : 1,
      );
    });
  }, []);

  const areas = shown.map((group): IndexArea => {
    const titled = headings.filter((heading) => heading.groupId === group.id);
    // An area's first link lands on the area itself, so its eyebrow shows.
    const area = {
      href: `#${group.id}`,
      target: () => document.getElementById(group.id),
    };
    return {
      group,
      headingCount: titled.length,
      links:
        titled.length === 0
          ? [{ key: group.id, title: group.title, ...area }]
          : titled.map((heading, index) =>
              index === 0
                ? { key: heading.key, title: heading.title, ...area }
                : {
                    key: heading.key,
                    title: heading.title,
                    // The nearest anchor: the section's own id, or one a
                    // wrapper gives it, else the area.
                    href: `#${heading.element.closest("[id]")?.id ?? group.id}`,
                    target: () => heading.element,
                  },
            ),
    };
  });
  const links = areas.flatMap((area) => area.links);
  const { activeKey, rootRef, select } = useActiveSection(links);

  const renderLink = (link: IndexLink) => {
    const active = link.key === activeKey;
    return (
      <li key={link.key}>
        <a
          href={link.href}
          aria-current={active ? "location" : undefined}
          onClick={(event) => {
            event.preventDefault();
            select(link);
          }}
          className={cn(
            "-ml-px block border-l py-1 pl-4 text-sm transition-colors hover:no-underline",
            active
              ? "border-foreground text-foreground font-medium"
              : "text-muted-foreground hover:text-foreground border-transparent",
          )}
        >
          {link.title}
        </a>
      </li>
    );
  };

  return (
    <UnsavedChangesGuard>
      {/* `relative` keeps visually hidden labels (absolutely positioned) inside
          the tab's scroll container. Without it they size the document, and a
          deep link's native hash scroll moves the window under the frame. */}
      <div
        ref={rootRef}
        className="relative grid gap-x-16 gap-y-8 pb-8 lg:-mb-32 lg:grid-cols-[176px_minmax(0,1fr)]"
      >
        <nav
          aria-label="Settings sections"
          // Sticks inside the tab's scroll container, level with its top padding.
          className="sticky top-8 hidden self-start lg:block"
        >
          <ul className="border-border space-y-px border-l">
            {areas.map((area) =>
              area.links.length > 1 ? (
                <li key={area.group.id} className="py-1.5 first:pt-0">
                  <span className="text-eyebrow block pb-1 pl-4">
                    {area.group.title}
                  </span>
                  <ul className="space-y-px">{area.links.map(renderLink)}</ul>
                </li>
              ) : (
                renderLink(area.links[0]!)
              ),
            )}
          </ul>
        </nav>

        {/* The frame's bottom padding (`pb-32` in McpDetailFrame) is pulled
            into the content column, so the index's row runs to the bottom of
            the scroll container and a long index never scrolls out under the
            tabs at the end of the page. */}
        <div className="min-w-0 lg:pb-32">
          {areas.map(({ group, headingCount }) => (
            <SettingsGroupSection
              key={group.id}
              group={group}
              report={report}
              // A lone section's heading names the area in the index, so the
              // eyebrow only shows where the index is hidden.
              labelHiddenOnWide={headingCount === 1}
            />
          ))}
        </div>
      </div>
    </UnsavedChangesGuard>
  );
}

function SettingsGroupSection({
  group,
  report,
  labelHiddenOnWide,
}: {
  group: SettingsGroup;
  report: ReportHeading;
  labelHiddenOnWide: boolean;
}): JSX.Element {
  const context = useMemo(
    () => ({ groupId: group.id, report }),
    [group.id, report],
  );
  return (
    <section
      id={group.id}
      aria-labelledby={`${group.id}-title`}
      // The last area keeps a viewport of room below it, so the index can
      // bring even a short final area to the top.
      className="border-border scroll-mt-8 border-t pt-8 pb-14 first:border-t-0 first:pt-0 last:min-h-[calc(100vh-24rem)] last:pb-0"
    >
      <h2
        id={`${group.id}-title`}
        className={cn("text-eyebrow mb-6", labelHiddenOnWide && "lg:sr-only")}
      >
        {group.title}
      </h2>
      <SettingsGroupContext.Provider value={context}>
        <div className="mb-6 empty:hidden">
          <ReadinessIssueNotice anchorId={group.id} />
        </div>
        <div className="space-y-10">{group.content}</div>
      </SettingsGroupContext.Provider>
    </section>
  );
}

/**
 * The section the reader is in, for the index highlight: the last section
 * whose top has reached the line sections land on when scrolled to, or the
 * last section once the container is scrolled to the bottom. A clicked
 * section holds the highlight until its scroll settles, so the smooth scroll
 * past earlier sections doesn't flicker through them.
 */
function useActiveSection(links: IndexLink[]): {
  activeKey: string | undefined;
  rootRef: React.RefObject<HTMLDivElement | null>;
  select: (link: IndexLink) => void;
} {
  const [activeKey, setActiveKey] = useState<string | undefined>(links[0]?.key);
  const rootRef = useRef<HTMLDivElement>(null);
  const linksRef = useRef(links);
  linksRef.current = links;
  // The clicked section, held while its scroll runs.
  const heldRef = useRef<string | null>(null);
  const releaseRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const key = links.map((link) => link.key).join(" ");

  const measure = useCallback(() => {
    const scroller = rootRef.current?.closest<HTMLElement>("[data-page-bound]");
    if (!scroller) return;
    if (heldRef.current) {
      setActiveKey(heldRef.current);
      return;
    }
    const targets = linksRef.current.flatMap((link) => {
      const element = link.target();
      return element ? [{ key: link.key, element }] : [];
    });
    if (targets.length === 0) return;
    const atBottom =
      scroller.scrollTop > 0 &&
      scroller.scrollTop + scroller.clientHeight >= scroller.scrollHeight - 2;
    if (atBottom) {
      setActiveKey(targets[targets.length - 1]!.key);
      return;
    }
    // A pixel of slack: a section scrolled to its anchor sits exactly on the
    // line, and subpixel layout can leave it a fraction below.
    const line =
      scroller.getBoundingClientRect().top + SECTION_SCROLL_MARGIN + 1;
    let current = targets[0]!;
    for (const target of targets) {
      if (target.element.getBoundingClientRect().top <= line) current = target;
    }
    setActiveKey(current.key);
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
    // A deep link or late-mounting section moves sections without a scroll.
    const resize = new ResizeObserver(() => measure());
    if (rootRef.current) resize.observe(rootRef.current);
    return () => {
      scroller.removeEventListener("scroll", onScroll);
      resize.disconnect();
      if (releaseRef.current) clearTimeout(releaseRef.current);
    };
  }, [key, measure]);

  const select = useCallback((link: IndexLink) => {
    const element = link.target();
    if (!element) return;
    heldRef.current = link.key;
    setActiveKey(link.key);
    // Only the page's scroll container moves; scrolling any ancestor would
    // push the pinned header and tabs out of view.
    scrollWithinPage(element, { offset: SECTION_SCROLL_MARGIN });
    window.history.replaceState(window.history.state, "", link.href);
    // Holds even when there was nothing to scroll.
    if (releaseRef.current) clearTimeout(releaseRef.current);
    releaseRef.current = setTimeout(() => {
      heldRef.current = null;
    }, 1000);
  }, []);

  return { activeKey, rootRef, select };
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
