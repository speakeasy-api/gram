import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Table, type Column } from "@/components/ui/Table";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/Command";
import type { Action } from "@/components/ui/MoreActions";
import { MoreActions } from "@/components/ui/MoreActions";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { Text } from "@/components/ui/Text";
import { useRBAC } from "@/hooks/useRBAC";
import { useLatestDeployment, useListResources } from "@/hooks/toolTypes";
import type { Resource, Toolset } from "@/lib/toolTypes";
import type { useUpdateToolsetMutation } from "@gram/client/react-query/updateToolset.js";
import { Dialog } from "@/components/ui/Dialog";
import { Stack } from "@/components/ui/Stack";
import { Newspaper } from "lucide-react";
import { useMemo, useState } from "react";
import { GettingStartedInstructions } from "@/components/functions/GettingStartedInstructions";
import { ResourcesEmptyState } from "./ResourcesEmptyState";

export function ResourcesTabContent({
  toolset,
  updateToolsetMutation,
}: {
  toolset: Toolset;
  updateToolsetMutation: ReturnType<typeof useUpdateToolsetMutation>;
}): JSX.Element {
  const { data: resourcesResponse } = useListResources({});
  const allResources = resourcesResponse?.resources ?? [];
  const [instructionsOpen, setInstructionsOpen] = useState(false);

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

  const functionResources = toolsetResources.filter(
    (resource) => resource.type === "function",
  );
  const availableResources = allResources.filter(
    (r) =>
      r.type === "function" &&
      r.resourceUrn &&
      !currentResourceUrns.includes(r.resourceUrn),
  );

  // With nothing left to pick, adding a resource means writing a function.
  const addAction =
    availableResources.length > 0 ? (
      <ResourceSelectPopover
        availableResources={availableResources}
        onSelect={(resourceUrn) => addResourceToToolset(resourceUrn)}
      />
    ) : (
      <Button size="sm" onClick={() => setInstructionsOpen(true)}>
        Add resource
      </Button>
    );

  const columns: Column<Resource>[] = [
    {
      key: "name",
      header: "Resource",
      width: "1fr",
      render: (resource) => {
        const functionName = functionIdToName?.[resource.functionId];
        return (
          <Stack gap={0.5} className="min-w-0">
            <Text small className="truncate font-medium">
              {resource.name}
            </Text>
            {functionName && (
              <Text small muted className="truncate">
                From {functionName}
              </Text>
            )}
          </Stack>
        );
      },
    },
    {
      key: "uri",
      header: "Address",
      width: "1fr",
      render: (resource) => (
        <Text small muted className="block truncate font-mono text-xs">
          {resource.uri}
        </Text>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "64px",
      render: (resource) => (
        <RemoveResourceAction
          onRemove={() => removeResourceFromToolset(resource.resourceUrn)}
        />
      ),
    },
  ];

  return (
    <>
      {functionResources.length === 0 ? (
        <ResourcesEmptyState
          action={
            <RequireScope scope="mcp:write" level="component">
              {addAction}
            </RequireScope>
          }
        />
      ) : (
        <Stack gap={4}>
          <Stack direction="horizontal" align="center" justify="space-between">
            <Text small muted>
              Agents connected to this server can read these resources.
            </Text>
            <RequireScope scope="mcp:write" level="component">
              {addAction}
            </RequireScope>
          </Stack>
          <Table
            columns={columns}
            data={functionResources}
            rowKey={(resource) => resource.resourceUrn}
          />
        </Stack>
      )}
      <Dialog open={instructionsOpen} onOpenChange={setInstructionsOpen}>
        <Dialog.Content className="max-w-2xl!">
          <Dialog.Header>
            <Dialog.Title>Add resources</Dialog.Title>
            <Dialog.Description>
              Resources come from functions. Deploy a function that defines one,
              then add it here.
            </Dialog.Description>
          </Dialog.Header>
          <GettingStartedInstructions />
        </Dialog.Content>
      </Dialog>
    </>
  );
}

function RemoveResourceAction({ onRemove }: { onRemove: () => void }) {
  const { hasScope } = useRBAC();
  const actions: Action[] = [
    {
      label: "Remove from MCP server",
      onClick: onRemove,
      icon: "trash" as const,
      destructive: true,
      disabled: !hasScope("mcp:write"),
    },
  ];
  return <MoreActions actions={actions} />;
}

function ResourceSelectPopover({
  availableResources,
  onSelect,
}: {
  availableResources: Resource[];
  onSelect: (resourceUrn: string) => void;
}) {
  const [open, setOpen] = useState(false);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button size="sm">Add resource</Button>
      </PopoverTrigger>
      <PopoverContent className="w-[400px] p-0" align="start">
        <Command>
          <CommandInput placeholder="Search resources…" className="h-9" />
          <CommandList>
            <CommandEmpty>No resources match your search.</CommandEmpty>
            <CommandGroup>
              {availableResources.map((resource) => {
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
                    <div className="flex w-full items-start gap-3">
                      <Newspaper
                        className="text-muted-foreground mt-0.5 size-4 shrink-0"
                        strokeWidth={1.5}
                      />
                      <Stack gap={0.5} className="min-w-0 flex-1">
                        <Text small className="font-medium">
                          {resource.name}
                        </Text>
                        <Text small muted className="truncate">
                          {resource.description || "No description"}
                        </Text>
                        <Text
                          small
                          muted
                          className="truncate font-mono text-xs"
                        >
                          {resource.uri}
                        </Text>
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
