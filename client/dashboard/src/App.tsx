import "./App.css";
import { useCallback, useEffect, useState } from "react";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { RootLayout } from "./components/root-layout.tsx";
import { AppRoute, ROUTES } from "./routes.ts";
import { MoonshineConfigProvider } from "@speakeasy-api/moonshine";
import { ThemeContext } from "./components/ui/theme-toggle.tsx";
import { AuthProvider } from "./contexts/Auth.tsx";
import { SdkProvider } from "./contexts/Sdk.tsx";

export default function App() {
  const [theme, setTheme] = useState<"light" | "dark">("light");

  const applyTheme = useCallback(
    (theme: "light" | "dark") => {
      if (theme === "dark") {
        document.documentElement.classList.add("dark");
      } else {
        document.documentElement.classList.remove("dark");
      }
      setTheme(theme);
    },
    [theme]
  );

  useEffect(() => {
    if (window.matchMedia("(prefers-color-scheme: dark)").matches) {
      applyTheme("dark");
    }
  }, []);

  return (
    <ThemeContext.Provider value={{ theme, setTheme: applyTheme }}>
      <MoonshineConfigProvider themeElement={document.documentElement}>
        <SdkProvider>
          <AuthProvider>
            <BrowserRouter>
              <Routes>
                <Route path="/" element={<RootLayout />}>
                  <Route
                    path={ROUTES.primaryCTA.url}
                    element={<ROUTES.primaryCTA.component />}
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
