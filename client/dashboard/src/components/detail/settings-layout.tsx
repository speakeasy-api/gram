import { scrollWithinPage } from "@/lib/scroll-within-page";
import { cn } from "@/lib/utils";
import { useEffect, useState, type ReactNode } from "react";

export type SettingsGroup = {
  /** Anchor id for the group; must not reuse a section's own id. */
  id: string;
  title: string;
  /** The group's settings sections. A group with nothing to show is left out. */
  content: ReactNode;
};

/**
 * A settings tab laid out as areas: a sticky index on the left, and each
 * area's sections under an eyebrow heading on the right. The page frame owns
 * the gutter, so this adds no padding of its own.
 *
 * Pass the group ids to `useScrollToSettingsHash` so the index links, and any
 * deep link to an area, land on it.
 */
export function SettingsLayout({
  groups,
}: {
  groups: (SettingsGroup | false | null | undefined)[];
}): JSX.Element {
  const shown = groups.filter((group): group is SettingsGroup => !!group);
  const [activeId, setActiveId] = useActiveGroup(
    shown.map((group) => group.id),
  );

  return (
    <div className="grid gap-x-16 gap-y-8 pb-8 lg:grid-cols-[176px_minmax(0,1fr)]">
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
                    // A short last area may never reach the band; the click
                    // still says where the reader is going.
                    setActiveId(group.id);
                    scrollToGroup(group.id);
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
  );
}

/** The area at the top of the scroll container, for the index highlight. */
function useActiveGroup(
  ids: string[],
): [string | undefined, (id: string) => void] {
  const [activeId, setActiveId] = useState<string | undefined>(ids[0]);
  const key = ids.join(" ");

  useEffect(() => {
    const elements = key
      .split(" ")
      .map((id) => document.getElementById(id))
      .filter((element): element is HTMLElement => element !== null);
    const top = elements[0];
    if (!top || typeof IntersectionObserver === "undefined") return;

    const visible = new Set<string>();
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) visible.add(entry.target.id);
          else visible.delete(entry.target.id);
        }
        // The first area, in page order, that crosses the reading band.
        const first = elements.find((element) => visible.has(element.id));
        if (first) setActiveId(first.id);
      },
      // A band across the top of the scroll container, under the rail.
      {
        root: top.parentElement?.closest<HTMLElement>("[data-page-bound]"),
        rootMargin: "-32px 0px -60% 0px",
      },
    );
    elements.forEach((element) => observer.observe(element));
    return () => observer.disconnect();
  }, [key]);

  return [activeId, setActiveId];
}

/**
 * Scrolls to an area within the page's scroll container, so the pinned frame
 * stays put, and records it in the URL without a navigation.
 */
function scrollToGroup(id: string): void {
  const element = document.getElementById(id);
  if (!element) return;
  // Only the page's scroll container moves; scrolling any ancestor would
  // push the pinned header and tabs out of view.
  scrollWithinPage(element, {
    offset: parseFloat(getComputedStyle(element).scrollMarginTop) || 32,
  });
  window.history.replaceState(window.history.state, "", `#${id}`);
}
