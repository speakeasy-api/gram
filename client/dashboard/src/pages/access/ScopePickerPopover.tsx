import { Checkbox } from "@/components/ui/checkbox";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { useOrganization } from "@/contexts/Auth";
import { cn } from "@/lib/utils";
import { useListToolsetsForOrg } from "@gram/client/react-query/listToolsetsForOrg.js";
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
      group.servers.push({ id: t.id, name: t.name });
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
      <span className="border-input text-muted-foreground inline-flex h-7 items-center rounded-md border bg-transparent px-2 py-1 text-xs">
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
          className="border-input hover:bg-muted/50 inline-flex h-7 shrink-0 items-center gap-1 rounded-md border bg-transparent px-2 py-1 text-xs shadow-xs transition-colors"
        >
          <span className="max-w-[120px] truncate">{label}</span>
          <ChevronDown className="h-3 w-3 shrink-0 opacity-50" />
        </button>
      </PopoverTrigger>
      <PopoverContent
        align="end"
        className="max-h-[300px] w-56 overflow-y-auto p-1"
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
            <div className="bg-border my-1 h-px" />
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
                    <div className="text-muted-foreground px-2 py-1 text-xs font-medium">
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
        "hover:bg-accent flex w-full cursor-pointer items-center gap-2 rounded-sm px-2 py-1.5 text-sm",
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
        "hover:bg-accent flex w-full cursor-pointer items-center gap-2 rounded-sm px-2 py-1.5 text-sm",
        selected && "font-medium",
      )}
    >
      <span className="flex w-4 shrink-0 items-center justify-center">
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
