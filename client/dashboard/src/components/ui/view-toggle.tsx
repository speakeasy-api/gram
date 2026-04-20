import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { LayoutGrid, List } from "lucide-react";
import type { ViewMode } from "@/components/ui/use-view-mode";

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
