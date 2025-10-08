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
  Navigate,
  useLocation,
} from "react-router";
import { AppLayout, LoginCheck } from "./components/app-layout.tsx";
import { AuthProvider, ProjectProvider, useSession } from "./contexts/Auth.tsx";
import { SdkProvider } from "./contexts/Sdk.tsx";
import { TelemetryProvider } from "./contexts/Telemetry.tsx";
import { AppRoute, useRoutes } from "./routes";
import { Toaster } from "@/components/ui/sonner";
import CliCallback from "./pages/cli/CliCallback";

export default function App() {
  return (
    <BrowserRouter>
      <AppContent />
    </BrowserRouter>
  );
}

function AppContent() {
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

  const cliFlow = useCliAuthFlow();

  const appDisplay = cliFlow ? (
    <CliCallback localCallbackUrl={cliFlow.cliCallbackUrl} />
  ) : (
    <AuthProvider>
      <ProjectProvider>
        <RouteProvider />
      </ProjectProvider>
    </AuthProvider>
  );

  return (
    <MoonshineConfigProvider theme={theme} setTheme={applyTheme}>
      <LocalTooltipProvider>
        <TooltipProvider>
          <TelemetryProvider>
            <SdkProvider>
              {appDisplay}
              <Toaster />
            </SdkProvider>
          </TelemetryProvider>
        </TooltipProvider>
      </LocalTooltipProvider>
    </MoonshineConfigProvider>
  );
}

const RouteProvider = () => {
  const routes = useRoutes();

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
