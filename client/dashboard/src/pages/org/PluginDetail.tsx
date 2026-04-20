import { InputField } from "@/components/moon/input-field";
import { Page } from "@/components/page-layout";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import {
  invalidateAllPlugin,
  usePluginSuspense,
} from "@gram/client/react-query/plugin";
import { invalidateAllPlugins } from "@gram/client/react-query/plugins";
import { useUpdatePluginMutation } from "@gram/client/react-query/updatePlugin";
import { useAddPluginServerMutation } from "@gram/client/react-query/addPluginServer";
import { useRemovePluginServerMutation } from "@gram/client/react-query/removePluginServer";
import { useListToolsets } from "@gram/client/react-query/listToolsets";
import { Button, Column, Icon, Stack, Table } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useParams } from "react-router";
import type { PluginServer } from "@gram/client/models/components";
import { useFetcher } from "@/contexts/Fetcher";

export default function PluginDetail() {
  const { pluginId } = useParams<{ pluginId: string }>();
  const queryClient = useQueryClient();
  const [isEditOpen, setIsEditOpen] = useState(false);
  const [isAddServerOpen, setIsAddServerOpen] = useState(false);

  const { data: plugin } = usePluginSuspense({ id: pluginId! });

  const { fetch: authFetch } = useFetcher();
  const { data: toolsetsData, isLoading: isLoadingToolsets } =
    useListToolsets();
  const toolsets = toolsetsData?.toolsets ?? [];

  const invalidateAll = async () => {
    await invalidateAllPlugin(queryClient);
    await invalidateAllPlugins(queryClient);
  };

  const updateMutation = useUpdatePluginMutation({
    onSuccess: () => {
      setIsEditOpen(false);
      invalidateAll();
    },
  });

  const addServerMutation = useAddPluginServerMutation({
    onSuccess: () => {
      setIsAddServerOpen(false);
      invalidateAll();
    },
  });

  const removeServerMutation = useRemovePluginServerMutation({
    onSuccess: () => invalidateAll(),
  });

  const handleUpdate: React.FormEventHandler<HTMLFormElement> = (e) => {
    e.preventDefault();
    const fd = new FormData(e.currentTarget);
    updateMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        updatePluginForm: {
          id: pluginId!,
          name: fd.get("name") as string,
          slug: fd.get("slug") as string,
          description: (fd.get("description") as string) || undefined,
        },
      },
    });
  };

  const handleAddServer: React.FormEventHandler<HTMLFormElement> = (e) => {
    e.preventDefault();
    const fd = new FormData(e.currentTarget);
    const toolsetId = fd.get("toolsetId") as string;
    const toolset = toolsets.find((t) => t.id === toolsetId);
    addServerMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        addPluginServerForm: {
          pluginId: pluginId!,
          toolsetId,
          displayName: toolset?.name ?? toolsetId,
          policy: "required",
        },
      },
    });
  };

  const handleRemoveServer = (server: PluginServer) => {
    removeServerMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: { id: server.id, pluginId: pluginId! },
    });
  };

  const handleDownload = async () => {
    const resp = await authFetch(
      `/rpc/plugins.downloadPluginPackage?plugin_id=${pluginId}&platform=claude`,
      {},
    );
    if (!resp.ok) return;
    const blob = await resp.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download =
      resp.headers.get("Content-Disposition")?.match(/filename="(.+)"/)?.[1] ??
      "plugin.zip";
    a.click();
    URL.revokeObjectURL(url);
  };

  const serverColumns: Column<PluginServer>[] = [
    {
      key: "displayName",
      header: "Name",
      width: "2fr",
      render: (s) => <Type variant="body">{s.displayName}</Type>,
    },
    {
      key: "policy",
      header: "Policy",
      width: "100px",
      render: (s) => <Type variant="body">{s.policy}</Type>,
    },
    {
      key: "actions",
      header: "",
      width: "80px",
      render: (s) => (
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => handleRemoveServer(s)}
          className="hover:text-destructive"
        >
          <Button.LeftIcon>
            <Icon name="trash-2" className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text className="sr-only">Remove</Button.Text>
        </Button>
      ),
    },
  ];

  if (!plugin) return null;

  return (
    <Page>
      <Page.Header>
        <Page.Header.Title>{plugin.name}</Page.Header.Title>
      </Page.Header>
      <Page.Body>
        {/* Plugin metadata */}
        <Stack
          direction="horizontal"
          justify="space-between"
          align="start"
          className="mb-6"
        >
          <div>
            <Heading variant="h4">{plugin.name}</Heading>
            <Type muted small className="mt-1">
              {plugin.description ?? "No description"}
            </Type>
            <Type muted small className="mt-1">
              Slug: <code>{plugin.slug}</code>
            </Type>
          </div>
          <Button variant="secondary" onClick={() => setIsEditOpen(true)}>
            Edit
          </Button>
        </Stack>

        {/* Servers section */}
        <Stack
          direction="horizontal"
          justify="space-between"
          align="center"
          className="mb-3"
        >
          <Heading variant="h5">MCP Servers</Heading>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => setIsAddServerOpen(true)}
          >
            Add Server
          </Button>
        </Stack>
        <Table
          columns={serverColumns}
          data={plugin.servers ?? []}
          rowKey={(row) => row.id}
          noResultsMessage={
            <Stack
              gap={2}
              className="bg-background h-full p-4"
              align="center"
              justify="center"
            >
              <Type variant="body">No servers added yet</Type>
              <Button
                size="sm"
                variant="secondary"
                onClick={() => setIsAddServerOpen(true)}
              >
                <Button.LeftIcon>
                  <Icon name="plus" className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>Add Server</Button.Text>
              </Button>
            </Stack>
          }
          className="mb-8"
        />

        {/* Download section */}
        <Heading variant="h5" className="mb-3">
          Download
        </Heading>
        <div>
          <Button variant="secondary" size="sm" onClick={handleDownload}>
            <Button.LeftIcon>
              <Icon name="download" className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Download Claude Plugin</Button.Text>
          </Button>
        </div>

        {/* Edit Dialog */}
        <Dialog open={isEditOpen} onOpenChange={setIsEditOpen}>
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>Edit Plugin</Dialog.Title>
            </Dialog.Header>
            <form onSubmit={handleUpdate} className="flex flex-col gap-4">
              <InputField
                label="Name"
                name="name"
                defaultValue={plugin.name}
                required
              />
              <InputField
                label="Slug"
                name="slug"
                defaultValue={plugin.slug}
                required
              />
              <InputField
                label="Description"
                name="description"
                defaultValue={plugin.description ?? ""}
              />
              <Dialog.Footer>
                <Button
                  variant="secondary"
                  onClick={() => setIsEditOpen(false)}
                  type="button"
                >
                  Cancel
                </Button>
                <Button type="submit" disabled={updateMutation.isPending}>
                  Save
                </Button>
              </Dialog.Footer>
            </form>
          </Dialog.Content>
        </Dialog>

        {/* Add Server Dialog */}
        <Dialog open={isAddServerOpen} onOpenChange={setIsAddServerOpen}>
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>Add MCP Server</Dialog.Title>
              <Dialog.Description>
                Add an MCP server to this plugin bundle.
              </Dialog.Description>
            </Dialog.Header>
            <form onSubmit={handleAddServer} className="flex flex-col gap-4">
              <div className="flex flex-col gap-2">
                <label className="text-sm font-medium">Toolset</label>
                {isLoadingToolsets ? (
                  <Skeleton className="h-9 w-full" />
                ) : toolsets.length > 0 ? (
                  <select
                    name="toolsetId"
                    className="bg-background rounded-md border px-3 py-2 text-sm"
                    required
                  >
                    <option value="">Select a toolset</option>
                    {toolsets.map((t) => (
                      <option key={t.id} value={t.id}>
                        {t.name}
                      </option>
                    ))}
                  </select>
                ) : (
                  <Type muted small>
                    No toolsets available. Create a toolset in this project
                    first.
                  </Type>
                )}
              </div>
              <Dialog.Footer>
                <Button
                  variant="secondary"
                  onClick={() => setIsAddServerOpen(false)}
                  type="button"
                >
                  Cancel
                </Button>
                <Button
                  type="submit"
                  disabled={
                    addServerMutation.isPending ||
                    isLoadingToolsets ||
                    toolsets.length === 0
                  }
                >
                  Add
                </Button>
              </Dialog.Footer>
            </form>
          </Dialog.Content>
        </Dialog>
      </Page.Body>
    </Page>
  );
}
