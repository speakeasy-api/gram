import { StatusDot } from "@/components/ui/StatusDot";
import { cn } from "@/lib/utils";
import { ChevronRight } from "lucide-react";
import { useState } from "react";
import { ToolMetadataDriftPanel } from "../x/tabs/ToolMetadataDriftPanel";
import type { ToolDrift } from "../x/tabs/toolMetadataSync";

/**
 * One quiet line saying the saved tool list is out of date. The full diff and
 * the Sync button open beneath it, so the tool list stays the first thing on
 * the page.
 */
export function DriftNotice({
  drift,
  onSync,
  isSyncing,
}: {
  drift: ToolDrift[];
  onSync: () => void;
  isSyncing: boolean;
}): JSX.Element {
  const [open, setOpen] = useState(false);
  const count = drift.length;
  return (
    <div className="flex flex-col gap-3">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        className="text-muted-foreground hover:text-foreground flex items-center gap-2 text-left text-sm"
      >
        <StatusDot tone="warning" />
        <span>
          {count} {count === 1 ? "tool differs" : "tools differ"} from the saved
          list for this project
        </span>
        <span className="text-foreground flex items-center gap-0.5 font-medium">
          Review
          <ChevronRight
            className={cn("size-3.5 transition-transform", open && "rotate-90")}
          />
        </span>
      </button>
      {open ? (
        <ToolMetadataDriftPanel
          drift={drift}
          onSync={onSync}
          isSyncing={isSyncing}
        />
      ) : null}
    </div>
  );
}
