import { AddButton } from "@/components/add-button";
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
import { groupTools } from "@/lib/toolNames";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { EnvironmentEntryInput, Toolset } from "@gram/client/models/components";
import { HTTPToolDefinition } from "@gram/client/models/components/httptooldefinition";
import {
  useDeleteToolsetMutation,
  useToolset,
  useUpdateEnvironmentMutation,
  useUpdateToolsetMutation,
} from "@gram/client/react-query/index.js";
import { Alert, Stack } from "@speakeasy-api/moonshine";
import { AlertTriangle, Check } from "lucide-react";
import { useState } from "react";
import { Outlet, useParams } from "react-router";
import { useEnvironment } from "../environments/Environment";
import { useEnvironments } from "../environments/Environments";
import { CreateThingCard, useToolsets } from "./Toolsets";

export function ToolsetRoot() {
  const { toolsetSlug } = useParams();
  const toolsets = useToolsets();
  const routes = useRoutes();

  const { data: toolset } = useToolset({
    slug: toolsetSlug || "",
  });

  const deleteToolsetMutation = useDeleteToolsetMutation({
    onSuccess: async () => {
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
  const routes = useRoutes();

  const { data: toolset, refetch } = useToolset({
    slug: toolsetSlug,
  });

  const environment = useEnvironment(
    environmentSlug || toolset?.defaultEnvironmentSlug
  );

  const [envVarsDialogOpen, setEnvVarsDialogOpen] = useState(false);
  const [envVars, setEnvVars] = useState<Record<string, string>>({});

  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => {
      refetch?.();
    },
  });

  const updateEnvironmentMutation = useUpdateEnvironmentMutation({
    onSuccess: () => {
      environment?.refetch();
    },
  });

  const removeToolFromToolset = (toolName: string) => {
    if (!toolset) {
      return;
    }

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
      Add Tools
    </Button>
  );

  const grouped = groupTools(toolset?.httpTools || []);
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
      className="w-fit mb-4"
    />
  );

  const toolsToDisplay = grouped
    .filter(
      (group) => !selectedGroups.length || selectedGroups.includes(group.key)
    )
    .flatMap((group) => group.tools);

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
      <Stack direction="horizontal" gap={2}>
        <ToolsBadge tools={toolset?.httpTools} size="md" variant="outline" />
        <ToolsetEnvironmentBadge
          toolset={toolset}
          size="md"
          variant="outline"
        />
      </Stack>
    </Stack>
  );
};

function ToolCard({
  tool,
  onRemove,
}: {
  tool: HTTPToolDefinition;
  onRemove: () => void;
}) {
  const toolNameParts = tool.name.split("_");
  const source = toolNameParts[0];
  const toolName = toolNameParts.slice(1).join(" ");

  const header = (
    <Stack direction="horizontal" gap={4} justify="space-between">
      <Stack direction="horizontal" gap={2}>
        <Heading
          variant="h4"
          className="text-muted-foreground capitalize"
          tooltip={"This tool is from your " + source + " source"}
        >
          {source}
        </Heading>
        <Dot />
        <Heading variant="h4">{toolName}</Heading>
      </Stack>
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

  return (
    <Card size="sm">
      <Card.Header>
        <Card.Title>{header}</Card.Title>
        <Card.Info>{tags}</Card.Info>
        <Card.Description>
          <HttpRoute method={tool.httpMethod} path={tool.path} />
        </Card.Description>
        <Card.Actions>
          <DeleteButton
            tooltip="Remove tool from this toolset"
            onClick={onRemove}
          />
        </Card.Actions>
      </Card.Header>
      <Card.Content>
        <div className="border-l-2 pl-4">
          <Type
            className={cn(
              "line-clamp-3 text-muted-foreground",
              !tool.description && "italic"
            )}
          >
            {tool.description || "No description provided"}
          </Type>
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
