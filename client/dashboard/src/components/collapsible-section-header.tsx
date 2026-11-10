import { Badge } from "@/components/ui/Badge";
import { cn } from "@/lib/utils";
import { ChevronRight } from "lucide-react";

/**
 * The row that opens and closes one group of a sectioned list: chevron, label,
 * a one-line description and, when the group is counted, how many rows it
 * holds. The Detection Rules chevron idiom, shared by the device agent's scan
 * target library and the Shadow AI policy's decisions list so a group reads
 * the same wherever it appears.
 */
export function CollapsibleSectionHeader({
  label,
  description,
  count,
  expanded,
  onToggle,
}: {
  label: string;
  description: string;
  count?: number;
  expanded: boolean;
  onToggle: () => void;
}): JSX.Element {
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-expanded={expanded}
      className="hover:bg-muted/40 flex w-full items-center gap-3 px-4 py-3 text-left transition-colors"
    >
      <ChevronRight
        className={cn(
          "text-muted-foreground h-4 w-4 shrink-0 transition-transform",
          expanded && "rotate-90",
        )}
      />
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium">{label}</div>
        <div className="text-muted-foreground line-clamp-1 text-xs">
          {description}
        </div>
      </div>
      {count !== undefined ? (
        <Badge variant="neutral" className="shrink-0">
          <Badge.Text>{String(count)}</Badge.Text>
        </Badge>
      ) : null}
    </button>
  );
}
