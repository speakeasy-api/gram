/**
 * Pure transforms for the enterprise-managed authorization graph.
 *
 *   assignments ──► resolveRoutes  (drop dangling refs)
 *                    │
 *                    ▼
 *                  anchorRoutes    (DOM rect → container-local Points)
 *                    │
 *                    ▼
 *                  generateRoutes  (Points → two bezier segments + label)
 *                    │
 *                    ▼
 *                  buildRouteGeometry
 *
 * Mirrors membership-pipeline.ts, with one difference that drives the whole
 * shape: a membership is binary (org, user) and an assignment is ternary
 * (app, user, resource). A hyperedge has no single line to draw, so it is
 * rendered as a route — app to user, user to resource — which is also the
 * sentence the row means: this app, acting for this user, reaches this
 * resource.
 *
 * The two segments stay separate rather than being one path, because a single
 * path from app to resource would be drawn straight through the user card
 * sitting between them.
 */
import type { EmaAppAssignment } from "./devidp";

export interface RouteCardElements {
  apps: Map<string, HTMLElement>;
  users: Map<string, HTMLElement>;
  resources: Map<string, HTMLElement>;
}

export interface ResolvedRoute {
  id: string;
  scope: string;
  appEl: HTMLElement;
  userEl: HTMLElement;
  resourceEl: HTMLElement;
}

export interface Point {
  x: number;
  y: number;
}

export interface AnchoredRoute {
  id: string;
  scope: string;
  /** Right edge of the app card. */
  app: Point;
  /** Left edge of the user card — where the first segment lands. */
  userIn: Point;
  /** Right edge of the user card — where the second segment departs. */
  userOut: Point;
  /** Left edge of the resource card. */
  resource: Point;
}

export interface RenderableRoute extends AnchoredRoute {
  /** app → user. */
  inbound: string;
  /** user → resource. */
  outbound: string;
  /**
   * Where the scope label sits: the midpoint of the outbound segment, since
   * the scope is what the user is granted *at the resource*.
   */
  labelAt: Point;
}

export interface RouteGeometry {
  width: number;
  height: number;
  routes: RenderableRoute[];
}

export const EMPTY_ROUTE_GEOMETRY: RouteGeometry = {
  width: 0,
  height: 0,
  routes: [],
};

/** Drop assignments whose endpoints aren't all currently mounted. */
export function resolveRoutes(
  assignments: EmaAppAssignment[],
  cards: RouteCardElements,
): ResolvedRoute[] {
  const out: ResolvedRoute[] = [];
  for (const a of assignments) {
    const appEl = cards.apps.get(a.app_id);
    const userEl = cards.users.get(a.user_id);
    const resourceEl = cards.resources.get(a.resource_id);
    if (appEl && userEl && resourceEl) {
      out.push({
        id: a.id,
        scope: a.granted_scopes,
        appEl,
        userEl,
        resourceEl,
      });
    }
  }
  return out;
}

/** Snap each route's four anchor points to card edges, container-local. */
export function anchorRoutes(
  routes: ResolvedRoute[],
  container: HTMLElement,
): AnchoredRoute[] {
  const cb = container.getBoundingClientRect();
  const midY = (r: DOMRect) => r.top + r.height / 2 - cb.top;

  return routes.map((r) => {
    const app = r.appEl.getBoundingClientRect();
    const user = r.userEl.getBoundingClientRect();
    const resource = r.resourceEl.getBoundingClientRect();
    return {
      id: r.id,
      scope: r.scope,
      app: { x: app.right - cb.left, y: midY(app) },
      userIn: { x: user.left - cb.left, y: midY(user) },
      userOut: { x: user.right - cb.left, y: midY(user) },
      resource: { x: resource.left - cb.left, y: midY(resource) },
    };
  });
}

/** A horizontal-tangent cubic bezier between two points. */
function curve(from: Point, to: Point): string {
  const dx = to.x - from.x;
  const cx1 = from.x + dx * 0.5;
  const cx2 = to.x - dx * 0.5;
  return (
    `M ${from.x} ${from.y} ` +
    `C ${cx1} ${from.y}, ${cx2} ${to.y}, ${to.x} ${to.y}`
  );
}

export function generateRoutes(routes: AnchoredRoute[]): RenderableRoute[] {
  return separateLabels(
    routes.map((r) => ({
      ...r,
      inbound: curve(r.app, r.userIn),
      outbound: curve(r.userOut, r.resource),
      labelAt: {
        x: (r.userOut.x + r.resource.x) / 2,
        y: (r.userOut.y + r.resource.y) / 2,
      },
    })),
  );
}

/** How close two labels may sit before one is nudged out of the way. */
const LABEL_MIN_GAP_Y = 18;
const LABEL_OVERLAP_X = 90;

/**
 * Push apart scope labels that would land on top of each other.
 *
 * Midpoints collide readily — two routes running to different resources can
 * cross near the same point — and a stack of unreadable labels is worse than
 * a label slightly off its line. Walks in y order and lifts each label clear
 * of the previous one when they overlap horizontally.
 */
export function separateLabels(routes: RenderableRoute[]): RenderableRoute[] {
  const byY = [...routes].sort((a, b) => a.labelAt.y - b.labelAt.y);
  const placed: RenderableRoute[] = [];

  for (const route of byY) {
    let { y } = route.labelAt;
    for (const other of placed) {
      const overlapsX =
        Math.abs(other.labelAt.x - route.labelAt.x) < LABEL_OVERLAP_X;
      if (overlapsX && y - other.labelAt.y < LABEL_MIN_GAP_Y) {
        y = other.labelAt.y + LABEL_MIN_GAP_Y;
      }
    }
    placed.push({ ...route, labelAt: { ...route.labelAt, y } });
  }

  // Restore the caller's order so paint-order sorting downstream is the only
  // thing deciding what draws on top.
  const byId = new Map(placed.map((r) => [r.id, r]));
  return routes.map((r) => byId.get(r.id) ?? r);
}

export function buildRouteGeometry(
  assignments: EmaAppAssignment[],
  container: HTMLElement | null,
  cards: RouteCardElements,
): RouteGeometry {
  if (!container) return EMPTY_ROUTE_GEOMETRY;
  const cb = container.getBoundingClientRect();
  return {
    width: cb.width,
    height: cb.height,
    routes: generateRoutes(
      anchorRoutes(resolveRoutes(assignments, cards), container),
    ),
  };
}
