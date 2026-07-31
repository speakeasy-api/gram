import { ToolList } from "@/components/tool-list";
import { Heading } from "@/components/ui/Heading";
import { MultiSelect } from "@/components/ui/MultiSelect";
import { Text } from "@/components/ui/Text";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRBAC } from "@/hooks/useRBAC";
import { useToolset } from "@/hooks/toolTypes";
import { useToolUpdate } from "@/hooks/useToolUpdate";
import { Toolset, useGroupedTools } from "@/lib/toolTypes";
import {
  EXCLUDED_TAG_KEY,
  MCPToolFilterScopesPanel,
} from "@/pages/mcp/MCPToolFilterScopesPanel";
import { ServerTabContent } from "@/pages/toolsets/ServerTab";
import { useRoutes } from "@/routes";
import {
  invalidateListToolsetToolFilters,
  useListToolsetToolFilters,
} from "@gram/client/react-query/listToolsetToolFilters.js";
import { invalidateAllToolset } from "@gram/client/react-query/toolset.js";
import { useUpdateToolsetMutation } from "@gram/client/react-query/updateToolset.js";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { useQueryClient } from "@tanstack/react-query";
import { AlertTriangle } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { AddToolsDialog } from "@/pages/toolsets/AddToolsDialog";
import { ToolsetEmptyState } from "@/pages/toolsets/ToolsetEmptyState";

/**
 * Tool selection for a toolset-backed MCP server. Tool membership is owned by
 * the toolset, so mutations here go through the toolsets API.
 */
export function ToolsetToolsTab({
  toolset,
}: {
  toolset: Toolset;
}): JSX.Element {
  const { hasScope } = useRBAC();
  const canWrite = hasScope("mcp:write");
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();
  const client = useSdkClient();
  const routes = useRoutes();
  const { data: fullToolset, refetch } = useToolset(toolset.slug);

  // Read-only tool filtering ("scopes") view. The resolved variation group, when
  // present, mirrors what the runtime ?tags= filter exposes.
  const { data: toolFilters } = useListToolsetToolFilters(
    { slug: toolset.slug },
    undefined,
    { throwOnError: false },
  );
  const filteringEnabled = toolFilters?.filteringEnabled ?? false;
  const [activeTag, setActiveTag] = useState<string | null>(null);

  const [addToolsDialogOpen, setAddToolsDialogOpen] = useState(false);

  const tools = fullToolset?.tools ?? [];

  // Validate the selected tag against the current filters (derived during render
  // so a refetch that drops the selected scope cleanly falls back to "all tools"
  // without a stale chip or a reset effect).
  const effectiveActiveTag = useMemo(() => {
    if (!toolFilters || activeTag === null) return null;
    if (activeTag === EXCLUDED_TAG_KEY) {
      return toolFilters.excluded.length > 0 ? EXCLUDED_TAG_KEY : null;
    }
    return toolFilters.scopes.some((s) => s.tag === activeTag)
      ? activeTag
      : null;
  }, [activeTag, toolFilters]);

  // When a scope chip is active, restrict the list below to that scope's tools
  // (or the excluded set), matched by URN so variation renames don't break it.
  const activeFilterUrns = useMemo(() => {
    if (!effectiveActiveTag || !toolFilters) return null;
    if (effectiveActiveTag === EXCLUDED_TAG_KEY) {
      return new Set(toolFilters.excluded.map((tool) => tool.toolUrn));
    }
    const scope = toolFilters.scopes.find((s) => s.tag === effectiveActiveTag);
    return scope ? new Set(scope.tools.map((tool) => tool.toolUrn)) : null;
  }, [effectiveActiveTag, toolFilters]);

  // Check if this is an external MCP proxy server
  const isExternalMcpProxy = fullToolset?.kind === "external-mcp-proxy";

  // Check if we have orphaned tool URNs (URNs exist but tools were deleted)
  const hasOrphanedTools =
    (fullToolset?.toolUrns?.length ?? 0) > 0 &&
    fullToolset?.rawTools.length === 0;

  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => {
      telemetry.capture("toolset_event", { action: "toolset_updated" });
      void refetch();
      void invalidateAllToolset(queryClient);
    },
    onError: (error) => {
      telemetry.capture("toolset_event", {
        action: "toolset_update_failed",
        error: error.message,
      });
    },
  });

  const handleToolsRemove = useCallback(
    (removedUrns: string[]) => {
      const currentUrns = fullToolset?.toolUrns || [];
      const updatedUrns = currentUrns.filter(
        (urn) => !removedUrns.includes(urn),
      );

      updateToolsetMutation.mutate(
        {
          request: {
            slug: toolset.slug,
            updateToolsetRequestBody: {
              toolUrns: updatedUrns,
            },
          },
        },
        {
          onSuccess: () => {
            telemetry.capture("toolset_event", {
              action: "tools_removed",
              count: removedUrns.length,
            });
            toast.success(
              `Removed ${removedUrns.length} tool${removedUrns.length !== 1 ? "s" : ""}`,
            );
          },
        },
      );
    },
    [fullToolset?.toolUrns, toolset.slug, updateToolsetMutation, telemetry],
  );

  const handleTestInPlayground = useCallback(() => {
    routes.playground.goTo(toolset.slug);
  }, [toolset.slug, routes.playground]);

  // Group filtering
  const grouped = useGroupedTools(tools);
  const [selectedGroups, setSelectedGroups] = useState<string[]>(
    grouped.map((group) => group.key),
  );

  const groupKeysJoined = grouped.map((group) => group.key).join(",");
  // Set initial selected groups when the tool list resolves
  useEffect(() => {
    setSelectedGroups(grouped.map((group) => group.key));
    // eslint-disable-next-line react-hooks/exhaustive-deps -- recalculate only when the set of group keys changes
  }, [groupKeysJoined]);

  const { updateTool, isUpdating } = useToolUpdate({
    telemetryEvent: "toolset_event",
    // Refresh the toolset and the tool filtering scopes. Editing a tool's tags
    // can add or remove filter scopes, so the read-only filtering panel above
    // must be invalidated too — otherwise new tags only appear after a reload.
    onSuccess: () => {
      void refetch();
      void invalidateListToolsetToolFilters(queryClient, [
        { slug: toolset.slug },
      ]);
    },
  });

  // For external MCP proxy servers, show the server info instead of tools list
  if (isExternalMcpProxy && fullToolset) {
    return <ServerTabContent toolset={fullToolset} />;
  }

  const groupFilterItems = grouped.map((group) => ({
    label: group.key,
    value: group.key,
  }));

  // Filter tools based on selected groups
  const groupedToolNames = new Set(
    grouped
      .filter((group) => selectedGroups.includes(group.key))
      .flatMap((group) => group.tools.map((t) => t.name)),
  );

  let toolsToDisplay = tools.filter((tool) => groupedToolNames.has(tool.name));
  if (toolsToDisplay.length === 0) {
    toolsToDisplay = tools;
  }
  if (activeFilterUrns) {
    toolsToDisplay = toolsToDisplay.filter((tool) =>
      activeFilterUrns.has(tool.toolUrn),
    );
  }

  return (
    <Stack className="mb-4">
      {!isExternalMcpProxy && (
        <Stack
          direction="horizontal"
          justify="space-between"
          align="center"
          className="mb-4"
        >
          <Heading variant="h3">Tools</Heading>
          <Stack direction="horizontal" gap={2}>
            {canWrite && (
              <routes.customTools.Link>
                <Button variant="secondary" size="sm">
                  <Button.Text>Custom Tools</Button.Text>
                </Button>
              </routes.customTools.Link>
            )}
            {canWrite && (
              <Button onClick={() => setAddToolsDialogOpen(true)} size="sm">
                <Button.LeftIcon>
                  <Icon name="plus" className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>Add Tools</Button.Text>
              </Button>
            )}
          </Stack>
        </Stack>
      )}

      {/* Read-only tool filtering scopes panel (only when filtering enabled) */}
      {!isExternalMcpProxy && filteringEnabled && toolFilters && (
        <MCPToolFilterScopesPanel
          filters={toolFilters}
          activeTag={effectiveActiveTag}
          onSelectTag={setActiveTag}
        />
      )}

      {/* Group filter */}
      {!isExternalMcpProxy && groupFilterItems.length > 1 && (
        <div className="relative mb-4 w-full">
          <MultiSelect
            options={groupFilterItems}
            defaultValue={groupFilterItems.map((item) => item.value)}
            onValueChange={setSelectedGroups}
            placeholder="Filter tools"
            className="capitalize"
            hideSelectAll={true}
            autoSize={true}
          />
        </div>
      )}

      {/* Tools list or empty state */}
      {hasOrphanedTools ? (
        <Stack gap={4} align="center" className="py-12">
          <div className="max-w-md text-center">
            <AlertTriangle className="text-warning mx-auto mb-4 h-12 w-12" />
            <Heading variant="h3" className="mb-2">
              Tool Source Deleted
            </Heading>
            <Text muted>
              This MCP server has tool references, but the underlying source has
              been deleted. Re-adding the source will reinstate the tools.
            </Text>
          </div>
        </Stack>
      ) : toolsToDisplay.length > 0 ? (
        <ToolList
          tools={toolsToDisplay}
          toolset={fullToolset}
          onToolUpdate={canWrite ? updateTool : undefined}
          isToolUpdating={isUpdating}
          onToolsRemove={canWrite ? handleToolsRemove : undefined}
          onTestInPlayground={handleTestInPlayground}
          readOnly={!canWrite}
        />
      ) : (
        <ToolsetEmptyState
          toolsetSlug={toolset.slug}
          onAddTools={canWrite ? () => setAddToolsDialogOpen(true) : undefined}
        />
      )}

      {/* Add Tools Dialog */}
      {fullToolset && !isExternalMcpProxy && (
        <AddToolsDialog
          open={addToolsDialogOpen}
          onOpenChange={setAddToolsDialogOpen}
          toolset={fullToolset}
          onAddTools={(toolUrns) => {
            void (async (toolUrns) => {
              const currentUrns = fullToolset.toolUrns || [];
              const newUrns = [...new Set([...currentUrns, ...toolUrns])];

              await client.toolsets.updateBySlug({
                slug: toolset.slug,
                updateToolsetRequestBody: {
                  toolUrns: newUrns,
                },
              });

              toast.success(
                `Added ${toolUrns.length} tool${toolUrns.length !== 1 ? "s" : ""} to ${toolset.name}`,
              );

              await refetch();
              void invalidateAllToolset(queryClient);
            })(toolUrns);
          }}
        />
      )}
    </Stack>
  );
}
