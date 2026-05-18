import { Page } from "@/components/page-layout";
import { ProjectAvatar } from "@/components/project-menu";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog } from "@/components/ui/dialog";
import { Combobox } from "@/components/ui/combobox";
import { Type } from "@/components/ui/type";
import { useOrganization } from "@/contexts/Auth";
import {
  AlertTriangle,
  Calendar,
  Download,
  FolderOpen,
  Loader2,
  Lock,
  Globe,
  LayoutGrid,
  SearchX,
  Server,
  Server as ServerIcon,
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
import type { PulseMCPServer as CatalogServer } from "@/pages/catalog/hooks";
import { buildCollectionMcpJson, formatMcpJson } from "@/lib/mcp-json";
import { toast } from "sonner";

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
  const [selectedProjectSlug, setSelectedProjectSlug] = useState<
    string | undefined
  >(defaultProjectSlug);
  const [pendingInstallServer, setPendingInstallServer] =
    useState<CatalogServer | null>(null);
  const [activeInstallServer, setActiveInstallServer] =
    useState<CatalogServer | null>(null);
  const [pendingBulkInstall, setPendingBulkInstall] = useState(false);
  const [bulkInstallServers, setBulkInstallServers] = useState<
    CatalogServer[] | null
  >(null);
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
  const projects = useMemo(
    () => organization.projects ?? [],
    [organization.projects],
  );
  const toolsetQueries = useQueries({
    queries: projects.map((project) => ({
      queryKey: ["toolsets", "list", project.slug],
      queryFn: () => client.toolsets.list({ gramProject: project.slug }),
      enabled: !!project.slug,
    })),
  });

  const toolsetsLoading = toolsetQueries.some((q) => q.isLoading);

  // All MCP-enabled toolsets from all projects.
  const allToolsets = useMemo(() => {
    const all: Array<{
      id: string;
      mcpSlug?: string;
      name: string;
      description?: string;
      projectName: string;
    }> = [];
    for (let i = 0; i < projects.length; i++) {
      const project = projects[i];
      const data = toolsetQueries[i]?.data;
      for (const t of data?.toolsets ?? []) {
        if (!t.mcpEnabled) continue;
        if (!t.mcpSlug) continue;
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

  // Collection attachments carry the concrete toolset_id they link to, so
  // membership is a direct identity check — no need to reconcile via origin
  // specifier or mcp slug (see plan.md decision #4).
  const attachedToolsetIds = useMemo(
    () =>
      new Set(
        rawServers
          .map((server) => server.toolsetId)
          .filter((id): id is string => !!id),
      ),
    [rawServers],
  );

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

  const collectionMcpJson = useMemo(
    () => buildCollectionMcpJson(rawServers),
    [rawServers],
  );

  // Servers that have an active endpoint and can be installed
  const installableServersWithEndpoint = useMemo(() => {
    const excludedSpecifiers = new Set(
      collectionMcpJson.excludedServers.map((s) => s.registrySpecifier),
    );
    return installableServers.filter(
      (s) => !excludedSpecifiers.has(s.registrySpecifier),
    );
  }, [installableServers, collectionMcpJson.excludedServers]);

  const excludedServersNotice =
    collectionMcpJson.excludedCount === 1
      ? "1 server was excluded because it has no active endpoint."
      : `${collectionMcpJson.excludedCount} servers were excluded because they have no active endpoint.`;

  const projectOptions = useMemo(
    () =>
      projects.map((project) => ({
        ...project,
        value: project.slug,
        label: project.name,
        icon: (
          <ProjectAvatar
            project={project}
            className="h-4 min-h-4 w-4 min-w-4"
          />
        ),
      })),
    [projects],
  );
  const selectedProjectOption =
    projectOptions.find((project) => project.value === selectedProjectSlug) ??
    projectOptions[0];

  const openInstallDialog = (server: CatalogServer) => {
    setPendingInstallServer(server);
    setSelectedProjectSlug((current) => current ?? defaultProjectSlug);
  };

  const openBulkInstallDialog = () => {
    const servers = installableServersWithEndpoint;
    if (servers.length === 0) return;

    if (collectionMcpJson.excludedCount > 0) {
      toast.info(
        `Installing ${servers.length} of ${rawServers.length} servers (${collectionMcpJson.excludedCount} ${collectionMcpJson.excludedCount === 1 ? "has" : "have"} no active endpoint).`,
      );
    }

    setPendingBulkInstall(true);
    setSelectedProjectSlug((current) => current ?? defaultProjectSlug);
  };

  const totalTools = servers.reduce((sum, s) => sum + s.toolCount, 0);

  const handleDownloadCollectionMcpJson = () => {
    if (!collection || collectionMcpJson.includedCount === 0) {
      return;
    }

    const blob = new Blob([formatMcpJson(collectionMcpJson.config)], {
      type: "application/json",
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${collection.slug ?? collection.id}-mcp.json`;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);

    toast.success("mcp.json generated");
  };

  if (!collection) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <div className="flex flex-col items-center justify-center py-16 text-center">
            <SearchX className="text-muted-foreground mb-3 h-10 w-10" />
            <p className="text-muted-foreground text-sm">
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
        <div className="flex flex-col gap-8 xl:flex-row">
          {/* Main content */}
          <div className="min-w-0 flex-1">
            {/* Header */}
            <div className="bg-card mb-6 rounded-xl border p-5 shadow-sm">
              <div className="flex flex-col gap-5 2xl:flex-row 2xl:items-start 2xl:justify-between">
                <div className="flex min-w-0 flex-col gap-4 sm:flex-row sm:items-start">
                  <div className="bg-muted/60 flex h-16 w-16 shrink-0 items-center justify-center rounded-xl border">
                    <LayoutGrid className="text-muted-foreground h-8 w-8" />
                  </div>
                  <div className="min-w-0 space-y-3">
                    <div className="space-y-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <h1 className="truncate text-2xl font-semibold">
                          {collection.name}
                        </h1>
                        <Badge variant="outline" className="text-xs">
                          {collection.visibility === "private" ? (
                            <>
                              <Lock className="mr-1 h-3 w-3" />
                              Private
                            </>
                          ) : (
                            <>
                              <Globe className="mr-1 h-3 w-3" />
                              Public
                            </>
                          )}
                        </Badge>
                      </div>
                      <div className="text-muted-foreground flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
                        <span>
                          {servers.length}{" "}
                          {servers.length === 1 ? "server" : "servers"}
                        </span>
                        <span aria-hidden="true">/</span>
                        <span>
                          {totalTools} {totalTools === 1 ? "tool" : "tools"}
                        </span>
                        {collectionMcpJson.excludedCount > 0 && (
                          <>
                            <span aria-hidden="true">/</span>
                            <span>
                              {collectionMcpJson.excludedCount} unavailable
                            </span>
                          </>
                        )}
                      </div>
                    </div>
                    <p className="text-muted-foreground max-w-2xl text-sm">
                      {collection.description ||
                        "A reusable collection of MCP servers that can be installed into a project together."}
                    </p>
                  </div>
                </div>
                <div className="flex flex-wrap gap-2 2xl:shrink-0 2xl:justify-end">
                  <Button
                    size="sm"
                    className="w-full sm:w-auto"
                    disabled={
                      isLoading ||
                      installableServersWithEndpoint.length === 0 ||
                      projects.length === 0
                    }
                    onClick={openBulkInstallDialog}
                  >
                    <Button.Icon>
                      <Download />
                    </Button.Icon>
                    <Button.Text>Install</Button.Text>
                  </Button>
                  <Button
                    size="sm"
                    variant="secondary"
                    className="w-full sm:w-auto"
                    disabled={
                      isLoading || collectionMcpJson.includedCount === 0
                    }
                    onClick={handleDownloadCollectionMcpJson}
                  >
                    <Button.Icon>
                      <Download />
                    </Button.Icon>
                    <Button.Text>Generate mcp.json</Button.Text>
                  </Button>
                  <Button
                    size="sm"
                    variant="secondary"
                    className="w-full sm:w-auto"
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

            {!isLoading && collectionMcpJson.excludedCount > 0 && (
              <div className="border-warning-default bg-warning-softest mb-4 flex items-start gap-3 rounded-md border p-3">
                <AlertTriangle className="text-warning-foreground mt-0.5 h-4 w-4 shrink-0" />
                <div>
                  <Type variant="body" className="font-medium">
                    Some servers were excluded
                  </Type>
                  <Type small className="text-warning-foreground">
                    {excludedServersNotice}
                  </Type>
                </div>
              </div>
            )}

            {/* Edit Form */}
            {editing && (
              <div className="mb-4 space-y-4 rounded-lg border p-5">
                <h2 className="text-base font-semibold">Edit Collection</h2>
                <div>
                  <label className="mb-1 block text-sm font-medium">Name</label>
                  <Input
                    value={editName}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                      setEditName(e.target.value)
                    }
                  />
                </div>
                <div>
                  <label className="mb-1 block text-sm font-medium">
                    Description
                  </label>
                  <Textarea
                    value={editDescription}
                    onChange={(e) => setEditDescription(e.target.value)}
                    rows={3}
                  />
                </div>
                <div>
                  <label className="mb-2 block text-sm font-medium">
                    Visibility
                  </label>
                  <div className="flex gap-2">
                    <button
                      type="button"
                      className={cn(
                        "flex items-center gap-1.5 rounded-md border px-3 py-1.5 text-sm transition-colors",
                        editVisibility === "public"
                          ? "border-foreground/30 bg-accent"
                          : "border-border hover:bg-accent/50",
                      )}
                      onClick={() => setEditVisibility("public")}
                    >
                      <Globe className="h-3.5 w-3.5" />
                      Public
                    </button>
                    <button
                      type="button"
                      className={cn(
                        "flex items-center gap-1.5 rounded-md border px-3 py-1.5 text-sm transition-colors",
                        editVisibility === "private"
                          ? "border-foreground/30 bg-accent"
                          : "border-border hover:bg-accent/50",
                      )}
                      onClick={() => setEditVisibility("private")}
                    >
                      <Lock className="h-3.5 w-3.5" />
                      Private
                    </button>
                  </div>
                </div>

                {/* Server picker (edit mode only) */}
                <div>
                  <label className="mb-2 block text-sm font-medium">
                    MCP Servers ({editSelectedToolsetIds.size} selected)
                  </label>
                  <div className="rounded-md border">
                    <div className="relative border-b">
                      <Search className="text-muted-foreground absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2" />
                      <input
                        type="text"
                        placeholder="Search servers..."
                        value={serverSearch}
                        onChange={(e) => setServerSearch(e.target.value)}
                        className="placeholder:text-muted-foreground w-full bg-transparent py-2.5 pr-3 pl-9 text-sm outline-none"
                      />
                    </div>
                    <div className="max-h-64 overflow-y-auto">
                      {toolsetsLoading ? (
                        <div className="flex items-center justify-center p-4">
                          <Loader2 className="text-muted-foreground h-5 w-5 animate-spin" />
                        </div>
                      ) : filteredToolsets.length === 0 ? (
                        <div className="flex flex-col items-center justify-center p-4 text-center">
                          <ServerIcon className="text-muted-foreground mb-1 h-6 w-6" />
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
                            className="hover:bg-accent/50 flex cursor-pointer items-start gap-3 border-b px-3 py-2.5 last:border-b-0"
                          >
                            <Checkbox
                              checked={editSelectedToolsetIds.has(toolset.id)}
                              disabled={isSaving}
                              onCheckedChange={() => toggleToolset(toolset.id)}
                              className="mt-0.5"
                            />
                            <div className="min-w-0 flex-1">
                              <div className="flex items-center gap-2">
                                <span className="truncate text-sm font-medium">
                                  {toolset.name}
                                </span>
                                <Badge
                                  variant="secondary"
                                  className="shrink-0 text-xs"
                                >
                                  {toolset.projectName}
                                </Badge>
                              </div>
                              {toolset.description && (
                                <div className="text-muted-foreground mt-0.5 truncate text-xs">
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
              <div className="mb-4 rounded-lg border p-5">
                <h2 className="mb-2 text-base font-semibold">
                  About this collection
                </h2>
                <p className="text-muted-foreground text-sm">
                  {collection.description || "No description provided."}
                </p>
              </div>
            )}

            {/* MCP Servers (read-only list) */}
            {!editing && (
              <div className="rounded-lg border p-5">
                <div className="mb-4 flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
                  <div>
                    <h2 className="text-base font-semibold">
                      Included servers
                    </h2>
                    <p className="text-muted-foreground mt-1 text-sm">
                      These servers install together into the selected project.
                    </p>
                  </div>
                  <Badge variant="secondary" className="shrink-0">
                    <Server className="mr-1 h-3 w-3" />
                    {servers.length}
                  </Badge>
                </div>
                {isLoading ? (
                  <p className="text-muted-foreground text-sm">
                    Loading servers...
                  </p>
                ) : servers.length === 0 ? (
                  <div className="flex flex-col items-center justify-center py-8 text-center">
                    <Server className="text-muted-foreground mb-2 h-8 w-8" />
                    <p className="text-muted-foreground text-sm">
                      No servers in this collection yet.
                    </p>
                  </div>
                ) : (
                  <div className="space-y-3">
                    {rawServers.map((server, index) => {
                      const installableServer = installableServers[index];
                      return (
                        <div
                          key={server.registrySpecifier}
                          className="bg-card hover:bg-accent/30 flex flex-col gap-4 rounded-lg border p-4 transition-colors sm:flex-row sm:items-center"
                        >
                          <div className="bg-muted/60 flex h-11 w-11 shrink-0 items-center justify-center rounded-lg border">
                            {server.iconUrl ? (
                              <img
                                src={server.iconUrl}
                                alt=""
                                className="h-6 w-6 rounded"
                              />
                            ) : (
                              <ServerIcon className="text-muted-foreground h-5 w-5" />
                            )}
                          </div>
                          <div className="min-w-0 flex-1">
                            <div className="flex items-center gap-2">
                              <span className="truncate text-sm font-medium">
                                {server.title ?? server.registrySpecifier}
                              </span>
                            </div>
                            <p className="text-muted-foreground mt-1 line-clamp-2 text-xs">
                              {server.description || server.registrySpecifier}
                            </p>
                          </div>
                          <Button
                            size="sm"
                            variant="secondary"
                            className="w-full sm:w-auto"
                            disabled={
                              !installableServer || projects.length === 0
                            }
                            onClick={() => {
                              if (installableServer) {
                                openInstallDialog(installableServer);
                              }
                            }}
                          >
                            <Button.Icon>
                              <Download />
                            </Button.Icon>
                            <Button.Text>Install Server</Button.Text>
                          </Button>
                        </div>
                      );
                    })}
                  </div>
                )}
              </div>
            )}
          </div>

          {/* Sidebar */}
          <div className="w-full shrink-0 space-y-4 xl:w-72">
            {/* Stats */}
            <div className="rounded-lg border p-5">
              <h3 className="mb-3 text-base font-semibold">Stats</h3>
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
            <div className="rounded-lg border p-5">
              <h3 className="mb-3 text-base font-semibold">Details</h3>
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
                      <Calendar className="h-3.5 w-3.5" />
                      {new Date(collection.createdAt).toLocaleDateString()}
                    </span>
                  </div>
                )}
              </div>
            </div>

            <div className="border-destructive/30 rounded-lg border p-5">
              <h3 className="text-destructive mb-2 text-base font-semibold">
                Danger Zone
              </h3>
              <p className="text-muted-foreground mb-3 text-sm">
                Permanently delete this collection. This action cannot be
                undone.
              </p>
              {confirmDelete ? (
                <div className="space-y-2">
                  <Button
                    variant="destructive-primary"
                    size="sm"
                    disabled={deleteCollection.isPending}
                    onClick={async () => {
                      await deleteCollection.mutateAsync({
                        request: {
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
        </div>
        <Dialog
          open={pendingInstallServer !== null || pendingBulkInstall}
          onOpenChange={(open) => {
            if (!open) {
              setPendingInstallServer(null);
              setPendingBulkInstall(false);
            }
          }}
        >
          <Dialog.Content className="sm:max-w-md">
            <Dialog.Header>
              <Dialog.Title>Select Project</Dialog.Title>
              <Dialog.Description>
                {pendingBulkInstall ? (
                  <>
                    Choose where to install{" "}
                    <span className="font-medium">
                      {installableServersWithEndpoint.length} servers
                    </span>
                    .
                  </>
                ) : (
                  <>
                    Choose where to install{" "}
                    <span className="font-medium">
                      {pendingInstallServer?.title ??
                        pendingInstallServer?.registrySpecifier}
                    </span>
                    .
                  </>
                )}
              </Dialog.Description>
            </Dialog.Header>
            <div className="space-y-4 py-2">
              {pendingInstallServer && !pendingBulkInstall && (
                <div className="rounded-lg border p-3">
                  <div className="text-sm font-medium">
                    {pendingInstallServer.title ??
                      pendingInstallServer.registrySpecifier}
                  </div>
                  {pendingInstallServer.description && (
                    <p className="text-muted-foreground mt-1 text-xs">
                      {pendingInstallServer.description}
                    </p>
                  )}
                </div>
              )}
              {pendingBulkInstall && (
                <div className="rounded-lg border p-3">
                  <div className="flex items-center gap-2 text-sm font-medium">
                    <Server className="h-4 w-4" />
                    {installableServersWithEndpoint.length} servers from{" "}
                    {collection.name}
                  </div>
                </div>
              )}
              {projectOptions.length === 0 ? (
                <div className="flex flex-col items-center py-6 text-center">
                  <FolderOpen className="text-muted-foreground mb-2 h-8 w-8" />
                  <p className="text-muted-foreground text-sm">
                    No projects found.
                  </p>
                </div>
              ) : (
                <div className="space-y-2">
                  <label className="text-sm font-medium">Project</label>
                  <Combobox
                    items={projectOptions}
                    selected={selectedProjectOption}
                    onSelectionChange={(project) =>
                      setSelectedProjectSlug(project.value)
                    }
                    className="w-full justify-between"
                  >
                    {selectedProjectOption ? (
                      <div className="flex items-center gap-2">
                        <ProjectAvatar
                          project={selectedProjectOption}
                          className="h-4 min-h-4 w-4 min-w-4"
                        />
                        <span className="truncate">
                          {selectedProjectOption.label}
                        </span>
                      </div>
                    ) : (
                      <span>Select a project</span>
                    )}
                  </Combobox>
                </div>
              )}
            </div>
            <Dialog.Footer>
              <Button
                variant="secondary"
                onClick={() => {
                  setPendingInstallServer(null);
                  setPendingBulkInstall(false);
                }}
              >
                Cancel
              </Button>
              <Button
                disabled={
                  (!pendingInstallServer && !pendingBulkInstall) ||
                  !selectedProjectOption
                }
                onClick={() => {
                  if (!selectedProjectOption) return;

                  setSelectedProjectSlug(selectedProjectOption.value);

                  if (pendingBulkInstall) {
                    setBulkInstallServers(installableServersWithEndpoint);
                    setPendingBulkInstall(false);
                    setShowAddDialog(true);
                  } else if (pendingInstallServer) {
                    setActiveInstallServer(pendingInstallServer);
                    setPendingInstallServer(null);
                    setShowAddDialog(true);
                  }
                }}
              >
                Continue
              </Button>
            </Dialog.Footer>
          </Dialog.Content>
        </Dialog>
        {activeInstallServer && selectedProjectSlug && (
          <AddServerDialog
            servers={[activeInstallServer]}
            projectSlug={selectedProjectSlug}
            open={showAddDialog}
            onOpenChange={(open) => {
              setShowAddDialog(open);
              if (!open) {
                setTimeout(() => setActiveInstallServer(null), 300);
              }
            }}
          />
        )}
        {bulkInstallServers && selectedProjectSlug && (
          <AddServerDialog
            servers={bulkInstallServers}
            projectSlug={selectedProjectSlug}
            open={showAddDialog}
            bulk
            onOpenChange={(open) => {
              setShowAddDialog(open);
              if (!open) {
                setTimeout(() => setBulkInstallServers(null), 300);
              }
            }}
          />
        )}
      </Page.Body>
    </Page>
  );
}
