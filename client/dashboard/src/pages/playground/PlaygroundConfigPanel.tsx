import { AnnotationBadges } from "@/components/tool-list/AnnotationBadges";
import { MethodBadge } from "@/components/tool-list/MethodBadge";
import { Button } from "@/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Slider } from "@/components/ui/slider";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { AVAILABLE_MODELS } from "@/lib/models";
import { cn } from "@/lib/utils";
import { Tool, getToolSourceLabel } from "@/lib/toolTypes";
import {
  ChevronDownIcon,
  ChevronRightIcon,
  FileCode,
  Layers,
  PencilRuler,
  PlusIcon,
  SquareFunction,
} from "lucide-react";
import { useEffect, useState } from "react";
import { McpIcon } from "@/components/ui/mcp-icon";
import { Badge } from "@/components/ui/badge";

interface ToolsetInfo {
  name: string;
  slug: string;
  description?: string;
  toolCount?: number;
  updatedAt?: Date;
}

interface ToolGroup {
  type: "package" | "function" | "custom" | "higher_order" | "platform";
  icon: typeof FileCode;
  title: string;
  tools: Tool[];
  packageName?: string;
}

/** A read-only tool advertised by a remote MCP server (no Gram-side identity). */
interface ReadOnlyTool {
  name: string;
  description?: string;
}

interface ToolsetSectionProps {
  tools?: Tool[];
  /**
   * Live tools from a remote-MCP-backed server, rendered read-only. When set,
   * the grouped/editable Gram-tool list is replaced by this flat list.
   */
  remoteTools?: ReadOnlyTool[];
  selectedTools?: Set<string>;
  onToolToggle?: (toolId: string) => void;
  temperature?: number;
  onTemperatureChange?: (temp: number) => void;
  model?: string;
  onModelChange?: (model: string) => void;
  maxTokens?: number;
  onMaxTokensChange?: (tokens: number) => void;
  toolsetSelector?: React.ReactNode;
  authSettings?: React.ReactNode;
  toolsetInfo?: ToolsetInfo;
  onToolsetUpdate?: (updates: { name?: string; description?: string }) => void;
  documentIdToName?: Record<string, string>;
  functionIdToName?: Record<string, string>;
  onOpenToolsModal?: () => void;
  onOpenGroupModal?: (groupTitle: string) => void;
  onToolClick?: (tool: Tool) => void;
}

// Sort HTTP tools by method in round-robin fashion for visual variety
const HTTP_METHOD_ORDER = ["GET", "POST", "PUT", "DELETE"];

function sortToolsByMethod(tools: Tool[]): Tool[] {
  const httpTools = tools.filter((t) => t.type === "http");
  const nonHttpTools = tools.filter((t) => t.type !== "http");

  const toolsByMethod = new Map<string, typeof httpTools>();
  httpTools.forEach((tool) => {
    if (tool.type === "http") {
      const method = tool.httpMethod?.toUpperCase() || "UNKNOWN";
      if (!toolsByMethod.has(method)) {
        toolsByMethod.set(method, []);
      }
      toolsByMethod.get(method)!.push(tool);
    }
  });

  const sortedTools: Tool[] = [];

  // Round-robin through GET, POST, PUT, DELETE
  while (httpTools.length > sortedTools.length) {
    HTTP_METHOD_ORDER.forEach((method) => {
      let toolsForMethod = toolsByMethod.get(method);

      // If PUT is empty but PATCH exists, use PATCH instead (both are orange)
      if (
        method === "PUT" &&
        (!toolsForMethod || toolsForMethod.length === 0)
      ) {
        toolsForMethod = toolsByMethod.get("PATCH");
      }

      if (toolsForMethod && toolsForMethod.length > 0) {
        sortedTools.push(toolsForMethod.shift()!);
      }
    });

    // Handle any other methods
    toolsByMethod.forEach((tools, method) => {
      if (
        !HTTP_METHOD_ORDER.includes(method) &&
        method !== "PATCH" &&
        tools.length > 0
      ) {
        sortedTools.push(tools.shift()!);
      }
    });
  }

  return [...sortedTools, ...nonHttpTools];
}

function groupTools(
  tools: Tool[],
  documentIdToName?: Record<string, string>,
  functionIdToName?: Record<string, string>,
): ToolGroup[] {
  const groups: ToolGroup[] = [];
  const packageMap = new Map<string, Tool[]>();
  const functionMap = new Map<string, Tool[]>();
  const platformSourceToTools = new Map<string, Tool[]>();
  const functionTools: Tool[] = [];
  const customTools: Tool[] = [];
  const higherOrderTools: Tool[] = [];

  tools.forEach((tool) => {
    if (tool.type === "http") {
      let groupKey: string | undefined;

      if (tool.packageName) {
        groupKey = tool.packageName;
      } else if (tool.openapiv3DocumentId && documentIdToName) {
        groupKey = documentIdToName[tool.openapiv3DocumentId];
      } else if (tool.deploymentId) {
        groupKey = tool.deploymentId;
      }

      if (groupKey) {
        const existing = packageMap.get(groupKey) || [];
        packageMap.set(groupKey, [...existing, tool]);
      } else {
        // HTTP tools without any identifier go to custom
        customTools.push(tool);
      }
    } else if (tool.type === "function") {
      let groupKey: string | undefined;

      if (tool.functionId && functionIdToName) {
        groupKey = functionIdToName[tool.functionId];
      }

      if (groupKey) {
        const existing = functionMap.get(groupKey) || [];
        functionMap.set(groupKey, [...existing, tool]);
      } else {
        // Function tools without a source go to the generic functions group
        functionTools.push(tool);
      }
    } else if (tool.type === "platform") {
      const groupKey = getToolSourceLabel(tool);
      const existing = platformSourceToTools.get(groupKey) || [];
      platformSourceToTools.set(groupKey, [...existing, tool]);
    } else {
      // Everything else (prompts, etc.)
      customTools.push(tool);
    }
  });

  // Add package groups with sorted tools
  packageMap.forEach((tools, packageName) => {
    groups.push({
      type: "package",
      icon: FileCode,
      title: packageName,
      tools: sortToolsByMethod(tools),
      packageName,
    });
  });

  // Add function groups
  functionMap.forEach((tools, functionName) => {
    groups.push({
      type: "function",
      icon: SquareFunction,
      title: functionName,
      tools,
      packageName: functionName,
    });
  });

  // Add function tools group
  if (functionTools.length > 0) {
    groups.push({
      type: "function",
      icon: SquareFunction,
      title: "Functions",
      tools: functionTools,
    });
  }

  platformSourceToTools.forEach((tools, sourceName) => {
    groups.push({
      type: "platform",
      icon: McpIcon,
      title: sourceName,
      tools,
    });
  });

  // Add custom tools group with sorted tools
  if (customTools.length > 0) {
    groups.push({
      type: "custom",
      icon: PencilRuler,
      title: "Custom",
      tools: sortToolsByMethod(customTools),
    });
  }

  // Add higher order tools group
  if (higherOrderTools.length > 0) {
    groups.push({
      type: "higher_order",
      icon: Layers,
      title: "Higher Order Tools",
      tools: higherOrderTools,
    });
  }

  return groups;
}

export function PlaygroundConfigPanel({
  tools = [],
  remoteTools,
  selectedTools: _selectedTools = new Set(),
  onToolToggle: _onToolToggle,
  temperature,
  onTemperatureChange,
  model,
  onModelChange,
  maxTokens,
  onMaxTokensChange,
  toolsetSelector,
  authSettings,
  toolsetInfo: _toolsetInfo,
  onToolsetUpdate: _onToolsetUpdate,
  documentIdToName,
  functionIdToName,
  onOpenToolsModal,
  onOpenGroupModal: _onOpenGroupModal,
  onToolClick,
}: ToolsetSectionProps): JSX.Element {
  const [toolsOpen, setToolsOpen] = useState(true);
  const [configOpen, setConfigOpen] = useState(true);
  const [authOpen, setAuthOpen] = useState(true);
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set());

  const toolGroups = groupTools(tools, documentIdToName, functionIdToName);

  // Initialize all groups as expanded
  const [isInitialized, setIsInitialized] = useState(false);

  useEffect(() => {
    if (!isInitialized && toolGroups.length > 0) {
      setExpandedGroups(new Set(toolGroups.map((g) => g.title)));
      setIsInitialized(true);
    }
  }, [toolGroups, isInitialized]);

  const toggleGroup = (groupTitle: string) => {
    setExpandedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(groupTitle)) {
        next.delete(groupTitle);
      } else {
        next.add(groupTitle);
      }
      return next;
    });
  };

  return (
    <div className="flex h-full flex-col overflow-y-auto border-r">
      {/* Toolset Selector - Always at top */}
      <div className="border-b px-4 py-3">
        <Label className="text-muted-foreground mb-1.5 block text-[11px] font-medium tracking-wider uppercase">
          MCP Server
        </Label>
        {toolsetSelector}
      </div>

      {/* Auth Settings Section */}
      {authSettings && (
        <div className="border-b">
          <Collapsible open={authOpen} onOpenChange={setAuthOpen}>
            <CollapsibleTrigger className="hover:bg-muted/30 group flex w-full items-center px-4 py-2.5 transition-colors">
              <div className="flex items-center gap-1.5">
                <span className="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">
                  Authentication
                </span>
                {authOpen ? (
                  <ChevronDownIcon className="text-muted-foreground h-3.5 w-3.5" />
                ) : (
                  <ChevronRightIcon className="text-muted-foreground h-3.5 w-3.5" />
                )}
              </div>
            </CollapsibleTrigger>
            <CollapsibleContent className="px-4 pt-2 pb-3">
              {authSettings}
            </CollapsibleContent>
          </Collapsible>
        </div>
      )}

      {/* Tools Section — fills remaining height and scrolls internally when open */}
      <div
        className={cn("border-b", toolsOpen && "flex min-h-0 flex-1 flex-col")}
      >
        <Collapsible
          open={toolsOpen}
          onOpenChange={setToolsOpen}
          className="flex min-h-0 flex-1 flex-col"
        >
          <CollapsibleTrigger className="hover:bg-muted/30 group flex w-full items-center justify-between px-4 py-2.5 transition-colors">
            <div className="flex items-center gap-1.5">
              <span className="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">
                Tools
              </span>
              {toolsOpen ? (
                <ChevronDownIcon className="text-muted-foreground h-3.5 w-3.5" />
              ) : (
                <ChevronRightIcon className="text-muted-foreground h-3.5 w-3.5" />
              )}
            </div>

            {onOpenToolsModal && (
              <Button
                size="sm"
                variant="tertiary"
                className="h-6 px-2"
                onClick={(e: React.MouseEvent) => {
                  e.stopPropagation();
                  onOpenToolsModal();
                }}
              >
                <PlusIcon className="size-3.5" />
              </Button>
            )}
          </CollapsibleTrigger>
          <CollapsibleContent className="flex min-h-0 flex-1 flex-col py-1">
            <div className="min-h-0 flex-1 overflow-y-auto">
              <ToolsBody
                remoteTools={remoteTools}
                toolGroups={toolGroups}
                expandedGroups={expandedGroups}
                onToggleGroup={toggleGroup}
                onToolClick={onToolClick}
              />
            </div>
          </CollapsibleContent>
        </Collapsible>
      </div>

      {/* Configuration Section */}
      <div className="border-b">
        <Collapsible open={configOpen} onOpenChange={setConfigOpen}>
          <CollapsibleTrigger className="hover:bg-muted/30 group flex w-full items-center px-4 py-2.5 transition-colors">
            <div className="flex items-center gap-1.5">
              <span className="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">
                Model Settings
              </span>
              {configOpen ? (
                <ChevronDownIcon className="text-muted-foreground h-3.5 w-3.5" />
              ) : (
                <ChevronRightIcon className="text-muted-foreground h-3.5 w-3.5" />
              )}
            </div>
          </CollapsibleTrigger>
          <CollapsibleContent className="space-y-4 px-4 pt-2 pb-3">
            {/* Model */}
            {model !== undefined && onModelChange && (
              <div className="space-y-2">
                <Label htmlFor="model" className="text-xs font-medium">
                  Model
                </Label>
                <Select value={model} onValueChange={onModelChange}>
                  <SelectTrigger size="sm" className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {AVAILABLE_MODELS.map((m) => (
                      <SelectItem key={m.value} value={m.value}>
                        <span className="flex items-center gap-2">
                          {m.label}
                          {m.expensive && (
                            <Badge size="sm" variant="warning" background>
                              <Badge.Text>Expensive</Badge.Text>
                            </Badge>
                          )}
                        </span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}

            {/* Temperature */}
            {temperature !== undefined && onTemperatureChange && (
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="temperature" className="text-xs font-medium">
                    Temperature
                  </Label>
                  <SimpleTooltip tooltip="Controls randomness in responses. Lower values (0.0-0.3) make outputs more focused and deterministic. Higher values (0.7-1.0) increase creativity and variety.">
                    <span className="text-muted-foreground cursor-help font-mono text-xs">
                      {temperature.toFixed(1)}
                    </span>
                  </SimpleTooltip>
                </div>
                <Slider
                  id="temperature"
                  value={temperature}
                  onChange={onTemperatureChange}
                  min={0}
                  max={1}
                  step={0.1}
                  className="w-full"
                />
              </div>
            )}

            {/* Max Tokens */}
            {maxTokens !== undefined && onMaxTokensChange && (
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label htmlFor="maxTokens" className="text-xs font-medium">
                    Max Tokens
                  </Label>
                  <SimpleTooltip tooltip="Maximum number of tokens in the response. Higher values allow longer responses but may increase cost.">
                    <span className="text-muted-foreground cursor-help font-mono text-xs">
                      {maxTokens}
                    </span>
                  </SimpleTooltip>
                </div>
                <Slider
                  id="maxTokens"
                  value={maxTokens}
                  onChange={onMaxTokensChange}
                  min={256}
                  max={8192}
                  step={256}
                  className="w-full"
                />
              </div>
            )}
          </CollapsibleContent>
        </Collapsible>
      </div>
    </div>
  );
}

/** Picks the right tool list for the panel: remote (read-only), grouped, or empty. */
function ToolsBody({
  remoteTools,
  toolGroups,
  expandedGroups,
  onToggleGroup,
  onToolClick,
}: {
  remoteTools?: ReadOnlyTool[];
  toolGroups: ToolGroup[];
  expandedGroups: Set<string>;
  onToggleGroup: (groupTitle: string) => void;
  onToolClick?: (tool: Tool) => void;
}): JSX.Element {
  if (remoteTools !== undefined) {
    return <ReadOnlyToolList tools={remoteTools} />;
  }
  if (toolGroups.length === 0) {
    return (
      <div className="px-4 py-6 text-center">
        <Type variant="small" className="text-muted-foreground">
          No tools added
        </Type>
      </div>
    );
  }
  return (
    <GroupedToolList
      toolGroups={toolGroups}
      expandedGroups={expandedGroups}
      onToggleGroup={onToggleGroup}
      onToolClick={onToolClick}
    />
  );
}

/** The grouped, editable Gram-tool list (toolset-backed servers). */
function GroupedToolList({
  toolGroups,
  expandedGroups,
  onToggleGroup,
  onToolClick,
}: {
  toolGroups: ToolGroup[];
  expandedGroups: Set<string>;
  onToggleGroup: (groupTitle: string) => void;
  onToolClick?: (tool: Tool) => void;
}): JSX.Element {
  return (
    <div>
      {toolGroups.map((group) => {
        const isExpanded = expandedGroups.has(group.title);
        return (
          <div key={group.title}>
            {/* Group Header */}
            <button
              onClick={() => onToggleGroup(group.title)}
              className="bg-surface-secondary-default hover:bg-active group/item flex w-full items-center gap-2 px-3 py-2 text-left transition-colors"
            >
              <group.icon className="text-muted-foreground size-3.5 shrink-0" />
              <div className="min-w-0 truncate text-xs">{group.title}</div>
              {isExpanded ? (
                <ChevronDownIcon className="text-muted-foreground size-3.5 shrink-0" />
              ) : (
                <ChevronRightIcon className="text-muted-foreground size-3.5 shrink-0" />
              )}
              <div className="flex-1" />
              <div className="text-muted-foreground text-[11px] tabular-nums">
                {group.tools.length}
              </div>
            </button>

            {/* Expanded Tool List */}
            {isExpanded && (
              <div>
                {group.tools.map((tool) => (
                  <div
                    key={tool.toolUrn}
                    className="group hover:bg-muted/30 flex w-full items-center gap-2 px-3 py-2 transition-colors"
                  >
                    <button
                      onClick={() => tool && onToolClick?.(tool)}
                      className="flex min-w-0 flex-1 items-center justify-between text-left"
                    >
                      <p className="text-foreground truncate text-xs leading-5">
                        {tool.name}
                      </p>
                      <div className="ml-2 flex shrink-0 items-center gap-2">
                        <AnnotationBadges tool={tool} />
                        {tool.type === "http" && tool.httpMethod && (
                          <MethodBadge method={tool.httpMethod} />
                        )}
                        {tool.type === "function" && (
                          <SquareFunction className="text-muted-foreground size-3.5" />
                        )}
                        {tool.type === "prompt" && (
                          <PencilRuler className="text-muted-foreground size-3.5" />
                        )}
                      </div>
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

/** A flat, read-only tool list for remote-MCP-backed servers. */
function ReadOnlyToolList({ tools }: { tools: ReadOnlyTool[] }): JSX.Element {
  if (tools.length === 0) {
    return (
      <div className="px-4 py-6 text-center">
        <Type variant="small" className="text-muted-foreground">
          No tools advertised
        </Type>
      </div>
    );
  }
  return (
    <div>
      {tools.map((tool) => (
        <div
          key={tool.name}
          className="flex w-full flex-col gap-0.5 px-4 py-2"
          title={tool.description}
        >
          <p className="text-foreground truncate text-xs leading-5">
            {tool.name}
          </p>
          {tool.description ? (
            <p className="text-muted-foreground truncate text-[11px] leading-4">
              {tool.description}
            </p>
          ) : null}
        </div>
      ))}
    </div>
  );
}
