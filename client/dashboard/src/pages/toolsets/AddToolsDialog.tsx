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

function getToolIdentifier(tool: Tool): string {
  return tool.toolUrn;
}

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
  const { data: deployment } = useLatestDeployment();

  // Get URNs of tools already in the toolset
  const existingToolUrns = useMemo(() => {
    return new Set(toolset.toolUrns || []);
  }, [toolset.toolUrns]);

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
    if (!allTools?.tools.length) return [];

    const sourceSet = new Set<string>();
    allTools.tools.forEach((tool) => {
      const source = getToolSource(tool, documentIdToName, functionIdToName);
      sourceSet.add(source);
    });

    return Array.from(sourceSet).sort();
  }, [allTools, documentIdToName, functionIdToName]);

  const availableTools = useMemo<Tool[]>(() => {
    if (!allTools?.tools.length) return [];

    return allTools.tools.filter((tool) => {
      const identifier = getToolIdentifier(tool);
      return identifier && !existingToolUrns.has(identifier);
    });
  }, [allTools, existingToolUrns]);

  const filteredTools = useMemo(() => {
    const searchLower = search.toLowerCase();

    return availableTools.filter((tool) => {
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
  }, [
    availableTools,
    search,
    sourceFilter,
    documentIdToName,
    functionIdToName,
  ]);

  const unavailableToolsMatchingSearch = useMemo(() => {
    return allTools?.tools.filter((tool) => {
      return (
        tool.name.toLowerCase().includes(search.toLowerCase()) ||
        (tool.description?.toLowerCase().includes(search.toLowerCase()) &&
          !existingToolUrns.has(tool.toolUrn))
      );
    });
  }, [allTools, search, existingToolUrns]);

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

  let noResultsMessage = "No tools found matching your search";
  if (
    unavailableToolsMatchingSearch &&
    unavailableToolsMatchingSearch.length > 0
  ) {
    noResultsMessage =
      "All tools matching your search are already in this toolset";
  } else if (availableTools.length === 0) {
    noResultsMessage = "All available tools are already in this toolset";
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="min-w-3xl max-h-[80vh] flex flex-col">
        <Dialog.Header>
          <Dialog.Title>
            Add tools to <span className="font-bold">{toolset.name}</span>
          </Dialog.Title>
          <Dialog.Description>
            Select tools from your deployments to add to this server
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
                {noResultsMessage}
              </div>
            ) : (
              <ToolList
                tools={filteredTools}
                toolset={toolset}
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
            <Button.Text>
              Add {selectedToolUrns.size > 0 ? selectedToolUrns.size : ""} Tool
              {selectedToolUrns.size !== 1 ? "s" : ""}
            </Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
