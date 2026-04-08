import { Checkbox } from "@/components/ui/checkbox";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { useOrganization } from "@/contexts/Auth";
import { cn } from "@/lib/utils";
import { useListToolsets } from "@gram/client/react-query/listToolsets.js";
import { Check, ChevronDown } from "lucide-react";
import { useMemo } from "react";
import type { ResourceType } from "./types";

interface ScopePickerPopoverProps {
  /** The resource type determines which resource list to show */
  resourceType: ResourceType;
  /** null = unrestricted; string[] = allowlist */
  resources: string[] | null;
  onChangeResources: (resources: string[] | null) => void;
}

interface ServerGroup {
  projectId: string;
  projectName: string;
  servers: { id: string; name: string }[];
}

function useMCPServers(enabled: boolean) {
  const organization = useOrganization();
  const { data } = useListToolsets(undefined, undefined, { enabled });

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
      group.servers.push({ id: t.slug, name: t.name });
    }
    return [...groups.values()];
  }, [data, organization.projects]);
}

export function ScopePickerPopover({
  resourceType,
  resources,
  onChangeResources,
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
  const label = getLabel(resourceType, resources);

  const toggleResource = (id: string) => {
    if (resources === null) return;
    const has = resources.includes(id);
    const next = has ? resources.filter((r) => r !== id) : [...resources, id];
    onChangeResources(next);
  };

  return (
    <Popover>
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
        className="w-56 p-1 max-h-[300px] overflow-y-auto"
      >
        {/* Scope mode options */}
        <ScopeOption
          label={resourceType === "project" ? "All projects" : "All servers"}
          selected={isUnrestricted}
          onClick={() => onChangeResources(null)}
        />
        <ScopeOption
          label={
            resourceType === "project"
              ? "Specific projects"
              : "Specific servers"
          }
          selected={!isUnrestricted}
          onClick={() => {
            if (isUnrestricted) onChangeResources([]);
          }}
        />

        {/* Resource list when scoped */}
        {!isUnrestricted && (
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
      </PopoverContent>
    </Popover>
  );
}

function ResourceCheckbox({
  id,
  name,
  checked,
  onToggle,
}: {
  id: string;
  name: string;
  checked: boolean;
  onToggle: (id: string) => void;
}) {
  return (
    <button
      type="button"
      onClick={() => onToggle(id)}
      className={cn(
        "flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-sm hover:bg-accent cursor-pointer",
        checked && "font-medium",
      )}
    >
      <Checkbox
        checked={checked}
        className="pointer-events-none"
        tabIndex={-1}
      />
      <span>{name}</span>
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
): string {
  if (resources === null) {
    return resourceType === "project" ? "All projects" : "All servers";
  }
  if (resources.length === 0) return "Select...";
  const noun = resourceType === "project" ? "project" : "server";
  return `${resources.length} ${noun}${resources.length === 1 ? "" : "s"}`;
}
