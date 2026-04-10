import { Page } from "@/components/page-layout";
import { ProjectAvatar } from "@/components/project-menu";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog } from "@/components/ui/dialog";
import { Type } from "@/components/ui/type";
import { useOrganization } from "@/contexts/Auth";
import {
  Calendar,
  Download,
  FolderOpen,
  Loader2,
  Lock,
  Globe,
  Monitor,
  SearchX,
  Server,
  Server as ServerIcon,
  Wrench,
} from "lucide-react";
import { Button, Input } from "@speakeasy-api/moonshine";
import { Textarea } from "@/components/moon/textarea";
import { useMemo, useState } from "react";
import { useParams, Outlet } from "react-router";
import { useSdkClient } from "@/contexts/Sdk";
import { useQueries } from "@tanstack/react-query";
import { Search } from "lucide-react";
import {
  useCollections,
  useCollectionServers,
  useDeleteCollection,
  useUpdateCollection,
  useAttachServer,
  useDetachServer,
} from "./hooks";
import { useOrgRoutes } from "@/routes";
import { Pencil } from "lucide-react";
import { cn } from "@/lib/utils";
import { AddServerDialog } from "@/pages/catalog/AddServerDialog";
import type { Server as CatalogServer } from "@/pages/catalog/hooks";

export function CollectionDetailRoot() {
  return <Outlet />;
}

export default function CollectionDetail() {
  const { collectionSlug } = useParams<{ collectionSlug: string }>();
  const { data: collections } = useCollections();
  const orgRoutes = useOrgRoutes();
  const deleteCollection = useDeleteCollection();
  const updateCollection = useUpdateCollection();
  const organization = useOrganization();
  const defaultProjectSlug = organization.projects?.[0]?.slug;
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [editing, setEditing] = useState(false);
  const [showProjectPicker, setShowProjectPicker] = useState(false);
  const [selectedProjectSlug, setSelectedProjectSlug] = useState<
    string | undefined
  >();
  const [showAddDialog, setShowAddDialog] = useState(false);
  const [editName, setEditName] = useState("");
  const [editDescription, setEditDescription] = useState("");
  const [editVisibility, setEditVisibility] = useState<"public" | "private">(
    "private",
  );
  const [editSelectedToolsetIds, setEditSelectedToolsetIds] = useState<
    Set<string>
  >(new Set());
  const [serverSearch, setServerSearch] = useState("");
  const [isSaving, setIsSaving] = useState(false);

  const client = useSdkClient();
  const attachServer = useAttachServer();
  const detachServer = useDetachServer();

  const collection = collections.find((c) => c.slug === collectionSlug);
  const { servers, rawServers, isLoading } = useCollectionServers(
    collection?.slug,
  );

  // Fetch toolsets from all projects for the inline server picker
  const projects = organization.projects ?? [];
  const toolsetQueries = useQueries({
    queries: projects.map((project) => ({
      queryKey: ["toolsets", "list", project.slug],
      queryFn: () => client.toolsets.list({ gramProject: project.slug }),
      enabled: !!project.slug,
    })),
  });

  const toolsetsLoading = toolsetQueries.some((q) => q.isLoading);

  // Build a set of attached mcpSlugs for checkbox state.
  // registrySpecifier from serve() is "namespace/mcpSlug" — extract the last segment.
  const attachedMcpSlugs = useMemo(
    () =>
      new Set(
        servers.map((s) => {
          const parts = s.registrySpecifier.split("/");
          return parts[parts.length - 1];
        }),
      ),
    [servers],
  );

  // All toolsets from all projects (only MCP-enabled, excluding catalog-installed)
  const allToolsets = useMemo(() => {
    const all: Array<{
      id: string;
      mcpSlug: string;
      name: string;
      description?: string;
      projectName: string;
    }> = [];
    for (let i = 0; i < projects.length; i++) {
      const project = projects[i];
      const data = toolsetQueries[i]?.data;
      for (const t of data?.toolsets ?? []) {
        if (!t.mcpSlug) continue;
        if (t.toolUrns?.some((u) => u.startsWith("tools:externalmcp:")))
          continue;
        all.push({
          id: t.id,
          mcpSlug: t.mcpSlug,
          name: t.name,
          description: t.description ?? undefined,
          projectName: project.name,
        });
      }
    }
    return all;
  }, [projects, toolsetQueries]);

  const filteredToolsets = useMemo(() => {
    if (!serverSearch) return allToolsets;
    const q = serverSearch.toLowerCase();
    return allToolsets.filter(
      (t) =>
        t.name.toLowerCase().includes(q) ||
        (t.description && t.description.toLowerCase().includes(q)) ||
        t.projectName.toLowerCase().includes(q),
    );
  }, [allToolsets, serverSearch]);

  // Build the set of toolset IDs that are currently attached (for diffing on save)
  const attachedToolsetIds = useMemo(() => {
    const ids = new Set<string>();
    for (const toolset of allToolsets) {
      if (attachedMcpSlugs.has(toolset.mcpSlug)) {
        ids.add(toolset.id);
      }
    }
    return ids;
  }, [allToolsets, attachedMcpSlugs]);

  const toggleToolset = (toolsetId: string) => {
    setEditSelectedToolsetIds((prev) => {
      const next = new Set(prev);
      if (next.has(toolsetId)) {
        next.delete(toolsetId);
      } else {
        next.add(toolsetId);
      }
      return next;
    });
  };

  const installableServers: CatalogServer[] = rawServers.map((s) => ({
    ...s,
    meta: {},
  }));

  const totalTools = servers.reduce((sum, s) => sum + s.toolCount, 0);

  if (!collection) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <div className="flex flex-col items-center justify-center py-16 text-center">
            <SearchX className="w-10 h-10 text-muted-foreground mb-3" />
            <p className="text-sm text-muted-foreground">
              Collection not found.
            </p>
          </div>
        </Page.Body>
      </Page>
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{ [collectionSlug ?? ""]: collection.name }}
        />
      </Page.Header>
      <Page.Body>
        <div className="flex gap-8">
          {/* Main content */}
          <div className="flex-1 min-w-0">
            {/* Header */}
            <div className="flex items-start gap-4 mb-6">
              <div className="flex items-center justify-center w-14 h-14 rounded-lg border bg-muted">
                <Monitor className="w-7 h-7 text-muted-foreground" />
              </div>
              <div>
                <div className="flex items-center gap-2 mb-1">
                  <h1 className="text-2xl font-semibold">{collection.name}</h1>
                  <Badge variant="outline" className="text-xs">
                    {collection.visibility === "private" ? (
                      <>
                        <Lock className="w-3 h-3 mr-1" />
                        Private
                      </>
                    ) : (
                      <>
                        <Globe className="w-3 h-3 mr-1" />
                        Public
                      </>
                    )}
                  </Badge>
                </div>
                {collection.description && (
                  <p className="text-sm text-muted-foreground mb-3">
                    {collection.description}
                  </p>
                )}
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    onClick={() => setShowProjectPicker(true)}
                    disabled={rawServers.length === 0}
                  >
                    <Button.Icon>
                      <Download />
                    </Button.Icon>
                    <Button.Text>Install</Button.Text>
                  </Button>
                  <Button
                    size="sm"
                    variant="secondary"
                    onClick={() => {
                      setEditName(collection.name);
                      setEditDescription(collection.description);
                      setEditVisibility(collection.visibility);
                      setEditSelectedToolsetIds(new Set(attachedToolsetIds));
                      setEditing(true);
                    }}
                  >
                    <Button.Icon>
                      <Pencil />
                    </Button.Icon>
                    <Button.Text>Edit</Button.Text>
                  </Button>
                </div>
              </div>
            </div>

            {/* Edit Form */}
            {editing && (
              <div className="border rounded-lg p-5 mb-4 space-y-4">
                <h2 className="text-base font-semibold">Edit Collection</h2>
                <div>
                  <label className="block text-sm font-medium mb-1">Name</label>
                  <Input
                    value={editName}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                      setEditName(e.target.value)
                    }
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium mb-1">
                    Description
                  </label>
                  <Textarea
                    value={editDescription}
                    onChange={(e) => setEditDescription(e.target.value)}
                    rows={3}
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium mb-2">
                    Visibility
                  </label>
                  <div className="flex gap-2">
                    <button
                      type="button"
                      className={cn(
                        "flex items-center gap-1.5 px-3 py-1.5 rounded-md border text-sm transition-colors",
                        editVisibility === "public"
                          ? "border-foreground/30 bg-accent"
                          : "border-border hover:bg-accent/50",
                      )}
                      onClick={() => setEditVisibility("public")}
                    >
                      <Globe className="w-3.5 h-3.5" />
                      Public
                    </button>
                    <button
                      type="button"
                      className={cn(
                        "flex items-center gap-1.5 px-3 py-1.5 rounded-md border text-sm transition-colors",
                        editVisibility === "private"
                          ? "border-foreground/30 bg-accent"
                          : "border-border hover:bg-accent/50",
                      )}
                      onClick={() => setEditVisibility("private")}
                    >
                      <Lock className="w-3.5 h-3.5" />
                      Private
                    </button>
                  </div>
                </div>

                {/* Server picker (edit mode only) */}
                <div>
                  <label className="block text-sm font-medium mb-2">
                    MCP Servers ({editSelectedToolsetIds.size} selected)
                  </label>
                  <div className="border rounded-md">
                    <div className="relative border-b">
                      <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                      <input
                        type="text"
                        placeholder="Search servers..."
                        value={serverSearch}
                        onChange={(e) => setServerSearch(e.target.value)}
                        className="w-full pl-9 pr-3 py-2.5 text-sm bg-transparent outline-none placeholder:text-muted-foreground"
                      />
                    </div>
                    <div className="max-h-64 overflow-y-auto">
                      {toolsetsLoading ? (
                        <div className="flex items-center justify-center p-4">
                          <Loader2 className="w-5 h-5 animate-spin text-muted-foreground" />
                        </div>
                      ) : filteredToolsets.length === 0 ? (
                        <div className="flex flex-col items-center justify-center p-4 text-center">
                          <ServerIcon className="w-6 h-6 text-muted-foreground mb-1" />
                          <Type small muted>
                            {serverSearch
                              ? "No servers match your search."
                              : "No MCP servers available."}
                          </Type>
                        </div>
                      ) : (
                        filteredToolsets.map((toolset) => (
                          <label
                            key={toolset.id}
                            className="flex items-start gap-3 px-3 py-2.5 hover:bg-accent/50 cursor-pointer border-b last:border-b-0"
                          >
                            <Checkbox
                              checked={editSelectedToolsetIds.has(toolset.id)}
                              disabled={isSaving}
                              onCheckedChange={() => toggleToolset(toolset.id)}
                              className="mt-0.5"
                            />
                            <div className="min-w-0 flex-1">
                              <div className="flex items-center gap-2">
                                <span className="text-sm font-medium truncate">
                                  {toolset.name}
                                </span>
                                <Badge
                                  variant="secondary"
                                  className="text-xs shrink-0"
                                >
                                  {toolset.projectName}
                                </Badge>
                              </div>
                              {toolset.description && (
                                <div className="text-xs text-muted-foreground truncate mt-0.5">
                                  {toolset.description}
                                </div>
                              )}
                            </div>
                          </label>
                        ))
                      )}
                    </div>
                  </div>
                </div>

                <div className="flex gap-2">
                  <Button
                    size="sm"
                    disabled={isSaving || !editName}
                    onClick={async () => {
                      setIsSaving(true);
                      try {
                        // Update collection metadata
                        await updateCollection.mutateAsync({
                          request: {
                            gramProject: defaultProjectSlug,
                            updateRequestBody: {
                              collectionId: collection.id,
                              name: editName,
                              description: editDescription,
                              visibility: editVisibility,
                            },
                          },
                        });

                        // Diff server changes: attach new, detach removed
                        const toAttach = [...editSelectedToolsetIds].filter(
                          (id) => !attachedToolsetIds.has(id),
                        );
                        const toDetach = [...attachedToolsetIds].filter(
                          (id) => !editSelectedToolsetIds.has(id),
                        );

                        await Promise.all([
                          ...toAttach.map((toolsetId) =>
                            attachServer.mutateAsync({
                              request: {
                                gramProject: defaultProjectSlug,
                                attachServerRequestBody: {
                                  collectionId: collection.id,
                                  toolsetId,
                                },
                              },
                            }),
                          ),
                          ...toDetach.map((toolsetId) =>
                            detachServer.mutateAsync({
                              request: {
                                gramProject: defaultProjectSlug,
                                attachServerRequestBody: {
                                  collectionId: collection.id,
                                  toolsetId,
                                },
                              },
                            }),
                          ),
                        ]);

                        setEditing(false);
                        setServerSearch("");
                      } finally {
                        setIsSaving(false);
                      }
                    }}
                  >
                    <Button.Text>{isSaving ? "Saving..." : "Save"}</Button.Text>
                  </Button>
                  <Button
                    size="sm"
                    variant="secondary"
                    disabled={isSaving}
                    onClick={() => {
                      setEditing(false);
                      setServerSearch("");
                    }}
                  >
                    <Button.Text>Cancel</Button.Text>
                  </Button>
                </div>
              </div>
            )}

            {/* About */}
            {!editing && (
              <div className="border rounded-lg p-5 mb-4">
                <h2 className="text-base font-semibold mb-2">About</h2>
                <p className="text-sm text-muted-foreground">
                  {collection.description || "No description provided."}
                </p>
              </div>
            )}

            {/* MCP Servers (read-only list) */}
            {!editing && (
              <div className="border rounded-lg p-5">
                <h2 className="text-base font-semibold mb-4">
                  MCP Servers ({servers.length})
                </h2>
                {isLoading ? (
                  <p className="text-sm text-muted-foreground">
                    Loading servers...
                  </p>
                ) : servers.length === 0 ? (
                  <div className="flex flex-col items-center justify-center py-8 text-center">
                    <Server className="w-8 h-8 text-muted-foreground mb-2" />
                    <p className="text-sm text-muted-foreground">
                      No servers in this collection yet.
                    </p>
                  </div>
                ) : (
                  <div className="space-y-3">
                    {servers.map((server) => (
                      <div
                        key={server.registrySpecifier}
                        className="flex items-center gap-3 p-3 rounded-md border"
                      >
                        <div className="flex items-center justify-center w-9 h-9 rounded border bg-muted shrink-0">
                          <Monitor className="w-4 h-4 text-muted-foreground" />
                        </div>
                        <div className="min-w-0 flex-1">
                          <div className="flex items-center gap-2">
                            <span className="text-sm font-medium truncate">
                              {server.title}
                            </span>
                            {server.toolCount > 0 && (
                              <Badge
                                variant="secondary"
                                className="text-xs shrink-0"
                              >
                                <Wrench className="w-3 h-3 mr-1" />
                                {server.toolCount} tools
                              </Badge>
                            )}
                          </div>
                          {server.description && (
                            <p className="text-xs text-muted-foreground truncate mt-0.5">
                              {server.description}
                            </p>
                          )}
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            )}

            {/* Danger Zone */}
            <div className="border border-destructive/30 rounded-lg p-5 mt-4">
              <h2 className="text-base font-semibold text-destructive mb-2">
                Danger Zone
              </h2>
              <p className="text-sm text-muted-foreground mb-3">
                Permanently delete this collection. This action cannot be
                undone.
              </p>
              {confirmDelete ? (
                <div className="flex items-center gap-2">
                  <Button
                    variant="destructive-primary"
                    size="sm"
                    disabled={deleteCollection.isPending}
                    onClick={async () => {
                      await deleteCollection.mutateAsync({
                        request: {
                          gramProject: defaultProjectSlug,
                          collectionId: collection.id,
                        },
                      });
                      orgRoutes.collections.goTo();
                    }}
                  >
                    <Button.Text>
                      {deleteCollection.isPending
                        ? "Deleting..."
                        : "Confirm Delete"}
                    </Button.Text>
                  </Button>
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => setConfirmDelete(false)}
                  >
                    <Button.Text>Cancel</Button.Text>
                  </Button>
                </div>
              ) : (
                <Button
                  variant="destructive-primary"
                  size="sm"
                  onClick={() => setConfirmDelete(true)}
                >
                  <Button.Text>Delete Collection</Button.Text>
                </Button>
              )}
            </div>
          </div>

          {/* Sidebar */}
          <div className="w-72 shrink-0 space-y-4">
            {/* Stats */}
            <div className="border rounded-lg p-5">
              <h3 className="text-base font-semibold mb-3">Stats</h3>
              <div className="space-y-2.5">
                <div className="flex items-center justify-between text-sm">
                  <span className="text-muted-foreground">Servers</span>
                  <span className="font-medium">{servers.length}</span>
                </div>
                <div className="flex items-center justify-between text-sm">
                  <span className="text-muted-foreground">Total Tools</span>
                  <span className="font-medium">{totalTools}</span>
                </div>
              </div>
            </div>

            {/* Details */}
            <div className="border rounded-lg p-5">
              <h3 className="text-base font-semibold mb-3">Details</h3>
              <div className="space-y-2.5">
                <div className="flex items-center justify-between text-sm">
                  <span className="text-muted-foreground">Visibility</span>
                  <span className="font-medium capitalize">
                    {collection.visibility}
                  </span>
                </div>
                {collection.createdAt && (
                  <div className="flex items-center justify-between text-sm">
                    <span className="text-muted-foreground">Created</span>
                    <span className="flex items-center gap-1 font-medium">
                      <Calendar className="w-3.5 h-3.5" />
                      {new Date(collection.createdAt).toLocaleDateString()}
                    </span>
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
        <Dialog open={showProjectPicker} onOpenChange={setShowProjectPicker}>
          <Dialog.Content className="sm:max-w-md">
            <Dialog.Header>
              <Dialog.Title>Select Project</Dialog.Title>
              <Dialog.Description>
                Choose a project to install this collection's servers into.
              </Dialog.Description>
            </Dialog.Header>
            <div className="space-y-1 py-2">
              {organization.projects.map((project) => (
                <button
                  key={project.id}
                  type="button"
                  className="flex items-center gap-3 w-full rounded-md px-3 py-2.5 text-left text-sm hover:bg-accent transition-colors"
                  onClick={() => {
                    setSelectedProjectSlug(project.slug);
                    setShowProjectPicker(false);
                    setShowAddDialog(true);
                  }}
                >
                  <ProjectAvatar project={project} className="h-6 w-6" />
                  <span className="font-medium">{project.name}</span>
                </button>
              ))}
              {organization.projects.length === 0 && (
                <div className="flex flex-col items-center py-6 text-center">
                  <FolderOpen className="w-8 h-8 text-muted-foreground mb-2" />
                  <p className="text-sm text-muted-foreground">
                    No projects found.
                  </p>
                </div>
              )}
            </div>
          </Dialog.Content>
        </Dialog>
        {selectedProjectSlug && (
          <AddServerDialog
            servers={installableServers}
            projectSlug={selectedProjectSlug}
            open={showAddDialog}
            onOpenChange={(open) => {
              setShowAddDialog(open);
              if (!open) {
                setTimeout(() => setSelectedProjectSlug(undefined), 300);
              }
            }}
          />
        )}
      </Page.Body>
    </Page>
  );
}
