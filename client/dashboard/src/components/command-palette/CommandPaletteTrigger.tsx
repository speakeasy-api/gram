import { Kbd } from "@/components/ui/kbd";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { useCommandPalette } from "@/contexts/CommandPalette";
import { cn } from "@/lib/utils";
import { Search } from "lucide-react";

function isMacPlatform(): boolean {
  if (typeof navigator === "undefined") return true;
  return /mac|iphone|ipad|ipod/i.test(
    navigator.platform || navigator.userAgent,
  );
}

/**
 * Magnifying-glass button that opens the command palette. Lives next to the
 * logo in the sidebar header; hovering reveals the ⌘K / Ctrl K shortcut. The
 * keyboard shortcut itself is bound globally in CommandPaletteProvider — this
 * is just the discoverable, clickable affordance.
 */
export function CommandPaletteTrigger({
  className,
}: {
  className?: string;
}): JSX.Element {
  const { open } = useCommandPalette();
  const isMac = isMacPlatform();

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          onClick={open}
          aria-label="Search (Command palette)"
          className={cn(
            "text-muted-foreground hover:text-foreground hover:bg-muted flex size-7 shrink-0 items-center justify-center transition-colors",
            className,
          )}
        >
          <Search className="size-4" />
        </button>
      </TooltipTrigger>
      <TooltipContent className="flex items-center gap-1.5">
        <span>Search</span>
        <Kbd className="pointer-events-none gap-0.5 px-1.5 select-none">
          {isMac ? <span className="text-xs">⌘</span> : "Ctrl"}K
        </Kbd>
      </TooltipContent>
    </Tooltip>
  );
}
