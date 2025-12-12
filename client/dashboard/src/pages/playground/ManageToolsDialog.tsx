import { ToolList } from "@/components/tool-list";
import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useLatestDeployment, useListTools } from "@/hooks/toolTypes";
import { Tool, Toolset } from "@/lib/toolTypes";
import { Button } from "@speakeasy-api/moonshine";
import { useMemo, useState } from "react";
import { EditToolDialog } from "./EditToolDialog";
import { toast } from "sonner";

function getToolSource(
  tool: Tool,
  documentIdToName?: Record<string, string>,
  functionIdToName?: Record<string, string>,
): string {
  if (tool.type === "http") {
    if (tool.packageName) return tool.packageName;
    if (tool.openapiv3DocumentId && documentIdToName) {
      return documentIdToName[tool.openapiv3DocumentId];
    }
    if (tool.deploymentId) return tool.deploymentId;
    return "custom";
  } else if (tool.type === "function") {
    if (tool.functionId && functionIdToName) {
      return functionIdToName[tool.functionId];
    }
    return "Functions";
  }
  return "unknown";
}

interface ManageToolsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  toolset: Toolset;
  currentTools: Tool[];
  onAddTools: (toolUrns: string[]) => void;
  onRemoveTools: (toolUrns: string[]) => void;
  initialGroup?: string; // If provided, filter to this source initially
}

export function ManageToolsDialog({
  open,
  onOpenChange,
  toolset,
  currentTools,
  onAddTools,
  onRemoveTools,
  initialGroup,
}: ManageToolsDialogProps) {
  const [search, setSearch] = useState("");
  const [mode, setMode] = useState<"add" | "manage">(
    initialGroup ? "manage" : "add",
  );
  const [selectedToolUrns, setSelectedToolUrns] = useState<Set<string>>(
    new Set(),
  );
  const [sourceFilter, setSourceFilter] = useState<string>(
    initialGroup || "all",
  );
  const [editingTool, setEditingTool] = useState<Tool | null>(null);

  const { data: allTools, isLoading } = useListTools();
  const { data: deployment } = useLatestDeployment();

  // Get URNs of tools currently in the playground
  const currentToolUrns = useMemo(() => {
    return new Set(currentTools.map((t) => t.toolUrn));
  }, [currentTools]);

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

  const sources = useMemo(() => {
    if (!allTools?.tools) return [];

    const sourceSet = new Set<string>();
    allTools.tools.forEach((tool) => {
      const source = getToolSource(tool, documentIdToName, functionIdToName);
      sourceSet.add(source);
    });

    return Array.from(sourceSet).sort();
  }, [allTools, documentIdToName, functionIdToName]);

  // For "add" mode: show tools not in playground
  const availableTools = useMemo<Tool[]>(() => {
    if (!allTools?.tools) return [];

    return allTools.tools.filter((tool) => {
      return tool.toolUrn && !currentToolUrns.has(tool.toolUrn);
    });
  }, [allTools, currentToolUrns]);

  // For "manage" mode: show current tools
  const displayTools = mode === "add" ? availableTools : currentTools;

  const filteredTools = useMemo(() => {
    const searchLower = search.toLowerCase();

    return displayTools.filter((tool) => {
      if (sourceFilter !== "all") {
        const source = getToolSource(tool, documentIdToName, functionIdToName);
        if (source !== sourceFilter) return false;
      }

      if (search) {
        const matchesSearch =
          tool.name.toLowerCase().includes(searchLower) ||
          tool.description?.toLowerCase().includes(searchLower);
        if (!matchesSearch) return false;
      }

      return true;
    });
  }, [displayTools, search, sourceFilter, documentIdToName, functionIdToName]);

  const handleSelectionChange = (urns: string[]) => {
    setSelectedToolUrns(new Set(urns));
  };

  const handleApply = () => {
    if (mode === "add") {
      onAddTools(Array.from(selectedToolUrns));
    } else {
      onRemoveTools(Array.from(selectedToolUrns));
    }
    handleClose();
  };

  const handleClose = () => {
    setSelectedToolUrns(new Set());
    setSearch("");
    setSourceFilter(initialGroup || "all");
    setMode(initialGroup ? "manage" : "add");
    onOpenChange(false);
  };

  let noResultsMessage = "No tools found matching your search";
  if (mode === "add" && availableTools.length === 0) {
    noResultsMessage = "All available tools are already in the playground";
  }

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <Dialog.Content className="min-w-3xl max-h-[80vh] flex flex-col">
          <Dialog.Header>
            <Dialog.Title>
              {mode === "add" ? "Add tools" : "Manage tools"}
              {sourceFilter !== "all" && ` from ${sourceFilter}`}
            </Dialog.Title>
            <Dialog.Description>
              {mode === "add"
                ? "Select tools to add to your playground session"
                : "Select tools to remove from your playground session"}
            </Dialog.Description>
          </Dialog.Header>

          <div className="flex flex-col gap-4 flex-1 min-h-0">
            {/* Mode toggle + Filters */}
            <div className="flex gap-2">
              <Select
                value={mode}
                onValueChange={(v) => setMode(v as typeof mode)}
              >
                <SelectTrigger className="w-[150px]">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="add">Add tools</SelectItem>
                  <SelectItem value="manage">Manage tools</SelectItem>
                </SelectContent>
              </Select>

              <Input
                placeholder="Search tools..."
                value={search}
                onChange={setSearch}
                className="flex-1"
                autoFocus
              />
              <Select value={sourceFilter} onValueChange={setSourceFilter}>
                <SelectTrigger className="w-[200px]">
                  <SelectValue placeholder="All sources" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All sources</SelectItem>
                  {sources.map((source) => (
                    <SelectItem key={source} value={source}>
                      {source}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* Tool list with selection mode */}
            <div className="flex-1 overflow-auto">
              {isLoading ? (
                <div className="text-center py-8 text-neutral-500">
                  Loading tools...
                </div>
              ) : filteredTools.length === 0 ? (
                <div className="text-center py-8 text-neutral-500">
                  {noResultsMessage}
                </div>
              ) : (
                <ToolList
                  tools={filteredTools}
                  toolset={toolset}
                  selectionMode={mode === "add" ? "add" : "remove"}
                  selectedUrns={Array.from(selectedToolUrns)}
                  onSelectionChange={handleSelectionChange}
                  onToolClick={setEditingTool}
                />
              )}
            </div>
          </div>

          <Dialog.Footer>
            <Button variant="secondary" onClick={handleClose}>
              Cancel
            </Button>
            <Button
              onClick={handleApply}
              disabled={selectedToolUrns.size === 0}
            >
              <Button.Text>
                {mode === "add" ? "Add" : "Remove"}{" "}
                {selectedToolUrns.size > 0 ? selectedToolUrns.size : ""} Tool
                {selectedToolUrns.size !== 1 ? "s" : ""}
              </Button.Text>
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>

      {/* Edit Tool Dialog - rendered as sibling to prevent nested dialog issues */}
      <EditToolDialog
        open={!!editingTool}
        onOpenChange={(open) => !open && setEditingTool(null)}
        tool={editingTool}
        documentIdToName={documentIdToName}
        functionIdToName={functionIdToName}
        onSave={(updates) => {
          // TODO: Implement save functionality
          console.log("Save tool:", editingTool?.name, updates);
          toast.success("Tool updated");
        }}
        onRemove={() => {
          if (editingTool?.toolUrn) {
            onRemoveTools([editingTool.toolUrn]);
            toast.success("Tool removed");
          }
        }}
      />
    </>
  );
}
