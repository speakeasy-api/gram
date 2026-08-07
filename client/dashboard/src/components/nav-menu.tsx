import { SidebarMenuItem } from "@/components/ui/Sidebar";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { cn } from "@/lib/utils";
import { AppRoute } from "@/routes";
import { ChevronRightIcon } from "lucide-react";
import { motion } from "motion/react";
import React from "react";
import { Link } from "react-router";
import { ProductTierBadge } from "./product-tier-badge";
import { ReleaseStage, ReleaseStageBadge } from "./release-stage-badge";
import { Tooltip, TooltipContent, TooltipTrigger } from "./ui/Tooltip";
import { Text } from "@/components/ui/Text";

export const NAV_LOADING_DURATION_MS = 600;

// ---------------------------------------------------------------------------
// Sliding highlight context
// ---------------------------------------------------------------------------

type HighlightRect = {
  top: number;
  left: number;
  width: number;
  height: number;
};

type NavContextValue = {
  openGroups: Set<string>;
  toggleGroup: (group: string) => void;
  openGroup: (group: string) => void;
  hoveredItem: string | null;
  setHoveredItem: (item: string | null) => void;
  activeItem: string | null;
  registerRef: (id: string, el: HTMLElement | null) => void;
  containerRef: React.RefObject<HTMLDivElement | null>;
};

const NavGroupContext = React.createContext<NavContextValue>({
  openGroups: new Set(),
  toggleGroup: () => {},
  openGroup: () => {},
  hoveredItem: null,
  setHoveredItem: () => {},
  activeItem: null,
  registerRef: () => {},
  containerRef: { current: null },
});

const ACCORDION_DURATION = 250;

export function NavGroupProvider({
  activeGroup,
  defaultOpenGroups,
  activeItem,
  children,
}: {
  activeGroup?: string;
  defaultOpenGroups?: string[];
  activeItem?: string;
  children: React.ReactNode;
}): React.JSX.Element {
  const defaultsRef = React.useRef(new Set(defaultOpenGroups ?? []));
  // `open` is what renders; `explicit` tracks groups the user expanded via the
  // chevron toggle — those survive navigation, while a group that is open only
  // because it holds the active route collapses on navigating away. The two
  // sets live in one state object so every updater derives them together,
  // keeping the state-updater functions pure.
  const [groupsState, _setGroupsState] = React.useState<{
    open: Set<string>;
    explicit: Set<string>;
  }>(() => {
    const initial = new Set(defaultOpenGroups ?? []);
    if (activeGroup) initial.add(activeGroup);
    return { open: initial, explicit: new Set() };
  });
  const openGroups = groupsState.open;
  const [hoveredItem, setHoveredItem] = React.useState<string | null>(null);
  const containerRef = React.useRef<HTMLDivElement>(null);
  const itemRefs = React.useRef<Map<string, HTMLElement>>(new Map());
  const [highlightRect, setHighlightRect] =
    React.useState<HighlightRect | null>(null);
  const suppressUntilRef = React.useRef(0);

  const toggleGroup = React.useCallback((group: string) => {
    suppressUntilRef.current = Date.now() + ACCORDION_DURATION;
    _setGroupsState(({ open, explicit }) => {
      const nextOpen = new Set(open);
      const nextExplicit = new Set(explicit);
      if (nextOpen.has(group)) {
        nextOpen.delete(group);
        nextExplicit.delete(group);
      } else {
        // Opening a non-default group collapses defaults
        if (!defaultsRef.current.has(group)) {
          for (const d of defaultsRef.current) {
            if (!nextExplicit.has(d)) nextOpen.delete(d);
          }
        }
        nextOpen.add(group);
        nextExplicit.add(group);
      }
      return { open: nextOpen, explicit: nextExplicit };
    });
  }, []);

  // Opens a group without marking it explicit — used when a closed group's
  // header link is clicked, where the navigation is about to make the group
  // active anyway.
  const openGroupFn = React.useCallback((group: string) => {
    suppressUntilRef.current = Date.now() + ACCORDION_DURATION;
    _setGroupsState((prev) => {
      if (prev.open.has(group)) return prev;
      const nextOpen = new Set(prev.open);
      // Opening a non-default group collapses defaults
      if (!defaultsRef.current.has(group)) {
        for (const d of defaultsRef.current) {
          if (!prev.explicit.has(d)) nextOpen.delete(d);
        }
      }
      nextOpen.add(group);
      return { open: nextOpen, explicit: prev.explicit };
    });
  }, []);

  React.useEffect(() => {
    defaultsRef.current = new Set(defaultOpenGroups ?? []);
  }, [defaultOpenGroups]);

  React.useEffect(() => {
    suppressUntilRef.current = Date.now() + ACCORDION_DURATION;
    _setGroupsState(({ open, explicit }) => {
      if (!activeGroup && defaultsRef.current.size > 0) {
        // Sidebars with a default-open set reset to it on top-level pages.
        return {
          open: new Set([...defaultsRef.current, ...explicit]),
          explicit,
        };
      }
      // Only defaults and explicitly expanded groups stay open across
      // navigation; the previously active group collapses.
      const nextOpen = new Set(
        [...open].filter((g) => defaultsRef.current.has(g) || explicit.has(g)),
      );
      if (activeGroup) nextOpen.add(activeGroup);
      return { open: nextOpen, explicit };
    });
  }, [activeGroup]);

  const resolvedActive = activeItem ?? activeGroup ?? null;
  const target = hoveredItem ?? resolvedActive;

  const computeRect = React.useCallback(() => {
    if (!target || !containerRef.current) {
      setHighlightRect(null);
      return;
    }
    const el = itemRefs.current.get(target);
    if (!el) {
      setHighlightRect(null);
      return;
    }
    const containerRect = containerRef.current.getBoundingClientRect();
    const elRect = el.getBoundingClientRect();
    setHighlightRect({
      top: elRect.top - containerRect.top,
      left: elRect.left - containerRect.left,
      width: elRect.width,
      height: elRect.height,
    });
  }, [target]);

  // Compute highlight position (with post-accordion delay). openGroups is a
  // dependency so toggling a group re-evaluates the highlight once the
  // accordion settles — collapsing the active group hides its highlighted row,
  // and the suppressed ResizeObserver below won't fire again after the window.
  React.useEffect(() => {
    const remaining = suppressUntilRef.current - Date.now();
    if (remaining > 0) {
      const timer = setTimeout(computeRect, remaining);
      return () => clearTimeout(timer);
    }
    computeRect();
  }, [computeRect, openGroups]);

  // Recompute on layout changes, but skip during accordion
  React.useEffect(() => {
    if (!containerRef.current) return;

    const observer = new ResizeObserver(() => {
      if (Date.now() < suppressUntilRef.current) return;
      computeRect();
    });

    observer.observe(containerRef.current);
    return () => observer.disconnect();
  }, [computeRect]);

  const registerRef = React.useCallback(
    (id: string, el: HTMLElement | null) => {
      if (el) {
        itemRefs.current.set(id, el);
      } else {
        itemRefs.current.delete(id);
      }
    },
    [],
  );

  const value = React.useMemo<NavContextValue>(
    () => ({
      openGroups,
      toggleGroup,
      openGroup: openGroupFn,
      hoveredItem,
      setHoveredItem,
      activeItem: resolvedActive,
      registerRef,
      containerRef,
    }),
    [
      openGroups,
      toggleGroup,
      openGroupFn,
      hoveredItem,
      resolvedActive,
      registerRef,
    ],
  );

  return (
    <NavGroupContext.Provider value={value}>
      <div
        ref={containerRef}
        className="relative"
        onMouseLeave={() => setHoveredItem(null)}
      >
        {highlightRect && (
          <motion.div
            className="bg-card border-border pointer-events-none absolute border"
            animate={{
              top: highlightRect.top,
              left: highlightRect.left,
              width: highlightRect.width,
              height: highlightRect.height,
            }}
            transition={{
              duration: 0.25,
              ease: [0.4, 0, 0.2, 1],
            }}
          />
        )}
        {children}
      </div>
    </NavGroupContext.Provider>
  );
}

// ---------------------------------------------------------------------------
// Hook for registering item ref + hover handlers
// ---------------------------------------------------------------------------

// Settle-based hover intent — fires setHoveredItem only when the mouse has
// genuinely paused on an element, filtering out fast drive-by movements.
//
// Every SETTLE_INTERVAL_MS the interval compares current vs previous mouse Y.
// If the delta is below SETTLE_THRESHOLD_PX the mouse is considered settled.
// While the mouse is still moving (dy !== 0) an additional CENTER_ZONE guard
// rejects triggers near the top/bottom edge, preventing false fires when
// passing through item boundaries at low speed. Once the mouse is fully stopped
// (dy === 0) the edge guard is skipped so a mouse parked at any position
// within the element still triggers correctly.
//
// hoveredItem is cleared at the container level (onMouseLeave on the wrapper
// div), not here, so the highlight stays on the last item while moving between
// items rather than snapping back to the active route on every item exit.
const SETTLE_INTERVAL_MS = 50; // ms between settle checks; lower = more responsive
const SETTLE_THRESHOLD_PX = 4; // max Y movement per interval to count as settled
const CENTER_ZONE = 0.3; // fraction of height from each edge excluded while moving

function useNavItem(id: string) {
  const { registerRef, setHoveredItem } = React.useContext(NavGroupContext);
  const elRef = React.useRef<HTMLElement | null>(null);
  const ref = React.useCallback(
    (el: HTMLElement | null) => {
      elRef.current = el;
      registerRef(id, el);
    },
    [id, registerRef],
  );

  const stateRef = React.useRef({
    intervalId: null as ReturnType<typeof setInterval> | null,
    prevX: 0,
    prevY: 0,
    curX: 0,
    curY: 0,
  });

  const onMouseMove = React.useCallback((e: React.MouseEvent) => {
    stateRef.current.curX = e.clientX;
    stateRef.current.curY = e.clientY;
  }, []);

  const onMouseEnter = React.useCallback(
    (e: React.MouseEvent) => {
      const s = stateRef.current;
      s.prevX = e.clientX;
      s.prevY = e.clientY;
      s.curX = e.clientX;
      s.curY = e.clientY;

      if (s.intervalId) clearInterval(s.intervalId);
      s.intervalId = setInterval(() => {
        const dy = s.curY - s.prevY;
        if (Math.abs(dy) < SETTLE_THRESHOLD_PX) {
          // Only apply edge exclusion while mouse is still moving — a stopped
          // mouse at the edge should still trigger.
          if (dy !== 0) {
            const el = elRef.current;
            if (el) {
              const rect = el.getBoundingClientRect();
              const margin = rect.height * CENTER_ZONE;
              if (s.curY < rect.top + margin || s.curY > rect.bottom - margin) {
                s.prevY = s.curY;
                return; // too close to edge — keep waiting
              }
            }
          }
          if (s.intervalId) clearInterval(s.intervalId);
          s.intervalId = null;
          setHoveredItem(id);
        }
        s.prevX = s.curX;
        s.prevY = s.curY;
      }, SETTLE_INTERVAL_MS);
    },
    [id, setHoveredItem],
  );

  const onMouseLeave = React.useCallback(() => {
    const s = stateRef.current;
    if (s.intervalId) {
      clearInterval(s.intervalId);
      s.intervalId = null;
    }
  }, []);

  React.useEffect(() => {
    const s = stateRef.current;
    return () => {
      if (s.intervalId) clearInterval(s.intervalId);
    };
  }, []);

  return { ref, onMouseEnter, onMouseLeave, onMouseMove };
}

// ---------------------------------------------------------------------------
// Top-level nav button (Home, Settings, etc.)
// ---------------------------------------------------------------------------

export function NavButton({
  id,
  title,
  titleNode,
  href,
  target,
  active,
  Icon,
  onClick,
  stage,
  tooltip,
}: {
  id?: string;
  title: string;
  titleNode?: React.ReactNode;
  href?: string;
  target?: string;
  onClick?: () => void;
  active?: boolean;
  Icon?: React.ComponentType<{ className?: string }>;
  stage?: ReleaseStage;
  // When set, wraps the actual focusable <Link> (not a wrapper div) in a
  // tooltip — e.g. the collapsed sidebar, where only the icon shows and the
  // title needs to reach keyboard/screen-reader users via the link itself.
  tooltip?: React.ReactNode;
}): React.JSX.Element {
  const itemId = id ?? title;
  const navItem = useNavItem(itemId);
  const [isLoading, setIsLoading] = React.useState(false);
  const timeoutRef = React.useRef<ReturnType<typeof setTimeout> | null>(null);

  React.useEffect(() => {
    return () => {
      if (timeoutRef.current) clearTimeout(timeoutRef.current);
    };
  }, []);

  const handleClick = () => {
    onClick?.();
    if (target === "_blank") return;
    setIsLoading(true);
    if (timeoutRef.current) clearTimeout(timeoutRef.current);
    timeoutRef.current = setTimeout(
      () => setIsLoading(false),
      NAV_LOADING_DURATION_MS,
    );
  };

  const link = (
    <Link
      to={href ?? "#"}
      target={target}
      onClick={handleClick}
      className={cn(
        "relative z-1 flex w-full items-center gap-2 px-2 py-1.5 text-sm transition-colors hover:no-underline",
        "group-data-[collapsible=icon]:min-w-8 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:gap-0 group-data-[collapsible=icon]:p-2!",
        active
          ? "text-foreground font-semibold"
          : "text-muted-foreground hover:text-foreground font-medium",
      )}
    >
      {Icon && (
        <Icon
          className={cn(
            "trans size-4 shrink-0",
            active ? "text-foreground" : "text-muted-foreground",
          )}
        />
      )}
      <Text
        variant="small"
        className={cn(
          "transition-[opacity,transform] duration-150 ease-out group-data-[collapsible=icon]:hidden group-data-[collapsible=icon]:-translate-x-2 group-data-[collapsible=icon]:opacity-0",
          active && "font-semibold",
          isLoading && "nav-shimmer",
        )}
      >
        {titleNode ?? title}
      </Text>
      {title === "Billing" && <ProductTierBadge />}
      {stage && (
        <ReleaseStageBadge
          stage={stage}
          noTooltip
          className="group-data-[collapsible=icon]:hidden"
        />
      )}
    </Link>
  );

  return (
    <div
      ref={navItem.ref}
      onMouseEnter={navItem.onMouseEnter}
      onMouseLeave={navItem.onMouseLeave}
      onMouseMove={navItem.onMouseMove}
      className="group-data-[collapsible=icon]:mx-auto group-data-[collapsible=icon]:w-fit"
    >
      {tooltip ? (
        <Tooltip>
          <TooltipTrigger asChild>{link}</TooltipTrigger>
          <TooltipContent side="right" sideOffset={4}>
            {tooltip}
          </TooltipContent>
        </Tooltip>
      ) : (
        link
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Collapsible nav group (Connect, Build, etc.)
// ---------------------------------------------------------------------------

export function CollapsibleNavGroup({
  label,
  Icon,
  defaultHref,
  stage,
  children,
}: {
  label: string;
  Icon: React.ComponentType<{ className?: string }>;
  defaultHref?: string;
  stage?: ReleaseStage;
  children: React.ReactNode;
}): React.JSX.Element {
  const { openGroups, toggleGroup, openGroup } =
    React.useContext(NavGroupContext);
  const navItem = useNavItem(label);
  const isOpen = openGroups.has(label);

  const handleClick = () => {
    if (!isOpen) {
      openGroup(label);
    }
  };

  return (
    <Collapsible open={isOpen} onOpenChange={() => toggleGroup(label)}>
      <SidebarMenuItem>
        <div
          ref={navItem.ref}
          onMouseEnter={navItem.onMouseEnter}
          onMouseLeave={navItem.onMouseLeave}
          onMouseMove={navItem.onMouseMove}
          className="relative group-data-[collapsible=icon]:mx-auto group-data-[collapsible=icon]:w-fit"
        >
          <Link
            to={defaultHref ?? "#"}
            onClick={handleClick}
            className={cn(
              "relative z-1 flex w-full items-center gap-2 px-2 py-1.5 pr-7 text-left text-sm transition-colors hover:no-underline",
              "group-data-[collapsible=icon]:min-w-8 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:gap-0 group-data-[collapsible=icon]:p-2!",
              "cursor-pointer outline-hidden",
              isOpen
                ? "text-foreground font-semibold"
                : "text-muted-foreground hover:text-foreground font-medium",
            )}
          >
            <Icon
              className={cn(
                "size-4 shrink-0 transition-colors",
                isOpen ? "text-foreground" : "text-muted-foreground",
              )}
            />
            <span className="flex-1 truncate transition-[opacity,transform] duration-150 ease-out group-data-[collapsible=icon]:hidden group-data-[collapsible=icon]:-translate-x-2 group-data-[collapsible=icon]:opacity-0">
              {label}
            </span>
            {stage && (
              <ReleaseStageBadge
                stage={stage}
                noTooltip
                className="transition-opacity duration-150 ease-out group-data-[collapsible=icon]:hidden group-data-[collapsible=icon]:opacity-0"
              />
            )}
          </Link>
          <CollapsibleTrigger asChild>
            <button
              type="button"
              aria-label={isOpen ? `Collapse ${label}` : `Expand ${label}`}
              className="text-muted-foreground hover:text-foreground absolute top-1/2 right-1 z-1 flex size-5 -translate-y-1/2 cursor-pointer items-center justify-center transition-colors group-data-[collapsible=icon]:hidden"
            >
              <ChevronRightIcon
                className={cn(
                  "size-3.5 transition-transform duration-200",
                  isOpen && "rotate-90",
                )}
              />
            </button>
          </CollapsibleTrigger>
        </div>

        <CollapsibleContent className="data-[state=closed]:animate-accordion-up data-[state=open]:animate-accordion-down overflow-hidden">
          <div className="border-border mt-0.5 ml-4 border-l pl-2 group-data-[collapsible=icon]:hidden">
            <motion.ul
              className="flex flex-col py-0.5"
              initial={isOpen ? "open" : "closed"}
              animate={isOpen ? "open" : "closed"}
              variants={{
                open: {
                  transition: { staggerChildren: 0.04, delayChildren: 0.05 },
                },
                closed: {
                  transition: { staggerChildren: 0.02, staggerDirection: -1 },
                },
              }}
            >
              {children}
            </motion.ul>
          </div>
        </CollapsibleContent>
      </SidebarMenuItem>
    </Collapsible>
  );
}

// ---------------------------------------------------------------------------
// Sub-item inside a CollapsibleNavGroup
// ---------------------------------------------------------------------------

export function CollapsibleNavItem({
  item,
  label,
  stage,
}: {
  item: AppRoute;
  // Display text override, when the group header already supplies the context
  // the route title spells out (e.g. "Remote Identity Providers" under a
  // "Platform Admin" header). Only the label changes: `useNavItem` still keys
  // on the route title, which must stay unique across the sidebar because
  // `registerRef` stores one element per id — two items sharing a title would
  // overwrite each other and move the hover/active highlight to the wrong row.
  // The title also remains what Recents and the command palette show, where
  // there is no group header to disambiguate.
  label?: string;
  stage?: ReleaseStage;
}): React.JSX.Element {
  const navItem = useNavItem(item.title);
  const [isLoading, setIsLoading] = React.useState(false);
  const timeoutRef = React.useRef<ReturnType<typeof setTimeout> | null>(null);

  React.useEffect(() => {
    return () => {
      if (timeoutRef.current) clearTimeout(timeoutRef.current);
    };
  }, []);

  const handleClick = () => {
    setIsLoading(true);
    if (timeoutRef.current) clearTimeout(timeoutRef.current);
    timeoutRef.current = setTimeout(
      () => setIsLoading(false),
      NAV_LOADING_DURATION_MS,
    );
  };

  return (
    <motion.li
      data-sidebar="menu-item"
      variants={{
        open: {
          opacity: 1,
          y: 0,
          transition: { duration: 0.15, ease: [0.4, 0, 0.2, 1] },
        },
        closed: { opacity: 0, y: -4, transition: { duration: 0.1 } },
      }}
    >
      <div
        ref={navItem.ref}
        onMouseEnter={navItem.onMouseEnter}
        onMouseLeave={navItem.onMouseLeave}
        onMouseMove={navItem.onMouseMove}
      >
        <Link
          to={item.href()}
          onClick={handleClick}
          className={cn(
            "relative z-1 flex items-center gap-2 px-2 py-1 text-sm transition-colors hover:no-underline",
            item.active
              ? "text-foreground font-semibold"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          <span className={cn("truncate", isLoading && "nav-shimmer")}>
            {label ?? item.title}
          </span>
          {item.title === "Billing" && <ProductTierBadge />}
          {(stage ?? item.stage) && (
            <ReleaseStageBadge stage={(stage ?? item.stage)!} noTooltip />
          )}
        </Link>
      </div>
    </motion.li>
  );
}
