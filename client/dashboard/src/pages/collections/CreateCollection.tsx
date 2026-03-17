import { Page } from "@/components/page-layout";
import { Card } from "@/components/ui/card";
import { Type } from "@/components/ui/type";
import { useOrgRoutes } from "@/routes";
import { Badge } from "@/components/ui/badge";
import { Button, Input, Stack } from "@speakeasy-api/moonshine";
import {
  Check,
  Eye,
  Loader2,
  Lock,
  Plus,
  Search,
  Server as ServerIcon,
  Wrench,
  X,
} from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import {
  type CatalogServer,
  useCatalogServers,
  useCreateCollection,
} from "./hooks";
import type { CollectionServer, CollectionVisibility } from "./types";

export default function CreateCollection() {
  const orgRoutes = useOrgRoutes();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [visibility, setVisibility] = useState<CollectionVisibility>("public");
  const [selectedServers, setSelectedServers] = useState<
    Map<string, CollectionServer>
  >(new Map());
  const [serverSearch, setServerSearch] = useState("");

  const { mutate: createCollection, isPending } = useCreateCollection();

  // Catalog servers for the picker (mock data — no project auth required)
  const { data: catalogServers, isLoading: catalogLoading } = useCatalogServers(
    serverSearch || undefined,
  );

  const toggleServer = (server: CatalogServer) => {
    const key = server.registrySpecifier;
    setSelectedServers((prev) => {
      const next = new Map(prev);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.set(key, {
          registrySpecifier: server.registrySpecifier,
          title: server.title,
          description: server.description,
          iconUrl: server.iconUrl,
          toolCount: server.toolCount,
        });
      }
      return next;
    });
  };

  const handleCreate = () => {
    if (!name.trim()) return;

    createCollection({
      name: name.trim(),
      description: description.trim(),
      visibility,
      servers: Array.from(selectedServers.values()),
      author: { orgName: "My Org", orgId: "org_current" },
    });
    toast.success("Collection created successfully");
    orgRoutes.collections.goTo();
  };

  const isValid = name.trim().length > 0 && selectedServers.size > 0;

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>Create Collection</Page.Section.Title>
          <Page.Section.Description>
            Create a curated collection of MCP servers that can be installed
            together
          </Page.Section.Description>
          <Page.Section.Body>
            <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
              {/* Left — form */}
              <div className="lg:col-span-2 space-y-6">
                {/* Name */}
                <Stack gap={2}>
                  <label className="text-sm font-medium">Name</label>
                  <Input
                    placeholder="e.g. Developer Productivity Suite"
                    value={name}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                      setName(e.target.value)
                    }
                    className="h-10"
                  />
                </Stack>

                {/* Description */}
                <Stack gap={2}>
                  <label className="text-sm font-medium">Description</label>
                  <textarea
                    placeholder="Describe what this collection is for and what servers it includes..."
                    value={description}
                    onChange={(e) => setDescription(e.target.value)}
                    rows={3}
                    className="flex w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 resize-none"
                  />
                </Stack>

                {/* Visibility */}
                <Stack gap={2}>
                  <label className="text-sm font-medium">Visibility</label>
                  <Stack direction="horizontal" gap={2}>
                    <button
                      onClick={() => setVisibility("public")}
                      className={`flex items-center gap-2 px-4 py-2 rounded-lg border text-sm transition-all ${
                        visibility === "public"
                          ? "border-primary bg-primary/5 text-primary"
                          : "border-border text-muted-foreground hover:border-foreground/30"
                      }`}
                    >
                      <Eye className="w-4 h-4" />
                      Public
                    </button>
                    <button
                      onClick={() => setVisibility("private")}
                      className={`flex items-center gap-2 px-4 py-2 rounded-lg border text-sm transition-all ${
                        visibility === "private"
                          ? "border-primary bg-primary/5 text-primary"
                          : "border-border text-muted-foreground hover:border-foreground/30"
                      }`}
                    >
                      <Lock className="w-4 h-4" />
                      Private
                    </button>
                  </Stack>
                  {visibility === "private" && (
                    <Type small muted>
                      Private collections are only visible to your organization.
                      Cross-org sharing coming soon.
                    </Type>
                  )}
                </Stack>

                {/* Server picker */}
                <Stack gap={2}>
                  <Stack
                    direction="horizontal"
                    justify="space-between"
                    align="center"
                  >
                    <label className="text-sm font-medium">
                      MCP Servers ({selectedServers.size} selected)
                    </label>
                  </Stack>

                  <div className="relative w-full">
                    <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                    <Input
                      placeholder="Search catalog servers..."
                      value={serverSearch}
                      onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                        setServerSearch(e.target.value)
                      }
                      className="pl-10 pr-9 h-10"
                    />
                    {serverSearch && (
                      <button
                        onClick={() => setServerSearch("")}
                        className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground transition-colors"
                        aria-label="Clear search"
                      >
                        <X className="w-4 h-4" />
                      </button>
                    )}
                  </div>

                  {catalogLoading ? (
                    <div className="flex items-center justify-center py-8">
                      <Loader2 className="w-6 h-6 animate-spin text-muted-foreground" />
                    </div>
                  ) : (
                    <div className="grid grid-cols-1 gap-3 max-h-[400px] overflow-y-auto pr-1">
                      {catalogServers.slice(0, 20).map((server) => {
                        const isSelected = selectedServers.has(
                          server.registrySpecifier,
                        );
                        const displayName = server.title;
                        return (
                          <button
                            key={server.registrySpecifier}
                            onClick={() => toggleServer(server)}
                            className={`flex items-center gap-3 p-3 rounded-lg border text-left transition-all ${
                              isSelected
                                ? "border-primary bg-primary/5 ring-1 ring-primary/20"
                                : "border-border hover:border-foreground/30"
                            }`}
                          >
                            <div className="w-8 h-8 rounded-md bg-muted/50 flex items-center justify-center shrink-0">
                              {server.iconUrl ? (
                                <img
                                  src={server.iconUrl}
                                  alt={displayName}
                                  className="w-5 h-5 object-contain"
                                />
                              ) : (
                                <ServerIcon className="w-4 h-4 text-muted-foreground" />
                              )}
                            </div>
                            <div className="flex-1 min-w-0">
                              <Type
                                variant="subheading"
                                as="div"
                                className="text-sm truncate"
                              >
                                {displayName}
                              </Type>
                              <Type small muted className="line-clamp-1">
                                {server.description}
                              </Type>
                            </div>
                            <div
                              className={`size-5 rounded-full border-2 flex items-center justify-center shrink-0 transition-colors ${
                                isSelected
                                  ? "border-primary bg-primary"
                                  : "border-muted-foreground/30"
                              }`}
                            >
                              {isSelected && (
                                <Check
                                  className="size-3 text-white"
                                  strokeWidth={3}
                                />
                              )}
                            </div>
                          </button>
                        );
                      })}
                    </div>
                  )}
                </Stack>

                {/* Config placeholder */}
                <Card>
                  <Card.Header>
                    <Card.Title>Configuration</Card.Title>
                  </Card.Header>
                  <Card.Content>
                    <div className="flex items-center gap-3 p-4 rounded-lg border border-dashed text-muted-foreground">
                      <Wrench className="w-5 h-5 shrink-0" />
                      <Type small muted>
                        Configuration options coming soon. You&apos;ll be able
                        to define environment variables and server-level
                        settings for your collections.
                      </Type>
                    </div>
                  </Card.Content>
                </Card>
              </div>

              {/* Right — preview sidebar */}
              <div className="space-y-4">
                <Card>
                  <Card.Header>
                    <Card.Title>Preview</Card.Title>
                  </Card.Header>
                  <Card.Content>
                    {name || selectedServers.size > 0 ? (
                      <Stack gap={3}>
                        <Type variant="subheading" as="div">
                          {name || "Untitled Collection"}
                        </Type>
                        {description && (
                          <Type small muted className="line-clamp-3">
                            {description}
                          </Type>
                        )}
                        <Stack direction="horizontal" gap={2}>
                          <Badge variant="secondary">
                            {visibility === "public" ? (
                              <Eye className="w-3 h-3 mr-1" />
                            ) : (
                              <Lock className="w-3 h-3 mr-1" />
                            )}
                            {visibility === "public" ? "Public" : "Private"}
                          </Badge>
                          <Badge variant="secondary">
                            <ServerIcon className="w-3 h-3 mr-1" />
                            {selectedServers.size} servers
                          </Badge>
                        </Stack>
                        {selectedServers.size > 0 && (
                          <Stack gap={1.5} className="mt-2">
                            {Array.from(selectedServers.values()).map(
                              (server) => (
                                <Stack
                                  key={server.registrySpecifier}
                                  direction="horizontal"
                                  gap={2}
                                  align="center"
                                  className="text-sm"
                                >
                                  <ServerIcon className="w-3.5 h-3.5 text-muted-foreground shrink-0" />
                                  <Type small className="truncate">
                                    {server.title}
                                  </Type>
                                </Stack>
                              ),
                            )}
                          </Stack>
                        )}
                      </Stack>
                    ) : (
                      <Type small muted className="text-center py-4">
                        Fill in the form to see a preview of your collection.
                      </Type>
                    )}
                  </Card.Content>
                </Card>

                <Button
                  className="w-full"
                  onClick={handleCreate}
                  disabled={!isValid || isPending}
                >
                  {isPending ? (
                    <>
                      <Loader2 className="w-4 h-4 animate-spin mr-2" />
                      Creating...
                    </>
                  ) : (
                    <>
                      <Plus className="w-4 h-4 mr-2" />
                      Create Collection
                    </>
                  )}
                </Button>
              </div>
            </div>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}
