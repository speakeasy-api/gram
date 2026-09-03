import { Checkbox } from "@/components/ui/Checkbox";
import { Heading } from "@/components/ui/Heading";
import { Label } from "@/components/ui/Label";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRBAC } from "@/hooks/useRBAC";
import { Tool, Toolset } from "@/lib/toolTypes";
import { cn } from "@/lib/utils";
import { invalidateAllToolset } from "@gram/client/react-query/toolset.js";
import { useUpdateToolsetMutation } from "@gram/client/react-query/updateToolset.js";
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
        "flex cursor-pointer flex-col gap-4 border p-5 text-left transition-colors",
        selected
          ? "border-primary bg-card ring-primary ring-1"
          : "border-border hover:border-muted-foreground/40 hover:bg-card/50",
      )}
    >
      <div className="flex items-center gap-3">
        <div
          className={cn(
            "flex h-9 w-9 items-center justify-center",
            selected
              ? "bg-primary/10 text-primary"
              : "bg-muted text-muted-foreground",
          )}
        >
          {icon}
        </div>
        <Heading variant="h4">{title}</Heading>
      </div>

      <Text muted className="text-sm leading-relaxed">
        {description}
      </Text>

      <div className="flex flex-col gap-1.5">
        <Text className="text-sm font-medium">Best for</Text>
        <ul className="flex flex-col gap-1 pl-4">
          {bestFor.map((item) => (
            <li key={item} className="text-muted-foreground list-disc text-sm">
              {item}
            </li>
          ))}
        </ul>
      </div>

      <div className="flex flex-col gap-1.5">
        <Text className="text-sm font-medium">Trade-off</Text>
        <Text muted className="text-sm">
          {tradeoff}
        </Text>
      </div>
    </button>
  );
}

export function MCPPerformanceTab({
  toolset,
}: {
  toolset: Toolset;
}): JSX.Element {
  const { hasScope } = useRBAC();
  const canWrite = hasScope("mcp:write");
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();

  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: (_data, variables) => {
      void invalidateAllToolset(queryClient);
      if (variables.request.updateToolsetRequestBody.topLevelToolUrns) {
        toast.success("Always-available tools updated");
      } else {
        toast.success("Tool selection mode updated");
      }
      telemetry.capture("mcp_event", {
        action: variables.request.updateToolsetRequestBody.topLevelToolUrns
          ? "top_level_tools_changed"
          : "tool_selection_mode_changed",
        slug: toolset.slug,
      });
    },
    onError: () => {
      toast.error("Failed to update toolset");
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

  const topLevelToolUrns = toolset.topLevelToolUrns ?? [];
  const pinned = new Set(topLevelToolUrns);

  const onToggleTopLevelTool = (toolUrn: string, nextChecked: boolean) => {
    if (!canWrite) return;
    const next = nextChecked
      ? [...topLevelToolUrns.filter((urn) => urn !== toolUrn), toolUrn]
      : topLevelToolUrns.filter((urn) => urn !== toolUrn);
    updateToolsetMutation.mutate({
      request: {
        slug: toolset.slug,
        updateToolsetRequestBody: { topLevelToolUrns: next },
      },
    });
  };

  const selectableTools = toolset.tools.toSorted((a, b) =>
    a.name.localeCompare(b.name),
  );

  return (
    <Stack gap={4}>
      <Stack gap={2}>
        <Heading variant="h3">Tool Selection Mode</Heading>
        <Text muted className="max-w-2xl text-sm">
          Choose how tools are exposed to the LLM. This affects token usage and
          how the model discovers available tools.
        </Text>
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

      {toolSelectionMode === "dynamic" ? (
        <Stack gap={3} className="border-border border p-5">
          <Stack gap={1}>
            <Heading variant="h4">Always-available tools</Heading>
            <Text muted className="max-w-2xl text-sm">
              These tools appear next to search_tools, describe_tools, and
              execute_tool so the model can call them without searching first.
            </Text>
          </Stack>
          {selectableTools.length === 0 ? (
            <Text muted className="text-sm">
              Add tools to this server to pin them as always available.
            </Text>
          ) : (
            <ul className="flex max-h-80 flex-col gap-2 overflow-y-auto">
              {selectableTools.map((tool) => (
                <TopLevelToolRow
                  key={tool.toolUrn}
                  tool={tool}
                  checked={pinned.has(tool.toolUrn)}
                  disabled={!canWrite || updateToolsetMutation.isPending}
                  onCheckedChange={(next) =>
                    onToggleTopLevelTool(tool.toolUrn, next)
                  }
                />
              ))}
            </ul>
          )}
        </Stack>
      ) : null}

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

function TopLevelToolRow({
  tool,
  checked,
  disabled,
  onCheckedChange,
}: {
  tool: Tool;
  checked: boolean;
  disabled: boolean;
  onCheckedChange: (checked: boolean) => void;
}): JSX.Element {
  const checkboxId = `top-level-tool-${tool.toolUrn}`;

  return (
    <li className="flex items-start gap-3">
      <Checkbox
        id={checkboxId}
        checked={checked}
        disabled={disabled}
        onCheckedChange={(value) => onCheckedChange(value === true)}
        className="mt-0.5"
      />
      <Label
        htmlFor={checkboxId}
        className="min-w-0 cursor-pointer font-normal"
      >
        <span className="text-foreground block truncate text-sm">
          {tool.name}
        </span>
        {tool.description ? (
          <span className="text-muted-foreground line-clamp-2 text-xs">
            {tool.description}
          </span>
        ) : null}
      </Label>
    </li>
  );
}
