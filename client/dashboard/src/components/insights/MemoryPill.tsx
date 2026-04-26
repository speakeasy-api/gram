import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import type { Memory } from "@gram/client/models/components";
import {
  invalidateAllInsightsListMemories,
  useInsightsForgetMemoryByIdMutation,
  useInsightsListMemories,
} from "@gram/client/react-query";
import { useQueryClient } from "@tanstack/react-query";
import { Brain, Trash2 } from "lucide-react";
import { toast } from "sonner";

/**
 * MemoryPill renders a compact chip strip of the top 5 most-recently-used
 * workspace memories. Clicking it opens a popover listing all memories with
 * a per-row Forget button.
 */
export function MemoryPill() {
  const queryClient = useQueryClient();
  // throwOnError: false — a 401/5xx from listMemories should degrade to "no
  // memories" rather than triggering the global error boundary and replacing
  // the dashboard with a fatal error screen.
  const { data, isLoading } = useInsightsListMemories(
    { limit: 50 },
    undefined,
    {
      throwOnError: false,
      // Poll every 10s so memories the agent saves mid-conversation appear
      // in the chip strip without a manual refresh.
      refetchInterval: 10_000,
    },
  );

  const memories = data?.memories ?? [];
  const top5 = memories.slice(0, 5);

  const forgetMutation = useInsightsForgetMemoryByIdMutation({
    onSuccess: () => {
      void invalidateAllInsightsListMemories(queryClient);
      toast.success("Memory forgotten");
    },
    onError: (err) => toast.error(`Forget failed: ${err.message}`),
  });

  if (isLoading) {
    return null;
  }

  // Empty state — keep the chip visible so users on a fresh project see the
  // feature exists. Static (no popover) when there's nothing to expand.
  if (memories.length === 0) {
    return (
      <div className="mx-4 mt-3">
        <div className="border-border bg-background text-muted-foreground flex w-full items-center gap-2 rounded-md border px-3 py-2 text-xs">
          <Brain className="size-3.5 shrink-0" />
          <span className="shrink-0 font-medium">Remembering:</span>
          <span className="truncate">
            nothing yet — the assistant can save facts across sessions.
          </span>
        </div>
      </div>
    );
  }

  const handleForget = (memory: Memory) => {
    forgetMutation.mutate({
      request: {
        forgetMemoryForm: { memoryId: memory.id },
      },
    });
  };

  return (
    <div className="mx-4 mt-3">
      <Popover>
        <PopoverTrigger asChild>
          <button
            type="button"
            className="border-border bg-background hover:bg-muted/50 flex w-full items-center gap-2 rounded-md border px-3 py-2 text-left text-xs transition-colors"
          >
            <Brain className="text-muted-foreground size-3.5 shrink-0" />
            <span className="text-muted-foreground shrink-0 font-medium">
              Remembering:
            </span>
            <span className="text-foreground truncate">
              {top5.map((m) => memoryLabel(m)).join(", ")}
            </span>
            {memories.length > top5.length && (
              <span className="text-muted-foreground shrink-0">
                +{memories.length - top5.length}
              </span>
            )}
          </button>
        </PopoverTrigger>
        <PopoverContent className="w-96 p-0" align="start">
          <div className="border-border border-b px-3 py-2">
            <span className="text-sm font-semibold">Workspace memory</span>
            <p className="text-muted-foreground mt-0.5 text-xs">
              Facts, playbooks, and findings the agent recalls across sessions.
            </p>
          </div>
          <div className="max-h-80 overflow-y-auto">
            {memories.map((memory) => (
              <MemoryRow
                key={memory.id}
                memory={memory}
                onForget={handleForget}
                disabled={forgetMutation.isPending}
              />
            ))}
          </div>
        </PopoverContent>
      </Popover>
    </div>
  );
}

function MemoryRow({
  memory,
  onForget,
  disabled,
}: {
  memory: Memory;
  onForget: (m: Memory) => void;
  disabled: boolean;
}) {
  return (
    <div className="border-border hover:bg-muted/50 flex items-start gap-2 border-b px-3 py-2 last:border-b-0">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <Badge variant="outline" size="sm">
            {memory.kind}
          </Badge>
          {memory.tags.slice(0, 3).map((tag) => (
            <span
              key={tag}
              className="text-muted-foreground font-mono text-[10px]"
            >
              #{tag}
            </span>
          ))}
        </div>
        <p className="text-foreground mt-1 text-xs break-words">
          {memory.content}
        </p>
      </div>
      <Button
        size="icon-sm"
        variant="ghost"
        onClick={() => onForget(memory)}
        disabled={disabled}
        aria-label="Forget memory"
      >
        <Trash2 className="size-3.5" />
      </Button>
    </div>
  );
}

function memoryLabel(m: Memory): string {
  // Keep pill text tight: use the first tag if present, otherwise truncate
  // content to ~24 chars.
  if (m.tags.length > 0) {
    return m.tags[0] ?? m.content.slice(0, 24);
  }
  return m.content.length > 24 ? `${m.content.slice(0, 24)}…` : m.content;
}
