import { TOOL_NAME_REGEX } from "@/lib/constants";
import { cn } from "@/lib/utils";
import { Tool, isHttpTool } from "@/lib/toolTypes";
import { ChevronDown, FileCode, Layers, SquareFunction } from "lucide-react";
import { MethodBadge } from "./MethodBadge";
import { useState, useEffect, useMemo } from "react";
import { MoreActions } from "@/components/ui/more-actions";
import { Input } from "@/components/ui/input";
import { TextArea } from "@/components/ui/textarea";
import { Dialog } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { useCommandPalette } from "@/contexts/CommandPalette";
import { useLatestDeployment } from "@gram/client/react-query/index.js";

interface ToolListProps {
  tools: Tool[];
  onToolUpdate?: (
    tool: Tool,
    updates: { name?: string; description?: string },
  ) => void;
  onToolsRemove?: (toolUrns: string[]) => void;
  onAddToToolset?: (toolUrns: string[]) => void;
  onCreateToolset?: (toolUrns: string[]) => void;
  onTestInPlayground?: () => void;
  className?: string;
  // Selection mode props for AddToolsDialog
  selectionMode?: "add" | "remove";
  selectedUrns?: string[];
  onSelectionChange?: (urns: string[]) => void;
}

interface ToolGroup {
  type: "package" | "function" | "custom" | "higher_order";
  icon: "file-code" | "square-function" | "square-stack";
  title: string;
  tools: Tool[];
  packageName?: string;
}

const HTTP_METHOD_ORDER = ["GET", "POST", "PUT", "DELETE"];

function sortToolsByMethod(tools: Tool[]): Tool[] {
  const httpTools = tools.filter(isHttpTool);
  const nonHttpTools = tools.filter((t) => !isHttpTool(t));

  // Group tools by method
  const toolsByMethod = new Map<string, typeof httpTools>();
  httpTools.forEach((tool) => {
    const method = tool.httpMethod?.toUpperCase() || "UNKNOWN";
    if (!toolsByMethod.has(method)) {
      toolsByMethod.set(method, []);
    }
    toolsByMethod.get(method)!.push(tool);
  });

  const sortedTools: Tool[] = [];

  // Round-robin through GET, POST, PUT, DELETE continuously
  // When PUT runs out, use PATCH instead (both are orange/warning)
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

    // Also handle any other methods not in our standard order
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
      // Everything else (prompts without higher order, etc.)
      customTools.push(tool);
    }
  });

  // Add package groups with sorted tools
  packageMap.forEach((tools, packageName) => {
    groups.push({
      type: "package",
      icon: "file-code",
      title: packageName,
      tools: sortToolsByMethod(tools),
      packageName,
    });
  });

  // Add function groups
  functionMap.forEach((tools, functionName) => {
    groups.push({
      type: "function",
      icon: "square-function",
      title: functionName,
      tools,
      packageName: functionName,
    });
  });

  // Add function tools group
  if (functionTools.length > 0) {
    groups.push({
      type: "function",
      icon: "square-function",
      title: "Functions",
      tools: functionTools,
    });
  }

  // Add custom tools group with sorted tools
  if (customTools.length > 0) {
    groups.push({
      type: "custom",
      icon: "file-code",
      title: "Custom",
      tools: sortToolsByMethod(customTools),
    });
  }

  // Add higher order tools group
  if (higherOrderTools.length > 0) {
    groups.push({
      type: "higher_order",
      icon: "square-stack",
      title: "Higher Order Tools",
      tools: higherOrderTools,
    });
  }

  return groups;
}

function getIcon(icon: ToolGroup["icon"]) {
  switch (icon) {
    case "file-code":
      return FileCode;
    case "square-function":
      return SquareFunction;
    case "square-stack":
      return Layers;
  }
}

function ToolRow({
  tool,
  onUpdate,
  isSelected,
  isFocused,
  onCheckboxChange,
  onTestInPlayground,
  onRemove,
}: {
  tool: Tool;
  onUpdate?: (updates: { name?: string; description?: string }) => void;
  isSelected: boolean;
  isFocused: boolean;
  onCheckboxChange: (checked: boolean) => void;
  onTestInPlayground?: () => void;
  onRemove?: () => void;
}) {
  const isDisabled = false;
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [editType, setEditType] = useState<"name" | "description">("name");
  const [editValue, setEditValue] = useState("");
  const [error, setError] = useState<string | null>(null);

  const openEditDialog = (type: "name" | "description") => {
    setEditType(type);
    setEditValue(type === "name" ? tool.name : tool.description);
    setError(null);
    setEditDialogOpen(true);
  };

  const handleSave = () => {
    if (editType === "name" && !TOOL_NAME_REGEX.test(editValue)) {
      setError("Tool name may only contain letters, numbers, and underscores");
      return;
    }

    onUpdate?.({ [editType]: editValue });
    setEditDialogOpen(false);
  };

  const handleCopyName = async () => {
    await navigator.clipboard.writeText(tool.name);
  };

  const actions = [
    {
      label: "Edit name",
      onClick: () => openEditDialog("name"),
      icon: "pencil" as const,
      disabled: isDisabled,
    },
    {
      label: "Edit description",
      onClick: () => openEditDialog("description"),
      icon: "pencil" as const,
    },
    {
      label: "Copy name",
      onClick: handleCopyName,
      icon: "copy" as const,
    },
    ...(onTestInPlayground
      ? [
          {
            label: "Test in Playground",
            onClick: onTestInPlayground,
            icon: "message-circle" as const,
          },
        ]
      : []),
    ...(onRemove
      ? [
          {
            label: "Remove",
            onClick: onRemove,
            icon: "trash" as const,
            destructive: true,
          },
        ]
      : []),
  ];

  return (
    <>
      <div
        className={cn(
          "group flex items-center justify-between overflow-hidden pl-4 pr-3 py-4 relative border-b border-neutral-softest last:border-b-0 transition-colors hover:bg-muted",
          isFocused && "bg-muted",
        )}
      >
        <div className="flex gap-4 items-center min-w-0 flex-[0_1_60%]">
          <Checkbox
            checked={isSelected}
            onCheckedChange={onCheckboxChange}
            className={cn(
              "shrink-0 transition-opacity",
              !isSelected && !isFocused && "opacity-0 group-hover:opacity-100",
            )}
          />
          <div className="flex flex-col min-w-0 flex-1">
            <p className="text-sm leading-6 text-foreground truncate">
              {tool.name}
            </p>
            <p className="text-sm leading-6 text-muted-foreground truncate">
              {tool.description || "No description"}
            </p>
          </div>
        </div>
        <div className="flex gap-4 items-center shrink-0">
          {tool.type === "http" && tool.httpMethod && (
            <MethodBadge method={tool.httpMethod} />
          )}
          <MoreActions actions={actions} />
        </div>
      </div>

      <Dialog open={editDialogOpen} onOpenChange={setEditDialogOpen}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>
              Edit {editType === "name" ? "tool name" : "description"}
            </Dialog.Title>
            <Dialog.Description>
              {editType === "name"
                ? `Update the name of tool '${tool.name}'`
                : `Update the description of tool '${tool.name}'`}
            </Dialog.Description>
          </Dialog.Header>
          <div className="space-y-4 py-4">
            {editType === "name" ? (
              <Input
                value={editValue}
                onChange={setEditValue}
                placeholder="Tool name"
              />
            ) : (
              <TextArea
                value={editValue}
                onChange={setEditValue}
                placeholder="Tool description"
                rows={3}
              />
            )}
            {error && <p className="text-sm text-destructive">{error}</p>}
          </div>
          <Dialog.Footer>
            <Button variant="outline" onClick={() => setEditDialogOpen(false)}>
              Cancel
            </Button>
            <Button onClick={handleSave}>Save</Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}

function ToolGroupHeader({
  group,
  isExpanded,
  onToggle,
  isFirstGroup = false,
}: {
  group: ToolGroup;
  isExpanded: boolean;
  onToggle: () => void;
  isFirstGroup?: boolean;
}) {
  const Icon = getIcon(group.icon);

  return (
    <button
      onClick={onToggle}
      aria-expanded={isExpanded}
      aria-label={`${isExpanded ? "Collapse" : "Expand"} ${group.title} group`}
      className={cn(
        "bg-surface-secondary-default flex items-center justify-between pl-4 pr-3 py-4 w-full hover:bg-active transition-colors",
        isExpanded && "border-b border-neutral-softest",
        !isFirstGroup && "border-t border-neutral-softest",
      )}
    >
      <div className="flex gap-4 items-center">
        <Icon className="size-4 shrink-0" strokeWidth={1.5} />
        <p className="text-sm leading-6 text-foreground">{group.title}</p>
      </div>
      <ChevronDown
        className={cn(
          "size-4 transition-transform",
          isExpanded ? "rotate-180" : "rotate-0",
        )}
        strokeWidth={1.5}
      />
    </button>
  );
}

export function ToolList({
  tools,
  onToolUpdate,
  onToolsRemove,
  onAddToToolset,
  onCreateToolset,
  onTestInPlayground,
  className,
  selectionMode,
  selectedUrns = [],
  onSelectionChange,
}: ToolListProps) {
  const { data: deployment } = useLatestDeployment(undefined, undefined, {
    staleTime: 1000 * 60 * 60,
  });

  const documentIdToName = useMemo(() => {
    return deployment?.deployment?.openapiv3Assets?.reduce(
      (acc, asset) => {
        acc[asset.id] = asset.name;
        return acc;
      },
      {} as Record<string, string>,
    );
  }, [deployment]);

  const functionIdToName = useMemo(() => {
    return deployment?.deployment?.functionsAssets?.reduce(
      (acc, asset) => {
        acc[asset.id] = asset.name;
        return acc;
      },
      {} as Record<string, string>,
    );
  }, [deployment]);

  const groups = useMemo(
    () => groupTools(tools, documentIdToName, functionIdToName),
    [tools, documentIdToName, functionIdToName],
  );
  const [expandedGroups, setExpandedGroups] = useState<Set<number>>(new Set());

  useEffect(() => {
    setExpandedGroups(new Set(groups.map((_, i) => i)));
  }, [groups.length]);

  // For normal mode (remove tools from toolset)
  const [selectedForRemoval, setSelectedForRemoval] = useState<Set<string>>(
    new Set(),
  );

  // For selection mode (add tools to toolset) - use controlled state
  const selectedSet = useMemo(
    () => new Set(selectionMode === "add" ? selectedUrns : []),
    [selectionMode, selectedUrns],
  );

  // All tools are identified by toolUrn
  const getToolIdentifier = (tool: Tool): string => {
    return tool.toolUrn;
  };

  const [focusedToolIndex, setFocusedToolIndex] = useState<number>(-1);
  const { addActions, removeActions, setContextBadge } = useCommandPalette();

  const hasChanges =
    selectionMode === "add"
      ? selectedSet.size > 0
      : selectedForRemoval.size > 0;

  // Get flat list of all visible tools for keyboard navigation
  const visibleTools = useMemo(
    () =>
      groups.flatMap((group, groupIndex) =>
        expandedGroups.has(groupIndex) ? group.tools : [],
      ),
    [groups, expandedGroups],
  );

  // Create a map of tool ID -> index for O(1) lookups instead of O(n) findIndex
  const toolIndexMap = useMemo(() => {
    const map = new Map<string, number>();
    visibleTools.forEach((tool, index) => {
      map.set(getToolIdentifier(tool), index);
    });
    return map;
  }, [visibleTools]);

  // Handle keyboard navigation
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Don't intercept if user is typing in an input or the command palette is open
      if (
        e.target instanceof HTMLInputElement ||
        e.target instanceof HTMLTextAreaElement ||
        document.querySelector("[cmdk-root]")
      ) {
        return;
      }

      if (e.key === "Escape") {
        e.preventDefault();
        if (focusedToolIndex >= 0) {
          setFocusedToolIndex(-1);
          if (hasChanges) {
            setSelectedForRemoval(new Set());
          }
        } else if (hasChanges) {
          setSelectedForRemoval(new Set());
        }
        return;
      }

      if (e.key === "ArrowDown") {
        e.preventDefault();
        setFocusedToolIndex((prev) =>
          prev < visibleTools.length - 1 ? prev + 1 : prev,
        );
      } else if (e.key === "ArrowUp") {
        e.preventDefault();
        setFocusedToolIndex((prev) => (prev > 0 ? prev - 1 : prev));
      } else if (e.key === " " && focusedToolIndex >= 0) {
        e.preventDefault();
        const tool = visibleTools[focusedToolIndex];
        if (tool) {
          const toolId = getToolIdentifier(tool);
          const isCurrentlySelected =
            selectionMode === "add"
              ? selectedSet.has(toolId)
              : selectedForRemoval.has(toolId);
          handleCheckboxChange(toolId, !isCurrentlySelected);
        }
      }
    };

    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [hasChanges, focusedToolIndex, visibleTools.length]);

  // Register command palette actions and context badge when tools are selected
  // Skip this in selection mode - the dialog handles its own UI
  useEffect(() => {
    if (selectionMode === "add") {
      return;
    }

    const toolActionIds = ["add-to-toolset", "create-toolset", "remove-tools"];

    if (!hasChanges) {
      removeActions(toolActionIds);
      setContextBadge(null);
      return;
    }

    const count = selectedForRemoval.size;

    // Set the context badge
    setContextBadge(`${count} tool${count === 1 ? "" : "s"} selected`);

    const actions = [
      ...(onAddToToolset
        ? [
            {
              id: "add-to-toolset",
              label: "Add to toolset",
              icon: "plus",
              group: "Tool Actions",
              onSelect: () => {
                setSelectedForRemoval((current) => {
                  onAddToToolset(Array.from(current));
                  return new Set();
                });
              },
            },
          ]
        : []),
      ...(onCreateToolset
        ? [
            {
              id: "create-toolset",
              label: "Create toolset",
              icon: "copy",
              group: "Tool Actions",
              onSelect: () => {
                setSelectedForRemoval((current) => {
                  onCreateToolset(Array.from(current));
                  return new Set();
                });
              },
            },
          ]
        : []),
      {
        id: "remove-tools",
        label: "Remove",
        icon: "trash",
        group: "Tool Actions",
        onSelect: () => {
          setSelectedForRemoval((current) => {
            if (current.size > 0) {
              onToolsRemove?.(Array.from(current));
            }
            return new Set();
          });
        },
      },
    ];

    addActions(actions);

    // Clean up actions and badge when component unmounts or selection changes
    return () => {
      removeActions(toolActionIds);
      setContextBadge(null);
    };
    // addActions, removeActions, and setContextBadge are memoized in CommandPaletteContext
    // with empty deps so they're stable and don't need to be in the dependency array
  }, [
    selectionMode,
    hasChanges,
    selectedForRemoval.size,
    onAddToToolset,
    onCreateToolset,
    onToolsRemove,
  ]);

  const toggleGroup = (index: number) => {
    setExpandedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(index)) {
        next.delete(index);
      } else {
        next.add(index);
      }
      return next;
    });
  };

  const handleCheckboxChange = (toolId: string, checked: boolean) => {
    if (selectionMode === "add" && onSelectionChange) {
      // For selection mode, update parent state
      const next = new Set(selectedUrns);
      if (checked) {
        next.add(toolId);
      } else {
        next.delete(toolId);
      }
      onSelectionChange(Array.from(next));
    } else {
      // For normal mode, update local state
      setSelectedForRemoval((prev) => {
        const next = new Set(prev);
        if (checked) {
          next.add(toolId);
        } else {
          next.delete(toolId);
        }
        return next;
      });
    }
  };

  const handleCancel = () => {
    setSelectedForRemoval(new Set());
  };

  return (
    <div className="relative w-full">
      <div
        className={cn(
          "border border-neutral-softest rounded-lg overflow-hidden w-full",
          className,
        )}
      >
        {groups.map((group, index) => (
          <div key={`${group.type}-${group.title}-${index}`} className="w-full">
            <ToolGroupHeader
              group={group}
              isExpanded={expandedGroups.has(index)}
              onToggle={() => toggleGroup(index)}
              isFirstGroup={index === 0}
            />
            {expandedGroups.has(index) && (
              <div className="w-full">
                {group.tools.map((tool) => {
                  const toolId = getToolIdentifier(tool);
                  const toolIndex = toolIndexMap.get(toolId) ?? -1;
                  return (
                    <ToolRow
                      key={tool.canonicalName}
                      tool={tool}
                      onUpdate={(updates) => onToolUpdate?.(tool, updates)}
                      isSelected={
                        selectionMode === "add"
                          ? selectedSet.has(toolId)
                          : selectedForRemoval.has(toolId)
                      }
                      isFocused={toolIndex === focusedToolIndex}
                      onCheckboxChange={(checked) =>
                        handleCheckboxChange(toolId, checked)
                      }
                      onTestInPlayground={onTestInPlayground}
                      onRemove={
                        selectionMode !== "add" && onToolsRemove
                          ? () => onToolsRemove([toolId])
                          : undefined
                      }
                    />
                  );
                })}
              </div>
            )}
          </div>
        ))}
      </div>

      {hasChanges && !selectionMode && (
        <div className="sticky bottom-0 left-0 right-0 flex justify-center mt-4">
          <div className="border border-neutral-softest bg-background shadow-lg rounded-lg px-4 py-3 flex items-center gap-4">
            <p className="text-sm text-foreground">
              {selectedForRemoval.size} tool(s) selected
            </p>
            <div className="flex items-center gap-2">
              <kbd className="pointer-events-none inline-flex h-5 select-none items-center gap-1 rounded border border-neutral-softest bg-muted px-1.5 font-mono text-[10px] font-medium text-muted-foreground opacity-100">
                <span className="text-xs">⌘</span>K
              </kbd>
              <span className="text-sm text-muted-foreground">for actions</span>
            </div>
            <Button variant="outline" onClick={handleCancel}>
              Cancel
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
