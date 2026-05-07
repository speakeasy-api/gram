import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRBAC } from "@/hooks/useRBAC";
import { Toolset } from "@/lib/toolTypes";
import { cn } from "@/lib/utils";
import {
  invalidateAllToolset,
  useUpdateToolsetMutation,
} from "@gram/client/react-query";
import { Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { ExternalLink, ListOrdered, Search } from "lucide-react";
import { toast } from "sonner";

interface ModeCardProps {
  selected: boolean;
  onSelect: () => void;
  icon: React.ReactNode;
  title: string;
  description: string;
  bestFor: string[];
  tradeoff: string;
}

function ModeCard({
  selected,
  onSelect,
  icon,
  title,
  description,
  bestFor,
  tradeoff,
}: ModeCardProps) {
  return (
    <button
      type="button"
      onClick={onSelect}
      className={cn(
        "flex cursor-pointer flex-col gap-4 rounded-lg border p-5 text-left transition-colors",
        selected
          ? "border-primary bg-card ring-primary ring-1"
          : "border-border hover:border-muted-foreground/40 hover:bg-card/50",
      )}
    >
      <div className="flex items-center gap-3">
        <div
          className={cn(
            "flex h-9 w-9 items-center justify-center rounded-md",
            selected
              ? "bg-primary/10 text-primary"
              : "bg-muted text-muted-foreground",
          )}
        >
          {icon}
        </div>
        <Heading variant="h4">{title}</Heading>
      </div>

      <Type muted className="text-sm leading-relaxed">
        {description}
      </Type>

      <div className="flex flex-col gap-1.5">
        <Type className="text-sm font-medium">Best for</Type>
        <ul className="flex flex-col gap-1 pl-4">
          {bestFor.map((item) => (
            <li key={item} className="text-muted-foreground list-disc text-sm">
              {item}
            </li>
          ))}
        </ul>
      </div>

      <div className="flex flex-col gap-1.5">
        <Type className="text-sm font-medium">Trade-off</Type>
        <Type muted className="text-sm">
          {tradeoff}
        </Type>
      </div>
    </button>
  );
}

export function MCPPerformanceTab({ toolset }: { toolset: Toolset }) {
  const { hasScope } = useRBAC();
  const canWrite = hasScope("mcp:write");
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();

  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => {
      invalidateAllToolset(queryClient);
      toast.success("Tool selection mode updated");
      telemetry.capture("mcp_event", {
        action: "tool_selection_mode_changed",
        slug: toolset.slug,
      });
    },
    onError: () => {
      toast.error("Failed to update tool selection mode");
    },
  });

  const toolSelectionMode = toolset.toolSelectionMode ?? "static";

  const onSelectMode = (mode: string) => {
    if (!canWrite || mode === toolSelectionMode) return;
    updateToolsetMutation.mutate({
      request: {
        slug: toolset.slug,
        updateToolsetRequestBody: { toolSelectionMode: mode },
      },
    });
  };

  return (
    <Stack gap={4}>
      <Stack gap={2}>
        <Heading variant="h3">Tool Selection Mode</Heading>
        <Type muted className="max-w-2xl text-sm">
          Choose how tools are exposed to the LLM. This affects token usage and
          how the model discovers available tools.
        </Type>
      </Stack>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <ModeCard
          selected={toolSelectionMode === "static"}
          onSelect={() => onSelectMode("static")}
          icon={<ListOrdered className="h-5 w-5" />}
          title="Static"
          description="Every tool schema is loaded into the LLM's context window upfront. Simple and predictable — the model sees all available tools immediately."
          bestFor={[
            "Small MCP servers (under ~20 tools)",
            "Predictable, fixed token budget per request",
          ]}
          tradeoff="Token cost scales linearly with tool count — large MCP servers can consume 200k+ tokens"
        />
        <ModeCard
          selected={toolSelectionMode === "dynamic"}
          onSelect={() => onSelectMode("dynamic")}
          icon={<Search className="h-5 w-5" />}
          title="Dynamic"
          description="Tools are discovered on demand through a three-step workflow: search available tools, describe the ones needed, then execute. Only relevant tool schemas enter the context window."
          bestFor={[
            "Large MCP servers (20+ tools, scales to 400+)",
            "Token-constrained environments — up to 96% fewer input tokens",
          ]}
          tradeoff="Requires 2–3x more tool calls (typically 6–8 vs 3 for complex tasks) and slight additional latency from the discovery steps"
        />
      </div>

      <a
        href="https://www.speakeasy.com/docs/mcp/build/toolsets/dynamic-toolsets"
        target="_blank"
        rel="noopener noreferrer"
        className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1.5 text-sm transition-colors"
      >
        Learn more about tool selection modes
        <ExternalLink className="h-3.5 w-3.5" />
      </a>
    </Stack>
  );
}
