import { Card, Cards } from "@/components/ui/card";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { HTTPToolDefinition } from "@gram/client/models/components/httptooldefinition";
import {
  useDeleteToolsetMutation,
  useToolset,
  useUpdateEnvironmentMutation,
  useUpdateToolsetMutation,
} from "@gram/client/react-query/index.js";
import {
  EnvironmentEntryInput,
  ToolsetDetails,
} from "@gram/client/models/components";
import { EditableText } from "@/components/editable-text";
import { CreateThingCard, useToolsets } from "./Toolsets";
import { useParams, Outlet } from "react-router";
import { Button } from "@/components/ui/button";
import { Page } from "@/components/page-layout";
import { AlertTriangle, Check } from "lucide-react";
import { useState } from "react";
import { DeleteButton } from "@/components/delete-button";
import { Alert, Stack } from "@speakeasy-api/moonshine";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { Dot } from "@/components/ui/dot";
import { useEnvironment } from "../environments/Environment";
import { InputDialog } from "@/components/input-dialog";
import { NameAndSlug } from "@/components/name-and-slug";
import { useRoutes } from "@/routes";
import { useEnvironments } from "../environments/Environments";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Skeleton } from "@/components/ui/skeleton";
import { AddButton } from "@/components/add-button";
import { useSdkClient } from "@/contexts/Sdk";
import { HttpRoute } from "@/components/http-route";

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

  return (
    <Page.Body className={cn(className, "max-w-2xl")}>
      <ToolsetHeader toolsetSlug={toolsetSlug} actions={actions} />
      {missingEnvVarsAlert}
      <Cards loading={!toolset}>
        {toolset?.httpTools.map((tool) => (
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

  const updateToolset = async (changes: Partial<ToolsetDetails>) => {
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
        <Badge className="h-8">
          {toolset?.httpTools?.length || "No"} Tools
        </Badge>
        <ToolsetEnvironmentBadge toolset={toolset} />
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
}: {
  toolset: ToolsetDetails | undefined;
  size?: "sm" | "md";
}) => {
  const environments = useEnvironments();
  const routes = useRoutes();

  const sizeClass = {
    sm: "h-6",
    md: "h-8",
  }[size];

  if (!toolset) {
    return <Skeleton className={cn("w-24", sizeClass)} />;
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

  return (
    toolset.defaultEnvironmentSlug && (
      <routes.environments.environment.Link
        params={[toolset.defaultEnvironmentSlug]}
      >
        <Badge
          variant="outline"
          className={cn("flex items-center gap-1", sizeClass)}
        >
          {defaultEnvironment &&
            (needsEnvVars ? (
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <div>
                      <AlertTriangle className="w-3 h-3 text-orange-500 cursor-pointer" />
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
              <Check className="w-3 h-3 text-green-500" />
            ))}
          Default Env
        </Badge>
      </routes.environments.environment.Link>
    )
  );
};
