/**
 * Pure transforms for the membership graph.
 *
 *   memberships ──► resolveEdges  (drop dangling refs)
 *                    │
 *                    ▼
 *                  anchorEdges    (DOM rect → container-local Points)
 *                    │
 *                    ▼
 *                  generatePaths  (Points → bezier path + midpoint)
 *                    │
 *                    ▼
 *                  buildGeometry  (collated, plus container size)
 *
 * Each step is independently testable. The renderer only consumes the final
 * RenderableEdge[]; layout reactivity belongs to the hook above the pipeline.
 */
import type { Membership } from "./devidp";

export interface CardElements {
  orgs: Map<string, HTMLElement>;
  users: Map<string, HTMLElement>;
}

export interface ResolvedEdge {
  id: string;
  role: string;
  fromEl: HTMLElement;
  toEl: HTMLElement;
}

export interface Point {
  x: number;
  y: number;
}

export interface AnchoredEdge {
  id: string;
  role: string;
  from: Point;
  to: Point;
}

export interface RenderableEdge extends AnchoredEdge {
  d: string;
  midpoint: Point;
}

export interface Geometry {
  width: number;
  height: number;
  edges: RenderableEdge[];
}

export const EMPTY_GEOMETRY: Geometry = { width: 0, height: 0, edges: [] };

/** Drop memberships whose endpoints aren't currently mounted. */
export function resolveEdges(
  memberships: Membership[],
  cards: CardElements,
): ResolvedEdge[] {
  const out: ResolvedEdge[] = [];
  for (const m of memberships) {
    const fromEl = cards.orgs.get(m.organization_id);
    const toEl = cards.users.get(m.user_id);
    if (fromEl && toEl) {
      out.push({ id: m.id, role: m.role, fromEl, toEl });
    }
  }
  return out;
}

/** Snap each edge's endpoints to its card's inner-facing midpoint, in
 *  container-local coordinates. */
export function anchorEdges(
  edges: ResolvedEdge[],
  container: HTMLElement,
): AnchoredEdge[] {
  const cb = container.getBoundingClientRect();
  return edges.map((e) => {
    const a = e.fromEl.getBoundingClientRect();
    const b = e.toEl.getBoundingClientRect();
    return {
      id: e.id,
      role: e.role,
      from: { x: a.right - cb.left, y: a.top + a.height / 2 - cb.top },
      to: { x: b.left - cb.left, y: b.top + b.height / 2 - cb.top },
    };
  });
}

/** Convert anchored points to a horizontal-tangent cubic bezier and a label
 *  midpoint. Control points sit on the same Y as their respective endpoint
 *  for clean horizontal entry/exit. */
export function generatePaths(edges: AnchoredEdge[]): RenderableEdge[] {
  return edges.map((e) => {
    const dx = e.to.x - e.from.x;
    const cx1 = e.from.x + dx * 0.5;
    const cx2 = e.to.x - dx * 0.5;
    const d =
      `M ${e.from.x} ${e.from.y} ` +
      `C ${cx1} ${e.from.y}, ${cx2} ${e.to.y}, ${e.to.x} ${e.to.y}`;
    const midpoint = {
      x: (e.from.x + e.to.x) / 2,
      y: (e.from.y + e.to.y) / 2,
    };
    return { ...e, d, midpoint };
  });
}

export function buildGeometry(
  memberships: Membership[],
  container: HTMLElement | null,
  cards: CardElements,
): Geometry {
  if (!container) return EMPTY_GEOMETRY;
  const cb = container.getBoundingClientRect();
  return {
    width: cb.width,
    height: cb.height,
    edges: generatePaths(
      anchorEdges(resolveEdges(memberships, cards), container),
    ),
  };
}
