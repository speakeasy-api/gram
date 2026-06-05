import { SidebarMenu, SidebarMenuItem } from "@/components/ui/sidebar";
import { Collapsible, CollapsibleContent } from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";
import { AppRoute } from "@/routes";
import { motion } from "motion/react";
import React from "react";
import { Link } from "react-router";
import { ProductTierBadge } from "./product-tier-badge";
import { ReleaseStage, ReleaseStageBadge } from "./release-stage-badge";
import { Type } from "./ui/type";

export const NAV_LOADING_DURATION_MS = 600;

function NavMenu({
  items,
  className,
  children,
}: {
  items: AppRoute[];
  className?: string;
  children?: React.ReactNode;
}) {
  return (
    <SidebarMenu className={className}>
      {items.map((item) => (
        <SidebarMenuItem key={item.title}>
          <NavMenuButton item={item} />
        </SidebarMenuItem>
      ))}
      {children}
    </SidebarMenu>
  );
}

function NavMenuButton({ item }: { item: AppRoute }) {
  return (
    <NavButton
      title={item.title}
      href={item.href()}
      active={item.active}
      Icon={item.Icon}
      target={item.external ? "_blank" : undefined}
      stage={item.stage}
    />
  );
}

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
}) {
  const defaultsRef = React.useRef(new Set(defaultOpenGroups ?? []));
  const [openGroups, _setOpenGroups] = React.useState<Set<string>>(() => {
    const initial = new Set(defaultOpenGroups ?? []);
    if (activeGroup) initial.add(activeGroup);
    return initial;
  });
  const [hoveredItem, setHoveredItem] = React.useState<string | null>(null);
  const containerRef = React.useRef<HTMLDivElement>(null);
  const itemRefs = React.useRef<Map<string, HTMLElement>>(new Map());
  const [highlightRect, setHighlightRect] =
    React.useState<HighlightRect | null>(null);
  const suppressUntilRef = React.useRef(0);

  const toggleGroup = React.useCallback((group: string) => {
    suppressUntilRef.current = Date.now() + ACCORDION_DURATION;
    _setOpenGroups((prev) => {
      const next = new Set(prev);
      if (next.has(group)) {
        next.delete(group);
      } else {
        // Opening a non-default group collapses defaults
        if (!defaultsRef.current.has(group)) {
          for (const d of defaultsRef.current) next.delete(d);
        }
        next.add(group);
      }
      return next;
    });
  }, []);

  const openGroupFn = React.useCallback((group: string) => {
    suppressUntilRef.current = Date.now() + ACCORDION_DURATION;
    _setOpenGroups((prev) => {
      if (prev.has(group)) return prev;
      const next = new Set<string>();
      // Opening a non-default group collapses defaults
      if (!defaultsRef.current.has(group)) {
        next.add(group);
      } else {
        for (const g of prev) next.add(g);
        next.add(group);
      }
      return next;
    });
  }, []);

  React.useEffect(() => {
    defaultsRef.current = new Set(defaultOpenGroups ?? []);
  }, [defaultOpenGroups]);

  React.useEffect(() => {
    suppressUntilRef.current = Date.now() + ACCORDION_DURATION;
    if (activeGroup) {
      _setOpenGroups((prev) => {
        if (prev.has(activeGroup)) return prev;
        const next = new Set(prev);
        next.add(activeGroup);
        return next;
      });
    } else if (defaultsRef.current.size > 0) {
      _setOpenGroups(new Set(defaultsRef.current));
    }
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

  // Compute highlight position (with post-accordion delay)
  React.useEffect(() => {
    const remaining = suppressUntilRef.current - Date.now();
    if (remaining > 0) {
      const timer = setTimeout(computeRect, remaining);
      return () => clearTimeout(timer);
    }
    computeRect();
  }, [computeRect]);

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
            className="bg-card ring-border/50 pointer-events-none absolute rounded-lg ring-1"
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
}) {
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

  return (
    <div
      ref={navItem.ref}
      onMouseEnter={navItem.onMouseEnter}
      onMouseLeave={navItem.onMouseLeave}
      onMouseMove={navItem.onMouseMove}
      className="group-data-[collapsible=icon]:mx-auto group-data-[collapsible=icon]:w-fit"
    >
      <Link
        to={href ?? "#"}
        target={target}
        onClick={handleClick}
        className={cn(
          "relative z-1 flex w-full items-center gap-2 rounded-lg px-2 py-2 text-sm transition-colors hover:no-underline",
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
        <Type
          variant="small"
          className={cn(
            "transition-[opacity,transform] duration-150 ease-out group-data-[collapsible=icon]:hidden group-data-[collapsible=icon]:-translate-x-2 group-data-[collapsible=icon]:opacity-0",
            active && "font-semibold",
            isLoading && "nav-shimmer",
          )}
        >
          {titleNode ?? title}
        </Type>
        {title === "Billing" && <ProductTierBadge />}
        {stage && (
          <ReleaseStageBadge
            stage={stage}
            noTooltip
            className="group-data-[collapsible=icon]:hidden"
          />
        )}
      </Link>
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
}) {
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
          className="group-data-[collapsible=icon]:mx-auto group-data-[collapsible=icon]:w-fit"
        >
          <Link
            to={defaultHref ?? "#"}
            onClick={handleClick}
            className={cn(
              "relative z-1 flex w-full items-center gap-2 rounded-lg px-2 py-2 text-left text-sm transition-colors hover:no-underline",
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
            {stage && isOpen && (
              <ReleaseStageBadge
                stage={stage}
                noTooltip
                className="transition-opacity duration-150 ease-out group-data-[collapsible=icon]:hidden group-data-[collapsible=icon]:opacity-0"
              />
            )}
          </Link>
        </div>

        <CollapsibleContent className="data-[state=closed]:animate-accordion-up data-[state=open]:animate-accordion-down overflow-hidden">
          <div className="border-border mt-1 ml-4 border-l pl-2 group-data-[collapsible=icon]:hidden">
            <motion.ul
              className="flex flex-col gap-0.5 py-0.5"
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
  stage,
}: {
  item: AppRoute;
  stage?: ReleaseStage;
}) {
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
            "relative z-1 flex items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors hover:no-underline",
            item.active
              ? "text-foreground font-semibold"
              : "text-muted-foreground hover:text-foreground",
          )}
        >
          <span className={cn("truncate", isLoading && "nav-shimmer")}>
            {item.title}
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
