import { AddButton } from "@/components/add-button";
import { AutoSummarizeBadge } from "@/components/auto-summarize-badge";
import { DeleteButton } from "@/components/delete-button";
import { EditableText } from "@/components/editable-text";
import { HttpRoute } from "@/components/http-route";
import { InputDialog } from "@/components/input-dialog";
import { NameAndSlug } from "@/components/name-and-slug";
import { Page } from "@/components/page-layout";
import { ToolsBadge } from "@/components/tools-badge";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, Cards } from "@/components/ui/card";
import { Dot } from "@/components/ui/dot";
import { Heading } from "@/components/ui/heading";
import { MultiSelect } from "@/components/ui/multi-select";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import {
  useRegisterEnvironmentTelemetry,
  useRegisterToolsetTelemetry,
  useTelemetry,
} from "@/contexts/Telemetry";
import { useGroupedTools } from "@/lib/toolNames";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import {
  Confirm,
  EnvironmentEntryInput,
  Toolset,
  UpsertGlobalToolVariationForm,
} from "@gram/client/models/components";
import { HTTPToolDefinition } from "@gram/client/models/components/httptooldefinition";
import {
  queryKeyInstance,
  useDeleteToolsetMutation,
  useDeployment,
  useToolset,
  useUpdateEnvironmentMutation,
  useUpdateToolsetMutation,
} from "@gram/client/react-query/index.js";
import { Alert, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Check } from "lucide-react";
import { useState } from "react";
import { Outlet, useParams } from "react-router";
import { useEnvironment } from "../environments/Environment";
import { useEnvironments } from "../environments/Environments";
import { useToolsets } from "./Toolsets";
import { CreateThingCard } from "@/components/create-thing-card";

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

export function ToolsetView({
  toolsetSlug,
  className,
  environmentSlug,
}: {
  toolsetSlug: string;
  className?: string;
  environmentSlug?: string;
}) {
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const { data: toolset, refetch } = useToolset({
    slug: toolsetSlug,
  });

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
        },
      },
    });
  };

  const missingEnvVars = environment
    ? toolset?.relevantEnvironmentVariables?.filter(
        (varName) =>
          !environment?.entries.find((entry) => entry.name === varName)
      )
    : [];

  const isMissingRequiredEnvVars = missingEnvVars?.some(
    (envVar) => !envVar.includes("SERVER_URL")
  );

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
      className="rounded-md my-2 p-4 max-w-4xl"
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

  const actions = (
    <Button
      icon="plus"
      onClick={() => routes.toolsets.toolset.update.goTo(toolsetSlug)}
    >
      Add/Remove Tools
    </Button>
  );

  const grouped = useGroupedTools(toolset?.httpTools || []);
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

  let toolsToDisplay: HTTPToolDefinition[] = grouped
    .filter(
      (group) => !selectedGroups.length || selectedGroups.includes(group.key)
    )
    .flatMap((group) => group.tools);

  // If no tools are selected, show all tools
  // Mostly a failsafe for if the filtering doesn't work as expected
  if (toolsToDisplay.length === 0) {
    toolsToDisplay = toolset?.httpTools || [];
  }

  return (
    <Page.Body className={className}>
      {/* This div is so that the scrollbox still extends the width of the page */}
      <div className="max-w-2xl">
        <ToolsetHeader toolsetSlug={toolsetSlug} actions={actions} />
        {groupFilterItems.length > 1 && filterButton}
        {missingEnvVarsAlert}
        <Cards loading={!toolset}>
          {toolsToDisplay.map((tool) => (
            <ToolCard
              key={tool.id}
              tool={tool}
              onRemove={() => removeToolFromToolset(tool.name)}
              onUpdate={onUpdate}
            />
          ))}
          <CreateThingCard
            onClick={() => routes.toolsets.toolset.update.goTo(toolsetSlug)}
          >
            + Add Tool
          </CreateThingCard>
        </Cards>
      </div>
    </Page.Body>
  );
}

export const ToolsetHeader = ({
  toolsetSlug,
  actions,
}: {
  toolsetSlug: string;
  actions?: React.ReactNode;
}) => {
  const client = useSdkClient();
  const { data: toolset, refetch } = useToolset({
    slug: toolsetSlug,
  });

  const updateToolset = async (changes: Partial<Toolset>) => {
    if (!toolset) {
      return;
    }

    await client.toolsets.updateBySlug({
      slug: toolset.slug,
      updateToolsetRequestBody: {
        name: changes.name,
        description: changes.description,
      },
    });

    refetch?.();
  };

  return (
    <Stack gap={2} className="mb-4">
      <Stack direction="horizontal" justify="space-between" className="h-10">
        <EditableText
          value={toolset?.name}
          onSubmit={(newValue) => updateToolset({ name: newValue })}
          label={"Toolset Name"}
          description={`Update the name of toolset '${toolset?.name}'`}
        >
          <Heading variant="h2">
            <NameAndSlug
              name={toolset?.name || ""}
              slug={toolset?.slug || ""}
            />
          </Heading>
        </EditableText>
        {actions}
      </Stack>
      <Stack direction="horizontal" gap={2} justify="space-between">
        <EditableText
          value={toolset?.description}
          onSubmit={(newValue) => updateToolset({ description: newValue })}
          label={"Toolset Description"}
          description={`Update the description of toolset '${toolset?.name}'`}
          validate={(value) =>
            value.length > 0 && value.length < 100
              ? true
              : "Description must contain fewer than 100 characters"
          }
        >
          <Type variant="body" className="text-muted-foreground">
            {toolset?.description}
          </Type>
        </EditableText>
        <Stack direction="horizontal" gap={2}>
          <ToolsBadge tools={toolset?.httpTools} size="md" variant="outline" />
          <ToolsetEnvironmentBadge
            toolset={toolset}
            size="md"
            variant="outline"
          />
        </Stack>
      </Stack>
    </Stack>
  );
};

function useToolSourceName(deploymentId: string, tool: HTTPToolDefinition) {
  const { data: deployment } = useDeployment(
    {
      id: deploymentId,
    },
    undefined,
    {
      enabled: !tool.packageName,
    }
  );

  if (tool.packageName) {
    return tool.packageName;
  }

  return deployment?.openapiv3Assets.find(
    (asset) => asset.id === tool.openapiv3DocumentId
  )?.slug;
}

function ToolCard({
  tool,
  onRemove,
  onUpdate,
}: {
  tool: HTTPToolDefinition;
  onRemove: () => void;
  onUpdate: () => void;
}) {
  const client = useSdkClient();
  const sourceName = useToolSourceName(tool.deploymentId, tool);
  const telemetry = useTelemetry();
  const toolNameDisplay = sourceName
    ? tool.name.replace(sourceName + "_", "")
    : tool.name;

  const updateVariation = async (
    vals: Partial<UpsertGlobalToolVariationForm>
  ) => {
    await client.variations.upsertGlobal({
      upsertGlobalToolVariationForm: {
        srcToolName: tool.name,
        ...tool.variation,
        confirm: tool.variation?.confirm as Confirm, // TODO: Should the server return the same type?
        ...vals,
      },
    });

    telemetry.capture("toolset_event", {
      action: "tool_variation_updated",
      tool_name: tool.name,
      overridden_fields: Object.keys(vals).join(", "),
    });

    onUpdate();
  };

  const header = (
    <Stack direction="horizontal" gap={2} align="center">
      <EditableText
        value={tool.name}
        onSubmit={(newValue) => updateVariation({ name: newValue })}
        label={"Tool Name"}
        description={`Update the name of tool '${tool.name}'`}
      >
        <Stack direction="horizontal" gap={2} align="center">
          <Heading
            variant="h4"
            className="text-muted-foreground capitalize"
            tooltip={"This tool is from your " + sourceName + " source"}
          >
            {sourceName}
          </Heading>
          <Dot />
          <Heading variant="h4">{toolNameDisplay}</Heading>
        </Stack>
      </EditableText>
      {tool.summarizer && <AutoSummarizeBadge />}
    </Stack>
  );

  const tags = (
    <>
      {tool.tags.map((tag) => (
        <Badge key={tag} variant="secondary" className="text-sm capitalize">
          {tag}
        </Badge>
      ))}
    </>
  );

  const autoSummarizeButton = (
    <Button
      icon={tool.summarizer ? "check" : "sparkles"}
      variant="ghost"
      size="sm"
      tooltip="An experimental feature. Attempt to Auto-summarize the tool's response via separate LLM and prevent large data from overwhelming the context window."
      onClick={() => {
        updateVariation({
          summarizer: tool.summarizer ? undefined : "auto",
        });
      }}
    >
      {tool.summarizer ? "Auto-Summarize" : "Auto-Summarize (alpha)"}
    </Button>
  );

  return (
    <Card size="sm">
      <Card.Header>
        <Card.Title>{header}</Card.Title>
        <Card.Info>{tags}</Card.Info>
        <Card.Description>
          <HttpRoute method={tool.httpMethod} path={tool.path} />
        </Card.Description>
        <Card.Actions>
          {autoSummarizeButton}
          <DeleteButton
            size="sm"
            tooltip="Remove tool from this toolset"
            onClick={onRemove}
          />
        </Card.Actions>
      </Card.Header>
      <Card.Content>
        <div className="border-l-2 pl-4">
          <EditableText
            value={tool.description}
            onSubmit={(newValue) => updateVariation({ description: newValue })}
            label={"Tool Description"}
            description={`Update the description of tool '${tool.name}'`}
            lines={3}
          >
            <Type
              className={cn(
                "line-clamp-3 text-muted-foreground",
                !tool.description && "italic"
              )}
            >
              {tool.description || "No description provided"}
            </Type>
          </EditableText>
        </div>
      </Card.Content>
    </Card>
  );
}

export const ToolsetEnvironmentBadge = ({
  toolset,
  size = "md",
  variant = "default",
}: {
  toolset: Toolset | undefined;
  size?: "sm" | "md";
  variant?: "outline" | "default";
}) => {
  const environments = useEnvironments();
  const routes = useRoutes();

  if (!toolset) {
    return <Badge size={size} isLoading />;
  }

  const defaultEnvironment = environments.find(
    (env) => env.slug === toolset.defaultEnvironmentSlug
  );

  // We consider a toolset to need env vars if it has relevant environment variables and the default environment is set
  // The environment does not have any variables from the toolset's relevant environment variables set
  const needsEnvVars =
    defaultEnvironment &&
    toolset.relevantEnvironmentVariables &&
    toolset.relevantEnvironmentVariables.length > 0 &&
    !toolset.relevantEnvironmentVariables.some((varName) =>
      defaultEnvironment.entries.some(
        (entry) =>
          entry.name === varName &&
          entry.value !== "" &&
          entry.value !== "<EMPTY>"
      )
    );

  const colors = {
    default: {
      warn: "dark:text-orange-800 text-orange-300",
      success: "dark:text-green-800 text-green-300",
    },
    outline: {
      warn: "text-orange-500",
      success: "text-green-500",
    },
  }[variant];

  return (
    toolset.defaultEnvironmentSlug && (
      <routes.environments.environment.Link
        params={[toolset.defaultEnvironmentSlug]}
      >
        <Badge
          size={size}
          variant={variant}
          className={"flex items-center gap-1"}
        >
          {defaultEnvironment &&
            (needsEnvVars ? (
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <div>
                      <AlertTriangle className={cn("w-4 h-4", colors.warn)} />
                    </div>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p>
                      You have not set environment variables for this toolset.
                      Navigate to the environment and use fill for toolset.
                    </p>
                  </TooltipContent>
                </Tooltip>
              </TooltipProvider>
            ) : (
              <Check className={cn("w-4 h-4 stroke-3", colors.success)} />
            ))}
          Default Env
        </Badge>
      </routes.environments.environment.Link>
    )
  );
};
