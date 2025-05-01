import "@speakeasy-api/moonshine/moonshine.css";
import "./App.css"; // Import this second to override certain values in moonshine.css

import { useEffect, useState } from "react";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { AppLayout } from "./components/app-layout.tsx";
import { AppRoute, ROUTES } from "./routes.ts";
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

  const primaryCTA = ROUTES.primaryCTA[0];

  return (
    <ThemeContext.Provider value={{ theme, setTheme: applyTheme }}>
      <MoonshineConfigProvider themeElement={document.documentElement}>
        <SdkProvider>
          <AuthProvider>
            <BrowserRouter>
              <Routes>
                {/* Register these unauthenticated paths outside of root layout */}
                {routesWithSubroutes(ROUTES.unauthenticatedRoutes)}
                <Route path="/" element={<AppLayout />}>
                  <Route
                    path={primaryCTA.url}
                    element={<primaryCTA.component />}
                  />
                  {routesWithSubroutes(ROUTES.navMain)}
                  {routesWithSubroutes(ROUTES.navSecondary)}
                </Route>
              </Routes>
            </BrowserRouter>
          </AuthProvider>
        </SdkProvider>
      </MoonshineConfigProvider>
    </ThemeContext.Provider>
  );
}

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
        {item.subPages?.map((subPage) => (
          <Route
            key={subPage.title}
            path={subPage.url}
            element={subPage.component ? <subPage.component /> : null}
          />
        ))}
      </Route>
    ));
};
