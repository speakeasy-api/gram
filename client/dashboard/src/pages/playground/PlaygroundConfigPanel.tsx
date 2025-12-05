import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Label } from "@/components/ui/label";
import { Slider } from "@/components/ui/slider";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  ChevronDownIcon,
  ChevronRightIcon,
  FileCode,
  SquareFunction,
  PencilRuler,
  Layers,
  PlusIcon,
} from "lucide-react";
import { MethodBadge } from "@/components/tool-list/MethodBadge";
import { useState, useEffect } from "react";
import { Tool } from "@/lib/toolTypes";

interface ToolsetInfo {
  name: string;
  slug: string;
  description?: string;
  toolCount?: number;
  updatedAt?: Date;
}

interface ToolGroup {
  type: "package" | "function" | "custom" | "higher_order";
  icon:
    | typeof FileCode
    | typeof SquareFunction
    | typeof PencilRuler
    | typeof Layers;
  title: string;
  tools: Tool[];
  packageName?: string;
}

interface ToolsetSectionProps {
  tools?: Tool[];
  selectedTools?: Set<string>;
  onToolToggle?: (toolId: string) => void;
  temperature?: number;
  onTemperatureChange?: (temp: number) => void;
  model?: string;
  onModelChange?: (model: string) => void;
  maxTokens?: number;
  onMaxTokensChange?: (tokens: number) => void;
  toolsetSelector?: React.ReactNode;
  environmentSelector?: React.ReactNode;
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
  selectedTools: _selectedTools = new Set(),
  onToolToggle: _onToolToggle,
  temperature,
  onTemperatureChange,
  model,
  onModelChange,
  maxTokens,
  onMaxTokensChange,
  toolsetSelector,
  environmentSelector,
  authSettings,
  toolsetInfo: _toolsetInfo,
  onToolsetUpdate: _onToolsetUpdate,
  documentIdToName,
  functionIdToName,
  onOpenToolsModal,
  onOpenGroupModal: _onOpenGroupModal,
  onToolClick,
}: ToolsetSectionProps) {
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
    <div className="h-full flex flex-col border-r overflow-y-auto">
      {/* Toolset Selector - Always at top */}
      <div className="px-4 py-3 border-b">
        <Label className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground mb-1.5 block">
          Toolset
        </Label>
        {toolsetSelector}
      </div>

      {/* Environment Selector */}
      {environmentSelector && (
        <div className="px-4 py-3 border-b">
          <Label className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground mb-1.5 block">
            Environment
          </Label>
          {environmentSelector}
        </div>
      )}

      {/* Auth Settings Section */}
      {authSettings && (
        <div className="border-b">
          <Collapsible open={authOpen} onOpenChange={setAuthOpen}>
            <CollapsibleTrigger className="flex w-full items-center px-4 py-2.5 hover:bg-muted/30 transition-colors group">
              <div className="flex items-center gap-1.5">
                <span className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                  Authentication
                </span>
                {authOpen ? (
                  <ChevronDownIcon className="h-3.5 w-3.5 text-muted-foreground" />
                ) : (
                  <ChevronRightIcon className="h-3.5 w-3.5 text-muted-foreground" />
                )}
              </div>
            </CollapsibleTrigger>
            <CollapsibleContent className="px-4 pb-3 pt-2">
              {authSettings}
            </CollapsibleContent>
          </Collapsible>
        </div>
      )}

      {/* Tools Section */}
      <div className="border-b">
        <Collapsible open={toolsOpen} onOpenChange={setToolsOpen}>
          <CollapsibleTrigger className="flex w-full items-center justify-between px-4 py-2.5 hover:bg-muted/30 transition-colors group">
            <div className="flex items-center gap-1.5">
              <span className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                Tools
              </span>
              {toolsOpen ? (
                <ChevronDownIcon className="h-3.5 w-3.5 text-muted-foreground" />
              ) : (
                <ChevronRightIcon className="h-3.5 w-3.5 text-muted-foreground" />
              )}
            </div>

            <Button
              size="sm"
              variant="ghost"
              className="h-6 px-2"
              onClick={(e: React.MouseEvent) => {
                e.stopPropagation();
                onOpenToolsModal?.();
              }}
            >
              <PlusIcon className="size-3.5" />
            </Button>
          </CollapsibleTrigger>
          <CollapsibleContent className="py-1">
            {toolGroups.length > 0 ? (
              <div>
                {toolGroups.map((group) => {
                  const isExpanded = expandedGroups.has(group.title);
                  return (
                    <div key={group.title}>
                      {/* Group Header */}
                      <button
                        onClick={() => toggleGroup(group.title)}
                        className="w-full px-3 py-2 flex items-center gap-2 bg-surface-secondary-default hover:bg-active transition-colors text-left group/item"
                      >
                        <group.icon className="size-3.5 shrink-0 text-muted-foreground" />
                        <div className="min-w-0 truncate text-xs">
                          {group.title}
                        </div>
                        {isExpanded ? (
                          <ChevronDownIcon className="size-3.5 shrink-0 text-muted-foreground" />
                        ) : (
                          <ChevronRightIcon className="size-3.5 shrink-0 text-muted-foreground" />
                        )}
                        <div className="flex-1" />
                        <div className="text-[11px] text-muted-foreground tabular-nums">
                          {group.tools.length}
                        </div>
                      </button>

                      {/* Expanded Tool List */}
                      {isExpanded && (
                        <div>
                          {group.tools.map((tool) => {
                            return (
                              <div
                                key={tool.toolUrn}
                                className="group w-full px-3 py-2 flex items-center gap-2 hover:bg-muted/30 transition-colors"
                              >
                                <button
                                  onClick={() => onToolClick?.(tool)}
                                  className="flex-1 flex items-center justify-between text-left min-w-0"
                                >
                                  <p className="text-xs leading-5 text-foreground truncate">
                                    {tool.name}
                                  </p>
                                  <div className="flex items-center gap-2 shrink-0 ml-2">
                                    {tool.type === "http" &&
                                      tool.httpMethod && (
                                        <MethodBadge method={tool.httpMethod} />
                                      )}
                                    {tool.type === "function" && (
                                      <SquareFunction className="size-3.5 text-muted-foreground" />
                                    )}
                                    {tool.type === "prompt" && (
                                      <PencilRuler className="size-3.5 text-muted-foreground" />
                                    )}
                                  </div>
                                </button>
                              </div>
                            );
                          })}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            ) : (
              <div className="px-4 py-6 text-center">
                <Type variant="small" className="text-muted-foreground">
                  No tools added
                </Type>
              </div>
            )}
          </CollapsibleContent>
        </Collapsible>
      </div>

      {/* Configuration Section */}
      <div className="border-b">
        <Collapsible open={configOpen} onOpenChange={setConfigOpen}>
          <CollapsibleTrigger className="flex w-full items-center px-4 py-2.5 hover:bg-muted/30 transition-colors group">
            <div className="flex items-center gap-1.5">
              <span className="text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                Model Settings
              </span>
              {configOpen ? (
                <ChevronDownIcon className="h-3.5 w-3.5 text-muted-foreground" />
              ) : (
                <ChevronRightIcon className="h-3.5 w-3.5 text-muted-foreground" />
              )}
            </div>
          </CollapsibleTrigger>
          <CollapsibleContent className="px-4 pb-3 pt-2 space-y-4">
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
                    <SelectItem value="anthropic/claude-sonnet-4.5">
                      Claude 4.5 Sonnet
                    </SelectItem>
                    <SelectItem value="anthropic/claude-haiku-4.5">
                      Claude 4.5 Haiku
                    </SelectItem>
                    <SelectItem value="anthropic/claude-sonnet-4">
                      Claude 4 Sonnet
                    </SelectItem>
                    <SelectItem value="openai/gpt-4o">GPT-4o</SelectItem>
                    <SelectItem value="openai/gpt-4o-mini">
                      GPT-4o-mini
                    </SelectItem>
                    <SelectItem value="openai/gpt-5">GPT-5</SelectItem>
                    <SelectItem value="openai/gpt-4.1">GPT-4.1</SelectItem>
                    <SelectItem value="anthropic/claude-3.7-sonnet">
                      Claude 3.7 Sonnet
                    </SelectItem>
                    <SelectItem value="anthropic/claude-opus-4">
                      Claude 4 Opus (Expensive)
                    </SelectItem>
                    <SelectItem value="google/gemini-2.5-pro-preview">
                      Gemini 2.5 Pro Preview
                    </SelectItem>
                    <SelectItem value="moonshotai/kimi-k2">Kimi K2</SelectItem>
                    <SelectItem value="mistralai/mistral-medium-3">
                      Mistral Medium 3
                    </SelectItem>
                    <SelectItem value="mistralai/codestral-2501">
                      Mistral Codestral 2501
                    </SelectItem>
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
                    <span className="text-xs text-muted-foreground cursor-help font-mono">
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
                    <span className="text-xs text-muted-foreground cursor-help font-mono">
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
