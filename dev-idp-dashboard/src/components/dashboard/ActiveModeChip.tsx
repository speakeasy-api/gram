import { cn } from "@/lib/utils";
import { useGramMode } from "@/hooks/use-gram-mode";
import { MODE_LABELS } from "@/lib/mode-labels";

/**
 * Small status chip for the global header. Always visible so the developer
 * knows at a glance which dev-idp mode Gram is wired to — the user's #1
 * complaint about the previous design.
 */
export function ActiveModeChip() {
  const { data, isLoading } = useGramMode();
  const mode = data?.mode ?? null;

  return (
    <div
      className={cn(
        "inline-flex items-center gap-2 rounded-full pl-2.5 pr-3 py-1",
        "border border-border bg-card text-xs",
      )}
    >
      <span
        className={cn(
          "inline-block size-1.5 rounded-full",
          isLoading
            ? "bg-muted-foreground animate-pulse"
            : mode
              ? "bg-[var(--retro-green)] shadow-[0_0_6px_var(--retro-green)]"
              : "bg-muted-foreground",
        )}
      />
      <span className="text-muted-foreground">
        {isLoading ? "checking…" : "active"}
      </span>
      <span className="font-mono font-medium">
        {isLoading ? "…" : mode ? MODE_LABELS[mode] : "none"}
      </span>
    </div>
  );
}
