import { InputDialog } from "@/components/input-dialog";
import { Page } from "@/components/page-layout";
import { ServerCard } from "@/components/server-card";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";
import {
  useCreateToolsetMutation,
  useListToolsets,
} from "@gram/client/react-query/index.js";
import { Button } from "@speakeasy-api/moonshine";
import { Plus } from "lucide-react";
import { useState } from "react";
import { Outlet } from "react-router";
import Sources, { useDeploymentIsEmpty } from "@/components/sources/Sources";
import { useCloneToolset } from "./Toolset";
import { ToolsetsEmptyState } from "./ToolsetsEmptyState";
import { handleAPIError } from "@/lib/errors";

export function useToolsets() {
  const { data: toolsets, refetch, isLoading } = useListToolsets();
  return Object.assign(toolsets?.toolsets || [], { refetch, isLoading });
}

export function ToolsetsRoot() {
  return <Outlet />;
}

export default function Toolsets() {
  const [createToolsetDialogOpen, setCreateToolsetDialogOpen] = useState(false);

  const routes = useRoutes();
  const telemetry = useTelemetry();

  const [toolsetName, setToolsetName] = useState("");
  const createToolsetMutation = useCreateToolsetMutation({
    onSuccess: async (data) => {
      telemetry.capture("toolset_event", {
        action: "toolset_created",
        toolset_slug: data.slug,
      });
      routes.toolsets.toolset.goTo(data.slug);
    },
    onError: (error) => {
      handleAPIError(error, "Failed to create toolset");
    },
  });

  const createToolset = () => {
    createToolsetMutation.mutate({
      request: {
        createToolsetRequestBody: {
          name: toolsetName,
          description: "New Toolset Description",
        },
      },
    });
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Sources />
        <ToolsetsContent
          setCreateToolsetDialogOpen={setCreateToolsetDialogOpen}
        />
        <InputDialog
          open={createToolsetDialogOpen}
          onOpenChange={setCreateToolsetDialogOpen}
          title="Create a Toolset"
          description="Give your toolset a name."
          submitButtonText="Create"
          inputs={{
            label: "Toolset name",
            placeholder: "Toolset name",
            value: toolsetName,
            onChange: (value) => setToolsetName(value),
            onSubmit: createToolset,
            validate: (value) => value.length > 0 && value.length <= 40,
            hint: (value) => (
              <div className="flex justify-between w-full">
                <p className="text-destructive">
                  {value.length > 40 && "Must be 40 characters or less"}
                </p>
                <p>{`${value.length}`}/40</p>
              </div>
            ),
          }}
        />
      </Page.Body>
    </Page>
  );
}

function ToolsetsContent({
  setCreateToolsetDialogOpen,
}: {
  setCreateToolsetDialogOpen: (open: boolean) => void;
}) {
  const toolsets = useToolsets();
  const deploymentIsEmpty = useDeploymentIsEmpty();
  const cloneToolset = useCloneToolset();

  if (!toolsets.isLoading && toolsets.length === 0) {
    // We do this because toolsets and apis are rendered on the same page, so if the APIs empty state is going to be shown, we don't need to show the toolsets empty state
    if (deploymentIsEmpty) {
      return null;
    }

    return (
      <ToolsetsEmptyState
        onCreateToolset={() => setCreateToolsetDialogOpen(true)}
      />
    );
  }

  return (
    <Page.Section>
      <Page.Section.Title>Toolsets</Page.Section.Title>
      <Page.Section.Description>
        Organized collections of tools and prompts for your AI applications
      </Page.Section.Description>
      <Page.Section.CTA>
        <Button onClick={() => setCreateToolsetDialogOpen(true)}>
          <Button.LeftIcon>
            <Plus className="w-4 h-4" />
          </Button.LeftIcon>
          <Button.Text>Add Toolset</Button.Text>
        </Button>
      </Page.Section.CTA>
      <Page.Section.Body>
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          {toolsets.map((toolset) => (
            <ServerCard
              key={toolset.id}
              toolset={toolset}
              className="bg-secondary"
              additionalActions={[
                {
                  label: "Clone",
                  onClick: () => cloneToolset(toolset.slug),
                  icon: "copy",
                },
              ]}
            />
          ))}
        </div>
      </Page.Section.Body>
    </Page.Section>
  );
}
