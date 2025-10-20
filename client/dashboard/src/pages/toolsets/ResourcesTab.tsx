import { Cards } from "@/components/ui/card";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command";
import { MoreActions } from "@/components/ui/more-actions";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Type } from "@/components/ui/type";
import { Resource } from "@gram/client/models/components";
import {
  useLatestDeployment,
  useListResources,
  useUpdateToolsetMutation,
} from "@gram/client/react-query";
import { Stack } from "@speakeasy-api/moonshine";
import { Newspaper } from "lucide-react";
import { useMemo, useState } from "react";
import { Toolset } from "@/lib/toolTypes";

export function ResourcesTabContent({
  toolset,
  updateToolsetMutation,
}: {
  toolset: Toolset;
  updateToolsetMutation: ReturnType<typeof useUpdateToolsetMutation>;
}) {
  const { data: resourcesResponse } = useListResources({});
  const allResources = resourcesResponse?.resources ?? [];

  const { data: deployment } = useLatestDeployment(undefined, undefined, {
    staleTime: 1000 * 60 * 60,
  });

  // Create a mapping of function ID to function name (slug)
  const functionIdToName = useMemo(() => {
    return deployment?.deployment?.functionsAssets?.reduce(
      (acc, asset) => {
        acc[asset.id] = asset.name;
        return acc;
      },
      {} as Record<string, string>,
    );
  }, [deployment]);

  const toolsetResources = useMemo(() => {
    return (
      toolset.resources?.map((resource) => {
        const fullResource = allResources.find(
          (r) =>
            r.functionResourceDefinition?.resourceUrn ===
            resource.functionResourceDefinition?.resourceUrn,
        );
        return fullResource || resource;
      }) ?? []
    );
  }, [toolset.resources, allResources]);

  const currentResourceUrns = toolset.resourceUrns ?? [];

  const addResourceToToolset = (resourceUrn: string) => {
    if (currentResourceUrns.includes(resourceUrn)) {
      return;
    }

    updateToolsetMutation.mutate({
      request: {
        slug: toolset.slug,
        updateToolsetRequestBody: {
          resourceUrns: [...currentResourceUrns, resourceUrn],
        },
      },
    });
  };

  const removeResourceFromToolset = (resourceUrn: string) => {
    updateToolsetMutation.mutate({
      request: {
        slug: toolset.slug,
        updateToolsetRequestBody: {
          resourceUrns: currentResourceUrns.filter(
            (urn) => urn !== resourceUrn,
          ),
        },
      },
    });
  };

  // Show empty state if no resources exist at all in the system
  if (allResources.length === 0) {
    return (
      <div className="flex items-center justify-center py-12">
        <Type muted>No current sources provide resources</Type>
      </div>
    );
  }

  return (
    <Cards>
      {toolsetResources.map((resource) => {
        const def = resource.functionResourceDefinition;
        if (!def) return null;

        return (
          <ResourceCard
            key={def.resourceUrn}
            resource={resource}
            functionName={functionIdToName?.[def.functionId]}
            onDelete={() => removeResourceFromToolset(def.resourceUrn)}
          />
        );
      })}
      {allResources && allResources.length > 0 && (
        <ResourceSelectPopover
          allResources={allResources}
          currentResourceUrns={currentResourceUrns}
          onSelect={(resourceUrn) => addResourceToToolset(resourceUrn)}
        />
      )}
    </Cards>
  );
}

function ResourceCard({
  resource,
  functionName,
  onDelete,
}: {
  resource: Resource;
  functionName?: string;
  onDelete: () => void;
}) {
  const def = resource.functionResourceDefinition;
  if (!def) return null;

  const actions = [
    {
      label: "Remove from toolset",
      onClick: onDelete,
      icon: "trash" as const,
      destructive: true,
    },
  ];

  return (
    <div className="border border-border rounded-lg p-4 flex items-start gap-4">
      <div className="p-2 rounded-md bg-muted shrink-0">
        <Newspaper className="size-5 text-muted-foreground" strokeWidth={1.5} />
      </div>
      <div className="flex-1 min-w-0">
        <Stack gap={1}>
          <Stack direction="horizontal" justify="space-between" align="start">
            <div className="text-sm font-medium truncate">
              {def.name}
              {functionName && (
                <span className="text-xs text-muted-foreground font-normal ml-1">
                  ({functionName})
                </span>
              )}
            </div>
            <MoreActions actions={actions} />
          </Stack>
          <Type small muted className="truncate">
            {def.description || "No description"}
          </Type>
          <Type small muted className="font-mono truncate">
            {def.uri}
          </Type>
        </Stack>
      </div>
    </div>
  );
}

function ResourceSelectPopover({
  allResources,
  currentResourceUrns,
  onSelect,
}: {
  allResources: Resource[];
  currentResourceUrns: string[];
  onSelect: (resourceUrn: string) => void;
}) {
  const [open, setOpen] = useState(false);

  // Filter out resources that are already in the toolset
  const availableResources = allResources.filter(
    (r) =>
      r.functionResourceDefinition?.resourceUrn &&
      !currentResourceUrns.includes(r.functionResourceDefinition.resourceUrn),
  );

  if (availableResources.length === 0) {
    return null;
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button className="w-full border border-border rounded-lg p-4 flex items-start gap-4 hover:bg-muted transition-colors cursor-pointer">
          <div className="p-2 rounded-md bg-muted shrink-0">
            <Newspaper
              className="size-5 text-muted-foreground"
              strokeWidth={1.5}
            />
          </div>
          <span className="text-sm text-foreground flex items-center h-9">
            + Add Resource
          </span>
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-[400px] p-0" align="start">
        <Command>
          <CommandInput placeholder="Search resources..." className="h-9" />
          <CommandList>
            <CommandEmpty>
              {availableResources.length === 0
                ? "No resources found."
                : "No items found."}
            </CommandEmpty>
            <CommandGroup>
              {availableResources.map((resource) => {
                const def = resource.functionResourceDefinition;
                if (!def) return null;

                return (
                  <CommandItem
                    key={def.resourceUrn}
                    value={`${def.name} ${def.description} ${def.uri}`}
                    className="cursor-pointer"
                    onSelect={() => {
                      onSelect(def.resourceUrn);
                      setOpen(false);
                    }}
                  >
                    <div className="flex items-start gap-3 w-full">
                      <Newspaper
                        className="size-4 text-muted-foreground shrink-0 mt-0.5"
                        strokeWidth={1.5}
                      />
                      <Stack gap={0.5} className="flex-1 min-w-0">
                        <Type small className="font-medium">
                          {def.name}
                        </Type>
                        <Type small muted className="truncate">
                          {def.description || "No description"}
                        </Type>
                        <Type
                          small
                          muted
                          className="font-mono truncate text-xs"
                        >
                          {def.uri}
                        </Type>
                      </Stack>
                    </div>
                  </CommandItem>
                );
              })}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
