import "@speakeasy-api/moonshine/moonshine.css";
import "./App.css"; // Import this second to override certain values in moonshine.css

import { NuqsAdapter } from "nuqs/adapters/react-router/v7";
import { Toaster } from "@/components/ui/sonner";
import { TooltipProvider as LocalTooltipProvider } from "@/components/ui/tooltip";
import { FontTexture, WebGLCanvas } from "@/components/webgl";
import {
  MoonshineConfigProvider,
  TooltipProvider,
} from "@speakeasy-api/moonshine";
import { useEffect, useMemo, useState } from "react";
import {
  BrowserRouter,
  Navigate,
  Route,
  Routes,
  useLocation,
  useSearchParams,
} from "react-router";
import { AppLayout, LoginCheck, OrgLayout } from "./components/app-layout.tsx";
import { CommandPalette } from "./components/command-palette";
import {
  recordVisit,
  useRecentsUserId,
} from "./components/command-palette/recentlyVisited";
import { useProjectNavRoutes } from "./hooks/useProjectNavRoutes";
import { useRBAC } from "./hooks/useRBAC";
import { AuthProvider, ProjectProvider } from "./contexts/AuthProvider.tsx";
import { useCommandPalette } from "./contexts/CommandPalette";
import type { CommandAction } from "./contexts/CommandPalette";
import { CommandPaletteProvider } from "./contexts/CommandPaletteProvider";
import { useSlugs } from "./contexts/Sdk.tsx";
import { SdkProvider } from "./contexts/SdkProvider.tsx";
import { TelemetryProvider } from "./contexts/TelemetryProvider.tsx";
import { RBACDevToolbar } from "./components/dev-toolbar";
import { usePageTitle } from "./hooks/use-page-title";
import { PREFERRED_THEME_STORAGE_KEY } from "./lib/local-storage-keys";
import CliCallback from "./pages/cli/CliCallback";
import ShadowMCPRequestAccess from "./pages/shadow-mcp/RequestAccess";
import SwitchOrg from "./pages/demo/SwitchOrg";
import { AppRoute, useRoutes, useOrgRoutes } from "./routes";

export default function App(): JSX.Element {
  // Initialize from storage so React/Moonshine match the theme the pre-paint
  // inline script (in index.html) already applied to <html> — avoids a flash.
  const [theme, setTheme] = useState<"light" | "dark">(() => {
    try {
      return localStorage.getItem(PREFERRED_THEME_STORAGE_KEY) === "dark"
        ? "dark"
        : "light";
    } catch {
      return "light";
    }
  });

  const applyTheme = (theme: "light" | "dark") => {
    const root = document.documentElement;
    if (root.classList.contains(theme)) return;
    root.classList.add(theme);
    root.classList.remove(theme === "dark" ? "light" : "dark");

    // Update favicon based on theme
    const favicon = document.getElementById("favicon") as HTMLLinkElement;
    const faviconAlt = document.getElementById(
      "favicon-alt",
    ) as HTMLLinkElement;

    if (favicon) {
      favicon.href = theme === "dark" ? "/favicon-dark.png" : "/favicon.png";
    }

    if (faviconAlt) {
      faviconAlt.href = theme === "dark" ? "/favicon-dark.ico" : "/favicon.ico";
    }

    localStorage.setItem(PREFERRED_THEME_STORAGE_KEY, theme);

    setTheme(theme);
  };

  useEffect(() => {
    const savedTheme = localStorage.getItem(PREFERRED_THEME_STORAGE_KEY) as
      | "light"
      | "dark"
      | null;

    // Light mode is the default; only honor an explicit prior choice. The
    // system color-scheme preference no longer forces dark on first load.
    const initialTheme = savedTheme || "light";
    applyTheme(initialTheme);
  }, []);

  return (
    <MoonshineConfigProvider theme={theme} setTheme={applyTheme}>
      <LocalTooltipProvider>
        <TooltipProvider>
          <TelemetryProvider>
            <CommandPaletteProvider>
              <BrowserRouter>
                <NuqsAdapter>
                  <SdkProvider>
                    <AppContent />
                    <Toaster />
                    <CommandPalette />
                  </SdkProvider>
                </NuqsAdapter>
              </BrowserRouter>
            </CommandPaletteProvider>
          </TelemetryProvider>
        </TooltipProvider>
      </LocalTooltipProvider>
    </MoonshineConfigProvider>
  );
}

function AppContent() {
  /**
   * NOTE(cjea): Do not wrap CliCallback in an AuthProvider.
   *
   * CLI requests don't include a redirect URL, so AuthProvider wouldn't know
   * where to send authenticated users. Instead, these components handle the flow:
   *
   * 1. Component receives an unauthenticated request.
   * 2. Sets the redirect query param to its current URL.
   * 3. Sends the user through the standard login flow via AuthHandler.
   * 4. Authenticated user is redirected back to the component.
   */
  const cliFlow = useCliAuthFlow();
  const location = useLocation();

  // Only render WebGL canvas during onboarding
  const isOnboarding = location.pathname.includes("/onboarding");

  if (cliFlow) {
    return <CliCallback localCallbackUrl={cliFlow.cliCallbackUrl} />;
  }

  return (
    <AuthProvider>
      <ProjectProvider>
        {isOnboarding && (
          <>
            <WebGLCanvas />
            <FontTexture />
          </>
        )}
        <RouteProvider />
        <RBACDevToolbar />
      </ProjectProvider>
    </AuthProvider>
  );
}

const RouteProvider = () => {
  const routes = useRoutes();
  const orgRoutes = useOrgRoutes();
  const { addActions, removeActions } = useCommandPalette();
  const { orgSlug, projectSlug } = useSlugs();
  const location = useLocation();
  const projectNavRoutes = useProjectNavRoutes();
  const { hasAnyScope } = useRBAC();
  const recentsUserId = useRecentsUserId();

  // Update document title based on active route
  usePageTitle(routes, orgRoutes);

  // Record the visited page for the command palette's "Recently Visited"
  // section. Stored client-side (localStorage), scoped per workspace.
  // matchesCurrent uses exact segment counts, so the active top-level route is
  // the real current page. We record the EXACT path (not the section base) so a
  // detail page like /sources/externalmcp/notion is its own entry — labelled by
  // the item slug ("notion") rather than the section ("Sources").
  useEffect(() => {
    // Wait until the user id resolves before recording — otherwise the early
    // write lands on the shared anonymous key instead of the per-user one.
    // recentsUserId is a dependency, so the effect re-runs (and records the
    // current page) as soon as the session loads.
    if (!recentsUserId) return;
    const active =
      Object.values(routes).find((r) => r.active && !r.external) ??
      Object.values(orgRoutes).find((r) => r.active && !r.external);
    if (!active) return;
    const iconName = (active as unknown as { icon?: string }).icon;
    recordVisit(recentsUserId, orgSlug, projectSlug, {
      label: pageLabel(active.title, active.href(), location.pathname),
      // Recents are page-level: record the pathname without query params so a
      // page and its deep-linked item state (e.g. ?policy=<id>) collapse into a
      // single entry instead of appearing as duplicates with the same label.
      href: location.pathname,
      icon: typeof iconName === "string" ? iconName : undefined,
    });
  }, [
    location.pathname,
    routes,
    orgRoutes,
    recentsUserId,
    orgSlug,
    projectSlug,
  ]);

  // Register command palette navigation actions. Project "Pages" mirror the
  // left sidebar exactly (same source, same order) so the palette only offers
  // pages a user can actually reach from the nav. Project pages register only
  // once a project is selected (their goTo() needs the :projectSlug); org pages
  // are always available.
  useEffect(() => {
    const projectActions = projectSlug
      ? projectNavRoutes
          .filter(
            ({ route, scope }) =>
              !route.external &&
              route.component &&
              route.title &&
              // Mirror the sidebar's per-page scope gating so the palette never
              // offers (nor navigates to) pages the user can't access.
              hasAnyScope(scope),
          )
          .map(({ route }) =>
            routeToNavAction(route, "Pages", `nav-page-${route.url || "home"}`),
          )
      : [];
    const orgActions = routesToNavActions(orgRoutes, "Organization", "nav-org");

    const allActions = [...projectActions, ...orgActions];
    addActions(allActions);

    return () => {
      removeActions(allActions.map((a) => a.id));
    };
  }, [
    projectNavRoutes,
    orgRoutes,
    projectSlug,
    hasAnyScope,
    addActions,
    removeActions,
  ]);

  const routeElements = useMemo(() => {
    const allRoutes = Object.values(routes);
    const unauthenticatedRoutes = allRoutes.filter(
      (route) => route.unauthenticated,
    );
    const outsideStructureRoutes = allRoutes.filter(
      (route) => route.outsideMainLayout,
    );
    const authenticatedRoutes = allRoutes.filter(
      (route) =>
        !outsideStructureRoutes.includes(route) && !route.unauthenticated,
    );

    const orgRouteValues = Object.values(orgRoutes);
    const orgHomeRoute = orgRouteValues.find((r) => r.url === "");
    const outsideOrgLayoutRoutes = orgRouteValues.filter(
      (route) => route.outsideMainLayout,
    );
    const otherOrgRoutes = orgRouteValues.filter(
      (r) => r.url !== "" && !r.outsideMainLayout,
    );

    return (
      <Routes>
        {/* Register these unauthenticated paths outside of root layout */}
        {routesWithSubroutes(unauthenticatedRoutes)}
        <Route path="/switch-org" element={<LoginCheck />}>
          <Route index element={<SwitchOrg />} />
        </Route>
        <Route
          path="/shadow-mcp/request"
          element={<ShadowMCPRequestAccess />}
        />
        <Route path="/" element={<LoginCheck />}>
          <Route path=":orgSlug/projects/:projectSlug">
            {routesWithSubroutes(outsideStructureRoutes)}
          </Route>
          <Route path=":orgSlug/projects/:projectSlug" element={<AppLayout />}>
            {/* Redirect legacy /chat-sessions bookmarks to the new /agent-sessions URL */}
            <Route
              path="chat-sessions"
              element={<Navigate to="../agent-sessions" replace />}
            />
            {routesWithSubroutes(authenticatedRoutes)}
          </Route>
          {/* Org routes that render without OrgLayout (full-screen standalone pages) */}
          <Route path=":orgSlug">
            {routesWithSubroutes(outsideOrgLayoutRoutes)}
          </Route>
          <Route path=":orgSlug" element={<OrgLayout />}>
            {orgHomeRoute?.component && (
              <Route index element={<orgHomeRoute.component />} />
            )}
            {routesWithSubroutes(otherOrgRoutes)}
          </Route>
        </Route>
      </Routes>
    );
  }, [routes, orgRoutes]);

  return routeElements;
};

// Opaque ids make poor Recents labels; detect UUIDs so detail pages keyed by id
// fall back to "<Section> <short id>" instead of a raw UUID.
const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

// Label a visited page for the Recents list. Section/list pages keep the route
// title; detail pages prefer the human-readable last path segment (a slug like
// "notion"), falling back to "<Title> <short id>" when that segment is opaque.
const pageLabel = (
  title: string,
  baseHref: string,
  pathname: string,
): string => {
  if (pathname === baseHref || !pathname.startsWith(baseHref)) return title;
  const segment = decodeURIComponent(
    pathname.split("/").filter(Boolean).pop() ?? "",
  );
  if (!segment) return title;
  if (UUID_RE.test(segment) || segment.length > 24) {
    return `${title} · ${segment.slice(0, 8)}`;
  }
  return segment;
};

// Convert a single route into a command-palette navigation action.
const routeToNavAction = (
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
        Boolean(route.title),
    )
    .map(([key, route]) =>
      routeToNavAction(route, group, `${idPrefix}-${key}`),
    );

const routesWithSubroutes = (routes: AppRoute[]) => {
  return routes
    .filter((item) => !item.external)
    .map((item) => (
      <Route
        key={item.title}
        path={item.url}
        element={item.component ? <item.component /> : null}
      >
        {item.indexComponent && (
          <Route index element={<item.indexComponent />} />
        )}
        {/* Check for any children routes stored on this item */}
        {routesWithSubroutes(
          Object.values(item).filter(
            (value) =>
              value &&
              typeof value === "object" &&
              "title" in value &&
              "url" in value,
          ) as unknown as AppRoute[],
        )}
      </Route>
    ));
};

function useCliAuthFlow() {
  const [searchParams] = useSearchParams();
  const location = useLocation();

  const fromCli = searchParams.get("from_cli") === "true";
  const cliCallbackUrl = searchParams.get("cli_callback_url");

  if (location.pathname === "/" && fromCli && cliCallbackUrl) {
    return { cliCallbackUrl };
  }

  return null;
}
