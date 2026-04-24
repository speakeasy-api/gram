import { InputField } from "@/components/moon/input-field";
import { Page } from "@/components/page-layout";
import { Dialog } from "@/components/ui/dialog";
import { SearchBar } from "@/components/ui/search-bar";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import { useRoutes } from "@/routes";
import { Plugin } from "@gram/client/models/components";
import { useCreatePluginMutation } from "@gram/client/react-query/createPlugin";
import {
  invalidateAllPlugins,
  usePluginsSuspense,
} from "@gram/client/react-query/plugins";
import { useDeletePluginMutation } from "@gram/client/react-query/deletePlugin";
import {
  invalidateAllPublishStatus,
  usePublishStatusSuspense,
} from "@gram/client/react-query/publishStatus";
import { usePublishPluginsMutation } from "@gram/client/react-query/publishPlugins";
import { Button, Column, Icon, Stack, Table } from "@speakeasy-api/moonshine";
import { Plus, Puzzle } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";
import { Link, Outlet, useNavigate } from "react-router";
import { toast } from "sonner";
import { PublishDialog } from "./PublishDialog";

export function PluginsRoot() {
  return <Outlet />;
}

export default function Plugins() {
  const [isCreateDialogOpen, setIsCreateDialogOpen] = useState(false);
  const [isPublishDialogOpen, setIsPublishDialogOpen] = useState(false);
  const [pluginToDelete, setPluginToDelete] = useState<Plugin | null>(null);
  const [search, setSearch] = useState("");
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const navigate = useNavigate();

  const { data } = usePluginsSuspense();
  const { data: publishStatus } = usePublishStatusSuspense();

  const publishMutation = usePublishPluginsMutation({
    onSuccess: () => {
      setIsPublishDialogOpen(false);
      invalidateAllPublishStatus(queryClient);
      toast.success("Plugins published to GitHub");
    },
    onError: () => {
      toast.error("Failed to publish plugins to GitHub");
    },
  });

  const hasPlugins = (data?.plugins ?? []).length > 0;

  const filteredPlugins = useMemo(() => {
    const plugins = data?.plugins ?? [];
    const q = search.trim().toLowerCase();
    if (!q) return plugins;
    return plugins.filter(
      (p) =>
        p.name.toLowerCase().includes(q) || p.slug.toLowerCase().includes(q),
    );
  }, [data?.plugins, search]);

  const createMutation = useCreatePluginMutation({
    onSuccess: async (data) => {
      setIsCreateDialogOpen(false);
      await invalidateAllPlugins(queryClient);
      navigate(routes.plugins.detail.href(data.id));
    },
  });

  const deleteMutation = useDeletePluginMutation({
    onSuccess: async () => {
      setPluginToDelete(null);
      await invalidateAllPlugins(queryClient);
    },
  });

  const handleCreate: React.FormEventHandler<HTMLFormElement> = (e) => {
    e.preventDefault();
    const formData = new FormData(e.currentTarget);
    const name = formData.get("name") as string;
    const description = formData.get("description") as string;

    createMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        createPluginForm: {
          name,
          description: description || undefined,
        },
      },
    });
  };

  const handleDelete = () => {
    if (!pluginToDelete) return;
    deleteMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: { id: pluginToDelete.id },
    });
  };

  // Destructure mutate so the dep array references the stable function
  // directly (TanStack Query keeps mutate referentially stable, but the
  // wrapper object is fresh per render). Keeps memo() on PublishDialog
  // effective and satisfies react-hooks/exhaustive-deps.
  const { mutate: publishMutate } = publishMutation;
  const handlePublish = useCallback(
    (githubUsername?: string) => {
      publishMutate({
        security: { sessionHeaderGramSession: "" },
        request: {
          publishPluginsRequestBody: { githubUsername },
        },
      });
    },
    [publishMutate],
  );

  const columns: Column<Plugin>[] = [
    {
      key: "name",
      header: "Name",
      width: "2fr",
      render: (p) => (
        <Link to={routes.plugins.detail.href(p.id)}>
          <Type variant="body" className="cursor-pointer hover:underline">
            {p.name}
          </Type>
        </Link>
      ),
    },
    {
      key: "slug",
      header: "Slug",
      width: "1fr",
      render: (p) => <Type variant="body">{p.slug}</Type>,
    },
    {
      key: "servers",
      header: "Servers",
      width: "100px",
      render: (p) => <Type variant="body">{p.serverCount ?? 0}</Type>,
    },
    {
      key: "updatedAt",
      header: "Updated",
      width: "1fr",
      render: (p) => <HumanizeDateTime date={p.updatedAt} />,
    },
    {
      key: "actions",
      header: "",
      width: "80px",
      render: (p) => (
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => setPluginToDelete(p)}
          className="hover:text-destructive"
        >
          <Button.LeftIcon>
            <Icon name="trash-2" className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text className="sr-only">Delete plugin</Button.Text>
        </Button>
      ),
    },
  ];

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>Plugins</Page.Section.Title>
          <Page.Section.Description className={hasPlugins ? "w-3/4" : ""}>
            Create distributable plugin bundles that package MCP servers and
            hooks together. Assign plugins to roles and publish them to Claude
            Code, Cursor, and Codex marketplaces via GitHub.
          </Page.Section.Description>
          <Page.Section.CTA>
            {hasPlugins && (
              <Stack direction="horizontal" gap={2}>
                {publishStatus?.configured && (
                  <Button
                    variant="secondary"
                    onClick={() => setIsPublishDialogOpen(true)}
                    disabled={publishMutation.isPending}
                  >
                    <Button.LeftIcon>
                      <Icon
                        name={publishStatus.connected ? "refresh-cw" : "upload"}
                        className="h-4 w-4"
                      />
                    </Button.LeftIcon>
                    <Button.Text>
                      {publishMutation.isPending
                        ? "Publishing..."
                        : publishStatus.connected
                          ? "Re-publish"
                          : "Publish to GitHub"}
                    </Button.Text>
                  </Button>
                )}
                <Button onClick={() => setIsCreateDialogOpen(true)}>
                  <Button.LeftIcon>
                    <Plus className="h-4 w-4" />
                  </Button.LeftIcon>
                  <Button.Text>New Plugin</Button.Text>
                </Button>
              </Stack>
            )}
          </Page.Section.CTA>
          <Page.Section.Body>
            {!hasPlugins ? (
              <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
                <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
                  <Puzzle className="text-muted-foreground h-6 w-6" />
                </div>
                <Type variant="subheading" className="mb-1">
                  No plugins yet
                </Type>
                <Type small muted className="mb-4 max-w-md text-center">
                  Add your first plugin to start distributing MCP servers and
                  hooks to Claude Code, Cursor, and Codex.
                </Type>
                <Button onClick={() => setIsCreateDialogOpen(true)}>
                  <Button.LeftIcon>
                    <Plus className="h-4 w-4" />
                  </Button.LeftIcon>
                  <Button.Text>New Plugin</Button.Text>
                </Button>
              </div>
            ) : (
              <>
                <Stack
                  direction="horizontal"
                  justify="space-between"
                  align="center"
                  className="mb-4"
                >
                  <SearchBar
                    value={search}
                    onChange={setSearch}
                    placeholder="Search plugins"
                    className="w-64"
                  />
                </Stack>
                <Table
                  columns={columns}
                  data={filteredPlugins}
                  rowKey={(row) => row.id}
                />
              </>
            )}
          </Page.Section.Body>
        </Page.Section>

        {/* Create Dialog */}
        <Dialog open={isCreateDialogOpen} onOpenChange={setIsCreateDialogOpen}>
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>Create Plugin</Dialog.Title>
              <Dialog.Description>
                Create a new plugin bundle for distributing MCP servers.
              </Dialog.Description>
            </Dialog.Header>
            <form onSubmit={handleCreate} className="flex flex-col gap-4">
              <InputField label="Name" name="name" required autoFocus />
              <InputField label="Description" name="description" />
              <Dialog.Footer>
                <Button
                  variant="secondary"
                  onClick={() => setIsCreateDialogOpen(false)}
                  type="button"
                >
                  Cancel
                </Button>
                <Button type="submit" disabled={createMutation.isPending}>
                  Create
                </Button>
              </Dialog.Footer>
            </form>
          </Dialog.Content>
        </Dialog>

        {/* Delete Confirmation Dialog */}
        <Dialog
          open={!!pluginToDelete}
          onOpenChange={() => setPluginToDelete(null)}
        >
          <Dialog.Content>
            <Dialog.Header>
              <Dialog.Title>Delete Plugin</Dialog.Title>
              <Dialog.Description>
                Are you sure you want to delete &quot;{pluginToDelete?.name}
                &quot;? This will remove it from all assigned users on the next
                publish.
              </Dialog.Description>
            </Dialog.Header>
            <Dialog.Footer>
              <Button
                variant="secondary"
                onClick={() => setPluginToDelete(null)}
              >
                Cancel
              </Button>
              <Button
                variant="destructive-primary"
                onClick={handleDelete}
                disabled={deleteMutation.isPending}
              >
                Delete
              </Button>
            </Dialog.Footer>
          </Dialog.Content>
        </Dialog>
        <PublishDialog
          open={isPublishDialogOpen}
          onOpenChange={setIsPublishDialogOpen}
          onPublish={handlePublish}
          isPending={publishMutation.isPending}
        />
      </Page.Body>
    </Page>
  );
}
