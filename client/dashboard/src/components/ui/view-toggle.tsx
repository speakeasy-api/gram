import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { LayoutGrid, List } from "lucide-react";
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

export function ViewToggle({
  value,
  onChange,
}: {
  value: ViewMode;
  onChange: (value: ViewMode) => void;
}) {
  return (
    <ToggleGroup
      type="single"
      variant="outline"
      size="sm"
      value={value}
      onValueChange={(v) => {
        if (v === "grid" || v === "table") {
          storeViewMode(v);
          onChange(v);
        }
      }}
    >
      <ToggleGroupItem value="grid" aria-label="Grid view" className="px-3">
        <LayoutGrid className="size-4" />
      </ToggleGroupItem>
      <ToggleGroupItem value="table" aria-label="Table view" className="px-3">
        <List className="size-4" />
      </ToggleGroupItem>
    </ToggleGroup>
  );
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
