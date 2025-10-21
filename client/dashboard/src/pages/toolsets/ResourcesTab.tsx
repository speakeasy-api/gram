import { Card, Cards } from "@/components/ui/card";
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
import { useUpdateToolsetMutation } from "@gram/client/react-query";
import { Stack } from "@speakeasy-api/moonshine";
import { Newspaper } from "lucide-react";
import { useMemo, useState } from "react";
import { Toolset, Resource } from "@/lib/toolTypes";
import { useLatestDeployment, useListResources } from "@/hooks/toolTypes";

export function ResourcesTabContent({
  toolset,
  updateToolsetMutation,
}: {
  toolset: Toolset;
  updateToolsetMutation: ReturnType<typeof useUpdateToolsetMutation>;
}) {
  const { data: resourcesResponse } = useListResources({});
  const allResources = resourcesResponse?.resources ?? [];

  const { data: deployment } = useLatestDeployment();

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

  const toolsetResources = toolset.resources ?? [];
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
        <Type muted>
          No resources present. Custom resources can be created through Gram
          Functions.
        </Type>
      </div>
    );
  }

  return (
    <Cards>
      {toolsetResources
        .filter((resource) => resource.type === "function")
        .map((resource) => {
          const functionName = functionIdToName?.[resource.functionId];
          return (
            <ResourceCard
              key={resource.resourceUrn}
              resource={resource}
              functionName={functionName}
              onDelete={() => removeResourceFromToolset(resource.resourceUrn)}
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
  const actions = [
    {
      label: "Remove from toolset",
      onClick: onDelete,
      icon: "trash" as const,
      destructive: true,
    },
  ];

  return (
    <Card>
      <Card.Header>
        <Stack direction="horizontal" gap={2} align="center">
          <div className="p-2 rounded-md bg-muted shrink-0">
            <Newspaper
              className="size-5 text-muted-foreground"
              strokeWidth={1.5}
            />
          </div>
          <Card.Title className="normal-case">
            {resource.name}
            {functionName && (
              <span className="text-xs text-muted-foreground font-normal ml-1">
                ({functionName})
              </span>
            )}
          </Card.Title>
        </Stack>
        <MoreActions actions={actions} />
      </Card.Header>
      <Card.Content>
        <Card.Description className="font-mono">
          {resource.uri}
        </Card.Description>
      </Card.Content>
    </Card>
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
    (r) => r.resourceUrn && !currentResourceUrns.includes(r.resourceUrn),
  );

  if (availableResources.length === 0) {
    return null;
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <div>
          <Card className="cursor-pointer hover:bg-muted/50 transition-colors">
            <Card.Header>
              <Stack direction="horizontal" gap={2} align="center">
                <div className="p-2 rounded-md bg-muted shrink-0">
                  <Newspaper
                    className="size-5 text-muted-foreground"
                    strokeWidth={1.5}
                  />
                </div>
                <Card.Title className="normal-case">+ Add Resource</Card.Title>
              </Stack>
            </Card.Header>
            <Card.Content>
              <Card.Description>
                Click to add a resource to this toolset
              </Card.Description>
            </Card.Content>
          </Card>
        </div>
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
                if (resource.type !== "function") return null;

                return (
                  <CommandItem
                    key={resource.resourceUrn}
                    value={`${resource.name} ${resource.description} ${resource.uri}`}
                    className="cursor-pointer"
                    onSelect={() => {
                      onSelect(resource.resourceUrn);
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
                          {resource.name}
                        </Type>
                        <Type small muted className="truncate">
                          {resource.description || "No description"}
                        </Type>
                        <Type
                          small
                          muted
                          className="font-mono truncate text-xs"
                        >
                          {resource.uri}
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
