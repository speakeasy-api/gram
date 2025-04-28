import { Card, Cards } from "@/components/ui/card";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { HTTPToolDefinition } from "@gram/client/models/components/httptooldefinition";
import {
  useDeleteToolsetMutation,
  useListTools,
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
import { useNavigate, useParams } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Page } from "@/components/page-layout";
import { PlusIcon } from "lucide-react";
import { Dialog } from "@/components/ui/dialog";
import { useState, useEffect } from "react";
import { MultiSelect } from "@/components/ui/multi-select";
import { DeleteButton } from "@/components/delete-button";
import { Alert, Stack } from "@speakeasy-api/moonshine";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";
import { Dot } from "@/components/ui/dot";
import { useEnvironment } from "../environments/Environment";
import { InputDialog } from "@/components/input-dialog";
import { NameAndSlug } from "@/components/name-and-slug";

export default function ToolsetPage() {
  const { toolsetSlug } = useParams();

  if (!toolsetSlug) {
    return <div>Toolset not found</div>;
  }

  return <ToolsetView toolsetSlug={toolsetSlug} isPage />;
}

export function ToolsetView({
  toolsetSlug,
  isPage,
  className,
  environmentSlug,
}: {
  toolsetSlug: string;
  isPage?: boolean;
  className?: string;
  environmentSlug?: string;
}) {
  const toolsets = useToolsets();
  const navigate = useNavigate();
  const project = useProject();

  const toolsetResult = useToolset({
    gramProject: project.slug,
    slug: toolsetSlug,
  });

  let toolset = toolsetResult.data;
  const refetch = toolsetResult.refetch;

  const environment = useEnvironment(
    environmentSlug || toolset?.defaultEnvironmentSlug
  );

  const [addToolDialogOpen, setAddToolDialogOpen] = useState(false);
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

  const updateToolset = (changes: Partial<ToolsetDetails>) => {
    if (!toolset) {
      return;
    }

    // Immediately update in-memory toolset
    toolset = { ...toolset, ...changes };
    updateToolsetMutation.mutate({
      request: {
        gramProject: project.slug,
        slug: toolset.slug,
        updateToolsetRequestBody: {
          name: changes.name,
          description: changes.description,
        },
      },
    });
  };

  const deleteToolsetMutation = useDeleteToolsetMutation({
    onSuccess: () => {
      toolsets.refetch();
      navigate("/toolsets");
    },
  });

  const addButton = (
    <Button
      variant="ghost"
      tooltip="Add Tool"
      onClick={() => setAddToolDialogOpen(true)}
    >
      <PlusIcon className="w-4 h-4" />
    </Button>
  );

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
              gramProject: project.slug,
              slug: toolset.slug,
            },
          });
        }
      }}
    />
  );

  const updateToolsetTools = (toolNames: string[]) => {
    if (!toolset) {
      return;
    }

    updateToolsetMutation.mutate(
      {
        request: {
          gramProject: project.slug,
          slug: toolset.slug,
          updateToolsetRequestBody: {
            httpToolNames: toolNames,
          },
        },
      },
      {
        onSuccess: () => {
          console.log("mutated");
          setAddToolDialogOpen(false);
        },
      }
    );
  };

  const removeToolFromToolset = (toolName: string) => {
    if (!toolset) {
      return;
    }

    updateToolsetMutation.mutate({
      request: {
        gramProject: project.slug,
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

    console.log(missingEnvVars);
    console.log(envVars);

    const envVarsToUpdate = missingEnvVars
      ?.map((envVar) => ({
        name: envVar,
        value: envVars[envVar],
      }))
      .filter((envVar): envVar is EnvironmentEntryInput => !!envVar.value);

    console.log(envVarsToUpdate);

    if (envVarsToUpdate) {
      console.log("mutating");
      updateEnvironmentMutation.mutate(
        {
          request: {
            gramProject: project.slug,
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
      className="rounded-md my-4 p-4 max-w-4xl"
      dismissible={false}
    >
      <Stack gap={4}>
        <Type>
          The following environment variables are missing from the{" "}
          {environmentSlug ? "selected" : "default"} environment:{" "}
          {missingEnvVars!.join(", ")}
        </Type>
        <Button
          variant="outline"
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

  const content = (
    <Page.Body className={className}>
      <Stack
        direction="horizontal"
        justify="space-between"
        align="center"
        className="max-w-2xl"
      >
        <EditableText
          value={toolset?.name}
          onSubmit={(newValue) => updateToolset({ name: newValue })}
          label={"Toolset Name"}
        >
          <Heading variant="h2">
            <NameAndSlug name={toolset?.name || ""} slug={toolset?.slug || ""} />
          </Heading>
        </EditableText>
        <Badge className="h-6">
          {toolset?.httpTools?.length || "No"} Tools
        </Badge>
      </Stack>
      <EditableText
        value={toolset?.description || ""}
        onSubmit={(newValue) => updateToolset({ description: newValue })}
        label={"Toolset Description"}
      >
        <Type
          variant="subheading"
          className="whitespace-nowrap"
          skeleton="line"
        >
          {toolset ? toolset.description || "Add a description..." : undefined}
        </Type>
      </EditableText>
      {missingEnvVarsAlert}
      <Cards loading={!toolset}>
        {toolset?.httpTools.map((tool) => (
          <ToolCard
            key={tool.id}
            tool={tool}
            onRemove={() => removeToolFromToolset(tool.name)}
          />
        ))}
        <CreateThingCard onClick={() => setAddToolDialogOpen(true)}>
          + Add Tool
        </CreateThingCard>
      </Cards>
      {toolset && (
        <AddToolDialog
          open={addToolDialogOpen}
          onOpenChange={setAddToolDialogOpen}
          toolset={toolset}
          onSubmit={updateToolsetTools}
        />
      )}
    </Page.Body>
  );

  if (isPage) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
          <Page.Header.Actions>
            {toolset && deleteButton}
            {addButton}
          </Page.Header.Actions>
        </Page.Header>
        {content}
      </Page>
    );
  }

  return content;
}

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

  const methodStyle = {
    GET: "text-blue-600! dark:text-blue-400!",
    POST: "text-emerald-600! dark:text-emerald-400!",
    PATCH: "text-amber-600! dark:text-amber-300!",
    PUT: "text-amber-600! dark:text-amber-300!",
    DELETE: "text-rose-600! dark:text-rose-400!",
  }[tool.httpMethod];

  const path = (
    <div className="flex items-center gap-2 overflow-hidden font-mono">
      <Type className={cn("text-xs font-semibold", methodStyle)}>
        {tool.httpMethod}
      </Type>
      <Type className="overflow-hidden text-ellipsis text-xs text-muted-foreground">
        {tool.path}
      </Type>
    </div>
  );

  return (
    <Card size="sm">
      <Card.Header>
        <Card.Title>{header}</Card.Title>
        <Card.Info>{tags}</Card.Info>
        <Card.Description>{path}</Card.Description>
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

function AddToolDialog({
  open,
  onOpenChange,
  toolset,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  toolset: ToolsetDetails;
  onSubmit: (newToolIds: string[]) => void;
}) {
  const project = useProject();
  const [selectedTools, setSelectedTools] = useState<string[]>(
    toolset.httpTools.map((tool) => tool.name)
  );

  // Reset selected tools when dialog closes
  useEffect(() => {
    if (!open) {
      setSelectedTools(toolset.httpTools.map((tool) => tool.name));
    }
  }, [open, toolset.httpTools]);

  const tools = useListTools({
    gramProject: project.slug,
  });

  const options = tools.data?.tools.map((tool: HTTPToolDefinition) => ({
    label: tool.name,
    value: tool.name,
  }));

  if (!options) {
    return <div>Loading...</div>;
  }

  const selector = (
    <MultiSelect
      options={options}
      onValueChange={setSelectedTools}
      defaultValue={selectedTools}
      placeholder="Select tools"
      variant="inverted"
    />
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Add Tools to {toolset.name}</Dialog.Title>
          <Dialog.Description>
            Add one or many tools to your toolset.
          </Dialog.Description>
        </Dialog.Header>
        {selector}
        <Dialog.Footer>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            onClick={() => onSubmit(selectedTools)}
            disabled={selectedTools.length === 0}
          >
            Add to Toolset
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
