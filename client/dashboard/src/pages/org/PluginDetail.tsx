import { InputField } from "@/components/moon/input-field";
import { Page } from "@/components/page-layout";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import {
  invalidateAllPlugin,
  usePluginSuspense,
} from "@gram/client/react-query/plugin";
import { invalidateAllPlugins } from "@gram/client/react-query/plugins";
import { useUpdatePluginMutation } from "@gram/client/react-query/updatePlugin";
import { useAddPluginServerMutation } from "@gram/client/react-query/addPluginServer";
import { useRemovePluginServerMutation } from "@gram/client/react-query/removePluginServer";
import { usePublishPluginsMutation } from "@gram/client/react-query/publishPlugins";
import {
  invalidateAllGitHubConnection,
  useGitHubConnection,
} from "@gram/client/react-query/gitHubConnection";
import { useGitHubInstallURL } from "@gram/client/react-query/gitHubInstallURL";
import { useConnectGitHubMutation } from "@gram/client/react-query/connectGitHub";
import { useListToolsetsForOrg } from "@gram/client/react-query/listToolsetsForOrg";
import { Button, Column, Icon, Stack, Table } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useParams, useSearchParams } from "react-router";
import type { PluginServer } from "@gram/client/models/components";

export default function PluginDetail() {
  const { pluginId } = useParams<{ pluginId: string }>();
  const queryClient = useQueryClient();
  const [isEditOpen, setIsEditOpen] = useState(false);
  const [isAddServerOpen, setIsAddServerOpen] = useState(false);
  const [addServerSourceType, setAddServerSourceType] = useState<
    "toolset" | "external"
  >("toolset");

  const { data: plugin } = usePluginSuspense({ id: pluginId! });

  const { data: ghConnection } = useGitHubConnection(undefined, undefined, {
    retry: false,
    throwOnError: false,
  });

  const { data: installURLData } = useGitHubInstallURL(undefined, undefined, {
    retry: false,
    throwOnError: false,
  });

  const { data: toolsetsData } = useListToolsetsForOrg();
  const toolsets = toolsetsData?.toolsets ?? [];

  const invalidateAll = async () => {
    await invalidateAllPlugin(queryClient);
    await invalidateAllPlugins(queryClient);
    await invalidateAllGitHubConnection(queryClient);
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

  const connectGitHubMutation = useConnectGitHubMutation({
    onSuccess: () => invalidateAll(),
  });

  const publishMutation = usePublishPluginsMutation({
    onSuccess: () => invalidateAll(),
  });

  // Handle GitHub App installation callback — GitHub redirects back with
  // installation_id in the URL after the admin installs the app.
  const [searchParams, setSearchParams] = useSearchParams();
  useEffect(() => {
    const installationId = searchParams.get("installation_id");
    if (installationId && !ghConnection && !connectGitHubMutation.isPending) {
      connectGitHubMutation.mutate({
        security: { sessionHeaderGramSession: "" },
        request: {
          connectGitHubForm: {
            installationId: parseInt(installationId, 10),
          },
        },
      });
      setSearchParams({}, { replace: true });
    }
  }, [searchParams]);

  const handleConnectGitHub = () => {
    if (installURLData?.installed) {
      // App is already installed — auto-detect and connect.
      connectGitHubMutation.mutate({
        security: { sessionHeaderGramSession: "" },
        request: { connectGitHubForm: {} },
      });
    } else if (installURLData?.url) {
      // Redirect to GitHub to install the app.
      window.location.href = installURLData.url;
    }
  };

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

    if (addServerSourceType === "toolset") {
      const toolsetId = fd.get("toolsetId") as string;
      const toolset = toolsets.find((t) => t.id === toolsetId);
      addServerMutation.mutate({
        security: { sessionHeaderGramSession: "" },
        request: {
          addPluginServerForm: {
            pluginId: pluginId!,
            displayName: toolset?.name ?? toolsetId,
            toolsetId,
            policy: "required",
          },
        } as any,
      });
    } else {
      const externalUrl = fd.get("externalUrl") as string;
      const displayName = fd.get("displayName") as string;
      addServerMutation.mutate({
        security: { sessionHeaderGramSession: "" },
        request: {
          addPluginServerForm: {
            pluginId: pluginId!,
            displayName,
            externalUrl,
            policy: "required",
          },
        } as any,
      });
    }
  };

  const handleRemoveServer = (server: PluginServer) => {
    removeServerMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: { id: server.id, pluginId: pluginId! },
    });
  };

  const handlePublish = () => {
    publishMutation.mutate({
      security: { sessionHeaderGramSession: "" },
    });
  };

  const handleDownload = async (platform: "claude" | "cursor") => {
    const resp = await fetch(
      `/rpc/plugins.downloadPluginPackage?platform=${platform}`,
      { credentials: "include" },
    );
    if (!resp.ok) return;
    const blob = await resp.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download =
      resp.headers.get("Content-Disposition")?.match(/filename="(.+)"/)?.[1] ??
      `plugins-${platform}.zip`;
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
      key: "type",
      header: "Source",
      width: "1fr",
      render: (s) => (
        <Type variant="body">
          {s.toolsetId ? "Toolset" : s.registryId ? "Registry" : "External"}
        </Type>
      ),
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

  const repoURL = ghConnection
    ? `https://github.com/${ghConnection.repoOwner}/${ghConnection.repoName}`
    : publishMutation.data?.repoUrl;

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
              className="h-full p-4 bg-background"
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

        {/* Distribution section */}
        <Heading variant="h5" className="mb-3">
          Distribution
        </Heading>

        <div className="border rounded-lg p-4 mb-6">
          {ghConnection ? (
            <Stack gap={3}>
              <Stack
                direction="horizontal"
                justify="space-between"
                align="center"
              >
                <div>
                  <Type variant="body">
                    Connected to{" "}
                    <a
                      href={repoURL}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="underline"
                    >
                      {ghConnection.repoOwner}/{ghConnection.repoName}
                    </a>
                  </Type>
                  <Type muted small className="mt-1">
                    Publish generates Claude Code and Cursor plugin packages and
                    pushes them to this repo. Add the repo URL to your platform
                    marketplace settings to distribute to your team.
                  </Type>
                </div>
                <Button
                  onClick={handlePublish}
                  disabled={publishMutation.isPending}
                >
                  {publishMutation.isPending ? "Publishing..." : "Publish"}
                </Button>
              </Stack>
            </Stack>
          ) : (
            <Stack gap={3} align="center" className="py-4">
              <div className="size-12 rounded-full bg-muted flex items-center justify-center">
                <Icon name="github" className="size-6 text-muted-foreground" />
              </div>
              <div className="text-center max-w-sm">
                <Type variant="body" className="mb-1">
                  Connect GitHub
                </Type>
                <Type muted small>
                  Install the Gram GitHub App on your GitHub organization. Gram
                  will create a repository and push plugin packages when you
                  publish.
                </Type>
              </div>
              <Button
                onClick={handleConnectGitHub}
                disabled={connectGitHubMutation.isPending}
              >
                <Button.LeftIcon>
                  <Icon name="github" className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>
                  {connectGitHubMutation.isPending
                    ? "Connecting..."
                    : installURLData?.installed
                      ? "Connect"
                      : "Install GitHub App"}
                </Button.Text>
              </Button>
            </Stack>
          )}
        </div>

        <Stack direction="horizontal" className="gap-3">
          <Button
            variant="secondary"
            size="sm"
            onClick={() => handleDownload("claude")}
          >
            <Button.LeftIcon>
              <Icon name="download" className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Download Claude Code ZIP</Button.Text>
          </Button>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => handleDownload("cursor")}
          >
            <Button.LeftIcon>
              <Icon name="download" className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Download Cursor ZIP</Button.Text>
          </Button>
        </Stack>

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
            <div className="flex gap-2 mb-4">
              <Button
                size="sm"
                variant={
                  addServerSourceType === "toolset" ? "primary" : "secondary"
                }
                onClick={() => setAddServerSourceType("toolset")}
                type="button"
              >
                Toolset
              </Button>
              <Button
                size="sm"
                variant={
                  addServerSourceType === "external" ? "primary" : "secondary"
                }
                onClick={() => setAddServerSourceType("external")}
                type="button"
              >
                External URL
              </Button>
            </div>
            <form onSubmit={handleAddServer} className="flex flex-col gap-4">
              {addServerSourceType === "toolset" ? (
                <div className="flex flex-col gap-2">
                  <label className="text-sm font-medium">Toolset</label>
                  {toolsets.length > 0 ? (
                    <select
                      name="toolsetId"
                      className="border rounded-md px-3 py-2 text-sm bg-background"
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
                      No toolsets available. Create a toolset first.
                    </Type>
                  )}
                </div>
              ) : (
                <>
                  <InputField
                    label="Display Name"
                    name="displayName"
                    required
                    autoFocus
                  />
                  <InputField
                    label="URL"
                    name="externalUrl"
                    placeholder="https://..."
                    required
                  />
                </>
              )}
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
                    (addServerSourceType === "toolset" && toolsets.length === 0)
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
