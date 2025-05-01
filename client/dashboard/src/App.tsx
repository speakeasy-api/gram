import "@speakeasy-api/moonshine/moonshine.css";
import "./App.css"; // Import this second to override certain values in moonshine.css

import { useEffect, useState } from "react";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { AppLayout } from "./components/app-layout.tsx";
import { AppRoute, useRoutes } from "@/routes";
import { MoonshineConfigProvider } from "@speakeasy-api/moonshine";
import { ThemeContext } from "./components/ui/theme-toggle.tsx";
import { AuthProvider } from "./contexts/Auth.tsx";
import { SdkProvider } from "./contexts/Sdk.tsx";

export default function App() {
  const [theme, setTheme] = useState<"light" | "dark">("light");

  const applyTheme = (theme: "light" | "dark") => {
    const root = document.documentElement;
    if (root.classList.contains(theme)) return;
    root.classList.add(theme);
    root.classList.remove(theme === "dark" ? "light" : "dark");
    setTheme(theme);
  };

  useEffect(() => {
    const prefersDark = window.matchMedia(
      "(prefers-color-scheme: dark)"
    ).matches;
    applyTheme(prefersDark ? "dark" : "light");
  }, []);

  return (
    <ThemeContext.Provider value={{ theme, setTheme: applyTheme }}>
      <MoonshineConfigProvider themeElement={document.documentElement}>
        <BrowserRouter>
          <SdkProvider>
            <AuthProvider>
              <RouteProvider />
            </AuthProvider>
          </SdkProvider>
        </BrowserRouter>
      </MoonshineConfigProvider>
    </ThemeContext.Provider>
  );
}

const RouteProvider = () => {
  const routes = useRoutes();

  const unauthenticatedRoutes = Object.values(routes).filter(
    (route) => route.unauthenticated
  );

  const authenticatedRoutes = Object.values(routes).filter(
    (route) => !route.unauthenticated
  );

  return (
    <Routes>
      {/* Register these unauthenticated paths outside of root layout */}
      {routesWithSubroutes(unauthenticatedRoutes)}
      <Route path="/" element={<AppLayout />}>
        <Route path=":orgSlug/:projectSlug">
          {routesWithSubroutes(authenticatedRoutes)}
        </Route>
      </Route>
    </Routes>
  );
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
        {routesWithSubroutes(Object.values(item.subPages ?? {}))}
      </Route>
    ));
};
