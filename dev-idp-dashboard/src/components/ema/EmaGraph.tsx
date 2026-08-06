import { useMemo } from "react";
import { AnimatePresence, motion } from "motion/react";
import { match } from "ts-pattern";
import { cn } from "@/lib/utils";
import type { RenderableRoute, RouteGeometry } from "@/lib/ema-pipeline";

export type EmaSelection =
  | { kind: "none" }
  | { kind: "app"; id: string }
  | { kind: "user"; id: string }
  | { kind: "resource"; id: string };

/** The three cards a route touches, keyed by assignment id. */
export interface RouteOwners {
  appId: string;
  userId: string;
  resourceId: string;
}

interface Props extends RouteGeometry {
  selection: EmaSelection;
  routeOwners: Map<string, RouteOwners>;
  /** Clicking a scope label edits that assignment. */
  onSelectRoute: (assignmentId: string) => void;
}

type Emphasis = "emphasized" | "default" | "dim";

const PAINT_ORDER: Record<Emphasis, number> = {
  dim: 0,
  default: 1,
  emphasized: 2,
};

function emphasisFor(
  selection: EmaSelection,
  owners: RouteOwners | undefined,
): Emphasis {
  if (!owners) return "default";
  return match(selection)
    .with({ kind: "none" }, () => "default" as const)
    .with({ kind: "app" }, (s) =>
      s.id === owners.appId ? ("emphasized" as const) : ("dim" as const),
    )
    .with({ kind: "user" }, (s) =>
      s.id === owners.userId ? ("emphasized" as const) : ("dim" as const),
    )
    .with({ kind: "resource" }, (s) =>
      s.id === owners.resourceId ? ("emphasized" as const) : ("dim" as const),
    )
    .exhaustive();
}

export function EmaGraph({
  width,
  height,
  routes,
  selection,
  routeOwners,
  onSelectRoute,
}: Props) {
  // Stable-sort by emphasis so emphasized routes paint last, over the rest.
  // SVG honors document order and the label layer honors DOM order, so one
  // sort serves both.
  const ordered = useMemo(() => {
    const decorated = routes.map((route) => ({
      route,
      emphasis: emphasisFor(selection, routeOwners.get(route.id)),
    }));
    decorated.sort((a, b) => PAINT_ORDER[a.emphasis] - PAINT_ORDER[b.emphasis]);
    return decorated;
  }, [routes, selection, routeOwners]);

  return (
    <>
      <svg
        aria-hidden
        className="absolute inset-0 pointer-events-none z-0"
        width={width}
        height={height}
        viewBox={`0 0 ${width || 1} ${height || 1}`}
      >
        <AnimatePresence>
          {ordered.map(({ route, emphasis }) => (
            <RoutePath key={route.id} route={route} emphasis={emphasis} />
          ))}
        </AnimatePresence>
      </svg>
      <div className="absolute inset-0 pointer-events-none z-10">
        <AnimatePresence>
          {ordered.map(({ route, emphasis }) => (
            <ScopeLabel
              key={route.id}
              route={route}
              emphasis={emphasis}
              onClick={() => onSelectRoute(route.id)}
            />
          ))}
        </AnimatePresence>
      </div>
    </>
  );
}

/**
 * One assignment: two segments drawn as a single logical edge. They share an
 * emphasis and animate together, so a route reads as one thing even though
 * it cannot be one path — the user card sits between the endpoints.
 */
function RoutePath({
  route,
  emphasis,
}: {
  route: RenderableRoute;
  emphasis: Emphasis;
}) {
  const stroke = match(emphasis)
    .with("emphasized", () => "var(--retro-orange)")
    .with("default", () => "var(--muted-foreground)")
    .with("dim", () => "var(--border)")
    .exhaustive();
  const strokeWidth = emphasis === "emphasized" ? 2 : 1.25;
  const opacity = match(emphasis)
    .with("emphasized", () => 1)
    .with("default", () => 0.55)
    .with("dim", () => 0.18)
    .exhaustive();

  const shared = {
    fill: "none",
    stroke,
    strokeWidth,
    strokeLinecap: "round" as const,
    initial: { pathLength: 0, opacity: 0 },
    animate: { pathLength: 1, opacity },
    exit: { pathLength: 0, opacity: 0 },
    transition: {
      pathLength: { duration: 0.5, ease: "easeOut" as const },
      opacity: { duration: 0.2 },
    },
  };

  return (
    <>
      <motion.path d={route.inbound} {...shared} />
      <motion.path d={route.outbound} {...shared} />
    </>
  );
}

function ScopeLabel({
  route,
  emphasis,
  onClick,
}: {
  route: RenderableRoute;
  emphasis: Emphasis;
  onClick: () => void;
}) {
  const opacity = match(emphasis)
    .with("emphasized", () => 1)
    .with("default", () => 0.7)
    .with("dim", () => 0.25)
    .exhaustive();

  return (
    <motion.button
      type="button"
      onClick={onClick}
      title="Edit this assignment"
      className={cn(
        "absolute -translate-x-1/2 -translate-y-1/2 pointer-events-auto cursor-pointer",
        "rounded-sm border bg-card px-1.5 py-[1px] text-[10px] font-mono uppercase tracking-wider whitespace-nowrap",
        "hover:border-[var(--retro-orange)] hover:text-[var(--retro-orange)]",
        emphasis === "emphasized"
          ? "border-[var(--retro-orange)] text-[var(--retro-orange)]"
          : "border-border text-muted-foreground",
      )}
      style={{ left: route.labelAt.x, top: route.labelAt.y }}
      initial={{ scale: 0.7, opacity: 0 }}
      animate={{ scale: 1, opacity }}
      exit={{ scale: 0.7, opacity: 0 }}
      transition={{ type: "spring", stiffness: 500, damping: 35 }}
    >
      {route.scope || "no scopes"}
    </motion.button>
  );
}
