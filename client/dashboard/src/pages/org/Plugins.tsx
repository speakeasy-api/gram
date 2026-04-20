import { InputField } from "@/components/moon/input-field";
import { Page } from "@/components/page-layout";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
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
import { Button, Column, Icon, Stack, Table } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { Link, Outlet, useNavigate } from "react-router";

export function PluginsRoot() {
  return <Outlet />;
}

export default function Plugins() {
  const [isCreateDialogOpen, setIsCreateDialogOpen] = useState(false);
  const [pluginToDelete, setPluginToDelete] = useState<Plugin | null>(null);
  const [search, setSearch] = useState("");
  const queryClient = useQueryClient();
  const routes = useRoutes();
  const navigate = useNavigate();

  const { data } = usePluginsSuspense();

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
        {(data?.plugins ?? []).length === 0 ? (
          <div className="flex flex-col items-center justify-center px-4 py-16">
            <div className="w-full max-w-md space-y-6 text-center">
              <div className="flex flex-col items-center gap-4">
                <div className="bg-muted flex size-16 items-center justify-center rounded-full">
                  <Icon
                    name="puzzle"
                    className="text-muted-foreground size-8"
                  />
                </div>
                <div>
                  <Heading variant="h4" className="mb-2">
                    No plugins yet
                  </Heading>
                  <Type muted small>
                    Create distributable plugin bundles that package MCP servers
                    and hooks together. Assign plugins to roles and publish them
                    to Claude Code and Cursor marketplaces via GitHub.
                  </Type>
                </div>
              </div>
              <Button onClick={() => setIsCreateDialogOpen(true)}>
                Create Your First Plugin
              </Button>
            </div>
          </div>
        ) : (
          <>
            <Heading variant="h4" className="mb-2">
              Plugins
            </Heading>
            <Type muted small className="mb-6">
              Create distributable plugin bundles that package MCP servers and
              hooks together. Assign plugins to roles and publish them to Claude
              Code and Cursor marketplaces via GitHub.
            </Type>
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
              <Button onClick={() => setIsCreateDialogOpen(true)}>
                New Plugin
              </Button>
            </Stack>
            <Table
              columns={columns}
              data={filteredPlugins}
              rowKey={(row) => row.id}
            />
          </>
        )}

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
      </Page.Body>
    </Page>
  );
}
