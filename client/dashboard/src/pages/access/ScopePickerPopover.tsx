import { Checkbox } from "@/components/ui/checkbox";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { useOrganization } from "@/contexts/Auth";
import { cn } from "@/lib/utils";
import { useListMCPRegistries } from "@gram/client/react-query/listMCPRegistries";
import { Check, ChevronDown } from "lucide-react";
import type { ResourceType } from "./types";

interface ScopePickerPopoverProps {
  /** The resource type determines which resource list to show */
  resourceType: ResourceType;
  /** null = unrestricted; string[] = allowlist */
  resources: string[] | null;
  onChangeResources: (resources: string[] | null) => void;
}

export function ScopePickerPopover({
  resourceType,
  resources,
  onChangeResources,
}: ScopePickerPopoverProps) {
  const organization = useOrganization();
  const { data: mcpData } = useListMCPRegistries(
    { gramSession: "" },
    undefined,
    { enabled: resourceType === "mcp" },
  );

  // Org-scoped permissions have no resource picker — they're always org-wide
  if (resourceType === "org") {
    return (
      <span className="inline-flex items-center rounded-md border border-input bg-transparent px-2 py-1 text-xs text-muted-foreground h-7">
        All
      </span>
    );
  }

  const isUnrestricted = resources === null;
  const resourceList =
    resourceType === "project"
      ? organization.projects.map((p) => ({ id: p.id, name: p.name }))
      : (mcpData?.registries ?? []).map((r) => ({ id: r.id, name: r.name }));
  const label = getLabel(resourceType, resources);

  const toggleProject = (id: string) => {
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
            {resourceList.map((resource) => {
              const checked = resources.includes(resource.id);
              return (
                <button
                  key={resource.id}
                  type="button"
                  onClick={() => toggleProject(resource.id)}
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
                  <span>{resource.name}</span>
                </button>
              );
            })}
          </>
        )}
      </PopoverContent>
    </Popover>
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
