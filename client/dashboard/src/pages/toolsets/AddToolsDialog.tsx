import { Input } from "@/components/ui/input";
import { Dialog } from "@/components/ui/dialog";
import { Button } from "@speakeasy-api/moonshine";
import { useMemo, useState } from "react";
import { useListTools } from "@/hooks/toolTypes";
import { ToolList } from "@/components/tool-list";
import { Tool, Toolset } from "@/lib/toolTypes";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

interface AddToolsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  toolset: Toolset;
  onAddTools: (toolUrns: string[]) => void;
}

export function AddToolsDialog({
  open,
  onOpenChange,
  toolset,
  onAddTools,
}: AddToolsDialogProps) {
  const [search, setSearch] = useState("");
  const [selectedToolUrns, setSelectedToolUrns] = useState<Set<string>>(
    new Set(),
  );
  const [sourceFilter, setSourceFilter] = useState<string>("all");

  const { data: allTools, isLoading } = useListTools();

  // Get URNs of tools already in the toolset
  const existingToolUrns = useMemo(() => {
    return new Set(toolset.toolUrns || []);
  }, [toolset.toolUrns]);

  // Get unique sources for filter dropdown
  // Use the same source IDs that the ToolList grouping uses
  const sources = useMemo(() => {
    if (!allTools?.tools) return [];

    const sourceSet = new Set<string>();
    allTools.tools.forEach((tool) => {
      if (tool.type === "http") {
        const source =
          tool.packageName ||
          tool.openapiv3DocumentId ||
          tool.deploymentId ||
          "custom";
        sourceSet.add(source);
      }
    });

    return Array.from(sourceSet).sort();
  }, [allTools]);

  // Filter out tools already in toolset
  const availableTools = useMemo<Tool[]>(() => {
    if (!allTools?.tools) return [];

    return allTools.tools.filter((tool) => {
      // Only include HTTP tools that aren't already in the toolset
      if (tool.type === "http") {
        const urn = tool.toolUrn;
        return urn && !existingToolUrns.has(urn);
      }
      return false;
    });
  }, [allTools, existingToolUrns]);

  // Filter by search and source
  const filteredTools = useMemo(() => {
    let filtered = availableTools;

    // Apply source filter
    if (sourceFilter !== "all") {
      filtered = filtered.filter((tool) => {
        if (tool.type !== "http") return false;
        const source =
          tool.packageName ||
          tool.openapiv3DocumentId ||
          tool.deploymentId ||
          "custom";
        return source === sourceFilter;
      });
    }

    // Apply search filter
    if (search) {
      const searchLower = search.toLowerCase();
      filtered = filtered.filter(
        (tool) =>
          tool.name.toLowerCase().includes(searchLower) ||
          tool.description?.toLowerCase().includes(searchLower),
      );
    }

    return filtered;
  }, [availableTools, search, sourceFilter]);

  const handleSelectionChange = (urns: string[]) => {
    setSelectedToolUrns(new Set(urns));
  };

  const handleAdd = () => {
    onAddTools(Array.from(selectedToolUrns));
    setSelectedToolUrns(new Set());
    setSearch("");
    setSourceFilter("all");
    onOpenChange(false);
  };

  const handleCancel = () => {
    setSelectedToolUrns(new Set());
    setSearch("");
    setSourceFilter("all");
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="max-w-4xl max-h-[80vh] flex flex-col">
        <Dialog.Header>
          <Dialog.Title>Add Tools to {toolset.name}</Dialog.Title>
          <Dialog.Description>
            Select tools from your deployments to add to this toolset
          </Dialog.Description>
        </Dialog.Header>

        <div className="flex flex-col gap-4 flex-1 min-h-0">
          {/* Filters */}
          <div className="flex gap-2">
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
                {availableTools.length === 0
                  ? "All available tools are already in this toolset"
                  : "No tools found matching your search"}
              </div>
            ) : (
              <ToolList
                tools={filteredTools}
                selectionMode="add"
                selectedUrns={Array.from(selectedToolUrns)}
                onSelectionChange={handleSelectionChange}
              />
            )}
          </div>
        </div>

        <Dialog.Footer>
          <Button variant="secondary" onClick={handleCancel}>
            Cancel
          </Button>
          <Button onClick={handleAdd} disabled={selectedToolUrns.size === 0}>
            Add {selectedToolUrns.size > 0 ? selectedToolUrns.size : ""} Tool
            {selectedToolUrns.size !== 1 ? "s" : ""}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
