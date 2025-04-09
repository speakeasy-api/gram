import { Card } from "@/components/ui/card";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { HTTPToolDefinition } from "@gram/sdk/models/components/httptooldefinition";
import {
  useDeleteToolsetMutation,
  useListToolsSuspense,
  useUpdateToolsetMutation,
} from "@gram/sdk/react-query";
import { Toolset } from "@gram/sdk/models/components";
import { EditableText } from "@/components/ui/editable-text";
import { CreateThingCard, useToolset, useToolsets } from "./Toolsets";
import { useNavigate } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Page } from "@/components/page-layout";
import { PlusIcon, Trash2Icon } from "lucide-react";
import {
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogFooter,
  DialogHeader,
} from "@/components/ui/dialog";
import { Dialog } from "@/components/ui/dialog";
import { useState } from "react";
import { MultiSelect } from "@/components/ui/multi-select";
import { DeleteButton } from "@/components/delete-button";

export default function ToolsetPage() {
  let toolset = useToolset();
  const toolsets = useToolsets();
  const navigate = useNavigate();
  const project = useProject();

  const [addToolDialogOpen, setAddToolDialogOpen] = useState(false);

  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => {
      toolset.refetch();
    },
  });

  if (!toolset) {
    return <div>Toolset not found</div>;
  }

  const updateToolset = (changes: Partial<Toolset>) => {
    // Immediately update in-memory toolset
    toolset = { ...toolset, ...changes };
    updateToolsetMutation.mutate({
      request: {
        gramProject: project.projectSlug,
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
          confirm(
            "Are you sure you want to delete this toolset? This action cannot be undone."
          )
        ) {
          deleteToolsetMutation.mutate({
            request: {
              gramProject: project.projectSlug,
              slug: toolset.slug,
            },
          });
        }
      }}
    />
  );

  const updateToolsetTools = (toolNames: string[]) => {

    updateToolsetMutation.mutate({
      request: {
        gramProject: project.projectSlug,
        slug: toolset.slug,
        updateToolsetRequestBody: {
          httpToolNames: toolNames,
        },
      },
    }, {
      onSuccess: () => {
          console.log("mutated");
          setAddToolDialogOpen(false);
        },
      }
    );
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
        <Page.Header.Actions>
          {toolset && deleteButton}
          {addButton}
        </Page.Header.Actions>
      </Page.Header>
      <Page.Body>
        <EditableText
          value={toolset.name}
          onSubmit={(newValue) => updateToolset({ name: newValue })}
          renderDisplay={(value) => <Heading variant="h2">{value}</Heading>}
          inputClassName="text-2xl font-semibold mb-2 px-1 border rounded"
        />
        <EditableText
          value={toolset.description || ""}
          onSubmit={(newValue) => updateToolset({ description: newValue })}
          renderDisplay={(value) => (
            <Type variant="subheading">{value || "Add a description..."}</Type>
          )}
          inputClassName="text-base mb-2 px-1 border rounded w-full"
        />
        {toolset.httpTools.map((tool) => (
          <ToolCard key={tool.id} tool={tool} />
        ))}
        <CreateThingCard onClick={() => setAddToolDialogOpen(true)}>
          + Add Tool
        </CreateThingCard>
        <AddToolDialog
          open={addToolDialogOpen}
          onOpenChange={setAddToolDialogOpen}
          toolset={toolset}
          onSubmit={updateToolsetTools}
        />
      </Page.Body>
    </Page>
  );
}

function ToolCard({ tool }: { tool: HTTPToolDefinition }) {
  return (
    <Card>
      <Card.Header>
        <Card.Title>{tool.name}</Card.Title>
        <Card.Description>{tool.description}</Card.Description>
      </Card.Header>
      <Card.Content>
        <div className="flex items-center gap-2">
          <Type>{tool.httpMethod}</Type>
          <Type>{tool.path}</Type>
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
  toolset: Toolset;
  onSubmit: (newToolIds: string[]) => void;
}) {
  const project = useProject();
  const [selectedTools, setSelectedTools] = useState<string[]>(toolset.httpToolNames ?? []);

  const tools = useListToolsSuspense({
    gramProject: project.projectSlug,
  });

  const options = tools.data.tools.map((tool: HTTPToolDefinition) => ({
    label: tool.name,
    value: tool.name,
  }));

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
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add Tools to {toolset.name}</DialogTitle>
          <DialogDescription>
            Add one or many tools to your toolset.
          </DialogDescription>
        </DialogHeader>
        {selector}
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            onClick={() => onSubmit(selectedTools)}
            disabled={selectedTools.length === 0}
          >
            Add to Toolset
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
