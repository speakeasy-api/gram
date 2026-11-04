import type { CommandAction } from "@/contexts/CommandPalette";
import type { PagedRoute } from "@/route-pages";
import type { AppRoute } from "@/routes";

// Same shape test as `childRoutes` in route-pages. Inlined so this module does
// not import the page map: `@/routes` is pulled in by shared components, and
// only the app shell may load `route-pages.tsx`.
const childRoutes = (route: PagedRoute): PagedRoute[] =>
  Object.values(route).filter(
    (value) =>
      value && typeof value === "object" && "title" in value && "url" in value,
  ) as unknown as PagedRoute[];

// Convert a single route into a command-palette navigation action.
export const routeToNavAction = (
  route: AppRoute,
  group: string,
  id: string,
): CommandAction => {
  // AppRoute type-omits the raw `icon` string (exposing only the <Icon>
  // component), but the builder spreads it through, so it's present at runtime.
  // CommandAction renders the string name, so read it back here.
  const iconName = (route as unknown as { icon?: string }).icon;
  return {
    id,
    label: route.title,
    icon: typeof iconName === "string" ? iconName : undefined,
    onSelect: () => route.goTo(),
    group,
    stage: route.stage,
  };
};

// Flatten a route map into command-palette navigation actions. Only top-level
// pages a user can actually land on: external links, unauthenticated pages, and
// full-screen pages outside the main layout are excluded.
const routesToNavActions = (
  routeMap: Record<string, PagedRoute>,
  group: string,
  idPrefix: string,
): CommandAction[] =>
  Object.entries(routeMap)
    .filter(
      ([, route]) =>
        !route.external &&
        !route.unauthenticated &&
        !route.outsideMainLayout &&
        Boolean(route.component) &&
        Boolean(route.title) &&
        !route.legacyRedirect,
    )
    .map(([key, route]) =>
      routeToNavAction(route, group, `${idPrefix}-${key}`),
    );

// Nested routes that opted in via `commandPalette`. Tabs have no flag, so they
// stay out. A route behind a path parameter or splat is skipped with everything
// under it, matching `pageHrefs`: it has no single destination. The scope on
// the route is the only gate — do not fetch page state (billing eligibility
// lives on the page).
const nestedPaletteRoutes = (
  orgRoutes: Record<string, PagedRoute>,
  showPlatformAdmin: boolean,
  hasAnyScope: (scopes: NonNullable<PagedRoute["commandPalette"]>) => boolean,
): Record<string, PagedRoute> => {
  const found: Record<string, PagedRoute> = {};
  const walk = (route: PagedRoute, key: string) => {
    if (
      route.external ||
      /[:*]/.test(route.url) ||
      (!showPlatformAdmin && route.url.startsWith("platform-"))
    ) {
      return;
    }
    for (const child of childRoutes(route)) {
      const childKey = `${key}-${child.url}`;
      if (
        child.commandPalette &&
        !/[:*]/.test(child.url) &&
        hasAnyScope(child.commandPalette)
      ) {
        found[childKey] = child;
      }
      walk(child, childKey);
    }
  };
  for (const [key, route] of Object.entries(orgRoutes)) walk(route, key);
  return found;
};

export const orgRoutesToNavActions = (
  orgRoutes: Record<string, PagedRoute>,
  showPlatformAdmin: boolean,
  hasAnyScope: (scopes: NonNullable<PagedRoute["commandPalette"]>) => boolean,
): CommandAction[] => {
  const paletteOrgRoutes = Object.fromEntries(
    Object.entries(orgRoutes).filter(
      ([key, route]) =>
        (showPlatformAdmin || !route.url.startsWith("platform-")) &&
        // Killswitch routes only redirect to identity access.
        key !== "killswitch" &&
        // Parameterized routes have no destination without an id.
        !route.url.includes(":"),
    ),
  );
  return routesToNavActions(
    {
      ...paletteOrgRoutes,
      ...nestedPaletteRoutes(orgRoutes, showPlatformAdmin, hasAnyScope),
    },
    "Organization",
    "nav-org",
  );
};
