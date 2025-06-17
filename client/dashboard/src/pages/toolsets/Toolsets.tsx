import { AddButton } from "@/components/add-button";
import { CreateThingCard } from "@/components/create-thing-card";
import { InputDialog } from "@/components/input-dialog";
import { Page } from "@/components/page-layout";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";
import {
  useCreateToolsetMutation,
  useListToolsetsSuspense,
  useToolsetSuspense,
} from "@gram/client/react-query/index.js";
import { useState } from "react";
import { Outlet, useParams } from "react-router";
import { ToolsetCard } from "./ToolsetCard";

export function useToolsets() {
  const { data: toolsets, refetch } = useListToolsetsSuspense();
  return Object.assign(toolsets.toolsets, { refetch });
}

export const useToolset = () => {
  const { toolsetSlug } = useParams();

  const { data: toolset, refetch: refetchToolset } = useToolsetSuspense({
    slug: toolsetSlug ?? "",
  });

  return Object.assign(toolset, { refetch: refetchToolset });
};

export function ToolsetsRoot() {
  return <Outlet />;
}

export default function Toolsets() {
  const toolsets = useToolsets();
  const routes = useRoutes();
  const telemetry = useTelemetry();

  const [createToolsetDialogOpen, setCreateToolsetDialogOpen] = useState(false);
  const [toolsetName, setToolsetName] = useState("");
  const createToolsetMutation = useCreateToolsetMutation({
    onSuccess: async (data) => {
      telemetry.capture("toolset_event", {
        action: "toolset_created",
        toolset_slug: data.slug,
      });
      await toolsets.refetch();
      routes.toolsets.toolset.goTo(data.slug);
    },
    onError: (error) => {
      console.error("Failed to create toolset:", error);
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
        <Page.Header.Actions>
          <AddButton
            onClick={() => setCreateToolsetDialogOpen(true)}
            tooltip="New Toolset"
          />
        </Page.Header.Actions>
      </Page.Header>
      <Page.Body>
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
      </Page.Body>
    </Page>
  );
}
