import { useMemo } from "react";
import { AnimatePresence, motion } from "motion/react";
import { match } from "ts-pattern";
import { cn } from "@/lib/utils";
import type { Geometry, RenderableEdge } from "@/lib/membership-pipeline";

type Selection =
  | { kind: "none" }
  | { kind: "org"; id: string }
  | { kind: "user"; id: string };

interface Props extends Geometry {
  selection: Selection;
  /**
   * For each edge, must accept the membership id and return the *original*
   * org/user ids — used to decide emphasis. Wired by HomeTab from the
   * memberships array, kept out of the renderer's domain.
   */
  edgeOwners: Map<string, { orgId: string; userId: string }>;
}

type Emphasis = "emphasized" | "default" | "dim";

const PAINT_ORDER: Record<Emphasis, number> = {
  dim: 0,
  default: 1,
  emphasized: 2,
};

function emphasisFor(
  selection: Selection,
  owners: { orgId: string; userId: string } | undefined,
): Emphasis {
  if (!owners) return "default";
  return match(selection)
    .with({ kind: "none" }, () => "default" as const)
    .with({ kind: "org" }, (s) =>
      s.id === owners.orgId ? ("emphasized" as const) : ("dim" as const),
    )
    .with({ kind: "user" }, (s) =>
      s.id === owners.userId ? ("emphasized" as const) : ("dim" as const),
    )
    .exhaustive();
}

export function MembershipGraph({
  width,
  height,
  edges,
  selection,
  edgeOwners,
}: Props) {
  // Stable-sort edges by emphasis priority so emphasized rows render last and
  // therefore paint on top. SVG honors document order; the label layer is
  // siblings in a div, which honors DOM order too. Same sort serves both.
  const ordered = useMemo(() => {
    const decorated = edges.map((edge) => ({
      edge,
      emphasis: emphasisFor(selection, edgeOwners.get(edge.id)),
    }));
    decorated.sort((a, b) => PAINT_ORDER[a.emphasis] - PAINT_ORDER[b.emphasis]);
    return decorated;
  }, [edges, selection, edgeOwners]);

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
          {ordered.map(({ edge, emphasis }) => (
            <EdgePath key={edge.id} edge={edge} emphasis={emphasis} />
          ))}
        </AnimatePresence>
      </svg>
      <div className="absolute inset-0 pointer-events-none z-10">
        <AnimatePresence>
          {ordered.map(({ edge, emphasis }) => (
            <RoleLabel key={edge.id} edge={edge} emphasis={emphasis} />
          ))}
        </AnimatePresence>
      </div>
    </>
  );
}

function EdgePath({
  edge,
  emphasis,
}: {
  edge: RenderableEdge;
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

  return (
    <motion.path
      d={edge.d}
      fill="none"
      stroke={stroke}
      strokeWidth={strokeWidth}
      strokeLinecap="round"
      initial={{ pathLength: 0, opacity: 0 }}
      animate={{ pathLength: 1, opacity }}
      exit={{ pathLength: 0, opacity: 0 }}
      transition={{
        pathLength: { duration: 0.5, ease: "easeOut" },
        opacity: { duration: 0.2 },
      }}
    />
  );
}

function RoleLabel({
  edge,
  emphasis,
}: {
  edge: RenderableEdge;
  emphasis: Emphasis;
}) {
  const opacity = match(emphasis)
    .with("emphasized", () => 1)
    .with("default", () => 0.7)
    .with("dim", () => 0.25)
    .exhaustive();

  return (
    <motion.div
      className={cn(
        "absolute -translate-x-1/2 -translate-y-1/2",
        "rounded-sm border bg-card px-1.5 py-[1px] text-[10px] font-mono uppercase tracking-wider whitespace-nowrap",
        emphasis === "emphasized"
          ? "border-[var(--retro-orange)] text-[var(--retro-orange)]"
          : "border-border text-muted-foreground",
      )}
      style={{ left: edge.midpoint.x, top: edge.midpoint.y }}
      initial={{ scale: 0.7, opacity: 0 }}
      animate={{ scale: 1, opacity }}
      exit={{ scale: 0.7, opacity: 0 }}
      transition={{ type: "spring", stiffness: 500, damping: 35 }}
    >
      {edge.role}
    </motion.div>
  );
}
