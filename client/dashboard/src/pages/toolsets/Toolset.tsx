import { AddButton } from "@/components/add-button";
import { CreateThingCard } from "@/components/create-thing-card";
import { DeleteButton } from "@/components/delete-button";
import { InputDialog } from "@/components/input-dialog";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import { Cards } from "@/components/ui/card";
import { MultiSelect } from "@/components/ui/multi-select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import {
  useRegisterEnvironmentTelemetry,
  useRegisterToolsetTelemetry,
  useTelemetry,
} from "@/contexts/Telemetry";
import { useGroupedTools } from "@/lib/toolNames";
import { useRoutes } from "@/routes";
import { EnvironmentEntryInput } from "@gram/client/models/components";
import {
  queryKeyInstance,
  useDeleteToolsetMutation,
  useToolset,
  useUpdateEnvironmentMutation,
  useUpdateToolsetMutation,
} from "@gram/client/react-query/index.js";
import { Alert, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Outlet, useParams } from "react-router";
import { useEnvironment } from "../environments/Environment";
import { MCPDetails } from "../mcp/MCPDetails";
import { PromptsTabContent } from "./PromptsTab";
import { ToolCard } from "./ToolCard";
import { ToolSelectDialog } from "./ToolSelectDialog";
import { ToolsetPlaygroundLink } from "./ToolsetCard";
import { ToolsetHeader } from "./ToolsetHeader";
import { useToolsets } from "./Toolsets";
import { ToolDefinition, useToolDefinitions } from "./types";

export function ToolsetRoot() {
  const { toolsetSlug } = useParams();
  const toolsets = useToolsets();
  const routes = useRoutes();
  const telemetry = useTelemetry();

  const { data: toolset } = useToolset({
    slug: toolsetSlug || "",
  });

  useRegisterToolsetTelemetry({
    toolsetSlug: toolsetSlug ?? "",
  });

  const deleteToolsetMutation = useDeleteToolsetMutation({
    onSuccess: async () => {
      telemetry.capture("toolset_event", {
        action: "toolset_deleted",
      });
      await toolsets.refetch();
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
        if (
          toolset &&
          confirm(
            "Are you sure you want to delete this toolset? This action cannot be undone."
          )
        ) {
          deleteToolsetMutation.mutate({
            request: {
              slug: toolset.slug,
            },
          });
        }
      }}
    />
  );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
        <Page.Header.Actions>
          {toolset && deleteButton}
          <AddButton
            onClick={() => routes.toolsets.toolset.update.goTo(toolsetSlug)}
            tooltip="Add Tool"
          />
        </Page.Header.Actions>
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
}: {
  toolsetSlug: string;
  className?: string;
  environmentSlug?: string;
  addToolsStyle?: "link" | "modal";
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

  const environment = useEnvironment(
    environmentSlug || toolset?.defaultEnvironmentSlug
  );

  const [envVarsDialogOpen, setEnvVarsDialogOpen] = useState(false);
  const [envVars, setEnvVars] = useState<Record<string, string>>({});

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

  const updateEnvironmentMutation = useUpdateEnvironmentMutation({
    onSuccess: () => {
      telemetry.capture("environment_event", { action: "environment_updated" });
    },
    onError: (error) => {
      telemetry.capture("environment_event", {
        action: "environment_update_failed",
        error: error.message,
      });
    },
  });

  const removeToolFromToolset = (toolName: string) => {
    if (!toolset) {
      return;
    }

    telemetry.capture("toolset_event", {
      action: "tool_removed",
      tool_name: toolName,
    });

    updateToolsetMutation.mutate({
      request: {
        slug: toolset.slug,
        updateToolsetRequestBody: {
          httpToolNames: toolset.httpTools
            .filter((tool) => tool.name !== toolName)
            .map((tool) => tool.name),
          promptTemplateNames: toolset.promptTemplates
            .filter((template) => template.name !== toolName)
            .map((template) => template.name),
        },
      },
    });
  };

  // For now to reduce user confusion we omit server url env variables
  // If a spec already has a security env variable set we will not surface variables as missing for that spec
  const relevantEnvVars = toolset?.relevantEnvironmentVariables?.filter(
    (varName) =>
      !varName.includes("SERVER_URL")
  );

  const missingEnvVars = relevantEnvVars?.filter(
        (varName) =>
          !environment?.entries?.find((entry) => {
            const entryPrefix = entry.name.split('_')[0];
            const varPrefix = varName.split('_')[0];
            return entryPrefix === varPrefix;
          })
      ) || [];

  const isMissingRequiredEnvVars = missingEnvVars?.length > 0;

  const submitEnvVars = () => {
    if (!environment) {
      throw new Error("Environment not found");
    }

    const envVarsToUpdate = missingEnvVars
      ?.map((envVar) => ({
        name: envVar,
        value: envVars[envVar],
      }))
      .filter((envVar): envVar is EnvironmentEntryInput => !!envVar.value);

    if (envVarsToUpdate) {
      updateEnvironmentMutation.mutate(
        {
          request: {
            slug: environment.slug,
            updateEnvironmentRequestBody: {
              entriesToUpdate: envVarsToUpdate,
              entriesToRemove: [],
            },
          },
        },
        {
          onError: (error) => {
            console.log("error", error);
          },
        }
      );
    }
  };

  const missingEnvVarsAlert = isMissingRequiredEnvVars && (
    <Alert
      variant="warning"
      className="rounded-md my-2 p-4 max-w-4xl bg-orange-300 dark:bg-orange-900"
      dismissible={false}
    >
      <Stack gap={4}>
        <Type>
          The following environment variables are missing from the{" "}
          {environmentSlug ? "selected" : "default"} environment:{" "}
          {missingEnvVars!.join(", ")}
        </Type>
        <Button
          size="sm"
          className="w-fit"
          onClick={() => setEnvVarsDialogOpen(true)}
        >
          Fill Variables
        </Button>
      </Stack>
      <InputDialog
        open={envVarsDialogOpen}
        onOpenChange={setEnvVarsDialogOpen}
        title="Environment Variables"
        description="Enter values for the environment variables in order to use this toolset."
        onSubmit={submitEnvVars}
        inputs={missingEnvVars!.map((envVar) => ({
          label: envVar,
          name: envVar,
          placeholder: "<EMPTY>",
          value: envVars[envVar] || "",
          validate: (value) =>
            value.length > 0 && value !== "<EMPTY>" && !value.includes(" "),
          onChange: (value) => {
            setEnvVars({ ...envVars, [envVar]: value });
          },
          optional: envVar.includes("SERVER_URL"), // Generally not required
        }))}
      />
    </Alert>
  );

  const gotoAddTools = () => {
    if (addToolsStyle === "modal") {
      setAddToolsDialogOpen(true);
    } else {
      routes.toolsets.toolset.update.goTo(toolsetSlug);
    }
  };

  const actions = (
    <Stack direction="horizontal" gap={2}>
      <ToolsetPlaygroundLink toolset={toolset} />
      <Button icon="plus" onClick={gotoAddTools}>
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
    .filter(
      (group) => !selectedGroups.length || selectedGroups.includes(group.key)
    )
    .flatMap((group) => group.tools);

  // If no tools are selected, show all tools
  // Mostly a failsafe for if the filtering doesn't work as expected
  if (toolsToDisplay.length === 0) {
    toolsToDisplay = toolDefinitions;
  }

  return (
    <Page.Body className={className}>
      <div className="max-w-2xl">
        <ToolsetHeader toolsetSlug={toolsetSlug} actions={actions} />
        {groupFilterItems.length > 1 && filterButton}
        {missingEnvVarsAlert}
      </div>
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
          <Cards loading={!toolset}>
            {toolDefinitions.map((tool) => (
              <ToolCard
                key={tool.canonicalName}
                tool={tool}
                onRemove={() => removeToolFromToolset(tool.name)}
                onUpdate={onUpdate}
              />
            ))}
            <CreateThingCard onClick={gotoAddTools}>+ Add Tool</CreateThingCard>
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
