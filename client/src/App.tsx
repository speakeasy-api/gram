import "./App.css";

import { NAV_ITEMS } from "@/components/app-sidebar";
import { useEffect } from "react";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { RootLayout } from "./components/root-layout";

export default function App() {
  useEffect(() => {
    if (window.matchMedia("(prefers-color-scheme: dark)").matches) {
      document.documentElement.classList.add("dark");
    }
  }, []);

  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<RootLayout />}>
          <Route path={NAV_ITEMS.primaryCTA.url} element={<NAV_ITEMS.primaryCTA.component />} />
          {NAV_ITEMS.navMain.map((item) => (
            <Route
              key={item.title}
              path={item.url}
              element={<item.component />}
            />
          ))}
        </Route>
      </Routes>
    </BrowserRouter>
  );
}
