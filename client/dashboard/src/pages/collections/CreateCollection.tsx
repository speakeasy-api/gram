import { Page } from "@/components/page-layout";
import { Card } from "@/components/ui/card";
import { Type } from "@/components/ui/type";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useOrgRoutes } from "@/routes";
import { Badge } from "@/components/ui/badge";
import { Button, Input, Stack } from "@speakeasy-api/moonshine";
import { useQueries } from "@tanstack/react-query";
import {
  Check,
  Eye,
  FolderOpen,
  Loader2,
  Lock,
  Plus,
  Search,
  Server as ServerIcon,
  X,
} from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { useCreateCollection } from "./hooks";
import type { CollectionVisibility } from "./types";

function slugify(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
}

export default function CreateCollection() {
  const orgRoutes = useOrgRoutes();
  const client = useSdkClient();
  const organization = useOrganization();
  const projects = organization.projects ?? [];

  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugManuallyEdited, setSlugManuallyEdited] = useState(false);
  const [description, setDescription] = useState("");
  const [visibility, setVisibility] = useState<CollectionVisibility>("private");
  const [selectedToolsetIds, setSelectedToolsetIds] = useState<Set<string>>(
    new Set(),
  );
  const [serverSearch, setServerSearch] = useState("");

  const createMutation = useCreateCollection();

  // Fetch toolsets from every project the user has access to
  const toolsetQueries = useQueries({
    queries: projects.map((project) => ({
      queryKey: ["toolsets", "list", project.slug],
      queryFn: () => client.toolsets.list({ gramProject: project.slug }),
      enabled: !!project.slug,
    })),
  });

  const toolsetsLoading = toolsetQueries.some((q) => q.isLoading);

  // Merge toolsets from all projects, tagging each with its project info
  const toolsets = useMemo(() => {
    const all: Array<{
      id: string;
      name: string;
      description?: string;
      projectName: string;
      projectSlug: string;
    }> = [];
    for (let i = 0; i < projects.length; i++) {
      const project = projects[i];
      const data = toolsetQueries[i]?.data;
      for (const t of data?.toolsets ?? []) {
        all.push({
          id: t.id,
          name: t.name,
          description: t.description ?? undefined,
          projectName: project.name,
          projectSlug: project.slug,
        });
      }
    }
    return all;
  }, [projects, toolsetQueries]);

  const filteredToolsets = useMemo(() => {
    if (!serverSearch) return toolsets;
    const q = serverSearch.toLowerCase();
    return toolsets.filter(
      (t) =>
        t.name.toLowerCase().includes(q) ||
        (t.description && t.description.toLowerCase().includes(q)) ||
        t.projectName.toLowerCase().includes(q),
    );
  }, [toolsets, serverSearch]);

  const toggleToolset = (id: string) => {
    setSelectedToolsetIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const handleNameChange = (value: string) => {
    setName(value);
    if (!slugManuallyEdited) {
      setSlug(slugify(value));
    }
  };

  const handleCreate = () => {
    if (!name.trim() || selectedToolsetIds.size === 0) return;

    createMutation.mutate(
      {
        name: name.trim(),
        slug: slug || slugify(name.trim()),
        toolsetIds: Array.from(selectedToolsetIds),
        visibility,
      },
      {
        onSuccess: () => {
          toast.success("Collection created successfully");
          orgRoutes.collections.goTo();
        },
      },
    );
  };

  const isValid = name.trim().length > 0 && selectedToolsetIds.size > 0;

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
                      handleNameChange(e.target.value)
                    }
                    className="h-10"
                  />
                </Stack>

                {/* Slug */}
                <Stack gap={2}>
                  <label className="text-sm font-medium">Slug</label>
                  <Input
                    placeholder="developer-productivity-suite"
                    value={slug}
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) => {
                      setSlug(e.target.value);
                      setSlugManuallyEdited(true);
                    }}
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
                  <Type small muted>
                    {visibility === "public"
                      ? "Public collections are visible to anyone and can be discovered by other organizations."
                      : "Private collections are only visible to your organization."}
                  </Type>
                </Stack>

                {/* Toolset picker */}
                <div className="rounded-lg bg-muted/30 p-4">
                  <Stack gap={3}>
                    <label className="text-sm font-medium">
                      MCP Servers ({selectedToolsetIds.size} selected)
                    </label>

                    <div className="relative w-full">
                      <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                      <Input
                        placeholder="Search servers..."
                        value={serverSearch}
                        onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                          setServerSearch(e.target.value)
                        }
                        className="pl-10 pr-9 h-10 bg-card"
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

                    {toolsetsLoading ? (
                      <div className="flex items-center justify-center py-8">
                        <Loader2 className="w-6 h-6 animate-spin text-muted-foreground" />
                      </div>
                    ) : filteredToolsets.length === 0 ? (
                      <div className="flex flex-col items-center justify-center py-8 text-center">
                        <ServerIcon className="w-8 h-8 text-muted-foreground mb-2" />
                        <Type small muted>
                          {serverSearch
                            ? "No servers match your search"
                            : "No MCP servers available. Create one first."}
                        </Type>
                      </div>
                    ) : (
                      <div className="grid grid-cols-1 gap-2 max-h-[400px] overflow-y-auto pr-1">
                        {filteredToolsets.map((toolset) => {
                          const isSelected = selectedToolsetIds.has(toolset.id);
                          return (
                            <button
                              key={toolset.id}
                              type="button"
                              onClick={() => toggleToolset(toolset.id)}
                              className={`flex items-center gap-3 p-3 rounded-lg border text-left transition-all ${
                                isSelected
                                  ? "border-border bg-card"
                                  : "border-border bg-card hover:border-foreground/30"
                              }`}
                            >
                              <div className="w-8 h-8 rounded-md bg-muted/50 flex items-center justify-center shrink-0">
                                <ServerIcon className="w-4 h-4 text-muted-foreground" />
                              </div>
                              <div className="flex-1 min-w-0">
                                <Type
                                  variant="subheading"
                                  as="div"
                                  className="text-sm truncate"
                                >
                                  {toolset.name}
                                </Type>
                                <div className="flex items-center gap-1.5 mt-0.5">
                                  <FolderOpen className="w-3 h-3 text-muted-foreground shrink-0" />
                                  <Type
                                    small
                                    muted
                                    className="text-xs truncate"
                                  >
                                    {toolset.projectName}
                                  </Type>
                                </div>
                              </div>
                              <div
                                className={`size-5 rounded-full border-2 flex items-center justify-center shrink-0 transition-colors ${
                                  isSelected
                                    ? "border-[#1DA1F2] bg-[#1DA1F2]"
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
                </div>
              </div>

              {/* Right — preview sidebar */}
              <div className="space-y-4">
                <Card>
                  <Card.Header>
                    <Card.Title>Preview</Card.Title>
                  </Card.Header>
                  <Card.Content>
                    {name || selectedToolsetIds.size > 0 ? (
                      <Stack gap={3}>
                        <Type variant="subheading" as="div">
                          {name || "Untitled Collection"}
                        </Type>
                        {description && (
                          <Type small muted className="line-clamp-3">
                            {description}
                          </Type>
                        )}
                        {slug && (
                          <Type small muted className="font-mono text-xs">
                            {slug}
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
                            {selectedToolsetIds.size} servers
                          </Badge>
                        </Stack>
                        {selectedToolsetIds.size > 0 && (
                          <Stack gap={1.5} className="mt-2">
                            {toolsets
                              .filter((t) => selectedToolsetIds.has(t.id))
                              .map((toolset) => (
                                <Stack
                                  key={toolset.id}
                                  direction="horizontal"
                                  gap={2}
                                  align="center"
                                  className="text-sm"
                                >
                                  <ServerIcon className="w-3.5 h-3.5 text-muted-foreground shrink-0" />
                                  <Type small className="truncate">
                                    {toolset.name}
                                  </Type>
                                </Stack>
                              ))}
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
                  disabled={!isValid || createMutation.isPending}
                >
                  {createMutation.isPending ? (
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
