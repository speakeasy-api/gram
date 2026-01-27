import { useEffect } from "react";
import { AppRoute, AppRoutes } from "@/routes";

const BASE_TITLE = "Gram";

function isAppRoute(value: unknown): value is AppRoute {
  return (
    value !== null &&
    typeof value === "object" &&
    "title" in value &&
    "url" in value &&
    "active" in value
  );
}

/**
 * Finds the deepest active route in the route tree.
 * Routes mark themselves as active when they or any of their children match the current URL.
 * We want the most specific (deepest) match for the page title.
 */
function findActiveRoute(routes: AppRoutes): AppRoute | null {
  for (const route of Object.values(routes)) {
    if (!route.active) continue;

    // Check for nested active routes (subPages are spread onto the route object)
    const nestedRoutes: AppRoute[] = [];
    for (const value of Object.values(route)) {
      if (isAppRoute(value)) {
        nestedRoutes.push(value);
      }
    }

    if (nestedRoutes.length > 0) {
      const nestedActive = findActiveRoute(
        Object.fromEntries(nestedRoutes.map((r, i) => [String(i), r])),
      );
      if (nestedActive) return nestedActive;
    }

    return route;
  }

  return null;
}

/**
 * Sets the document title based on the currently active route.
 * Automatically updates when navigation occurs.
 *
 * @param routes - The routes object from useRoutes()
 */
export function usePageTitle(routes: AppRoutes): void {
  useEffect(() => {
    const activeRoute = findActiveRoute(routes);
    document.title = activeRoute
      ? `${activeRoute.title} | ${BASE_TITLE}`
      : BASE_TITLE;
  }, [routes]);
}
