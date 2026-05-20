import { SidebarMenu, SidebarMenuItem } from "@/components/ui/sidebar";
import { Collapsible, CollapsibleContent } from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";
import { AppRoute } from "@/routes";
import { Loader2 } from "lucide-react";
import { motion } from "motion/react";
import React from "react";
import { Link } from "react-router";
import { ProductTierBadge } from "./product-tier-badge";
import { ReleaseStage, ReleaseStageBadge } from "./release-stage-badge";
import { Type } from "./ui/type";

const NAV_LOADING_DURATION_MS = 600;

export function NavMenu({
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
  openGroup: string | null;
  setOpenGroup: (group: string | null) => void;
  hoveredItem: string | null;
  setHoveredItem: (item: string | null) => void;
  activeItem: string | null;
  registerRef: (id: string, el: HTMLElement | null) => void;
  containerRef: React.RefObject<HTMLDivElement | null>;
};

const NavGroupContext = React.createContext<NavContextValue>({
  openGroup: null,
  setOpenGroup: () => {},
  hoveredItem: null,
  setHoveredItem: () => {},
  activeItem: null,
  registerRef: () => {},
  containerRef: { current: null },
});

export function NavGroupProvider({
  activeGroup,
  activeItem,
  children,
}: {
  activeGroup?: string;
  activeItem?: string;
  children: React.ReactNode;
}) {
  const [openGroup, _setOpenGroup] = React.useState<string | null>(
    activeGroup ?? null,
  );
  const [hoveredItem, setHoveredItem] = React.useState<string | null>(null);
  const containerRef = React.useRef<HTMLDivElement>(null);
  const itemRefs = React.useRef<Map<string, HTMLElement>>(new Map());
  const [highlightRect, setHighlightRect] =
    React.useState<HighlightRect | null>(null);
  const suppressUntilRef = React.useRef(0);

  // Accordion animation takes ~200ms — suppress highlight recomputation
  // during that window to avoid jitter on shifting sub-items.
  const ACCORDION_DURATION = 250;

  const setOpenGroup = React.useCallback((group: string | null) => {
    suppressUntilRef.current = Date.now() + ACCORDION_DURATION;
    _setOpenGroup(group);
  }, []);

  React.useEffect(() => {
    suppressUntilRef.current = Date.now() + ACCORDION_DURATION;
    _setOpenGroup(activeGroup ?? null);
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
      openGroup,
      setOpenGroup,
      hoveredItem,
      setHoveredItem,
      activeItem: resolvedActive,
      registerRef,
      containerRef,
    }),
    [openGroup, setOpenGroup, hoveredItem, resolvedActive, registerRef],
  );

  return (
    <NavGroupContext.Provider value={value}>
      <div ref={containerRef} className="relative">
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
              type: "spring",
              bounce: 0.15,
              duration: 0.3,
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

const HOVER_INTENT_MS = 600;

function useNavItem(id: string) {
  const { registerRef, setHoveredItem } = React.useContext(NavGroupContext);
  const timerRef = React.useRef<ReturnType<typeof setTimeout> | null>(null);
  const ref = React.useCallback(
    (el: HTMLElement | null) => registerRef(id, el),
    [id, registerRef],
  );

  const onMouseEnter = React.useCallback(() => {
    timerRef.current = setTimeout(() => setHoveredItem(id), HOVER_INTENT_MS);
  }, [id, setHoveredItem]);

  const onMouseLeave = React.useCallback(() => {
    if (timerRef.current) clearTimeout(timerRef.current);
    setHoveredItem(null);
  }, [setHoveredItem]);

  React.useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  return { ref, onMouseEnter, onMouseLeave };
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
    >
      <Link
        to={href ?? "#"}
        target={target}
        onClick={handleClick}
        className={cn(
          "relative z-[1] flex w-full items-center gap-2 rounded-lg px-2 py-2 text-sm transition-colors hover:no-underline",
          "group-data-[collapsible=icon]:size-8! group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:p-2!",
          active
            ? "text-foreground font-semibold"
            : "text-muted-foreground font-medium",
        )}
      >
        {isLoading ? (
          <Loader2 className="trans text-muted-foreground size-4 shrink-0 animate-spin" />
        ) : (
          Icon && (
            <Icon
              className={cn(
                "trans size-4 shrink-0",
                active ? "text-foreground" : "text-muted-foreground",
              )}
            />
          )
        )}
        <Type
          variant="small"
          className="transition-[opacity,transform] duration-150 ease-out group-data-[collapsible=icon]:-translate-x-2 group-data-[collapsible=icon]:opacity-0"
        >
          {titleNode ?? title}
        </Type>
        {title === "Billing" && <ProductTierBadge />}
        {stage && (
          <ReleaseStageBadge
            stage={stage}
            size="xs"
            className="ml-auto group-data-[collapsible=icon]:hidden"
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
  isActive,
  children,
}: {
  label: string;
  Icon: React.ComponentType<{ className?: string }>;
  defaultHref?: string;
  isActive: boolean;
  children: React.ReactNode;
}) {
  const { openGroup, setOpenGroup } = React.useContext(NavGroupContext);
  const navItem = useNavItem(label);
  const isOpen = openGroup === label || (openGroup === null && isActive);

  const handleClick = () => {
    if (!isOpen) {
      setOpenGroup(label);
    }
  };

  return (
    <Collapsible
      open={isOpen}
      onOpenChange={() => setOpenGroup(isOpen ? null : label)}
    >
      <SidebarMenuItem>
        <div
          ref={navItem.ref}
          onMouseEnter={navItem.onMouseEnter}
          onMouseLeave={navItem.onMouseLeave}
        >
          <Link
            to={defaultHref ?? "#"}
            onClick={handleClick}
            className={cn(
              "relative z-[1] flex w-full items-center gap-2 rounded-lg px-2 py-2 text-left text-sm transition-colors hover:no-underline",
              "group-data-[collapsible=icon]:size-8! group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:p-2!",
              "cursor-pointer outline-hidden",
              isOpen
                ? "text-foreground font-semibold"
                : "text-muted-foreground font-medium",
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
          </Link>
        </div>

        <CollapsibleContent className="data-[state=closed]:animate-accordion-up data-[state=open]:animate-accordion-down overflow-hidden">
          <div className="border-border mt-1 ml-4 border-l pl-2 group-data-[collapsible=icon]:hidden">
            <ul className="flex flex-col gap-0.5 py-0.5">{children}</ul>
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

  return (
    <li data-sidebar="menu-item">
      <div
        ref={navItem.ref}
        onMouseEnter={navItem.onMouseEnter}
        onMouseLeave={navItem.onMouseLeave}
      >
        <Link
          to={item.href()}
          className={cn(
            "relative z-[1] flex items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors hover:no-underline",
            item.active
              ? "text-foreground font-semibold"
              : "text-muted-foreground",
          )}
        >
          <span className="truncate">{item.title}</span>
          {item.title === "Billing" && <ProductTierBadge />}
          {(stage ?? item.stage) && (
            <ReleaseStageBadge
              stage={(stage ?? item.stage)!}
              size="xs"
              className="ml-auto"
            />
          )}
        </Link>
      </div>
    </li>
  );
}
