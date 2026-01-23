import "@speakeasy-api/moonshine/moonshine.css";
import "./App.css"; // Import this second to override certain values in moonshine.css

import {
  MoonshineConfigProvider,
  TooltipProvider,
} from "@speakeasy-api/moonshine";
import { TooltipProvider as LocalTooltipProvider } from "@/components/ui/tooltip";
import { useEffect, useMemo, useState } from "react";
import {
  BrowserRouter,
  Route,
  Routes,
  useSearchParams,
  useLocation,
} from "react-router";
import { AppLayout, LoginCheck } from "./components/app-layout.tsx";
import { AuthProvider, ProjectProvider } from "./contexts/Auth.tsx";
import { SdkProvider } from "./contexts/Sdk.tsx";
import { TelemetryProvider } from "./contexts/Telemetry.tsx";
import { AppRoute, useRoutes } from "./routes";
import { Toaster } from "@/components/ui/sonner";
import CliCallback from "./pages/cli/CliCallback";
import {
  CommandPaletteProvider,
  useCommandPalette,
} from "./contexts/CommandPalette";
import { CommandPalette } from "./components/command-palette";
import { WebGLCanvas, FontTexture } from "@/components/webgl";

export default function App() {
  const [theme, setTheme] = useState<"light" | "dark">("light");

  // Initialize Pylon widget in production only
  useEffect(() => {
    if (import.meta.env.PROD) {
      import("./lib/pylon").then((module) => {
        module.initializePylon();
      });
    }
  }, []);

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

    localStorage.setItem("preferred-theme", theme);

    setTheme(theme);
  };

  useEffect(() => {
    const savedTheme = localStorage.getItem("preferred-theme") as
      | "light"
      | "dark"
      | null;
    const systemPrefersDark = window.matchMedia(
      "(prefers-color-scheme: dark)",
    ).matches;

    const initialTheme = savedTheme || (systemPrefersDark ? "dark" : "light");
    applyTheme(initialTheme);
  }, []);

  return (
    <MoonshineConfigProvider theme={theme} setTheme={applyTheme}>
      <LocalTooltipProvider>
        <TooltipProvider>
          <TelemetryProvider>
            <CommandPaletteProvider>
              <BrowserRouter>
                <SdkProvider>
                  <AppContent />
                  <Toaster />
                  <CommandPalette />
                </SdkProvider>
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
   * where to send authenticated users. Instead, CliCallback handles the flow:
   *
   * 1. CliCallback receives an unauthenticated request.
   * 2. Sets the redirect query param to its current URL.
   * 3. Sends the user through the standard login flow via AuthHandler.
   * 4. Authenticated user is redirected back to CliCallback.
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
      </ProjectProvider>
    </AuthProvider>
  );
}

const RouteProvider = () => {
  const routes = useRoutes();
  const { addActions, removeActions } = useCommandPalette();

  // Register global command palette actions
  useEffect(() => {
    const globalActions = [
      {
        id: "go-home",
        label: "Go to Home",
        icon: "home",
        onSelect: () => routes.home.goTo(),
        group: "Navigation",
      },
      {
        id: "go-deployments",
        label: "Go to Deployments",
        icon: "package",
        onSelect: () => routes.deployments.goTo(),
        group: "Navigation",
      },
      {
        id: "go-toolsets",
        label: "Go to Toolsets",
        icon: "blocks",
        onSelect: () => routes.mcp.goTo(),
        group: "Navigation",
      },
      {
        id: "go-playground",
        label: "Go to Playground",
        icon: "message-square",
        onSelect: () => routes.playground.goTo(),
        group: "Navigation",
      },
    ];

    addActions(globalActions);

    return () => {
      removeActions(globalActions.map((a) => a.id));
    };
  }, [routes, addActions, removeActions]);

  const unauthenticatedRoutes = Object.values(routes).filter(
    (route) => route.unauthenticated,
  );

  const outsideStructureRoutes = Object.values(routes).filter(
    (route) => route.outsideMainLayout,
  );

  const authenticatedRoutes = Object.values(routes).filter(
    (route) =>
      !outsideStructureRoutes.includes(route) && !route.unauthenticated,
  );

  const routeElements = useMemo(
    () => (
      <Routes>
        {/* Register these unauthenticated paths outside of root layout */}
        {routesWithSubroutes(unauthenticatedRoutes)}
        <Route path="/" element={<LoginCheck />}>
          <Route path=":orgSlug/:projectSlug">
            {routesWithSubroutes(outsideStructureRoutes)}
          </Route>
          <Route path=":orgSlug/:projectSlug" element={<AppLayout />}>
            {routesWithSubroutes(authenticatedRoutes)}
          </Route>
        </Route>
      </Routes>
    ),
    [routes],
  );

  return routeElements;
};

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
