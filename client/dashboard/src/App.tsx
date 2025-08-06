import "@speakeasy-api/moonshine/moonshine.css";
import "./App.css"; // Import this second to override certain values in moonshine.css

import { MoonshineConfigProvider } from "@speakeasy-api/moonshine";
import { useEffect, useMemo, useState } from "react";
import { BrowserRouter, Route, Routes } from "react-router";
import { AppLayout, LoginCheck } from "./components/app-layout.tsx";
import { AuthProvider, ProjectProvider } from "./contexts/Auth.tsx";
import { SdkProvider } from "./contexts/Sdk.tsx";
import { TelemetryProvider } from "./contexts/Telemetry.tsx";
import { AppRoute, useRoutes } from "./routes";

export default function App() {
  const [theme, setTheme] = useState<"light" | "dark">("light");

  const applyTheme = (theme: "light" | "dark") => {
    const root = document.documentElement;
    if (root.classList.contains(theme)) return;
    root.classList.add(theme);
    root.classList.remove(theme === "dark" ? "light" : "dark");

    // Update favicon based on theme
    const favicon = document.getElementById("favicon") as HTMLLinkElement;
    const faviconAlt = document.getElementById(
      "favicon-alt"
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
      "(prefers-color-scheme: dark)"
    ).matches;

    const initialTheme = savedTheme || (systemPrefersDark ? "dark" : "light");
    applyTheme(initialTheme);
  }, []);

  return (
    <MoonshineConfigProvider theme={theme} setTheme={applyTheme}>
      <TelemetryProvider>
        <BrowserRouter>
          <SdkProvider>
            <AuthProvider>
              <ProjectProvider>
                <RouteProvider />
              </ProjectProvider>
            </AuthProvider>
          </SdkProvider>
        </BrowserRouter>
      </TelemetryProvider>
    </MoonshineConfigProvider>
  );
}

const RouteProvider = () => {
  const routes = useRoutes();

  const unauthenticatedRoutes = Object.values(routes).filter(
    (route) => route.unauthenticated
  );

  const outsideStructureRoutes = Object.values(routes).filter(
    (route) => route.outsideMainLayout
  );

  const authenticatedRoutes = Object.values(routes).filter(
    (route) => !outsideStructureRoutes.includes(route) && !route.unauthenticated
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
    [routes]
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
              "url" in value
          ) as unknown as AppRoute[]
        )}
      </Route>
    ));
};
