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
  return (
    <div className="flex flex-col gap-3">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        className="text-muted-foreground hover:text-foreground flex items-center gap-2 text-left text-sm"
      >
        <StatusDot tone="warning" />
        <span>{driftSummary(drift)}</span>
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

/**
 * What changed, by kind: "1 tool was removed on the server", or
 * "2 tools were added and 1 tool changed on the server".
 */
function driftSummary(drift: ToolDrift[]): string {
  const count = (kind: ToolDrift["kind"]) =>
    drift.filter((entry) => entry.kind === kind).length;
  const tools = (n: number, verb: string) =>
    n === 1 ? `1 tool ${verb}` : `${n} tools ${verb}`;
  const added = count("new");
  const changed = count("changed");
  const removed = count("removed");
  const parts = [
    added > 0 ? tools(added, added === 1 ? "was added" : "were added") : "",
    changed > 0 ? tools(changed, "changed") : "",
    removed > 0
      ? tools(removed, removed === 1 ? "was removed" : "were removed")
      : "",
  ].filter(Boolean);
  const joined =
    parts.length > 1
      ? `${parts.slice(0, -1).join(", ")} and ${parts.at(-1)}`
      : (parts[0] ?? "");
  return `${joined} on the server`;
}
