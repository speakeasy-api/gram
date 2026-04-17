import { useState } from "react";

export type ViewMode = "grid" | "table";

const STORAGE_KEY = "gram-view-mode";

function getStoredViewMode(): ViewMode {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored === "grid" || stored === "table") return stored;
  } catch {
    // localStorage unavailable
  }
  return "grid";
}

function storeViewMode(mode: ViewMode) {
  try {
    localStorage.setItem(STORAGE_KEY, mode);
  } catch {
    // localStorage unavailable
  }
}

export function useViewMode() {
  const [mode, setMode] = useState(getStoredViewMode);
  return [
    mode,
    (v: ViewMode) => {
      storeViewMode(v);
      setMode(v);
    },
  ] as const;
}
