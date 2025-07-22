import { DeleteButton } from "@/components/delete-button";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import { Cards } from "@/components/ui/card";
import { MultiSelect } from "@/components/ui/multi-select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  useRegisterEnvironmentTelemetry,
  useRegisterToolsetTelemetry,
  useTelemetry,
} from "@/contexts/Telemetry";
import { useApiError } from "@/hooks/useApiError";
import { useGroupedTools } from "@/lib/toolNames";
import { useRoutes } from "@/routes";
import {
  queryKeyInstance,
  useDeleteToolsetMutation,
  useToolset,
  useUpdateToolsetMutation,
} from "@gram/client/react-query/index.js";
import { Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Outlet, useParams } from "react-router";
import { MCPDetails } from "../mcp/MCPDetails";
import { PromptsTabContent } from "./PromptsTab";
import { ToolCard } from "./ToolCard";
import { ToolSelectDialog } from "./ToolSelectDialog";
import { ToolsetPlaygroundLink } from "./ToolsetCard";
import { ToolsetHeader } from "./ToolsetHeader";
import { useToolsets } from "./Toolsets";
import { ToolDefinition, useToolDefinitions } from "./types";

export function useDeleteToolset({
  onSuccess,
}: { onSuccess?: () => void } = {}) {
  const toolsets = useToolsets();
  const telemetry = useTelemetry();
  const { handleApiError } = useApiError();

  const mutation = useDeleteToolsetMutation({
    onSuccess: async () => {
      telemetry.capture("toolset_event", {
        action: "toolset_deleted",
      });
      await toolsets.refetch();
      onSuccess?.();
    },
    onError: (error) => {
      handleApiError(error, "Failed to delete toolset");
    },
  });

  return (slug: string) => {
    if (
      confirm(
        "Are you sure you want to delete this toolset? This action cannot be undone."
      )
    ) {
      mutation.mutate({
        request: {
          slug,
        },
      });
    }
  };
}

export function ToolsetRoot() {
  const { toolsetSlug } = useParams();
  const routes = useRoutes();

  const { data: toolset } = useToolset({
    slug: toolsetSlug || "",
  });

  useRegisterToolsetTelemetry({
    toolsetSlug: toolsetSlug ?? "",
  });

  const deleteToolset = useDeleteToolset({
    onSuccess: () => {
      routes.toolsets.goTo();
    },
  });

  if (!toolsetSlug) {
    return <div>Toolset not found</div>;
  }

  const deleteButton = (
    <DeleteButton
      tooltip="Delete Toolset"
      onClick={() => {
        if (toolset) {
          deleteToolset(toolset.slug);
        }
      }}
    />
  );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
        <Page.Header.Actions>{toolset && deleteButton}</Page.Header.Actions>
      </Page.Header>
      <Outlet />
    </Page>
  );
}

export default function ToolsetPage() {
  const { toolsetSlug } = useParams();

  if (!toolsetSlug) {
    return <div>Toolset not found</div>;
  }

  return <ToolsetView toolsetSlug={toolsetSlug} />;
}

type ToolsetTabs = "tools" | "prompts" | "mcp";

export function ToolsetView({
  toolsetSlug,
  className,
  environmentSlug,
  addToolsStyle = "link",
  showEnvironmentBadge,
  noGrid,
}: {
  toolsetSlug: string;
  className?: string;
  environmentSlug?: string;
  addToolsStyle?: "link" | "modal";
  showEnvironmentBadge?: boolean;
  noGrid?: boolean;
}) {
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const { data: toolset, refetch } = useToolset({
    slug: toolsetSlug,
  });

  const toolDefinitions = useToolDefinitions(toolset);

  const [activeTab, setActiveTab] = useState<ToolsetTabs>("tools");
  const [addToolsDialogOpen, setAddToolsDialogOpen] = useState(false);

  useRegisterToolsetTelemetry({
    toolsetSlug: toolsetSlug ?? "",
  });

  useRegisterEnvironmentTelemetry({
    environmentSlug: environmentSlug ?? "",
  });

  // Refetch any loaded instances of this toolset on update (primarily for the playground)
  const refetchInstance = () => {
    const queryKey = queryKeyInstance({
      toolsetSlug,
      environmentSlug,
    });

    queryClient.invalidateQueries({ queryKey });
  };

  const onUpdate = () => {
    refetch?.();
    refetchInstance();
  };

  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => {
      telemetry.capture("toolset_event", { action: "toolset_updated" });
      onUpdate();
    },
    onError: (error) => {
      telemetry.capture("toolset_event", {
        action: "toolset_update_failed",
        error: error.message,
      });
    },
  });

  // For now to reduce user confusion we omit server url env variables
  // If a spec already has a security env variable set we will not surface variables as missing for that spec

  const gotoAddTools = () => {
    if (addToolsStyle === "modal") {
      setAddToolsDialogOpen(true);
    } else {
      routes.toolsets.toolset.update.goTo(toolsetSlug);
    }
  };

  const actions = (
    <Stack direction="horizontal" gap={2}>
      {!routes.playground.active && (
        <ToolsetPlaygroundLink toolset={toolset} />
      )}
      <Button icon="plus" onClick={gotoAddTools} caps>
        Add/Remove Tools
      </Button>
    </Stack>
  );

  const grouped = useGroupedTools(toolDefinitions);
  const [selectedGroups, setSelectedGroups] = useState<string[]>(
    grouped.map((group) => group.key)
  );
  const groupFilterItems = grouped.map((group) => ({
    label: group.key,
    value: group.key,
  }));
  const filterButton = (
    <MultiSelect
      options={groupFilterItems}
      defaultValue={groupFilterItems.map((group) => group.value)}
      onValueChange={setSelectedGroups}
      placeholder="Filter tools"
      className="w-fit mb-4 capitalize"
    />
  );

  let toolsToDisplay: ToolDefinition[] = grouped
    .filter((group) => selectedGroups.includes(group.key))
    .flatMap((group) => group.tools);

  // If no tools are selected, show all tools
  // Mostly a failsafe for if the filtering doesn't work as expected
  if (toolsToDisplay.length === 0) {
    toolsToDisplay = toolDefinitions;
  }

  return (
    <Page.Body className={className}>
      <ToolsetHeader
        toolsetSlug={toolsetSlug}
        actions={actions}
        showEnvironmentBadge={showEnvironmentBadge}
        environmentSlug={environmentSlug}
      />
      {groupFilterItems.length > 1 && filterButton}
      <Tabs
        value={activeTab}
        onValueChange={(value) => setActiveTab(value as ToolsetTabs)}
        className="h-full relative"
      >
        <TabsList className="mb-4">
          <TabsTrigger value="tools">Tools</TabsTrigger>
          <TabsTrigger value="prompts">Prompts</TabsTrigger>
          <TabsTrigger value="mcp">MCP</TabsTrigger>
        </TabsList>
        <TabsContent value="tools">
          <Cards isLoading={!toolset} noGrid={noGrid}>
            {toolsToDisplay.map((tool) => (
              <ToolCard
                key={tool.canonicalName}
                tool={tool}
                onUpdate={onUpdate}
              />
            ))}
          </Cards>
        </TabsContent>
        <TabsContent value="prompts">
          {toolset && (
            <PromptsTabContent
              toolset={toolset}
              updateToolsetMutation={updateToolsetMutation}
            />
          )}
        </TabsContent>
        <TabsContent value="mcp">
          {toolset && <MCPDetails toolset={toolset} />}
        </TabsContent>
      </Tabs>
      {addToolsStyle === "modal" && (
        <ToolSelectDialog
          toolsetSlug={toolsetSlug}
          open={addToolsDialogOpen}
          onOpenChange={setAddToolsDialogOpen}
        />
      )}
    </Page.Body>
  );
}
