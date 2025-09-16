import { Page } from "@/components/page-layout";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { Cards } from "@/components/ui/card";
import { Heading } from "@/components/ui/heading";
import { MoreActions } from "@/components/ui/more-actions";
import { MultiSelect } from "@/components/ui/multi-select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  useRegisterEnvironmentTelemetry,
  useRegisterToolsetTelemetry,
  useTelemetry,
} from "@/contexts/Telemetry";
import { useApiError } from "@/hooks/useApiError";
import { useGroupedTools } from "@/lib/toolNames";
import { cn } from "@/lib/utils";
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
import { MCPDetails, MCPEnableButton } from "../mcp/MCPDetails";
import { PromptsTabContent } from "./PromptsTab";
import { ToolCard } from "./ToolCard";
import { ToolSelectDialog } from "./ToolSelectDialog";
import { ToolsetAuth } from "./ToolsetAuth";
import { ToolsetAuthAlert } from "./ToolsetAuthAlert";
import { ToolsetEmptyState } from "./ToolsetEmptyState";
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

  useRegisterToolsetTelemetry({
    toolsetSlug: toolsetSlug ?? "",
  });

  if (!toolsetSlug) {
    return <div>Toolset not found</div>;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Outlet />
      </Page.Body>
    </Page>
  );
}

export default function ToolsetPage() {
  const { toolsetSlug } = useParams();
  const [selectedEnvironment, setSelectedEnvironment] = useState<
    string | undefined
  >(undefined);

  if (!toolsetSlug) {
    return <div>Toolset not found</div>;
  }

  return (
    <ToolsetView
      toolsetSlug={toolsetSlug}
      environmentSlug={selectedEnvironment}
      onEnvironmentChange={setSelectedEnvironment}
    />
  );
}

type ToolsetTabs = "tools" | "prompts" | "auth" | "mcp";

export function ToolsetView({
  toolsetSlug,
  className,
  environmentSlug,
  addToolsStyle = "link",
  noGrid,
  onEnvironmentChange,
  context = "toolset",
}: {
  toolsetSlug: string;
  className?: string;
  environmentSlug?: string;
  addToolsStyle?: "link" | "modal";
  showEnvironmentBadge?: boolean;
  noGrid?: boolean;
  onEnvironmentChange?: (environmentSlug: string) => void;
  context?: "playground" | "toolset";
}) {
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const { data: toolset, refetch } = useToolset(
    { slug: toolsetSlug },
    undefined,
    { enabled: !!toolsetSlug }
  );

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

  const deleteToolset = useDeleteToolset({
    onSuccess: () => {
      routes.toolsets.goTo();
    },
  });

  const actions = (
    <Stack direction="horizontal" gap={2} align="center">
      <Button onClick={gotoAddTools} size="sm">
        <Button.LeftIcon>
          <Icon name="plus" className="h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>Add/Remove Tools</Button.Text>
      </Button>
      <MoreActions
        actions={[
          {
            label: "Playground",
            onClick: () => {
              routes.playground.goTo(toolsetSlug);
            },
            icon: "message-circle",
          },
          {
            label: "Delete Toolset",
            onClick: () => {
              deleteToolset(toolsetSlug);
            },
            icon: "trash",
            destructive: true,
          },
        ]}
      />
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
    <div className={cn("flex flex-col gap-6", className)}>
      {toolset && (
        <ToolsetAuthAlert
          toolset={toolset}
          environmentSlug={environmentSlug}
          onConfigureClick={() => setActiveTab("auth")}
          context={context}
        />
      )}
      <ToolsetHeader toolsetSlug={toolsetSlug} actions={actions} />
      {groupFilterItems.length > 1 && filterButton}
      <Tabs
        value={activeTab}
        onValueChange={(value) => setActiveTab(value as ToolsetTabs)}
        className="h-full relative"
      >
        <TabsList className="mb-4">
          <TabsTrigger value="tools">Tools</TabsTrigger>
          <TabsTrigger value="prompts">Prompts</TabsTrigger>
          <TabsTrigger value="auth">Auth</TabsTrigger>
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
          {toolsToDisplay.length === 0 && (
            <ToolsetEmptyState toolsetSlug={toolsetSlug} />
          )}
        </TabsContent>
        <TabsContent value="prompts">
          {toolset && (
            <PromptsTabContent
              toolset={toolset}
              updateToolsetMutation={updateToolsetMutation}
            />
          )}
        </TabsContent>
        <TabsContent value="auth">
          {toolset && (
            <ToolsetAuth
              toolset={toolset}
              environmentSlug={environmentSlug}
              onEnvironmentChange={onEnvironmentChange}
            />
          )}
        </TabsContent>
        <TabsContent value="mcp">
          {toolset && (
            <Stack gap={6}>
              <Stack
                direction="horizontal"
                align="center"
                justify="space-between"
                gap={2}
              >
                <Heading variant="h2">MCP Server Settings</Heading>
                <MCPEnableButton toolset={toolset} />
              </Stack>
              <MCPDetails toolset={toolset} />
            </Stack>
          )}
        </TabsContent>
      </Tabs>
      {addToolsStyle === "modal" && (
        <ToolSelectDialog
          toolsetSlug={toolsetSlug}
          open={addToolsDialogOpen}
          onOpenChange={setAddToolsDialogOpen}
        />
      )}
    </div>
  );
}
