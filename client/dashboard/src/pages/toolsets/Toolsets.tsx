import { AddButton } from "@/components/add-button";
import { CreateThingCard } from "@/components/create-thing-card";
import { InputDialog } from "@/components/input-dialog";
import { Page } from "@/components/page-layout";
import { Cards } from "@/components/ui/card";
import { useTelemetry } from "@/contexts/Telemetry";
import { useApiError } from "@/hooks/useApiError";
import { useRoutes } from "@/routes";
import {
  useCreateToolsetMutation,
  useListToolsets,
} from "@gram/client/react-query/index.js";
import { useState } from "react";
import { Outlet } from "react-router";
import { ToolsetCard } from "./ToolsetCard";
import { ToolsetsEmptyState } from "./ToolsetsEmptyState";

export function useToolsets() {
  const { data: toolsets, refetch, isLoading } = useListToolsets();
  return Object.assign(toolsets?.toolsets || [], { refetch, isLoading });
}

export function ToolsetsRoot() {
  return <Outlet />;
}

export default function Toolsets() {
  const [createToolsetDialogOpen, setCreateToolsetDialogOpen] = useState(false);

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
        <Page.Header.Actions>
          <AddButton
            onClick={() => setCreateToolsetDialogOpen(true)}
            tooltip="New Toolset"
          />
        </Page.Header.Actions>
      </Page.Header>
      <Page.Body>
        <ToolsetsContent
          createToolsetDialogOpen={createToolsetDialogOpen}
          setCreateToolsetDialogOpen={setCreateToolsetDialogOpen}
        />
      </Page.Body>
    </Page>
  );
}

function ToolsetsContent({
  createToolsetDialogOpen,
  setCreateToolsetDialogOpen,
}: {
  createToolsetDialogOpen: boolean;
  setCreateToolsetDialogOpen: (open: boolean) => void;
}) {
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const { handleApiError } = useApiError();
  const toolsets = useToolsets();

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
      handleApiError(error, "Failed to create toolset");
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

  if (toolsets.length === 0) {
    return (
      <ToolsetsEmptyState
        onCreateToolset={() => setCreateToolsetDialogOpen(true)}
      />
    );
  }

  return (
    <Cards loading={toolsets.isLoading}>
      {toolsets.map((toolset) => (
        <ToolsetCard key={toolset.id} toolset={toolset} />
      ))}
      <CreateThingCard onClick={() => setCreateToolsetDialogOpen(true)}>
        + New Toolset
      </CreateThingCard>
      <InputDialog
        open={createToolsetDialogOpen}
        onOpenChange={setCreateToolsetDialogOpen}
        title="Create a Toolset"
        description="Give your toolset a name."
        inputs={{
          label: "Toolset name",
          placeholder: "Toolset name",
          value: toolsetName,
          onChange: (value) => setToolsetName(value),
          onSubmit: createToolset,
          validate: (value) => value.length > 0,
        }}
      />
    </Cards>
  );
}
