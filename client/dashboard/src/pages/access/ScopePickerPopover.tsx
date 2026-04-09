import { Checkbox } from "@/components/ui/checkbox";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useOrganization } from "@/contexts/Auth";
import { cn } from "@/lib/utils";
import { useListToolsetsForOrg } from "@gram/client/react-query/listToolsetsForOrg.js";
import { Check, ChevronDown, ChevronRight, X } from "lucide-react";
import { useCallback, useMemo, useRef, useState } from "react";
import type { ResourceType } from "./types";

interface ScopePickerPopoverProps {
  /** The resource type determines which resource list to show */
  resourceType: ResourceType;
  /** null = unrestricted; string[] = allowlist */
  resources: string[] | null;
  onChangeResources: (resources: string[] | null) => void;
  /** Whether "Custom" mode is active (MCP scopes only) */
  customMode?: boolean;
  onCustomModeChange?: (custom: boolean) => void;
}

interface ServerTool {
  id: string;
  name: string;
}

interface Server {
  id: string;
  name: string;
  tools: ServerTool[];
}

interface ServerGroup {
  projectId: string;
  projectName: string;
  servers: Server[];
}

function useMCPServers(enabled: boolean) {
  const organization = useOrganization();
  const { data } = useListToolsetsForOrg(undefined, undefined, { enabled });

  return useMemo((): ServerGroup[] => {
    const projectNames = new Map(
      organization.projects.map((p) => [p.id, p.name]),
    );
    const groups = new Map<string, ServerGroup>();
    for (const t of data?.toolsets ?? []) {
      const projectName = projectNames.get(t.projectId) ?? "Unknown";
      let group = groups.get(t.projectId);
      if (!group) {
        group = { projectId: t.projectId, projectName, servers: [] };
        groups.set(t.projectId, group);
      }
      group.servers.push({
        id: t.slug,
        name: t.name,
        tools: t.tools.map((tool) => ({ id: tool.id, name: tool.name })),
      });
    }
    return [...groups.values()];
  }, [data, organization.projects]);
}

export function ScopePickerPopover({
  resourceType,
  resources,
  onChangeResources,
  customMode,
  onCustomModeChange,
}: ScopePickerPopoverProps) {
  const organization = useOrganization();
  const mcpServers = useMCPServers(resourceType === "mcp");

  // Org-scoped permissions have no resource picker — they're always org-wide
  if (resourceType === "org") {
    return (
      <span className="inline-flex items-center rounded-md border border-input bg-transparent px-2 py-1 text-xs text-muted-foreground h-7">
        All
      </span>
    );
  }

  const isUnrestricted = resources === null;
  const projectList = organization.projects.map((p) => ({
    id: p.id,
    name: p.name,
  }));
  const label = getLabel(resourceType, resources, customMode);

  const toggleResource = (id: string) => {
    if (resources === null) return;
    const has = resources.includes(id);
    const next = has ? resources.filter((r) => r !== id) : [...resources, id];
    onChangeResources(next);
  };

  return (
    <Popover modal={false}>
      <PopoverTrigger asChild>
        <button
          type="button"
          className="inline-flex items-center gap-1 rounded-md border border-input bg-transparent px-2 py-1 text-xs shadow-xs hover:bg-muted/50 transition-colors shrink-0 h-7"
        >
          <span className="truncate max-w-[120px]">{label}</span>
          <ChevronDown className="h-3 w-3 opacity-50 shrink-0" />
        </button>
      </PopoverTrigger>
      <PopoverContent
        align="end"
        className={cn(
          "p-1 transition-[min-width] duration-200 ease-out w-56 min-w-56",
          customMode
            ? "min-w-[400px] overflow-hidden"
            : "max-h-[300px] overflow-y-auto",
        )}
      >
        {/* Scope mode options */}
        <div className="pb-1">
          <ScopeOption
            label={resourceType === "project" ? "All projects" : "All servers"}
            selected={isUnrestricted && !customMode}
            onClick={() => {
              if (customMode) onCustomModeChange?.(false);
              onChangeResources(null);
            }}
          />
          <ScopeOption
            label={
              resourceType === "project"
                ? "Specific projects"
                : "Specific servers"
            }
            selected={!isUnrestricted && !customMode}
            onClick={() => {
              if (customMode) onCustomModeChange?.(false);
              if (isUnrestricted) onChangeResources([]);
            }}
          />

          {/* Custom option for MCP scopes */}
          {resourceType === "mcp" && (
            <ScopeOption
              label="Specific tools"
              selected={!!customMode}
              onClick={() => {
                onCustomModeChange?.(true);
                if (isUnrestricted) onChangeResources([]);
              }}
            />
          )}
        </div>

        {/* Resource list when scoped to specific resources */}
        {!isUnrestricted && !customMode && (
          <>
            <div className="my-1 h-px bg-border" />
            {resourceType === "project"
              ? projectList.map((resource) => (
                  <ResourceCheckbox
                    key={resource.id}
                    id={resource.id}
                    name={resource.name}
                    checked={resources.includes(resource.id)}
                    onToggle={toggleResource}
                  />
                ))
              : mcpServers.map((group) => (
                  <div key={group.projectId}>
                    <div className="px-2 py-1 text-xs text-muted-foreground font-medium">
                      {group.projectName}
                    </div>
                    {group.servers.map((server) => (
                      <ResourceCheckbox
                        key={server.id}
                        id={server.id}
                        name={server.name}
                        checked={resources.includes(server.id)}
                        onToggle={toggleResource}
                      />
                    ))}
                  </div>
                ))}
          </>
        )}

        {/* Custom mode — tabbed fine-grained picker */}
        {customMode && (
          <>
            <Tabs defaultValue="select" className="gap-0 -mx-1 -mb-1">
              <TabsList className="w-full rounded-none bg-transparent p-1 gap-1 border-y border-border">
                <TabsTrigger
                  value="select"
                  className="flex-1 rounded-sm border-none shadow-none py-1.5 text-xs hover:bg-muted/50 data-[state=active]:bg-muted data-[state=active]:shadow-none"
                >
                  Manual selection
                </TabsTrigger>
                <TabsTrigger
                  value="auto-groups"
                  className="flex-1 rounded-sm border-none shadow-none py-1.5 text-xs hover:bg-muted/50 data-[state=active]:bg-muted data-[state=active]:shadow-none"
                >
                  Auto Groups
                </TabsTrigger>
              </TabsList>
              <TabsContent value="select" className="p-0">
                <ToolSelectionPanel
                  mcpServers={mcpServers}
                  resources={resources ?? []}
                  onToggle={toggleResource}
                />
              </TabsContent>
              <TabsContent value="auto-groups" className="px-2 py-2">
                <div className="text-xs text-muted-foreground">
                  No auto groups configured
                </div>
              </TabsContent>
            </Tabs>
          </>
        )}
      </PopoverContent>
    </Popover>
  );
}

function ToolSelectionPanel({
  mcpServers,
  resources,
  onToggle,
}: {
  mcpServers: ServerGroup[];
  resources: string[];
  onToggle: (id: string) => void;
}) {
  const allServers = useMemo(
    () => mcpServers.flatMap((g) => g.servers),
    [mcpServers],
  );
  const [selectedServerId, setSelectedServerId] = useState<string | null>(
    allServers[0]?.id ?? null,
  );
  const [search, setSearch] = useState("");
  const selectedServer = allServers.find((s) => s.id === selectedServerId);
  const tools = selectedServer?.tools ?? [];
  const filteredTools = useMemo(
    () =>
      search
        ? tools.filter((t) =>
            t.name.toLowerCase().includes(search.toLowerCase()),
          )
        : tools,
    [tools, search],
  );

  const scrollRef = useRef<HTMLDivElement>(null);
  const handleWheel = useCallback((e: React.WheelEvent) => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop += e.deltaY;
    }
  }, []);

  return (
    <div className="flex">
      {/* Left column — server list */}
      <div className="w-[120px] shrink-0 border-r border-border overflow-y-auto">
        <div className="flex items-center px-2 h-9 bg-muted/50 text-[10px] font-medium text-muted-foreground uppercase tracking-wider border-b border-border">
          Servers
        </div>
        {allServers.map((server) => {
          const isActive = selectedServerId === server.id;
          return (
            <button
              key={server.id}
              type="button"
              onClick={() => {
                setSelectedServerId(server.id);
                setSearch("");
              }}
              className={cn(
                "flex w-full items-center justify-between px-2 py-1.5 text-xs cursor-pointer hover:bg-muted/50 truncate",
                isActive && "bg-muted font-medium",
              )}
            >
              <span className="truncate">{server.name}</span>
              {isActive && (
                <ChevronRight className="h-3 w-3 text-muted-foreground shrink-0" />
              )}
            </button>
          );
        })}
      </div>

      {/* Right column — tools for selected server */}
      <div className="flex-1 min-w-0 flex flex-col min-h-0">
        <div className="flex items-center gap-1 px-2 h-9 border-b border-border">
          <input
            type="text"
            placeholder="Search tools…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="flex-1 bg-transparent text-xs placeholder:text-muted-foreground outline-none"
          />
          {search && (
            <button
              type="button"
              onClick={() => setSearch("")}
              className="shrink-0 text-muted-foreground hover:text-foreground"
            >
              <X className="h-3 w-3" />
            </button>
          )}
        </div>
        <div
          ref={scrollRef}
          onWheel={handleWheel}
          className="min-h-[300px] max-h-[300px] overflow-y-auto pb-2"
        >
          {filteredTools.length === 0 ? (
            <div className="px-2 py-3 text-xs text-muted-foreground">
              {tools.length === 0 ? "No tools found" : "No matching tools"}
            </div>
          ) : (
            filteredTools.map((tool) => {
              const toolId = `${selectedServerId}:${tool.name}`;
              return (
                <ResourceCheckbox
                  key={tool.id}
                  id={toolId}
                  name={tool.name}
                  checked={resources.includes(toolId)}
                  onToggle={onToggle}
                  compact
                />
              );
            })
          )}
        </div>
      </div>
    </div>
  );
}

function ResourceCheckbox({
  id,
  name,
  checked,
  onToggle,
  compact,
}: {
  id: string;
  name: string;
  checked: boolean;
  onToggle: (id: string) => void;
  compact?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={() => onToggle(id)}
      className={cn(
        "flex w-full items-center gap-2 px-2 hover:bg-accent cursor-pointer",
        compact ? "text-xs rounded-none py-1.5" : "text-sm rounded-sm py-1.5",
        checked && "font-medium",
      )}
    >
      <Checkbox
        checked={checked}
        className="pointer-events-none focus-visible:ring-0 focus-visible:border-input"
        tabIndex={-1}
      />
      <span className="truncate">{name}</span>
    </button>
  );
}

function ScopeOption({
  label,
  selected,
  onClick,
}: {
  label: string;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-sm hover:bg-accent cursor-pointer",
        selected && "font-medium",
      )}
    >
      <span className="w-4 flex items-center justify-center shrink-0">
        {selected && <Check className="h-3.5 w-3.5" />}
      </span>
      <span>{label}</span>
    </button>
  );
}

function getLabel(
  resourceType: ResourceType,
  resources: string[] | null,
  customMode?: boolean,
): string {
  if (customMode) return "Specific tools";
  if (resources === null) {
    return resourceType === "project" ? "All projects" : "All servers";
  }
  if (resources.length === 0) return "Select...";
  const noun = resourceType === "project" ? "project" : "server";
  return `${resources.length} ${noun}${resources.length === 1 ? "" : "s"}`;
}
