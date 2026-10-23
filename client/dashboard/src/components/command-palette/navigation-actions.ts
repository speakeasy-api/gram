import type { CommandAction } from "@/contexts/CommandPalette";
import type { AppRoute, useOrgRoutes } from "@/routes";

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
export const routesToNavActions = (
  routeMap: Record<string, AppRoute>,
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

export const orgRoutesToNavActions = (
  orgRoutes: ReturnType<typeof useOrgRoutes>,
  showPlatformAdmin: boolean,
  canAccessBilling: boolean,
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
      // Only these nested pages are launcher destinations. Their pages own
      // billing eligibility checks; registration must not fetch billing state.
      ...(canAccessBilling
        ? {
            "billing-setup": orgRoutes.billing.setup,
            "billing-payment-method": orgRoutes.billing.paymentMethod,
          }
        : {}),
    },
    "Organization",
    "nav-org",
  );
};
