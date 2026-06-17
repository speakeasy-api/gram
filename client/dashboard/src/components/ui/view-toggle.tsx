import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { LayoutGrid, List } from "lucide-react";
import { cn } from "@/lib/utils";
import type { ViewMode } from "@/components/ui/use-view-mode";

export function ViewToggle({
  value,
  onChange,
  itemClassName,
}: {
  value: ViewMode;
  onChange: (value: ViewMode) => void;
  /**
   * Per-item override (e.g. `h-9` to line up with a filter bar). The toggle's
   * height comes from the items, so this is applied to each one rather than the
   * group. Defaults leave the shared `size="sm"` (h-8) height untouched for the
   * non-filter-bar usages (e.g. Sources).
   */
  itemClassName?: string;
}): JSX.Element {
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
      <ToggleGroupItem
        value="grid"
        aria-label="Grid view"
        title="Grid view"
        className={cn("px-3", itemClassName)}
      >
        <LayoutGrid className="size-4" />
      </ToggleGroupItem>
      <ToggleGroupItem
        value="table"
        aria-label="Table view"
        title="Table view"
        className={cn("px-3", itemClassName)}
      >
        <List className="size-4" />
      </ToggleGroupItem>
    </ToggleGroup>
  );
}
