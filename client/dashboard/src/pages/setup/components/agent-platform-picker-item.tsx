import { ChevronRight } from "lucide-react";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

export function AgentPlatformPickerItem({
  platformId,
  name,
  description,
  complete = false,
  statusBadge,
  disabled = false,
  onClick,
}: {
  platformId: string;
  name: string;
  description: string;
  complete?: boolean;
  statusBadge?: ReactNode;
  disabled?: boolean;
  onClick: () => void;
}): JSX.Element {
  return (
    <button
      type="button"
      disabled={disabled}
      onClick={onClick}
      className={cn(
        "flex w-full items-center gap-4 border p-4 text-left transition-all disabled:cursor-not-allowed disabled:opacity-50",
        complete
          ? "border-foreground/10 bg-secondary/20"
          : "border-border bg-card hover:border-foreground/20",
      )}
    >
      <div
        className={cn(
          "flex h-10 w-10 flex-shrink-0 items-center justify-center",
          complete ? "bg-foreground/10" : "bg-secondary",
        )}
      >
        <AgentProviderIcon source={platformId} className="h-5 w-5" />
      </div>
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex items-center gap-2">
          <p className="text-foreground text-sm font-medium">{name}</p>
          {statusBadge}
        </div>
        <p className="text-muted-foreground text-xs">{description}</p>
      </div>
      <ChevronRight className="text-muted-foreground h-4 w-4 flex-shrink-0" />
    </button>
  );
}
